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
	"strings"

	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

const (
	PatchJobScan    = "Scan"
	PatchJobInstall = "Install"
	PatchJobReboot  = "Reboot"

	PatchJobRunning   = "Running"
	PatchJobSucceeded = "Succeeded"
	PatchJobFailed    = "Failed"
)

// PatchUpdate is one update an asset has not installed: a Windows update or a Linux package.
type PatchUpdate struct {
	Id             string `json:"id"`
	Title          string `json:"title"`
	Kb             string `json:"kb"`
	Category       string `json:"category"`
	Severity       string `json:"severity"`
	IsSecurity     bool   `json:"isSecurity"`
	CurrentVersion string `json:"currentVersion"`
	NewVersion     string `json:"newVersion"`
	RebootRequired bool   `json:"rebootRequired"`
}

// PatchStatus is the result of the last scan of an SSH asset, keyed by the asset.
type PatchStatus struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	UpdatedTime string `xorm:"varchar(100)" json:"updatedTime"`

	Platform       string         `xorm:"varchar(20)" json:"platform"`
	Os             string         `xorm:"varchar(200)" json:"os"`
	Manager        string         `xorm:"varchar(50)" json:"manager"`
	Updates        []*PatchUpdate `xorm:"mediumtext" json:"updates"`
	UpdateCount    int            `json:"updateCount"`
	SecurityCount  int            `json:"securityCount"`
	RebootRequired bool           `json:"rebootRequired"`
	ScannedTime    string         `xorm:"varchar(100)" json:"scannedTime"`
	Message        string         `xorm:"varchar(1000)" json:"message"`

	Job     string `xorm:"varchar(100)" json:"job"`
	JobType string `xorm:"varchar(20)" json:"jobType"`

	// filled from the asset when listed
	DisplayName string `xorm:"-" json:"displayName"`
	Host        string `xorm:"-" json:"host"`
	Tag         string `xorm:"-" json:"tag"`
}

// PatchJob is one scan, install or reboot run on an asset.
type PatchJob struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100) index" json:"createdTime"`
	EndTime     string `xorm:"varchar(100)" json:"endTime"`

	Asset   string   `xorm:"varchar(200) index" json:"asset"`
	Type    string   `xorm:"varchar(20)" json:"type"`
	Updates []string `xorm:"mediumtext" json:"updates"`
	User    string   `xorm:"varchar(200)" json:"user"`
	Status  string   `xorm:"varchar(20)" json:"status"`
	Message string   `xorm:"varchar(1000)" json:"message"`
	Output  string   `xorm:"mediumtext" json:"output"`

	// how far the log of the install on the asset has been read, to pick it up again after Casvisor restarts
	RemoteOffset int64 `json:"remoteOffset"`
}

func GetPatchStatuses(owner string) ([]*PatchStatus, error) {
	assets, err := GetAssets(owner)
	if err != nil {
		return nil, err
	}

	statuses := []*PatchStatus{}
	err = adapter.engine.Find(&statuses, &PatchStatus{Owner: owner})
	if err != nil {
		return nil, err
	}

	statusMap := map[string]*PatchStatus{}
	for _, status := range statuses {
		statusMap[util.GetIdFromOwnerAndName(status.Owner, status.Name)] = status
	}

	res := []*PatchStatus{}
	for _, asset := range assets {
		if !strings.EqualFold(asset.Protocol, "SSH") {
			continue
		}

		status := statusMap[asset.GetId()]
		if status == nil {
			status = &PatchStatus{Owner: asset.Owner, Name: asset.Name, Updates: []*PatchUpdate{}}
		}
		status.DisplayName = asset.DisplayName
		status.Host = asset.Host
		status.Tag = asset.Tag
		res = append(res, status)
	}

	return res, nil
}

func GetPatchStatus(id string) (*PatchStatus, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	asset, err := getAsset(owner, name)
	if err != nil || asset == nil {
		return nil, err
	}

	status, err := getPatchStatus(owner, name)
	if err != nil {
		return nil, err
	}
	if status == nil {
		status = &PatchStatus{Owner: owner, Name: name, Updates: []*PatchUpdate{}}
	}
	status.DisplayName = asset.DisplayName
	status.Host = asset.Host
	status.Tag = asset.Tag
	return status, nil
}

func getPatchStatus(owner string, name string) (*PatchStatus, error) {
	status := PatchStatus{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&status)
	if err != nil || !existed {
		return nil, err
	}
	return &status, nil
}

func savePatchStatus(status *PatchStatus, cols ...string) error {
	status.UpdatedTime = util.GetCurrentTime()
	existed, err := adapter.engine.Exist(&PatchStatus{Owner: status.Owner, Name: status.Name})
	if err != nil {
		return err
	}

	if !existed {
		if status.Updates == nil {
			status.Updates = []*PatchUpdate{}
		}
		_, err = adapter.engine.Insert(status)
		return err
	}

	_, err = adapter.engine.ID(core.PK{status.Owner, status.Name}).Cols(append(cols, "updated_time")...).Update(status)
	return err
}

func deletePatchStatus(owner string, name string) error {
	_, err := adapter.engine.ID(core.PK{owner, name}).Delete(&PatchStatus{})
	return err
}

func GetPatchJobCount(owner, field, value string) (int64, error) {
	session := GetSession(owner, -1, -1, field, value, "", "")
	return session.Count(&PatchJob{})
}

func GetPaginationPatchJobs(owner string, offset, limit int, field, value, sortField, sortOrder string) ([]*PatchJob, error) {
	jobs := []*PatchJob{}
	session := GetSession(owner, offset, limit, field, value, sortField, sortOrder).Omit("output")
	err := session.Find(&jobs)
	if err != nil {
		return jobs, err
	}

	return jobs, nil
}

func GetPatchJob(id string) (*PatchJob, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	job := PatchJob{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&job)
	if err != nil || !existed {
		return nil, err
	}

	run := getPatchRun(job.Asset)
	if run != nil && run.job.Name == job.Name {
		job.Output = run.output()
	}
	return &job, nil
}
