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

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

// The ways to connect to an asset.
const (
	// ConnectMethodWeb is the browser: the web terminal, the remote desktop, the database console,
	// websites and the file panel
	ConnectMethodWeb = "Web"
	// ConnectMethodSsh is a terminal through the SSH gateway: ssh, Xshell, VS Code Remote
	ConnectMethodSsh = "SSH client"
	// ConnectMethodSftp is file transfer through the SSH gateway: sftp, WinSCP, scp, rsync
	ConnectMethodSftp = "SFTP client"
	// ConnectMethodForward is port forwarding through the SSH gateway, which any client can use
	ConnectMethodForward = "Port forwarding"
	// ConnectMethodRdp is Remote Desktop Connection (mstsc) through the Remote Desktop Gateway
	ConnectMethodRdp = "RDP client"
	// ConnectMethodKubectl is kubectl and other Kubernetes clients through the Kubernetes proxy, from a kubeconfig
	ConnectMethodKubectl = "kubectl"
	// ConnectMethodDatabase is a database client (DBeaver, Navicat, mysql, psql) through the database proxy
	ConnectMethodDatabase = "Database client"

	ConnectActionAllow  = "Allow"
	ConnectActionReject = "Reject"
)

var connectMethods = []string{ConnectMethodWeb, ConnectMethodSsh, ConnectMethodSftp, ConnectMethodForward, ConnectMethodRdp, ConnectMethodKubectl, ConnectMethodDatabase}

// ConnectRule allows or rejects ways to connect to assets, for the users, assets and accounts it
// covers. Rules are tried by priority, the smallest first, and the first rule that matches decides;
// without a matching rule the connection goes on, as far as permissions allow. Empty users and
// groups cover everyone, empty assets and asset tags every asset, empty accounts every account
// and empty methods every method.
type ConnectRule struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	UpdatedTime string `xorm:"varchar(100)" json:"updatedTime"`
	DisplayName string `xorm:"varchar(100)" json:"displayName"`
	Description string `xorm:"varchar(500)" json:"description"`

	Priority  int      `json:"priority"`
	Users     []string `xorm:"mediumtext" json:"users"`
	Groups    []string `xorm:"mediumtext" json:"groups"`
	Assets    []string `xorm:"mediumtext" json:"assets"`
	AssetTags []string `xorm:"mediumtext" json:"assetTags"`
	Accounts  []string `xorm:"mediumtext" json:"accounts"`
	Methods   []string `xorm:"varchar(500)" json:"methods"`
	Action    string   `xorm:"varchar(20)" json:"action"`
	IsEnabled bool     `json:"isEnabled"`
}

func (rule *ConnectRule) GetId() string {
	return util.GetIdFromOwnerAndName(rule.Owner, rule.Name)
}

func (rule *ConnectRule) GetDisplayName() string {
	if rule.DisplayName != "" {
		return rule.DisplayName
	}
	return rule.Name
}

func GetConnectRuleCount(owner, field, value string) (int64, error) {
	session := GetSession(owner, -1, -1, field, value, "", "")
	return session.Count(&ConnectRule{})
}

func GetConnectRules(owner string) ([]*ConnectRule, error) {
	rules := []*ConnectRule{}
	err := adapter.engine.Asc("priority").Asc("created_time").Find(&rules, &ConnectRule{Owner: owner})
	return rules, err
}

func GetPaginationConnectRules(owner string, offset, limit int, field, value, sortField, sortOrder string) ([]*ConnectRule, error) {
	rules := []*ConnectRule{}
	if sortField == "" {
		sortField, sortOrder = "priority", "ascend"
	}
	session := GetSession(owner, offset, limit, field, value, sortField, sortOrder)
	err := session.Find(&rules)
	return rules, err
}

func getConnectRule(owner string, name string) (*ConnectRule, error) {
	if owner == "" || name == "" {
		return nil, nil
	}
	rule := ConnectRule{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&rule)
	if err != nil || !existed {
		return nil, err
	}
	return &rule, nil
}

func GetConnectRule(id string) (*ConnectRule, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	return getConnectRule(owner, name)
}

func (rule *ConnectRule) check() error {
	if rule.Action != ConnectActionAllow && rule.Action != ConnectActionReject {
		return fmt.Errorf("invalid action: %s, it should be %s or %s", rule.Action, ConnectActionAllow, ConnectActionReject)
	}
	for _, method := range rule.Methods {
		if !slices.Contains(connectMethods, method) {
			return fmt.Errorf("invalid connection method: %s, it should be one of: %s", method, strings.Join(connectMethods, ", "))
		}
	}
	return nil
}

func UpdateConnectRule(id string, rule *ConnectRule) (bool, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	old, err := getConnectRule(owner, name)
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
	return affected != 0, err
}

func AddConnectRule(rule *ConnectRule) (bool, error) {
	err := rule.check()
	if err != nil {
		return false, err
	}
	affected, err := adapter.engine.Insert(rule)
	return affected != 0, err
}

func DeleteConnectRule(rule *ConnectRule) (bool, error) {
	affected, err := adapter.engine.ID(core.PK{rule.Owner, rule.Name}).Delete(&ConnectRule{})
	return affected != 0, err
}

func matchesAny(values []string, candidates ...string) bool {
	for _, value := range values {
		if value == "*" || (value != "" && slices.Contains(candidates, value)) {
			return true
		}
	}
	return false
}

func (rule *ConnectRule) matches(user *casdoorsdk.User, asset *Asset, account string, method string) bool {
	if len(rule.Users) != 0 || len(rule.Groups) != 0 {
		groups := []string{}
		for _, group := range user.Groups {
			groups = append(groups, group, getGroupName(group))
		}
		if !matchesAny(rule.Users, user.Name) && !matchesAny(rule.Groups, groups...) {
			return false
		}
	}
	if len(rule.Assets) != 0 || len(rule.AssetTags) != 0 {
		if !matchesAny(rule.Assets, asset.Name) && !matchesAny(rule.AssetTags, asset.Tag) {
			return false
		}
	}
	if len(rule.Accounts) != 0 && !matchesAny(rule.Accounts, account) {
		return false
	}
	return len(rule.Methods) == 0 || slices.Contains(rule.Methods, method)
}

// CheckConnectMethod tells, with an error, when a connect rule rejects the way the user connects
// to the asset as the account; the asset's own account is meant when account is empty.
func CheckConnectMethod(user *casdoorsdk.User, asset *Asset, account string, method string) error {
	if user == nil || asset == nil {
		return fmt.Errorf("no user or asset to check the connect rules for")
	}
	if account == "" {
		account = asset.Username
	}

	rules, err := GetConnectRules(asset.Owner)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if !rule.IsEnabled || !rule.matches(user, asset, account, method) {
			continue
		}
		if rule.Action == ConnectActionReject {
			return fmt.Errorf("the connect rule: %s does not let you connect to the asset: %s with %s", rule.GetDisplayName(), asset.Name, getConnectMethodName(method))
		}
		return nil
	}
	return nil
}

func getConnectMethodName(method string) string {
	switch method {
	case ConnectMethodWeb:
		return "the browser"
	case ConnectMethodSsh:
		return "an SSH client"
	case ConnectMethodSftp:
		return "an SFTP client, scp or rsync"
	case ConnectMethodForward:
		return "port forwarding"
	case ConnectMethodRdp:
		return "Remote Desktop Connection"
	case ConnectMethodKubectl:
		return "kubectl"
	case ConnectMethodDatabase:
		return "a database client"
	}
	return method
}
