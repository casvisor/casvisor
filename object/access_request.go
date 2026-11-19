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
	"time"

	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

const (
	AccessRequestPending   = "Pending"
	AccessRequestApproved  = "Approved"
	AccessRequestRejected  = "Rejected"
	AccessRequestCancelled = "Cancelled"
	AccessRequestRevoked   = "Revoked"
)

// AccessRequest is a user's request for access to assets, which becomes a permission once an
// administrator approves it.
type AccessRequest struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	UpdatedTime string `xorm:"varchar(100)" json:"updatedTime"`

	User      string   `xorm:"varchar(100) index" json:"user"`
	Assets    []string `xorm:"mediumtext" json:"assets"`
	Accounts  []string `xorm:"mediumtext" json:"accounts"`
	Actions   []string `xorm:"varchar(500)" json:"actions"`
	StartTime string   `xorm:"varchar(100)" json:"startTime"`
	EndTime   string   `xorm:"varchar(100)" json:"endTime"`
	Reason    string   `xorm:"varchar(1000)" json:"reason"`

	Status        string `xorm:"varchar(20) index" json:"status"`
	Reviewer      string `xorm:"varchar(100)" json:"reviewer"`
	ReviewedTime  string `xorm:"varchar(100)" json:"reviewedTime"`
	ReviewComment string `xorm:"varchar(1000)" json:"reviewComment"`
	Permission    string `xorm:"varchar(100)" json:"permission"`
}

