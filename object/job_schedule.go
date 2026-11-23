// Copyright 2026 The casbin Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package object

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

const (
	ScheduleRunStarted = "Started"
	ScheduleRunSkipped = "Skipped"
	ScheduleRunFailed  = "Failed"
)

// JobSchedule starts a job on a cron schedule, as the administrator who last saved it.
type JobSchedule struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	UpdatedTime string `xorm:"varchar(100)" json:"updatedTime"`
	DisplayName string `xorm:"varchar(100)" json:"displayName"`
	Description string `xorm:"varchar(500)" json:"description"`

	// what each job runs, as in Job
	Type        string   `xorm:"varchar(20)" json:"type"`
	Assets      []string `xorm:"mediumtext" json:"assets"`
	AssetTags   []string `xorm:"mediumtext" json:"assetTags"`
	Content     string   `xorm:"mediumtext" json:"content"`
	ScriptType  string   `xorm:"varchar(20)" json:"scriptType"`
	FileKey     string   `xorm:"varchar(100)" json:"fileKey"`
	FileName    string   `xorm:"varchar(500)" json:"fileName"`
	FileSize    int64    `json:"fileSize"`
	FilePath    string   `xorm:"varchar(1000)" json:"filePath"`
	Timeout     int      `json:"timeout"`
	Concurrency int      `json:"concurrency"`

	// Cron is a crontab line in the time zone, such as "0 3 * * *" for 03:00 every day
	Cron      string `xorm:"varchar(100)" json:"cron"`
	TimeZone  string `xorm:"varchar(100)" json:"timeZone"`
	IsEnabled bool   `json:"isEnabled"`

	// User is who the jobs run as, with the groups the user had, for the command rules
	User       string   `xorm:"varchar(200)" json:"user"`
	UserGroups []string `xorm:"mediumtext" json:"userGroups"`

	NextRunTime    string `xorm:"varchar(100)" json:"nextRunTime"`
	LastRunTime    string `xorm:"varchar(100)" json:"lastRunTime"`
	LastJob        string `xorm:"varchar(100)" json:"lastJob"`
	LastRunStatus  string `xorm:"varchar(20)" json:"lastRunStatus"`
	LastRunMessage string `xorm:"varchar(1000)" json:"lastRunMessage"`
}

func (schedule *JobSchedule) GetId() string {
	return util.GetIdFromOwnerAndName(schedule.Owner, schedule.Name)
}

func GetJobScheduleCount(owner, field, value string) (int64, error) {
	session := GetSession(owner, -1, -1, field, value, "", "")
	return session.Count(&JobSchedule{})
}

func GetPaginationJobSchedules(owner string, offset, limit int, field, value, sortField, sortOrder string) ([]*JobSchedule, error) {
	schedules := []*JobSchedule{}
	session := GetSession(owner, offset, limit, field, value, sortField, sortOrder)
	err := session.Find(&schedules)
	return schedules, err
}

func getJobSchedule(owner string, name string) (*JobSchedule, error) {
	if owner == "" || name == "" {
		return nil, nil
	}
	schedule := JobSchedule{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&schedule)
	if err != nil || !existed {
		return nil, err
	}
	return &schedule, nil
}

func GetJobSchedule(id string) (*JobSchedule, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	return getJobSchedule(owner, name)
}

// toJob is the job the schedule starts.
func (schedule *JobSchedule) toJob() *Job {
	displayName := schedule.DisplayName
	if displayName == "" {
		displayName = schedule.Name
	}
	return &Job{
		Owner:       schedule.Owner,
		DisplayName: displayName,
		Type:        schedule.Type,
		Assets:      schedule.Assets,
		AssetTags:   schedule.AssetTags,
		Content:     schedule.Content,
		ScriptType:  schedule.ScriptType,
		FileKey:     schedule.FileKey,
		FileName:    schedule.FileName,
		FilePath:    schedule.FilePath,
		Timeout:     schedule.Timeout,
		Concurrency: schedule.Concurrency,
		Schedule:    schedule.Name,
	}
}

func (schedule *JobSchedule) getLocation() (*time.Location, error) {
	if schedule.TimeZone == "" {
		return time.Local, nil
	}
	return time.LoadLocation(schedule.TimeZone)
}

