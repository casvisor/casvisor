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
	"net"
	"strings"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
)

const permissionModelText = `
[request_definition]
r = sub, obj, act, ip

[policy_definition]
p = sub, obj, act, ip

[role_definition]
g = _, _
g2 = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && g2(r.obj, p.obj) && r.act == p.act && ipAllowed(r.ip, p.ip)
`

func IsAdminUser(user *casdoorsdk.User) bool {
	if user == nil {
		return false
	}
	return user.Owner == "built-in" || user.IsAdmin
}

func ipAllowedFunc(args ...interface{}) (interface{}, error) {
	clientIp, _ := args[0].(string)
	pattern, _ := args[1].(string)
	if pattern == "*" {
		return true, nil
	}

	ip := net.ParseIP(clientIp)
	if ip == nil {
		return false, nil
	}

	if _, cidr, err := net.ParseCIDR(pattern); err == nil {
		return cidr.Contains(ip), nil
	}

	patternIp := net.ParseIP(pattern)
	return patternIp != nil && patternIp.Equal(ip), nil
}

func newPermissionEnforcer(user *casdoorsdk.User, assets []*Asset) (*casbin.Enforcer, error) {
	m, err := model.NewModelFromString(permissionModelText)
	if err != nil {
		return nil, err
	}

	enforcer, err := casbin.NewEnforcer(m)
	if err != nil {
		return nil, err
	}
	enforcer.AddFunction("ipAllowed", ipAllowedFunc)

	permissions, err := GetPermissions(user.Owner)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	policies := map[[4]string]bool{}
	for _, permission := range permissions {
		if !permission.isActive(now) {
			continue
		}

		subjects := []string{}
		for _, name := range permission.Users {
			subjects = append(subjects, "user:"+name)
		}
		for _, name := range permission.Groups {
			subjects = append(subjects, "group:"+getGroupName(name))
		}

		objects := []string{}
		for _, name := range permission.Assets {
			objects = append(objects, "asset:"+name)
		}
		for _, tag := range permission.AssetTags {
			objects = append(objects, "tag:"+tag)
		}

		clientIps := permission.ClientIps
		if len(clientIps) == 0 {
			clientIps = []string{"*"}
		}

		for _, sub := range subjects {
			for _, obj := range objects {
				for _, act := range permission.Actions {
					for _, clientIp := range clientIps {
						policies[[4]string{sub, obj, act, clientIp}] = true
					}
				}
			}
		}
	}

	rules := [][]string{}
	for policy := range policies {
		rules = append(rules, policy[:])
	}
	if len(rules) > 0 {
		_, err = enforcer.AddPolicies(rules)
		if err != nil {
			return nil, err
		}
	}

	userSub := "user:" + user.Name
	groupings := [][]string{{userSub, "user:*"}}
	for _, group := range user.Groups {
		groupings = append(groupings, []string{userSub, "group:" + getGroupName(group)})
	}
	_, err = enforcer.AddGroupingPolicies(groupings)
	if err != nil {
		return nil, err
	}

	assetGroupings := map[[2]string]bool{}
	for _, asset := range assets {
		assetGroupings[[2]string{"asset:" + asset.Name, "asset:*"}] = true
		if asset.Tag != "" {
			assetGroupings[[2]string{"asset:" + asset.Name, "tag:" + asset.Tag}] = true
		}
	}
	rules = [][]string{}
	for grouping := range assetGroupings {
		rules = append(rules, grouping[:])
	}
	if len(rules) > 0 {
		_, err = enforcer.AddNamedGroupingPolicies("g2", rules)
		if err != nil {
			return nil, err
		}
	}

	return enforcer, nil
}

func getGroupName(group string) string {
	tokens := strings.Split(group, "/")
	return tokens[len(tokens)-1]
}

func getActionsFromEnforcer(enforcer *casbin.Enforcer, user *casdoorsdk.User, asset *Asset, clientIp string) ([]string, error) {
	res := []string{}
	for _, action := range AllActions {
		allowed, err := enforcer.Enforce("user:"+user.Name, "asset:"+asset.Name, action, clientIp)
		if err != nil {
			return nil, err
		}
		if allowed {
			res = append(res, action)
		}
	}

	if len(res) > 0 && res[0] != ActionConnect {
		return []string{}, nil
	}
	return res, nil
}

func GetAssetActions(user *casdoorsdk.User, asset *Asset, clientIp string) ([]string, error) {
	if user == nil || asset == nil {
		return []string{}, nil
	}

	if user.Owner == "built-in" {
		return AllActions, nil
	}

	if user.Owner != asset.Owner {
		return []string{}, nil
	}

	if user.IsAdmin {
		return AllActions, nil
	}

	enforcer, err := newPermissionEnforcer(user, []*Asset{asset})
	if err != nil {
		return nil, err
	}

	return getActionsFromEnforcer(enforcer, user, asset, clientIp)
}

func FilterAllowedAssets(user *casdoorsdk.User, assets []*Asset, clientIp string) ([]*Asset, error) {
	res := []*Asset{}
	if user == nil || len(assets) == 0 {
		return res, nil
	}

	enforcer, err := newPermissionEnforcer(user, assets)
	if err != nil {
		return nil, err
	}

	for _, asset := range assets {
		if asset.Owner != user.Owner {
			continue
		}

		allowed, err := enforcer.Enforce("user:"+user.Name, "asset:"+asset.Name, ActionConnect, clientIp)
		if err != nil {
			return nil, err
		}
		if allowed {
			res = append(res, asset)
		}
	}

	return res, nil
}

func GetSessionOperations(actions []string) []string {
	res := []string{}
	for _, action := range actions {
		switch action {
		case ActionCopy:
			res = append(res, "copy")
		case ActionPaste:
			res = append(res, "paste")
		case ActionUpload:
			res = append(res, "upload", "createDir", "edit", "rename", "delete")
		case ActionDownload:
			res = append(res, "download")
		}
	}

	for _, action := range actions {
		if action == ActionUpload || action == ActionDownload {
			res = append(res, "fileSystem")
			break
		}
	}

	return res
}
