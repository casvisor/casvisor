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
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

// Report is an audit report kept to be downloaded or emailed on a cron schedule, as an Excel workbook.
type Report struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	UpdatedTime string `xorm:"varchar(100)" json:"updatedTime"`
	DisplayName string `xorm:"varchar(100)" json:"displayName"`
	Description string `xorm:"varchar(500)" json:"description"`

	// Period is the time the report covers when it is made, such as last-week
	Period   string   `xorm:"varchar(20)" json:"period"`
	Sections []string `xorm:"varchar(500)" json:"sections"`
	Language string   `xorm:"varchar(10)" json:"language"`

	// the working hours, outside which sign-ins are counted as off-hours
	Weekdays      []int  `xorm:"varchar(100)" json:"weekdays"`
	WorkStartTime string `xorm:"varchar(10)" json:"workStartTime"`
	WorkEndTime   string `xorm:"varchar(10)" json:"workEndTime"`

	// Webhooks are the webhooks of the Email type that the report is emailed with
	Webhooks  []string `xorm:"varchar(1000)" json:"webhooks"`
	Cron      string   `xorm:"varchar(100)" json:"cron"`
	TimeZone  string   `xorm:"varchar(100)" json:"timeZone"`
	IsEnabled bool     `json:"isEnabled"`

	NextRunTime    string `xorm:"varchar(100)" json:"nextRunTime"`
	LastRunTime    string `xorm:"varchar(100)" json:"lastRunTime"`
	LastRunStatus  string `xorm:"varchar(20)" json:"lastRunStatus"`
	LastRunMessage string `xorm:"varchar(1000)" json:"lastRunMessage"`
}

func (report *Report) GetId() string {
	return util.GetIdFromOwnerAndName(report.Owner, report.Name)
}

func GetReportCount(owner, field, value string) (int64, error) {
	session := GetSession(owner, -1, -1, field, value, "", "")
	return session.Count(&Report{})
}

func GetPaginationReports(owner string, offset, limit int, field, value, sortField, sortOrder string) ([]*Report, error) {
	reports := []*Report{}
	session := GetSession(owner, offset, limit, field, value, sortField, sortOrder)
	err := session.Find(&reports)
	return reports, err
}

func getReport(owner string, name string) (*Report, error) {
	if owner == "" || name == "" {
		return nil, nil
	}
	report := Report{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&report)
	if err != nil || !existed {
		return nil, err
	}
	return &report, nil
}

func GetReport(id string) (*Report, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	return getReport(owner, name)
}

func (report *Report) GetOptions() *ReportOptions {
	return &ReportOptions{
		Owner:         report.Owner,
		Period:        report.Period,
		TimeZone:      report.TimeZone,
		Weekdays:      report.Weekdays,
		WorkStartTime: report.WorkStartTime,
		WorkEndTime:   report.WorkEndTime,
		Sections:      report.Sections,
		Language:      report.Language,
	}
}

func (report *Report) getSchedule() *JobSchedule {
	return &JobSchedule{Cron: report.Cron, TimeZone: report.TimeZone, IsEnabled: report.IsEnabled}
}

func (report *Report) check() error {
	if report.Period == "" {
		report.Period = ReportPeriodLastWeek
	}
	if report.Period == ReportPeriodCustom {
		return fmt.Errorf("a saved report needs a period such as last-week, as it is made again each time")
	}
	if report.Language != "" && report.Language != "en" && report.Language != "zh" {
		return fmt.Errorf("unknown language: %s, it should be en or zh", report.Language)
	}
	err := report.GetOptions().check()
	if err != nil {
		return err
	}
	if _, err = getReportLocation(report.TimeZone); err != nil {
		return err
	}

	webhooks := []string{}
	for _, name := range report.Webhooks {
		if name == "" || slices.Contains(webhooks, name) {
			continue
		}
		webhook, err := getWebhook(report.Owner, name)
		if err != nil {
			return err
		}
		if webhook == nil {
			return fmt.Errorf("the webhook: %s does not exist", name)
		}
		if webhook.Type != WebhookTypeEmail {
			return fmt.Errorf("the webhook: %s is not of the Email type, a report can only be emailed", name)
		}
		webhooks = append(webhooks, name)
	}
	report.Webhooks = webhooks

	report.Cron = strings.TrimSpace(report.Cron)
	report.NextRunTime = ""
	if report.Cron == "" {
		report.IsEnabled = false
		return nil
	}
	if len(report.Webhooks) == 0 {
		return fmt.Errorf("a report on a schedule needs a webhook of the Email type to send it with")
	}
	next, err := report.getSchedule().getNextRunTime(time.Now())
	if err != nil {
		return err
	}
	if report.IsEnabled {
		report.NextRunTime = next
	}
	return nil
}