// GetJobScheduleRunTimes returns the next times the cron expression fires, for the form to show.
func GetJobScheduleRunTimes(expr string, timeZone string, count int) ([]string, error) {
	schedule := &JobSchedule{Cron: expr, TimeZone: timeZone}
	res := []string{}
	after := time.Now()
	for i := 0; i < count; i++ {
		next, err := schedule.getNextRunTime(after)
		if err != nil {
			return nil, err
		}
		res = append(res, next)
		after, _ = time.Parse(time.RFC3339, next)
	}
	return res, nil
}

func (schedule *JobSchedule) getNextRunTime(after time.Time) (string, error) {
	cron, err := parseCron(schedule.Cron)
	if err != nil {
		return "", err
	}
	location, err := schedule.getLocation()
	if err != nil {
		return "", fmt.Errorf("invalid time zone: %s", schedule.TimeZone)
	}
	next := cron.next(after.In(location))
	if next.IsZero() {
		return "", fmt.Errorf("the cron expression: %s never fires", schedule.Cron)
	}
	return next.Format(time.RFC3339), nil
}

func (schedule *JobSchedule) check() error {
	job := schedule.toJob()
	err := job.check()
	if err != nil {
		return err
	}
	schedule.Content, schedule.FileSize, schedule.Timeout, schedule.Concurrency = job.Content, job.FileSize, job.Timeout, job.Concurrency

	schedule.NextRunTime, err = schedule.getNextRunTime(time.Now())
	if err != nil {
		return err
	}
	if !schedule.IsEnabled {
		schedule.NextRunTime = ""
	}
	return nil
}

// saveAs makes the schedule run as the administrator saving it.
func (schedule *JobSchedule) saveAs(user *casdoorsdk.User) {
	schedule.User = util.GetIdFromOwnerAndName(user.Owner, user.Name)
	schedule.UserGroups = []string{}
	for _, group := range user.Groups {
		schedule.UserGroups = append(schedule.UserGroups, getGroupName(group))
	}
}

func UpdateJobSchedule(id string, schedule *JobSchedule, user *casdoorsdk.User) (bool, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	old, err := getJobSchedule(owner, name)
	if err != nil {
		return false, err
	} else if old == nil {
		return false, nil
	}

	schedule.saveAs(user)
	schedule.UpdatedTime = util.GetCurrentTime()
	schedule.LastRunTime, schedule.LastJob, schedule.LastRunStatus, schedule.LastRunMessage = old.LastRunTime, old.LastJob, old.LastRunStatus, old.LastRunMessage
	err = schedule.check()
	if err != nil {
		return false, err
	}

	affected, err := adapter.engine.ID(core.PK{owner, name}).AllCols().Update(schedule)
	if err != nil {
		return false, err
	}
	if affected != 0 && old.FileKey != "" && old.FileKey != schedule.FileKey {
		removeJobFile(old.Owner, old.FileKey)
	}
	return affected != 0, nil
}

func AddJobSchedule(schedule *JobSchedule, user *casdoorsdk.User) (bool, error) {
	schedule.saveAs(user)
	schedule.LastRunTime, schedule.LastJob, schedule.LastRunStatus, schedule.LastRunMessage = "", "", "", ""
	err := schedule.check()
	if err != nil {
		return false, err
	}

	affected, err := adapter.engine.Insert(schedule)
	return affected != 0, err
}

func DeleteJobSchedule(schedule *JobSchedule) (bool, error) {
	old, err := getJobSchedule(schedule.Owner, schedule.Name)
	if err != nil {
		return false, err
	}

	affected, err := adapter.engine.ID(core.PK{schedule.Owner, schedule.Name}).Delete(&JobSchedule{})
	if err != nil {
		return false, err
	}
	if old != nil && old.FileKey != "" {
		removeJobFile(old.Owner, old.FileKey)
	}
	return affected != 0, nil
}

func removeJobFile(owner string, key string) {
	path, err := GetJobFilePath(owner, key)
	if err == nil {
		_ = os.Remove(path)
	}
}

// copyJobFile gives a job a copy of the schedule's file, so that deleting the job leaves the
// schedule's file where it is.
func copyJobFile(owner string, key string) (string, error) {
	src, err := GetJobFilePath(owner, key)
	if err != nil {
		return "", err
	}
	newKey := util.GenerateId()
	dst, err := GetJobFilePath(owner, newKey)
	if err != nil {
		return "", err
	}

	in, err := os.Open(src)
	if err != nil {
		return "", fmt.Errorf("the file of the schedule is gone, upload it again")
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(dst)
		return "", err
	}
	return newKey, nil
}

