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
	"net"
	"slices"
	"time"

	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

const (
	ActionConnect  = "Connect"
	ActionCopy     = "Copy"
	ActionPaste    = "Paste"
	ActionUpload   = "Upload"
	ActionDownload = "Download"
)

var AllActions = []string{ActionConnect, ActionCopy, ActionPaste, ActionUpload, ActionDownload}

type Permission struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	UpdatedTime string `xorm:"varchar(100)" json:"updatedTime"`
	DisplayName string `xorm:"varchar(100)" json:"displayName"`
	Description string `xorm:"varchar(500)" json:"description"`

	Users     []string `xorm:"mediumtext" json:"users"`
	Groups    []string `xorm:"mediumtext" json:"groups"`
	Assets    []string `xorm:"mediumtext" json:"assets"`
	AssetTags []string `xorm:"mediumtext" json:"assetTags"`
	Actions   []string `xorm:"varchar(500)" json:"actions"`
	ClientIps []string `xorm:"mediumtext" json:"clientIps"`

	StartTime string `xorm:"varchar(100)" json:"startTime"`
	EndTime   string `xorm:"varchar(100)" json:"endTime"`
	IsEnabled bool   `json:"isEnabled"`
}

func GetPermissionCount(owner, field, value string) (int64, error) {
	session := GetSession(owner, -1, -1, field, value, "", "")
	return session.Count(&Permission{})
}

func GetPermissions(owner string) ([]*Permission, error) {
	permissions := []*Permission{}
	err := adapter.engine.Desc("created_time").Find(&permissions, &Permission{Owner: owner})
	if err != nil {
		return permissions, err
	}

	return permissions, nil
}

func GetPaginationPermissions(owner string, offset, limit int, field, value, sortField, sortOrder string) ([]*Permission, error) {
	permissions := []*Permission{}
	session := GetSession(owner, offset, limit, field, value, sortField, sortOrder)
	err := session.Find(&permissions)
	if err != nil {
		return permissions, err
	}

	return permissions, nil
}

func getPermission(owner string, name string) (*Permission, error) {
	if owner == "" || name == "" {
		return nil, nil
	}

	permission := Permission{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&permission)
	if err != nil {
		return &permission, err
	}

	if existed {
		return &permission, nil
	} else {
		return nil, nil
	}
}

func GetPermission(id string) (*Permission, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	return getPermission(owner, name)
}

func UpdatePermission(id string, permission *Permission) (bool, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	p, err := getPermission(owner, name)
	if err != nil {
		return false, err
	} else if p == nil {
		return false, nil
	}

	err = permission.check()
	if err != nil {
		return false, err
	}

	permission.UpdatedTime = util.GetCurrentTime()
	affected, err := adapter.engine.ID(core.PK{owner, name}).AllCols().Update(permission)
	if err != nil {
		return false, err
	}

	return affected != 0, nil
}

func AddPermission(permission *Permission) (bool, error) {
	err := permission.check()
	if err != nil {
		return false, err
	}

	affected, err := adapter.engine.Insert(permission)
	if err != nil {
		return false, err
	}

	return affected != 0, nil
}

func DeletePermission(permission *Permission) (bool, error) {
	affected, err := adapter.engine.ID(core.PK{permission.Owner, permission.Name}).Delete(&Permission{})
	if err != nil {
		return false, err
	}

	return affected != 0, nil
}

func (permission *Permission) GetId() string {
	return fmt.Sprintf("%s/%s", permission.Owner, permission.Name)
}

func (permission *Permission) check() error {
	for _, action := range permission.Actions {
		if !slices.Contains(AllActions, action) {
			return fmt.Errorf("invalid action: %s", action)
		}
	}

	for _, clientIp := range permission.ClientIps {
		if clientIp == "*" || net.ParseIP(clientIp) != nil {
			continue
		}
		if _, _, err := net.ParseCIDR(clientIp); err != nil {
			return fmt.Errorf("invalid client IP or CIDR: %s", clientIp)
		}
	}

	for _, t := range []string{permission.StartTime, permission.EndTime} {
		if t == "" {
			continue
		}
		if _, err := time.Parse(time.RFC3339, t); err != nil {
			return fmt.Errorf("invalid time: %s, it should be in RFC3339 format", t)
		}
	}

	return nil
}

func (permission *Permission) isActive(now time.Time) bool {
	if !permission.IsEnabled {
		return false
	}

	if permission.StartTime != "" {
		startTime, err := time.Parse(time.RFC3339, permission.StartTime)
		if err != nil || now.Before(startTime) {
			return false
		}
	}

	if permission.EndTime != "" {
		endTime, err := time.Parse(time.RFC3339, permission.EndTime)
		if err != nil || now.After(endTime) {
			return false
		}
	}

	return true
}
