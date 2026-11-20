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
	"sort"
	"strings"
	"time"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

const (
	LoginActionAllow  = "Allow"
	LoginActionReject = "Reject"
	LoginActionReview = "Review"
	LoginActionMfa    = "Mfa"
)

var loginActions = []string{LoginActionAllow, LoginActionReject, LoginActionReview, LoginActionMfa}

const (
	LoginMethodWeb = "Web"
	LoginMethodSsh = "SSH"
)

// LoginRule decides what happens when the users it covers sign in from the client IPs and in the time
// it covers. Rules are tried by priority, the smallest first, and the first rule that matches decides;
// without a matching rule the sign-in goes on. Empty users and groups cover everyone, empty client IPs
// every address, empty weekdays every day and empty start and end times the whole day.
type LoginRule struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	UpdatedTime string `xorm:"varchar(100)" json:"updatedTime"`
	DisplayName string `xorm:"varchar(100)" json:"displayName"`
	Description string `xorm:"varchar(500)" json:"description"`

	Priority  int      `json:"priority"`
	Users     []string `xorm:"mediumtext" json:"users"`
	Groups    []string `xorm:"mediumtext" json:"groups"`
	ClientIps []string `xorm:"mediumtext" json:"clientIps"`
	Weekdays  []int    `xorm:"varchar(100)" json:"weekdays"`
	StartTime string   `xorm:"varchar(10)" json:"startTime"`
	EndTime   string   `xorm:"varchar(10)" json:"endTime"`
	TimeZone  string   `xorm:"varchar(100)" json:"timeZone"`
	Methods   []string `xorm:"varchar(100)" json:"methods"`
	Action    string   `xorm:"varchar(20)" json:"action"`
	Reviewers []string `xorm:"mediumtext" json:"reviewers"`
	IsEnabled bool     `json:"isEnabled"`
}

func GetLoginRuleCount(owner, field, value string) (int64, error) {
	session := GetSession(owner, -1, -1, field, value, "", "")
	return session.Count(&LoginRule{})
}

func GetLoginRules(owner string) ([]*LoginRule, error) {
	rules := []*LoginRule{}
	err := adapter.engine.Asc("priority").Asc("created_time").Find(&rules, &LoginRule{Owner: owner})
	if err != nil {
		return rules, err
	}
	return rules, nil
}

func GetPaginationLoginRules(owner string, offset, limit int, field, value, sortField, sortOrder string) ([]*LoginRule, error) {
	rules := []*LoginRule{}
	if sortField == "" || sortOrder == "" {
		sortField, sortOrder = "priority", "ascend"
	}
	session := GetSession(owner, offset, limit, field, value, sortField, sortOrder)
	err := session.Find(&rules)
	if err != nil {
		return rules, err
	}
	return rules, nil
}

func getLoginRule(owner string, name string) (*LoginRule, error) {
	if owner == "" || name == "" {
		return nil, nil
	}

	rule := LoginRule{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&rule)
	if err != nil {
		return nil, err
	}
	if !existed {
		return nil, nil
	}
	return &rule, nil
}

func GetLoginRule(id string) (*LoginRule, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	return getLoginRule(owner, name)
}

func UpdateLoginRule(id string, rule *LoginRule) (bool, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	old, err := getLoginRule(owner, name)
	if err != nil {
		return false, err
	} else if old == nil {
		return false, nil
	}

	err = rule.check()
	if err != nil {
		return false, err
	}

	rule.UpdatedTime = util.GetCurrentTime()
	affected, err := adapter.engine.ID(core.PK{owner, name}).AllCols().Update(rule)
	if err != nil {
		return false, err
	}
	return affected != 0, nil
}

func AddLoginRule(rule *LoginRule) (bool, error) {
	err := rule.check()
	if err != nil {
		return false, err
	}

	affected, err := adapter.engine.Insert(rule)
	if err != nil {
		return false, err
	}
	return affected != 0, nil
}

func DeleteLoginRule(rule *LoginRule) (bool, error) {
	affected, err := adapter.engine.ID(core.PK{rule.Owner, rule.Name}).Delete(&LoginRule{})
	if err != nil {
		return false, err
	}
	return affected != 0, nil
}

func (rule *LoginRule) GetId() string {
	return fmt.Sprintf("%s/%s", rule.Owner, rule.Name)
}

func parseClockTime(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, fmt.Errorf("invalid time: %s, it should be like 09:00", s)
	}
	return t.Hour()*60 + t.Minute(), nil
}

