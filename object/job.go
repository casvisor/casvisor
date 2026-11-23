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
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

const (
	JobTypeCommand = "Command"
	JobTypeScript  = "Script"
	JobTypeFile    = "File"

	JobScriptBash       = "Bash"
	JobScriptPowerShell = "PowerShell"

	JobPending     = "Pending"
	JobRunning     = "Running"
	JobSucceeded   = "Succeeded"
	JobFailed      = "Failed"
	JobTimeout     = "Timeout"
	JobBlocked     = "Blocked"
	JobCanceled    = "Canceled"
	JobInterrupted = "Interrupted"

	defaultJobTimeout     = 60
	maxJobTimeout         = 24 * 3600
	defaultJobConcurrency = 10
	maxJobConcurrency     = 100
)

var jobFileKeyRegex = regexp.MustCompile(`^[0-9a-f-]{36}$`)

// Job runs a command, a script or a file transfer on many SSH assets at once.
type Job struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100) index" json:"createdTime"`
	DisplayName string `xorm:"varchar(100)" json:"displayName"`

	Type       string   `xorm:"varchar(20)" json:"type"`
	Assets     []string `xorm:"mediumtext" json:"assets"`
	AssetTags  []string `xorm:"mediumtext" json:"assetTags"`
	Content    string   `xorm:"mediumtext" json:"content"`
	ScriptType string   `xorm:"varchar(20)" json:"scriptType"`

	FileKey  string `xorm:"varchar(100)" json:"fileKey"`
	FileName string `xorm:"varchar(500)" json:"fileName"`
	FileSize int64  `json:"fileSize"`
	FilePath string `xorm:"varchar(1000)" json:"filePath"`

	Timeout     int    `json:"timeout"`
	Concurrency int    `json:"concurrency"`
	User        string `xorm:"varchar(200)" json:"user"`
	ClientIp    string `xorm:"varchar(100)" json:"clientIp"`
	// Schedule is the job schedule that started the job, if any
	Schedule string `xorm:"varchar(100) index" json:"schedule"`

	Status    string `xorm:"varchar(20) index" json:"status"`
	StartTime string `xorm:"varchar(100)" json:"startTime"`
	EndTime   string `xorm:"varchar(100)" json:"endTime"`
	Message   string `xorm:"varchar(1000)" json:"message"`

	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`

	Results []*JobResult `xorm:"-" json:"results,omitempty"`
}

// JobResult is how a job went on one asset.
type JobResult struct {
	Owner string `xorm:"varchar(100) notnull pk" json:"owner"`
	Job   string `xorm:"varchar(100) notnull pk" json:"job"`
	Asset string `xorm:"varchar(100) notnull pk" json:"asset"`

	DisplayName string `xorm:"varchar(100)" json:"displayName"`
	Host        string `xorm:"varchar(200)" json:"host"`
	Session     string `xorm:"varchar(200)" json:"session"`
	Status      string `xorm:"varchar(20)" json:"status"`
	ExitCode    int    `json:"exitCode"`
	Output      string `xorm:"mediumtext" json:"output"`
	Error       string `xorm:"varchar(1000)" json:"error"`
	Rule        string `xorm:"varchar(200)" json:"rule"`
	StartTime   string `xorm:"varchar(100)" json:"startTime"`
	EndTime     string `xorm:"varchar(100)" json:"endTime"`
}

func GetJobCount(owner, field, value string) (int64, error) {
	session := GetSession(owner, -1, -1, field, value, "", "")
	return session.Count(&Job{})
}

func GetPaginationJobs(owner string, offset, limit int, field, value, sortField, sortOrder string) ([]*Job, error) {
	jobs := []*Job{}
	session := GetSession(owner, offset, limit, field, value, sortField, sortOrder).Omit("content")
	err := session.Find(&jobs)
	if err != nil {
		return jobs, err
	}

	return jobs, nil
}

func getJob(owner string, name string) (*Job, error) {
	if owner == "" || name == "" {
		return nil, nil
	}

	job := Job{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&job)
	if err != nil || !existed {
		return nil, err
	}
	return &job, nil
}

// GetJob returns the job with the result on each asset, the output of the running ones live.
func GetJob(id string) (*Job, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	job, err := getJob(owner, name)
	if err != nil || job == nil {
		return nil, err
	}

	results, err := getJobResults(owner, name)
	if err != nil {
		return nil, err
	}

	if run := getJobRun(job.GetId()); run != nil {
		run.fillLive(results)
	}
	job.Results = results
	return job, nil
}

func getJobResults(owner string, job string) ([]*JobResult, error) {
	results := []*JobResult{}
	err := adapter.engine.Asc("asset").Find(&results, &JobResult{Owner: owner, Job: job})
	return results, err
}

func DeleteJob(job *Job) (bool, error) {
	if getJobRun(job.GetId()) != nil {
		return false, fmt.Errorf("the job: %s is still running, cancel it first", job.GetId())
	}

	old, err := getJob(job.Owner, job.Name)
	if err != nil || old == nil {
		return false, err
	}

	affected, err := adapter.engine.ID(core.PK{job.Owner, job.Name}).Delete(&Job{})
	if err != nil {
		return false, err
	}

	_, err = adapter.engine.Delete(&JobResult{Owner: job.Owner, Job: job.Name})
	if err != nil {
		return false, err
	}

	if old.FileKey != "" {
		path, err := GetJobFilePath(old.Owner, old.FileKey)
		if err == nil {
			_ = os.Remove(path)
		}
	}

	return affected != 0, nil
}

func (job *Job) GetId() string {
	return util.GetIdFromOwnerAndName(job.Owner, job.Name)
}

// GetJobFilePath returns where a file uploaded for a job is kept.
func GetJobFilePath(owner string, key string) (string, error) {
	if !jobFileKeyRegex.MatchString(key) || owner == "" || strings.ContainsAny(owner, `/\.`) {
		return "", fmt.Errorf("invalid file: %s", key)
	}
	return filepath.Join("data", "jobs", owner, key), nil
}

func (job *Job) check() error {
	job.Content = strings.TrimSpace(job.Content)
	switch job.Type {
	case JobTypeCommand:
		if job.Content == "" {
			return fmt.Errorf("the command should not be empty")
		}
	case JobTypeScript:
		if job.Content == "" {
			return fmt.Errorf("the script should not be empty")
		}
		if job.ScriptType != JobScriptBash && job.ScriptType != JobScriptPowerShell {
			return fmt.Errorf("unknown script type: %s", job.ScriptType)
		}
	case JobTypeFile:
		if job.FileKey == "" {
			return fmt.Errorf("no file is uploaded")
		}
		if strings.TrimSpace(job.FilePath) == "" {
			return fmt.Errorf("the target path should not be empty")
		}
		path, err := GetJobFilePath(job.Owner, job.FileKey)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("the uploaded file is gone, upload it again")
		}
		job.FileSize = info.Size()
		job.Content = ""
	default:
		return fmt.Errorf("unknown job type: %s", job.Type)
	}

	if job.Timeout <= 0 {
		job.Timeout = defaultJobTimeout
	}
	job.Timeout = min(job.Timeout, maxJobTimeout)
	if job.Concurrency <= 0 {
		job.Concurrency = defaultJobConcurrency
	}
	job.Concurrency = min(job.Concurrency, maxJobConcurrency)
	return nil
}

// getJobAssets returns the SSH assets the job runs on: the ones named, and the ones with the tags.
func (job *Job) getJobAssets() ([]*Asset, error) {
	assets, err := GetAssets(job.Owner)
	if err != nil {
		return nil, err
	}

	byName := map[string]*Asset{}
	for _, asset := range assets {
		byName[asset.Name] = asset
	}

	res := []*Asset{}
	seen := map[string]bool{}
	for _, name := range job.Assets {
		asset := byName[name]
		if asset == nil {
			return nil, fmt.Errorf("the asset: %s does not exist", name)
		}
		if !strings.EqualFold(asset.Protocol, "SSH") {
			return nil, fmt.Errorf("the asset: %s is not an SSH asset, jobs run over SSH", name)
		}
		if !seen[name] {
			seen[name] = true
			res = append(res, asset)
		}
	}

	for _, asset := range assets {
		if seen[asset.Name] || asset.Tag == "" || !slices.Contains(job.AssetTags, asset.Tag) || !strings.EqualFold(asset.Protocol, "SSH") {
			continue
		}
		seen[asset.Name] = true
		res = append(res, asset)
	}

	if len(res) == 0 {
		return nil, fmt.Errorf("no SSH assets are picked")
	}
	return res, nil
}