func UpdateReport(id string, report *Report) (bool, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	old, err := getReport(owner, name)
	if err != nil {
		return false, err
	} else if old == nil {
		return false, nil
	}

	err = report.check()
	if err != nil {
		return false, err
	}
	report.UpdatedTime = util.GetCurrentTime()
	report.LastRunTime, report.LastRunStatus, report.LastRunMessage = old.LastRunTime, old.LastRunStatus, old.LastRunMessage
	affected, err := adapter.engine.ID(core.PK{owner, name}).AllCols().Update(report)
	return affected != 0, err
}

func AddReport(report *Report) (bool, error) {
	err := report.check()
	if err != nil {
		return false, err
	}
	report.LastRunTime, report.LastRunStatus, report.LastRunMessage = "", "", ""
	affected, err := adapter.engine.Insert(report)
	return affected != 0, err
}

func DeleteReport(report *Report) (bool, error) {
	affected, err := adapter.engine.ID(core.PK{report.Owner, report.Name}).Delete(&Report{})
	return affected != 0, err
}

// GetReportFile makes the report as an Excel workbook, with its file name.
func GetReportFile(options *ReportOptions, now time.Time) ([]byte, string, *ReportData, error) {
	data, err := GetReportData(options, now)
	if err != nil {
		return nil, "", nil, err
	}
	file, err := BuildReportXlsx(data, options.Language)
	if err != nil {
		return nil, "", nil, err
	}
	start, _ := time.Parse(time.RFC3339, data.StartTime)
	end, _ := time.Parse(time.RFC3339, data.EndTime)
	fileName := fmt.Sprintf("casvisor-report-%s-%s-%s.xlsx", data.Owner, start.Format("20060102"), end.Add(-time.Second).Format("20060102"))
	return file, fileName, data, nil
}

var reportLock sync.Mutex

// RunReport makes the report now and emails it with its webhooks.
func RunReport(id string) error {
	reportLock.Lock()
	defer reportLock.Unlock()

	report, err := GetReport(id)
	if err != nil {
		return err
	}
	if report == nil {
		return fmt.Errorf("the report: %s does not exist", id)
	}
	return report.run(time.Now())
}

func (report *Report) run(now time.Time) (err error) {
	defer func() {
		update := &Report{LastRunTime: now.Format(time.RFC3339), LastRunStatus: BackupRunSucceeded}
		update.LastRunMessage = fmt.Sprintf("sent to %s", strings.Join(report.Webhooks, ", "))
		if err != nil {
			update.LastRunStatus, update.LastRunMessage = BackupRunFailed, truncate(err.Error(), 1000)
			notifyReportFailed(report, err.Error(), update.LastRunTime)
		}
		if report.IsEnabled && report.Cron != "" {
			update.NextRunTime, _ = report.getSchedule().getNextRunTime(now)
		}
		_, updateErr := adapter.engine.ID(core.PK{report.Owner, report.Name}).Cols("last_run_time", "last_run_status", "last_run_message", "next_run_time").Update(update)
		if updateErr != nil {
			logs.Error(fmt.Sprintf("Report.run(): %s", updateErr.Error()))
		}
	}()

	if len(report.Webhooks) == 0 {
		return fmt.Errorf("the report has no webhook of the Email type to send it with")
	}
	file, fileName, data, err := GetReportFile(report.GetOptions(), now)
	if err != nil {
		return err
	}

	displayName := report.DisplayName
	if displayName == "" {
		displayName = report.Name
	}
	subject, body := getReportMail(report, displayName, data, fileName)
	attachment := &MailAttachment{Name: fileName, ContentType: XlsxContentType, Data: file}

	failed := []string{}
	for _, name := range report.Webhooks {
		webhook, getErr := getWebhook(report.Owner, name)
		if getErr == nil && webhook == nil {
			getErr = fmt.Errorf("it does not exist")
		} else if getErr == nil && webhook.Type != WebhookTypeEmail {
			getErr = fmt.Errorf("it is not of the Email type")
		}
		if getErr == nil {
			getErr = webhook.sendMail(subject, body, []*MailAttachment{attachment})
		}
		if getErr != nil {
			failed = append(failed, fmt.Sprintf("%s: %s", name, getErr.Error()))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("failed to send the report with %s", strings.Join(failed, "; "))
	}
	return nil
}

// runDueReports sends the reports whose cron expression matches this minute.
func runDueReports(now time.Time) {
	reports := []*Report{}
	err := adapter.engine.Where("is_enabled = ?", true).Find(&reports)
	if err != nil {
		logs.Error(fmt.Sprintf("runDueReports(): %s", err.Error()))
		return
	}

	minute := now.Truncate(time.Minute)
	for _, report := range reports {
		cron, err := parseCron(report.Cron)
		if err != nil {
			continue
		}
		location, err := report.getSchedule().getLocation()
		if err != nil || !cron.matches(minute.In(location)) {
			continue
		}
		if last, err := time.Parse(time.RFC3339, report.LastRunTime); err == nil && !last.Before(minute) {
			continue
		}

		reportLock.Lock()
		err = report.run(minute)
		reportLock.Unlock()
		if err != nil {
			logs.Warning(fmt.Sprintf("runDueReports(): the report: %s failed: %s", report.GetId(), err.Error()))
		}
	}
}