func (rule *LoginRule) check() error {
	if !slices.Contains(loginActions, rule.Action) {
		return fmt.Errorf("invalid action: %s, it should be one of: %s", rule.Action, strings.Join(loginActions, ", "))
	}

	for _, clientIp := range rule.ClientIps {
		if clientIp == "*" || net.ParseIP(clientIp) != nil {
			continue
		}
		if _, _, err := net.ParseCIDR(clientIp); err != nil {
			return fmt.Errorf("invalid client IP or CIDR: %s", clientIp)
		}
	}

	for _, weekday := range rule.Weekdays {
		if weekday < 0 || weekday > 6 {
			return fmt.Errorf("invalid weekday: %d, it should be from 0 (Sunday) to 6 (Saturday)", weekday)
		}
	}

	if (rule.StartTime == "") != (rule.EndTime == "") {
		return fmt.Errorf("the start time and the end time should be both set or both empty")
	}
	for _, t := range []string{rule.StartTime, rule.EndTime} {
		if t == "" {
			continue
		}
		if _, err := parseClockTime(t); err != nil {
			return err
		}
	}

	if rule.TimeZone != "" {
		if _, err := time.LoadLocation(rule.TimeZone); err != nil {
			return fmt.Errorf("invalid time zone: %s", rule.TimeZone)
		}
	}

	for _, method := range rule.Methods {
		if method != LoginMethodWeb && method != LoginMethodSsh {
			return fmt.Errorf("invalid sign-in method: %s, it should be %s or %s", method, LoginMethodWeb, LoginMethodSsh)
		}
	}
	return nil
}

// matchUser tells whether the rule covers the user, and whether it names the user instead of
// covering them through "*" or a group.
func (rule *LoginRule) matchUser(user *casdoorsdk.User) (bool, bool) {
	if len(rule.Users) == 0 && len(rule.Groups) == 0 {
		return true, false
	}
	if slices.Contains(rule.Users, user.Name) {
		return true, true
	}
	if slices.Contains(rule.Users, "*") || slices.Contains(rule.Groups, "*") {
		return true, false
	}
	for _, group := range user.Groups {
		if slices.Contains(rule.Groups, getGroupName(group)) || slices.Contains(rule.Groups, group) {
			return true, false
		}
	}
	return false, false
}

func (rule *LoginRule) matchClientIp(clientIp string) bool {
	if len(rule.ClientIps) == 0 {
		return true
	}
	for _, pattern := range rule.ClientIps {
		if allowed, _ := ipAllowedFunc(clientIp, pattern); allowed == true {
			return true
		}
	}
	return false
}

func (rule *LoginRule) matchTime(now time.Time) bool {
	if rule.TimeZone != "" {
		if location, err := time.LoadLocation(rule.TimeZone); err == nil {
			now = now.In(location)
		}
	}

	minute := now.Hour()*60 + now.Minute()
	weekday := int(now.Weekday())
	if rule.StartTime != "" && rule.EndTime != "" {
		start, err1 := parseClockTime(rule.StartTime)
		end, err2 := parseClockTime(rule.EndTime)
		if err1 != nil || err2 != nil {
			return false
		}
		switch {
		case start < end:
			if minute < start || minute >= end {
				return false
			}
		case start > end:
			// a window over midnight, such as 22:00 to 06:00, belongs to the day it starts on
			if minute < start && minute >= end {
				return false
			}
			if minute < end {
				weekday = (weekday + 6) % 7
			}
		}
	}

	return len(rule.Weekdays) == 0 || slices.Contains(rule.Weekdays, weekday)
}

// LoginDecision is what the login rules say about a sign-in; a nil Rule means no rule matched.
type LoginDecision struct {
	Rule   *LoginRule
	Action string
}

func (d *LoginDecision) RuleName() string {
	if d == nil || d.Rule == nil {
		return ""
	}
	return d.Rule.Name
}

// CheckLoginRules finds the first rule that matches the sign-in. A rule that covers an administrator of
// the organization only through "*" or a group does not reject or hold the administrator for review, so
// that a broad rule can't lock every administrator out; a rule naming the administrator does.
func CheckLoginRules(user *casdoorsdk.User, clientIp string, method string, now time.Time) (*LoginDecision, error) {
	decision := &LoginDecision{Action: LoginActionAllow}
	if user == nil || user.Owner == "built-in" {
		return decision, nil
	}

	rules, err := GetLoginRules(user.Owner)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].Priority < rules[j].Priority })

	for _, rule := range rules {
		if !rule.IsEnabled {
			continue
		}
		if len(rule.Methods) > 0 && !slices.Contains(rule.Methods, method) {
			continue
		}
		matched, named := rule.matchUser(user)
		if !matched || !rule.matchClientIp(clientIp) || !rule.matchTime(now) {
			continue
		}
		if user.IsAdmin && !named && (rule.Action == LoginActionReject || rule.Action == LoginActionReview) {
			continue
		}
		return &LoginDecision{Rule: rule, Action: rule.Action}, nil
	}
	return decision, nil
}

// CanReviewLogin tells whether the user may approve or reject sign-ins held by the rule.
func (rule *LoginRule) CanReviewLogin(user *casdoorsdk.User) bool {
	if user == nil {
		return false
	}
	if user.Owner == "built-in" {
		return true
	}
	if user.Owner != rule.Owner || !user.IsAdmin {
		return false
	}
	return len(rule.Reviewers) == 0 || slices.Contains(rule.Reviewers, user.Name)
}
