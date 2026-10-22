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
	"regexp"
	"slices"
	"sync"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

const (
	CommandActionBlock = "Block"
	CommandActionAlert = "Alert"
)

// CommandRule blocks or flags the commands that match its pattern, for the users
// and assets it covers; empty users and groups cover everyone, empty assets and
// asset tags cover every asset.
type CommandRule struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	UpdatedTime string `xorm:"varchar(100)" json:"updatedTime"`
	DisplayName string `xorm:"varchar(100)" json:"displayName"`
	Description string `xorm:"varchar(500)" json:"description"`

	Pattern   string   `xorm:"varchar(1000)" json:"pattern"`
	Action    string   `xorm:"varchar(20)" json:"action"`
	Users     []string `xorm:"mediumtext" json:"users"`
	Groups    []string `xorm:"mediumtext" json:"groups"`
	Assets    []string `xorm:"mediumtext" json:"assets"`
	AssetTags []string `xorm:"mediumtext" json:"assetTags"`
	IsEnabled bool     `json:"isEnabled"`
}

func GetCommandRuleCount(owner, field, value string) (int64, error) {
	session := GetSession(owner, -1, -1, field, value, "", "")
	return session.Count(&CommandRule{})
}

func GetCommandRules(owner string) ([]*CommandRule, error) {
	rules := []*CommandRule{}
	err := adapter.engine.Desc("created_time").Find(&rules, &CommandRule{Owner: owner})
	if err != nil {
		return rules, err
	}

	return rules, nil
}

func GetPaginationCommandRules(owner string, offset, limit int, field, value, sortField, sortOrder string) ([]*CommandRule, error) {
	rules := []*CommandRule{}
	session := GetSession(owner, offset, limit, field, value, sortField, sortOrder)
	err := session.Find(&rules)
	if err != nil {
		return rules, err
	}

	return rules, nil
}

func getCommandRule(owner string, name string) (*CommandRule, error) {
	if owner == "" || name == "" {
		return nil, nil
	}

	rule := CommandRule{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&rule)
	if err != nil {
		return nil, err
	}

	if existed {
		return &rule, nil
	}
	return nil, nil
}

func GetCommandRule(id string) (*CommandRule, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	return getCommandRule(owner, name)
}

func UpdateCommandRule(id string, rule *CommandRule) (bool, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	old, err := getCommandRule(owner, name)
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

func AddCommandRule(rule *CommandRule) (bool, error) {
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

func DeleteCommandRule(rule *CommandRule) (bool, error) {
	affected, err := adapter.engine.ID(core.PK{rule.Owner, rule.Name}).Delete(&CommandRule{})
	if err != nil {
		return false, err
	}

	return affected != 0, nil
}

func (rule *CommandRule) GetId() string {
	return util.GetIdFromOwnerAndName(rule.Owner, rule.Name)
}

func (rule *CommandRule) check() error {
	if rule.Pattern == "" {
		return fmt.Errorf("the pattern of the command rule should not be empty")
	}
	_, err := regexp.Compile(rule.Pattern)
	if err != nil {
		return fmt.Errorf("invalid pattern: %s", err.Error())
	}

	if rule.Action != CommandActionBlock && rule.Action != CommandActionAlert {
		return fmt.Errorf("invalid action: %s", rule.Action)
	}
	return nil
}

var patternCache sync.Map

func getPattern(pattern string) (*regexp.Regexp, error) {
	if value, ok := patternCache.Load(pattern); ok {
		return value.(*regexp.Regexp), nil
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	patternCache.Store(pattern, re)
	return re, nil
}

func containsAny(values []string, candidates ...string) bool {
	for _, value := range values {
		if value == "*" || slices.Contains(candidates, value) {
			return true
		}
	}
	return false
}

func (rule *CommandRule) covers(user *casdoorsdk.User, asset *Asset) bool {
	if len(rule.Users) != 0 || len(rule.Groups) != 0 {
		groups := []string{}
		for _, group := range user.Groups {
			groups = append(groups, getGroupName(group))
		}
		if !containsAny(rule.Users, user.Name) && !containsAny(rule.Groups, groups...) {
			return false
		}
	}

	if len(rule.Assets) != 0 || len(rule.AssetTags) != 0 {
		if !containsAny(rule.Assets, asset.Name) && (asset.Tag == "" || !containsAny(rule.AssetTags, asset.Tag)) {
			return false
		}
	}
	return true
}

// MatchCommandRule returns the rule a command breaks on the asset, a Block rule
// before an Alert rule, or nil when the command may run without notice.
func MatchCommandRule(user *casdoorsdk.User, asset *Asset, command string) (*CommandRule, error) {
	rules, err := GetCommandRules(asset.Owner)
	if err != nil {
		return nil, err
	}

	var res *CommandRule
	for _, rule := range rules {
		if !rule.IsEnabled || !rule.covers(user, asset) {
			continue
		}

		re, err := getPattern(rule.Pattern)
		if err != nil || !re.MatchString(command) {
			continue
		}

		if rule.Action == CommandActionBlock {
			return rule, nil
		}
		if res == nil {
			res = rule
		}
	}
	return res, nil
}
