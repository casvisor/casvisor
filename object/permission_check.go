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
	"strings"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
)

const permissionModelText = `
[request_definition]
r = sub, obj, act, ip, acct

[policy_definition]
p = sub, obj, act, ip, acct

[role_definition]
g = _, _
g2 = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && g2(r.obj, p.obj) && r.act == p.act && ipAllowed(r.ip, p.ip) && accountAllowed(r.acct, p.acct, r.sub)
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

// accountAllowed matches an account key ("default:<username>" or "account:<username>") with an
// account of a permission: "*", "@default", "@same" (the account named like the user) or a username.
func accountAllowedFunc(args ...interface{}) (interface{}, error) {
	key, _ := args[0].(string)
	pattern, _ := args[1].(string)
	sub, _ := args[2].(string)
	if pattern == "*" {
		return true, nil
	}

	kind, username, _ := strings.Cut(key, ":")
	switch pattern {
	case AccountDefault:
		return kind == "default", nil
	case AccountSame:
		return username != "" && username == strings.TrimPrefix(sub, "user:"), nil
	default:
		return username == pattern, nil
	}
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
	enforcer.AddFunction("accountAllowed", accountAllowedFunc)

	permissions, err := GetPermissions(user.Owner)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	policies := map[[5]string]bool{}
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
		for _, node := range permission.Nodes {
			objects = append(objects, "node:"+node)
		}

		clientIps := permission.ClientIps
		if len(clientIps) == 0 {
			clientIps = []string{"*"}
		}

		accounts := permission.Accounts
		if len(accounts) == 0 {
			accounts = []string{AccountDefault}
		}

		actions := permission.Actions
		if permission.Watermark {
			actions = append(slices.Clone(actions), actionWatermark)
		}
		if slices.Contains(permission.Actions, ActionConnect) {
			actions = append(slices.Clone(actions), permission.getKubernetesActions()...)
		}

		for _, sub := range subjects {
			for _, obj := range objects {
				for _, act := range actions {
					for _, clientIp := range clientIps {
						for _, account := range accounts {
							policies[[5]string{sub, obj, act, clientIp, account}] = true
						}
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

	nodes, err := GetAssetNodes(user.Owner)
	if err != nil {
		return nil, err
	}

	assetGroupings := map[[2]string]bool{}
	for _, asset := range assets {
		assetGroupings[[2]string{"asset:" + asset.Name, "asset:*"}] = true
		if asset.Tag != "" {
			assetGroupings[[2]string{"asset:" + asset.Name, "tag:" + asset.Tag}] = true
		}
		for _, node := range asset.Nodes {
			assetGroupings[[2]string{"asset:" + asset.Name, "node:" + node}] = true
		}
	}
	for _, node := range nodes {
		if node.Parent != "" && node.Parent != node.Name {
			assetGroupings[[2]string{"node:" + node.Name, "node:" + node.Parent}] = true
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

// actionWatermark is granted along with the actions of a permission that shows a watermark.
const actionWatermark = "Watermark"

// GetSessionWatermark returns the text of the watermark over a session of the user on the asset as
// the account, or an empty string when no permission covering the session shows a watermark.
func GetSessionWatermark(user *casdoorsdk.User, asset *Asset, account string, clientIp string) (string, error) {
	if user == nil || asset == nil || user.Owner != asset.Owner {
		return "", nil
	}

	enforcer, err := newPermissionEnforcer(user, []*Asset{asset})
	if err != nil {
		return "", err
	}
	ok, err := enforcer.Enforce("user:"+user.Name, "asset:"+asset.Name, actionWatermark, clientIp, getAccountKey(asset, account))
	if err != nil || !ok {
		return "", err
	}

	name := user.Owner + "/" + user.Name
	if user.DisplayName != "" && user.DisplayName != user.Name {
		name = fmt.Sprintf("%s (%s)", user.DisplayName, name)
	}
	return fmt.Sprintf("%s  %s", name, clientIp), nil
}

func getGroupName(group string) string {
	tokens := strings.Split(group, "/")
	return tokens[len(tokens)-1]
}

func getActionsFromEnforcer(enforcer *casbin.Enforcer, user *casdoorsdk.User, asset *Asset, accountKey string, clientIp string) ([]string, error) {
	res := []string{}
	for _, action := range AllActions {
		allowed, err := enforcer.Enforce("user:"+user.Name, "asset:"+asset.Name, action, clientIp, accountKey)
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

func hasAllAssets(user *casdoorsdk.User, asset *Asset) bool {
	return user.Owner == "built-in" || (user.Owner == asset.Owner && user.IsAdmin)
}

// GetAssetActions returns what the user may do on the asset as any of its accounts.
func GetAssetActions(user *casdoorsdk.User, asset *Asset, clientIp string) ([]string, error) {
	return GetAssetAccountActions(user, asset, nil, clientIp)
}

// GetAssetAccountActions returns what the user may do on the asset as the account, or as any
// account when username is nil.
func GetAssetAccountActions(user *casdoorsdk.User, asset *Asset, username *string, clientIp string) ([]string, error) {
	if user == nil || asset == nil {
		return []string{}, nil
	}

	accounts, err := GetAssetAccounts(asset.Owner, asset.Name)
	if err != nil {
		return nil, err
	}
	keys := getAccountKeys(asset, accounts)
	if username != nil {
		key := getAccountKey(asset, *username)
		if !slices.Contains(keys, key) {
			return []string{}, nil
		}
		keys = []string{key}
	}

	if hasAllAssets(user, asset) {
		return AllActions, nil
	}
	if user.Owner != asset.Owner {
		return []string{}, nil
	}

	enforcer, err := newPermissionEnforcer(user, []*Asset{asset})
	if err != nil {
		return nil, err
	}

	res := []string{}
	for _, key := range keys {
		actions, err := getActionsFromEnforcer(enforcer, user, asset, key, clientIp)
		if err != nil {
			return nil, err
		}
		for _, action := range actions {
			if !slices.Contains(res, action) {
				res = append(res, action)
			}
		}
	}

	sorted := []string{}
	for _, action := range AllActions {
		if slices.Contains(res, action) {
			sorted = append(sorted, action)
		}
	}
	return sorted, nil
}

// GetAllowedAccounts returns the accounts of the asset the user may connect as.
func GetAllowedAccounts(user *casdoorsdk.User, asset *Asset, clientIp string) ([]*AccountInfo, error) {
	res := []*AccountInfo{}
	if user == nil || asset == nil {
		return res, nil
	}

	accounts, err := GetAssetAccounts(asset.Owner, asset.Name)
	if err != nil {
		return nil, err
	}

	var enforcer *casbin.Enforcer
	if !hasAllAssets(user, asset) {
		if user.Owner != asset.Owner {
			return res, nil
		}
		enforcer, err = newPermissionEnforcer(user, []*Asset{asset})
		if err != nil {
			return nil, err
		}
	}

	allowed := func(key string) (bool, error) {
		if enforcer == nil {
			return true, nil
		}
		return enforcer.Enforce("user:"+user.Name, "asset:"+asset.Name, ActionConnect, clientIp, key)
	}

	ok, err := allowed(getAccountKey(asset, ""))
	if err != nil {
		return nil, err
	}
	if ok {
		res = append(res, &AccountInfo{Username: asset.Username, IsDefault: true})
	}
	for _, account := range accounts {
		if !account.IsEnabled {
			continue
		}
		ok, err = allowed(getAccountKey(asset, account.Username))
		if err != nil {
			return nil, err
		}
		if ok {
			res = append(res, &AccountInfo{Username: account.Username, DisplayName: account.DisplayName})
		}
	}
	return res, nil
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

	accounts, err := getAllAssetAccounts(user.Owner)
	if err != nil {
		return nil, err
	}

	for _, asset := range assets {
		if asset.Owner != user.Owner {
			continue
		}

		for _, key := range getAccountKeys(asset, accounts) {
			allowed, err := enforcer.Enforce("user:"+user.Name, "asset:"+asset.Name, ActionConnect, clientIp, key)
			if err != nil {
				return nil, err
			}
			if allowed {
				res = append(res, asset)
				break
			}
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

// the namespaces a permission covers on Kubernetes assets are granted as actions like
// "Kubernetes:Write:default", along with Connect
const (
	actionKubernetesRead  = "Kubernetes:Read:"
	actionKubernetesWrite = "Kubernetes:Write:"
)

func (permission *Permission) getKubernetesActions() []string {
	prefix := actionKubernetesWrite
	if permission.ReadOnly {
		prefix = actionKubernetesRead
	}
	namespaces := permission.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{"*"}
	}

	res := []string{}
	for _, namespace := range namespaces {
		res = append(res, prefix+namespace)
	}
	return res
}

// KubernetesScope is what a user may do on a Kubernetes asset: the namespaces, "*" for all of
// them and for what has no namespace, each with whether it may be changed or only read.
type KubernetesScope struct {
	Namespaces map[string]bool `json:"namespaces"`
}

func NewKubernetesScopeAll() *KubernetesScope {
	return &KubernetesScope{Namespaces: map[string]bool{"*": true}}
}

func (s *KubernetesScope) CanRead(namespace string) bool {
	_, ok := s.Namespaces[namespace]
	_, all := s.Namespaces["*"]
	return ok || all
}

func (s *KubernetesScope) CanWrite(namespace string) bool {
	return s.Namespaces[namespace] || s.Namespaces["*"]
}

// IsAll tells whether every namespace may be read.
func (s *KubernetesScope) IsAll() bool {
	return s.CanRead("*")
}

// GetNames returns the namespaces named in the scope, without "*".
func (s *KubernetesScope) GetNames() []string {
	res := []string{}
	for namespace := range s.Namespaces {
		if namespace != "*" {
			res = append(res, namespace)
		}
	}
	slices.Sort(res)
	return res
}

// GetKubernetesAccess returns what the user may do on the asset as the account, and the namespaces
// the permissions that let the user connect to it cover; an empty scope when none does.
func GetKubernetesAccess(user *casdoorsdk.User, asset *Asset, account string, clientIp string) ([]string, *KubernetesScope, error) {
	res := &KubernetesScope{Namespaces: map[string]bool{}}
	if user == nil || asset == nil {
		return []string{}, res, nil
	}

	actions, err := GetAssetAccountActions(user, asset, &account, clientIp)
	if err != nil {
		return nil, nil, err
	}
	if !slices.Contains(actions, ActionConnect) {
		return actions, res, nil
	}
	if hasAllAssets(user, asset) {
		return actions, NewKubernetesScopeAll(), nil
	}

	enforcer, err := newPermissionEnforcer(user, []*Asset{asset})
	if err != nil {
		return nil, nil, err
	}
	key := getAccountKey(asset, account)
	for _, action := range enforcer.GetAllActions() {
		var namespace string
		write := strings.HasPrefix(action, actionKubernetesWrite)
		if write {
			namespace = strings.TrimPrefix(action, actionKubernetesWrite)
		} else if strings.HasPrefix(action, actionKubernetesRead) {
			namespace = strings.TrimPrefix(action, actionKubernetesRead)
		} else {
			continue
		}
		if res.Namespaces[namespace] {
			continue
		}

		allowed, err := enforcer.Enforce("user:"+user.Name, "asset:"+asset.Name, action, clientIp, key)
		if err != nil {
			return nil, nil, err
		}
		if allowed {
			res.Namespaces[namespace] = write
		}
	}
	return actions, res, nil
}