// RequestableAsset is what a user sees of an asset they may ask for access to.
type RequestableAsset struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"displayName"`
	Protocol    string   `json:"protocol"`
	Tag         string   `json:"tag"`
	Accounts    []string `json:"accounts"`
}

func (request *AccessRequest) GetId() string {
	return util.GetIdFromOwnerAndName(request.Owner, request.Name)
}

func GetAccessRequestCount(owner, user, field, value string) (int64, error) {
	session := GetSession(owner, -1, -1, field, value, "", "")
	return session.Count(&AccessRequest{User: user})
}

func GetPaginationAccessRequests(owner, user string, offset, limit int, field, value, sortField, sortOrder string) ([]*AccessRequest, error) {
	requests := []*AccessRequest{}
	session := GetSession(owner, offset, limit, field, value, sortField, sortOrder)
	err := session.Find(&requests, &AccessRequest{User: user})
	return requests, err
}

func GetAccessRequests(owner, user string) ([]*AccessRequest, error) {
	requests := []*AccessRequest{}
	err := adapter.engine.Desc("created_time").Find(&requests, &AccessRequest{Owner: owner, User: user})
	return requests, err
}

func GetPendingAccessRequestCount(owner string) (int64, error) {
	return adapter.engine.Count(&AccessRequest{Owner: owner, Status: AccessRequestPending})
}

func getAccessRequest(owner string, name string) (*AccessRequest, error) {
	if owner == "" || name == "" {
		return nil, nil
	}

	request := AccessRequest{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&request)
	if err != nil {
		return nil, err
	}
	if !existed {
		return nil, nil
	}
	return &request, nil
}

func GetAccessRequest(id string) (*AccessRequest, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	return getAccessRequest(owner, name)
}

// GetRequestableAssets lists the assets of the organization with the usernames of their accounts,
// and nothing that helps reach them.
func GetRequestableAssets(owner string) ([]*RequestableAsset, error) {
	assets, err := GetAssets(owner)
	if err != nil {
		return nil, err
	}
	accounts, err := getAllAssetAccounts(owner)
	if err != nil {
		return nil, err
	}

	res := []*RequestableAsset{}
	for _, asset := range assets {
		item := &RequestableAsset{Name: asset.Name, DisplayName: asset.DisplayName, Protocol: asset.Protocol, Tag: asset.Tag, Accounts: []string{}}
		if asset.Username != "" {
			item.Accounts = append(item.Accounts, asset.Username)
		}
		for _, account := range accounts {
			if account.Asset == asset.Name && account.IsEnabled {
				item.Accounts = append(item.Accounts, account.Username)
			}
		}
		res = append(res, item)
	}
	return res, nil
}

func (request *AccessRequest) check() error {
	if len(request.Assets) == 0 {
		return fmt.Errorf("choose at least one asset")
	}
	for _, name := range request.Assets {
		asset, err := getAsset(request.Owner, name)
		if err != nil {
			return err
		}
		if asset == nil {
			return fmt.Errorf("the asset: %s does not exist", name)
		}
	}

	if len(request.Actions) == 0 {
		return fmt.Errorf("choose at least one action")
	}
	if !slices.Contains(request.Actions, ActionConnect) {
		return fmt.Errorf("the actions must include %s", ActionConnect)
	}
	for _, action := range request.Actions {
		if !slices.Contains(AllActions, action) {
			return fmt.Errorf("invalid action: %s", action)
		}
	}
	for _, account := range request.Accounts {
		if account == "" || account == "*" || strings.ContainsAny(account, ":/ ") {
			return fmt.Errorf("invalid account: %s", account)
		}
	}

	if request.EndTime == "" {
		return fmt.Errorf("access requests must end at some time")
	}
	start := time.Now()
	if request.StartTime != "" {
		t, err := time.Parse(time.RFC3339, request.StartTime)
		if err != nil {
			return fmt.Errorf("invalid start time: %s", request.StartTime)
		}
		start = t
	}
	end, err := time.Parse(time.RFC3339, request.EndTime)
	if err != nil {
		return fmt.Errorf("invalid end time: %s", request.EndTime)
	}
	if !end.After(start) || !end.After(time.Now()) {
		return fmt.Errorf("the end time must be later than the start time and now")
	}

	if strings.TrimSpace(request.Reason) == "" {
		return fmt.Errorf("give a reason for the request")
	}
	return nil
}

func AddAccessRequest(request *AccessRequest) (bool, error) {
	request.Name = util.GenerateId()
	request.CreatedTime = util.GetCurrentTime()
	request.UpdatedTime = request.CreatedTime
	request.Status = AccessRequestPending
	request.Reviewer = ""
	request.ReviewedTime = ""
	request.ReviewComment = ""
	request.Permission = ""

	err := request.check()
	if err != nil {
		return false, err
	}

	affected, err := adapter.engine.Insert(request)
	if err != nil {
		return false, err
	}
	return affected != 0, nil
}

func updateAccessRequestStatus(request *AccessRequest, cols ...string) error {
	request.UpdatedTime = util.GetCurrentTime()
	cols = append(cols, "status", "updated_time")
	_, err := adapter.engine.ID(core.PK{request.Owner, request.Name}).Cols(cols...).Update(request)
	return err
}

func CancelAccessRequest(request *AccessRequest) error {
	if request.Status != AccessRequestPending {
		return fmt.Errorf("only pending requests can be cancelled, the request is %s", request.Status)
	}
	request.Status = AccessRequestCancelled
	return updateAccessRequestStatus(request)
}

func RejectAccessRequest(request *AccessRequest, reviewer string, comment string) error {
	if request.Status != AccessRequestPending {
		return fmt.Errorf("only pending requests can be rejected, the request is %s", request.Status)
	}
	request.Status = AccessRequestRejected
	request.Reviewer = reviewer
	request.ReviewedTime = util.GetCurrentTime()
	request.ReviewComment = comment
	return updateAccessRequestStatus(request, "reviewer", "reviewed_time", "review_comment")
}

// ApproveAccessRequest grants the request as a permission of its own, after the reviewer may have
// narrowed its actions and shortened its end time.
func ApproveAccessRequest(request *AccessRequest, reviewer string, comment string, actions []string, endTime string) (*Permission, error) {
	if request.Status != AccessRequestPending {
		return nil, fmt.Errorf("only pending requests can be approved, the request is %s", request.Status)
	}
	if len(actions) > 0 {
		request.Actions = actions
	}
	if endTime != "" {
		request.EndTime = endTime
	}
	err := request.check()
	if err != nil {
		return nil, err
	}

	accounts := request.Accounts
	if len(accounts) == 0 {
		accounts = []string{AccountDefault}
	}
	permission := &Permission{
		Owner:       request.Owner,
		Name:        "request_" + request.Name,
		CreatedTime: util.GetCurrentTime(),
		UpdatedTime: util.GetCurrentTime(),
		DisplayName: fmt.Sprintf("Access request of %s", request.User),
		Description: truncate(fmt.Sprintf("Approved by %s: %s", reviewer, request.Reason), 500),
		Users:       []string{request.User},
		Groups:      []string{},
		Assets:      request.Assets,
		AssetTags:   []string{},
		Accounts:    accounts,
		Actions:     request.Actions,
		ClientIps:   []string{},
		StartTime:   request.StartTime,
		EndTime:     request.EndTime,
		IsEnabled:   true,
	}
	_, err = AddPermission(permission)
	if err != nil {
		return nil, err
	}

	request.Status = AccessRequestApproved
	request.Reviewer = reviewer
	request.ReviewedTime = util.GetCurrentTime()
	request.ReviewComment = comment
	request.Permission = permission.Name
	err = updateAccessRequestStatus(request, "reviewer", "reviewed_time", "review_comment", "permission", "actions", "end_time")
	if err != nil {
		return nil, err
	}
	return permission, nil
}

// RevokeAccessRequest takes back an approved request by deleting the permission it became.
func RevokeAccessRequest(request *AccessRequest, reviewer string, comment string) error {
	if request.Status != AccessRequestApproved {
		return fmt.Errorf("only approved requests can be revoked, the request is %s", request.Status)
	}

	if request.Permission != "" {
		_, err := DeletePermission(&Permission{Owner: request.Owner, Name: request.Permission})
		if err != nil {
			return err
		}
	}

	request.Status = AccessRequestRevoked
	request.Reviewer = reviewer
	request.ReviewedTime = util.GetCurrentTime()
	if comment != "" {
		request.ReviewComment = comment
	}
	return updateAccessRequestStatus(request, "reviewer", "reviewed_time", "review_comment")
}

func DeleteAccessRequest(request *AccessRequest) (bool, error) {
	affected, err := adapter.engine.ID(core.PK{request.Owner, request.Name}).Delete(&AccessRequest{})
	if err != nil {
		return false, err
	}
	return affected != 0, nil
}