var scheduleRunLock sync.Mutex

// RunJobSchedule starts a job of the schedule now, unless the last one is still running.
func RunJobSchedule(id string) (*Job, error) {
	scheduleRunLock.Lock()
	defer scheduleRunLock.Unlock()

	schedule, err := GetJobSchedule(id)
	if err != nil {
		return nil, err
	}
	if schedule == nil {
		return nil, fmt.Errorf("the job schedule: %s does not exist", id)
	}
	return schedule.run(time.Now())
}

func (schedule *JobSchedule) run(now time.Time) (*Job, error) {
	status, message := ScheduleRunStarted, ""
	var job *Job
	var err error
	defer func() {
		if err != nil {
			status, message = ScheduleRunFailed, err.Error()
		}
		if runes := []rune(message); len(runes) > 1000 {
			message = string(runes[:1000])
		}
		update := &JobSchedule{LastRunTime: now.Format(time.RFC3339), LastRunStatus: status, LastRunMessage: message, LastJob: schedule.LastJob}
		if job != nil {
			update.LastJob = job.Name
		}
		if schedule.IsEnabled {
			update.NextRunTime, _ = schedule.getNextRunTime(now)
		}
		_, updateErr := adapter.engine.ID(core.PK{schedule.Owner, schedule.Name}).Cols("last_run_time", "last_run_status", "last_run_message", "last_job", "next_run_time").Update(update)
		if updateErr != nil {
			logs.Error(fmt.Sprintf("JobSchedule.run(): %s", updateErr.Error()))
		}
		if status != ScheduleRunStarted {
			notifyJobScheduleMissed(schedule, status, message, update.LastRunTime)
		}
	}()

	if schedule.LastJob != "" {
		last, getErr := getJob(schedule.Owner, schedule.LastJob)
		if getErr == nil && last != nil && last.Status == JobRunning {
			status, message = ScheduleRunSkipped, fmt.Sprintf("the last job: %s is still running", last.Name)
			return nil, fmt.Errorf("%s", message)
		}
	}

	newJob := schedule.toJob()
	if newJob.Type == JobTypeFile {
		newJob.FileKey, err = copyJobFile(schedule.Owner, schedule.FileKey)
		if err != nil {
			return nil, err
		}
	}

	owner, name := util.GetOwnerAndNameFromIdNoCheck(schedule.User)
	user := &casdoorsdk.User{Owner: owner, Name: name, IsAdmin: true, Groups: schedule.UserGroups}
	job, err = StartJob(newJob, user, "")
	if err != nil {
		if newJob.Type == JobTypeFile {
			removeJobFile(schedule.Owner, newJob.FileKey)
		}
		job = nil
		return nil, err
	}
	return job, nil
}

// runDueJobSchedules starts the jobs of the schedules whose cron expression matches this minute.
func runDueJobSchedules(now time.Time) {
	schedules := []*JobSchedule{}
	err := adapter.engine.Where("is_enabled = ?", true).Find(&schedules)
	if err != nil {
		logs.Error(fmt.Sprintf("runDueJobSchedules(): %s", err.Error()))
		return
	}

	minute := now.Truncate(time.Minute)
	for _, schedule := range schedules {
		cron, err := parseCron(schedule.Cron)
		if err != nil {
			continue
		}
		location, err := schedule.getLocation()
		if err != nil || !cron.matches(minute.In(location)) {
			continue
		}
		if last, err := time.Parse(time.RFC3339, schedule.LastRunTime); err == nil && !last.Before(minute) {
			continue
		}

		scheduleRunLock.Lock()
		_, err = schedule.run(minute)
		scheduleRunLock.Unlock()
		if err != nil {
			logs.Warning(fmt.Sprintf("runDueJobSchedules(): the job schedule: %s did not start a job: %s", schedule.GetId(), err.Error()))
		}
	}
}

// StartJobScheduler checks the job schedules, the account backups and the reports at the start of every minute.
func StartJobScheduler() {
	go func() {
		for {
			now := time.Now()
			time.Sleep(now.Truncate(time.Minute).Add(time.Minute + time.Second).Sub(now))
			now = time.Now()
			runDueJobSchedules(now)
			runDueAccountBackups(now)
			go runDueReports(now)
		}
	}()
}
