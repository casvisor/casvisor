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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"xorm.io/xorm"
)

func TestIsAdminUser(t *testing.T) {
	tests := []struct {
		name string
		user *casdoorsdk.User
		want bool
	}{
		{"nil user", nil, false},
		{"built-in without admin flag", &casdoorsdk.User{Owner: "built-in"}, true},
		{"built-in with admin flag", &casdoorsdk.User{Owner: "built-in", IsAdmin: true}, true},
		{"organization admin", &casdoorsdk.User{Owner: "org1", IsAdmin: true}, true},
		{"regular user", &casdoorsdk.User{Owner: "org1"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAdminUser(tt.user); got != tt.want {
				t.Errorf("IsAdminUser = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestIPAllowedFunc(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		pattern string
		want    bool
	}{
		{"wildcard", "192.0.2.10", "*", true},
		{"wildcard with missing IP", "", "*", true},
		{"IPv4 exact match", "10.2.3.4", "10.2.3.4", true},
		{"IPv4 exact mismatch", "10.2.3.5", "10.2.3.4", false},
		{"IPv4 CIDR match", "10.2.3.4", "10.0.0.0/8", true},
		{"IPv4 CIDR mismatch", "192.0.2.10", "10.0.0.0/8", false},
		{"IPv6 exact match", "2001:db8::1", "2001:0db8:0:0:0:0:0:1", true},
		{"IPv6 exact mismatch", "2001:db8::2", "2001:db8::1", false},
		{"IPv6 CIDR match", "2001:db8:1::3", "2001:db8:1::/64", true},
		{"IPv6 CIDR mismatch", "2001:db8:2::3", "2001:db8:1::/64", false},
		{"mapped IPv4 exact match", "::ffff:10.2.3.4", "10.2.3.4", true},
		{"mapped IPv4 CIDR match", "::ffff:10.2.3.4", "10.0.0.0/8", true},
		{"missing client IP", "", "10.0.0.0/8", false},
		{"invalid client IP", "not-an-ip", "10.2.3.4", false},
		{"invalid pattern", "10.2.3.4", "bad", false},
		{"invalid CIDR", "10.2.3.4", "10.0.0.0/99", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ipAllowedFunc(tt.ip, tt.pattern)
			if err != nil || got != tt.want {
				t.Errorf("ipAllowedFunc(%q, %q) = (%v, %v), want (%t, nil)", tt.ip, tt.pattern, got, err, tt.want)
			}
		})
	}
}

func TestAccountAllowedFunc(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		pattern string
		subject string
		want    bool
	}{
		{"wildcard default", "default:root", "*", "user:alice", true},
		{"wildcard named", "account:deploy", "*", "user:alice", true},
		{"default account", "default:root", AccountDefault, "user:alice", true},
		{"default rejects named account", "account:root", AccountDefault, "user:alice", false},
		{"same as user", "account:alice", AccountSame, "user:alice", true},
		{"default same as user", "default:alice", AccountSame, "user:alice", true},
		{"different from user", "account:bob", AccountSame, "user:alice", false},
		{"empty username cannot match empty user", "default:", AccountSame, "user:", false},
		{"named account", "account:deploy", "deploy", "user:alice", true},
		{"named default", "default:deploy", "deploy", "user:alice", true},
		{"wrong account name", "account:root", "deploy", "user:alice", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := accountAllowedFunc(tt.key, tt.pattern, tt.subject)
			if err != nil || got != tt.want {
				t.Errorf("accountAllowedFunc(%q, %q, %q) = (%v, %v), want (%t, nil)", tt.key, tt.pattern, tt.subject, got, err, tt.want)
			}
		})
	}
}

func TestGetSessionOperations(t *testing.T) {
	tests := []struct {
		name    string
		actions []string
		want    []string
	}{
		{"empty", nil, nil},
		{"connect and forward only", []string{ActionConnect, ActionForward}, nil},
		{"copy", []string{ActionCopy}, []string{"copy"}},
		{"paste", []string{ActionPaste}, []string{"paste"}},
		{"upload", []string{ActionUpload}, []string{"upload", "createDir", "edit", "rename", "delete", "fileSystem"}},
		{"download", []string{ActionDownload}, []string{"download", "fileSystem"}},
		{"upload and download", []string{ActionUpload, ActionDownload}, []string{"upload", "createDir", "edit", "rename", "delete", "download", "fileSystem"}},
		{"all actions", AllActions, []string{"copy", "paste", "upload", "createDir", "edit", "rename", "delete", "download", "fileSystem"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetSessionOperations(tt.actions); !slices.Equal(got, tt.want) {
				t.Errorf("GetSessionOperations(%v) = %v, want %v", tt.actions, got, tt.want)
			}
		})
	}
}

func TestPermissionCheck(t *testing.T) {
	valid := Permission{
		Actions:   AllActions,
		Accounts:  []string{AccountDefault, AccountSame, "*", "deploy"},
		ClientIps: []string{"*", "192.0.2.10", "10.0.0.0/8", "2001:db8::/32"},
		StartTime: "2026-01-01T00:00:00Z",
		EndTime:   "2026-12-31T23:59:59Z",
	}
	tests := []struct {
		name       string
		permission Permission
		wantError  string
	}{
		{"valid permission", valid, ""},
		{"invalid action", Permission{Actions: []string{"Unknown"}}, "invalid action:"},
		{"invalid account", Permission{Accounts: []string{"account:deploy"}}, "invalid account:"},
		{"empty account", Permission{Accounts: []string{""}}, "invalid account:"},
		{"invalid client IP", Permission{ClientIps: []string{"not-an-ip"}}, "invalid client IP or CIDR:"},
		{"invalid client CIDR", Permission{ClientIps: []string{"10.0.0.0/99"}}, "invalid client IP or CIDR:"},
		{"invalid start time", Permission{StartTime: "2026-01-01"}, "invalid time:"},
		{"invalid end time", Permission{EndTime: "2026-12-31"}, "invalid time:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.permission.check()
			if tt.wantError == "" {
				if err != nil {
					t.Errorf("Permission.check = %v, want nil", err)
				}
			} else if err == nil || !strings.HasPrefix(err.Error(), tt.wantError) {
				t.Errorf("Permission.check = %v, want an error starting with %q", err, tt.wantError)
			}
		})
	}
}

func newPermissionTestDB(t *testing.T) *xorm.Engine {
	t.Helper()
	engine, err := xorm.NewEngine("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open permission test database: %v", err)
	}
	engine.SetMaxOpenConns(1)
	previous := adapter
	t.Cleanup(func() {
		adapter = previous
		if err := engine.Close(); err != nil {
			t.Errorf("close permission test database: %v", err)
		}
	})
	if err := engine.Sync2(new(Permission), new(AssetAccount)); err != nil {
		t.Fatalf("create permission test tables: %v", err)
	}
	adapter = &Adapter{driverName: "sqlite", dataSourceName: ":memory:", engine: engine}
	return engine
}

func insertPermissionTestRows(t *testing.T, engine *xorm.Engine, rows ...any) {
	t.Helper()
	for _, row := range rows {
		if _, err := engine.Insert(row); err != nil {
			t.Fatalf("insert %T: %v", row, err)
		}
	}
}

func checkAssetAccess(t *testing.T, user *casdoorsdk.User, asset *Asset, clientIP string, wantActions []string, wantAccounts []AccountInfo) {
	t.Helper()
	actions, err := GetAssetActions(user, asset, clientIP)
	if err != nil {
		t.Fatalf("GetAssetActions: %v", err)
	}
	if !slices.Equal(actions, wantActions) {
		t.Errorf("GetAssetActions = %v, want %v", actions, wantActions)
	}

	accounts, err := GetAllowedAccounts(user, asset, clientIP)
	if err != nil {
		t.Fatalf("GetAllowedAccounts: %v", err)
	}
	gotAccounts := make([]AccountInfo, len(accounts))
	for i, account := range accounts {
		if account == nil {
			t.Fatalf("GetAllowedAccounts returned a nil account at index %d", i)
		}
		gotAccounts[i] = *account
	}
	if !slices.Equal(gotAccounts, wantAccounts) {
		t.Errorf("GetAllowedAccounts = %+v, want %+v", gotAccounts, wantAccounts)
	}
}

func checkAssetAccountActions(t *testing.T, user *casdoorsdk.User, asset *Asset, username, clientIP string, want []string) {
	t.Helper()
	got, err := GetAssetAccountActions(user, asset, &username, clientIP)
	if err != nil {
		t.Fatalf("GetAssetAccountActions for %q: %v", username, err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("GetAssetAccountActions for %q = %v, want %v", username, got, want)
	}
}

func TestAssetAccessForAdmins(t *testing.T) {
	engine := newPermissionTestDB(t)
	asset := &Asset{Owner: "org1", Name: "host", Username: "root"}
	insertPermissionTestRows(t, engine,
		&AssetAccount{Owner: "org1", Name: "deploy", Asset: "host", Username: "deploy", DisplayName: "Deploy", IsEnabled: true},
		&AssetAccount{Owner: "org1", Name: "retired", Asset: "host", Username: "retired", IsEnabled: false},
	)
	orgAdmin := &casdoorsdk.User{Owner: "org1", Name: "admin", IsAdmin: true}
	allowed := []AccountInfo{{Username: "root", IsDefault: true}, {Username: "deploy", DisplayName: "Deploy"}}
	tests := []struct {
		name         string
		user         *casdoorsdk.User
		asset        *Asset
		wantActions  []string
		wantAccounts []AccountInfo
	}{
		{"global admin", &casdoorsdk.User{Owner: "built-in", Name: "root"}, asset, AllActions, allowed},
		{"same-org admin", orgAdmin, asset, AllActions, allowed},
		{"cross-org admin", &casdoorsdk.User{Owner: "org2", Name: "admin", IsAdmin: true}, asset, nil, nil},
		{"nil user", nil, asset, nil, nil},
		{"nil asset", orgAdmin, nil, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkAssetAccess(t, tt.user, tt.asset, "", tt.wantActions, tt.wantAccounts)
			checkAssetAccountActions(t, tt.user, tt.asset, "deploy", "", tt.wantActions)
		})
	}
}

func TestAssetAccessForRegularUsers(t *testing.T) {
	engine := newPermissionTestDB(t)
	asset := &Asset{Owner: "org1", Name: "host", Username: "root"}
	alice := &casdoorsdk.User{Owner: "org1", Name: "alice"}
	operator := &casdoorsdk.User{Owner: "org1", Name: "operator"}
	viewer := &casdoorsdk.User{Owner: "org1", Name: "viewer"}
	const (
		exactIP       = "10.20.30.40"
		otherIPInCIDR = "10.20.30.41"
	)

	insertPermissionTestRows(t, engine,
		&AssetAccount{Owner: "org1", Name: "personal", Asset: "host", Username: "alice", DisplayName: "Personal", IsEnabled: true},
		&AssetAccount{Owner: "org1", Name: "deploy", Asset: "host", Username: "deploy", DisplayName: "Deploy", IsEnabled: true},
		&AssetAccount{Owner: "org1", Name: "retired", Asset: "host", Username: "retired", IsEnabled: false},
	)
	for _, permission := range []*Permission{
		{Name: "default", Users: []string{"alice"}, Actions: []string{ActionDownload, ActionConnect}},
		{
			Name:      "same-user",
			Users:     []string{"alice"},
			Accounts:  []string{AccountSame},
			Actions:   []string{ActionCopy, ActionConnect},
			ClientIps: []string{"10.0.0.0/8"},
		},
		{
			Name:      "named",
			Users:     []string{"alice"},
			Accounts:  []string{"deploy"},
			Actions:   []string{ActionForward, ActionConnect},
			ClientIps: []string{exactIP},
		},
		{
			Name:      "named-paste",
			Users:     []string{"alice"},
			Accounts:  []string{"deploy"},
			Actions:   []string{ActionPaste},
			ClientIps: []string{exactIP},
		},
		{Name: "any-account", Users: []string{"operator"}, Accounts: []string{"*"}, Actions: []string{ActionConnect}},
		{Name: "no-connect", Users: []string{"viewer"}, Actions: []string{ActionCopy, ActionDownload}},
	} {
		permission.Owner = asset.Owner
		permission.Assets = []string{asset.Name}
		permission.IsEnabled = true
		insertPermissionTestRows(t, engine, permission)
	}

	root := AccountInfo{Username: "root", IsDefault: true}
	personal := AccountInfo{Username: "alice", DisplayName: "Personal"}
	deploy := AccountInfo{Username: "deploy", DisplayName: "Deploy"}
	defaultActions := []string{ActionConnect, ActionDownload}
	tests := []struct {
		name         string
		user         *casdoorsdk.User
		clientIP     string
		wantActions  []string
		wantAccounts []AccountInfo
	}{
		{"exact IP and CIDR merge actions", alice, exactIP, []string{ActionConnect, ActionCopy, ActionPaste, ActionDownload, ActionForward}, []AccountInfo{root, personal, deploy}},
		{"CIDR only", alice, otherIPInCIDR, []string{ActionConnect, ActionCopy, ActionDownload}, []AccountInfo{root, personal}},
		{"both IP restrictions miss", alice, "192.0.2.10", defaultActions, []AccountInfo{root}},
		{"empty IP and account constraints default to wildcard IP and default account", alice, "", defaultActions, []AccountInfo{root}},
		{"account wildcard excludes disabled accounts", operator, "", []string{ActionConnect}, []AccountInfo{root, personal, deploy}},
		{"actions without connect", viewer, exactIP, nil, nil},
		{"no matching user", &casdoorsdk.User{Owner: "org1", Name: "bob"}, exactIP, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkAssetAccess(t, tt.user, asset, tt.clientIP, tt.wantActions, tt.wantAccounts)
		})
	}

	accountTests := []struct {
		name     string
		user     *casdoorsdk.User
		username string
		clientIP string
		want     []string
	}{
		{"default username", alice, "root", exactIP, defaultActions},
		{"empty username selects default", alice, "", exactIP, defaultActions},
		{"matching username in CIDR", alice, "alice", otherIPInCIDR, []string{ActionConnect, ActionCopy}},
		{"matching username outside CIDR", alice, "alice", "192.0.2.10", nil},
		{"named account at exact IP", alice, "deploy", exactIP, []string{ActionConnect, ActionPaste, ActionForward}},
		{"named account at wrong IP", alice, "deploy", otherIPInCIDR, nil},
		{"unknown account despite wildcard", operator, "unknown", exactIP, nil},
		{"disabled account despite wildcard", operator, "retired", exactIP, nil},
	}
	for _, tt := range accountTests {
		t.Run(tt.name, func(t *testing.T) {
			checkAssetAccountActions(t, tt.user, asset, tt.username, tt.clientIP, tt.want)
		})
	}
}

func TestAssetAccessPermissionWindows(t *testing.T) {
	engine := newPermissionTestDB(t)
	user := &casdoorsdk.User{Owner: "org1", Name: "alice"}
	now := time.Now().UTC()
	before := now.Add(-time.Hour).Format(time.RFC3339)
	after := now.Add(time.Hour).Format(time.RFC3339)
	tests := []struct {
		name      string
		isEnabled bool
		startTime string
		endTime   string
		active    bool
	}{
		{"active window", true, before, after, true},
		{"disabled in active window", false, before, after, false},
		{"future start", true, after, "", false},
		{"expired end", true, "", before, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			asset := &Asset{Owner: "org1", Name: tt.name, Username: "root"}
			insertPermissionTestRows(t, engine, &Permission{
				Owner:     asset.Owner,
				Name:      tt.name,
				Users:     []string{user.Name},
				Assets:    []string{asset.Name},
				Actions:   []string{ActionConnect},
				StartTime: tt.startTime,
				EndTime:   tt.endTime,
				IsEnabled: tt.isEnabled,
			})
			if !tt.active {
				checkAssetAccess(t, user, asset, "", nil, nil)
				return
			}
			checkAssetAccess(t, user, asset, "", []string{ActionConnect}, []AccountInfo{{Username: "root", IsDefault: true}})
		})
	}
}

func TestFilterAllowedAssets(t *testing.T) {
	engine := newPermissionTestDB(t)
	tagged := &Asset{Owner: "org1", Name: "tagged", Tag: "prod", Username: "root"}
	disabled := &Asset{Owner: "org1", Name: "disabled-account", Tag: "prod", Username: "root"}
	wrongTag := &Asset{Owner: "org1", Name: "wrong-tag", Tag: "dev", Username: "root"}
	gateway := &Asset{Owner: "org1", Name: "gateway", Username: "root"}
	copyOnly := &Asset{Owner: "org1", Name: "copy-only", Username: "root"}
	foreign := &Asset{Owner: "org2", Name: "tagged", Tag: "prod", Username: "root"}
	allAssets := []*Asset{foreign, disabled, tagged, wrongTag, gateway, copyOnly}
	const exactIP = "192.0.2.40"

	for _, asset := range []*Asset{tagged, disabled, wrongTag} {
		insertPermissionTestRows(t, engine, &AssetAccount{
			Owner:     asset.Owner,
			Name:      asset.Name + "-deploy",
			Asset:     asset.Name,
			Username:  "deploy",
			IsEnabled: asset != disabled,
		})
	}
	insertPermissionTestRows(t, engine,
		&Permission{
			Owner:     "org1",
			Name:      "tagged-hosts",
			Groups:    []string{"org1/sre"},
			AssetTags: []string{"prod"},
			Accounts:  []string{"deploy"},
			Actions:   []string{ActionConnect},
			ClientIps: []string{"10.0.0.0/8"},
			IsEnabled: true,
		},
		&Permission{
			Owner:     "org1",
			Name:      "gateway",
			Groups:    []string{"sre"},
			Assets:    []string{gateway.Name},
			Actions:   []string{ActionConnect},
			ClientIps: []string{exactIP},
			IsEnabled: true,
		},
		&Permission{
			Owner:     "org1",
			Name:      "no-connect",
			Groups:    []string{"sre"},
			Assets:    []string{copyOnly.Name},
			Actions:   []string{ActionCopy, ActionDownload},
			IsEnabled: true,
		},
		&Permission{
			Owner:     "org1",
			Name:      "all-assets",
			Users:     []string{"operator"},
			Assets:    []string{"*"},
			Actions:   []string{ActionConnect},
			IsEnabled: true,
		},
		&Permission{
			Owner:     "org2",
			Name:      "tagged-host",
			Groups:    []string{"sre"},
			Assets:    []string{foreign.Name},
			Actions:   []string{ActionConnect},
			IsEnabled: true,
		},
	)

	qualifiedSRE := &casdoorsdk.User{Owner: "org1", Name: "alice", Groups: []string{"org1/sre"}}
	shortSRE := &casdoorsdk.User{Owner: "org1", Name: "alice", Groups: []string{"sre"}}
	otherGroup := &casdoorsdk.User{Owner: "org1", Name: "alice", Groups: []string{"org1/ops"}}
	foreignSRE := &casdoorsdk.User{Owner: "org2", Name: "alice", Groups: []string{"org2/sre"}}
	operator := &casdoorsdk.User{Owner: "org1", Name: "operator"}
	tests := []struct {
		name     string
		user     *casdoorsdk.User
		assets   []*Asset
		clientIP string
		wantIDs  []string
	}{
		{"qualified group within CIDR", qualifiedSRE, allAssets, "10.2.3.4", []string{"org1/tagged"}},
		{"short group within CIDR", shortSRE, allAssets, "10.2.3.4", []string{"org1/tagged"}},
		{"qualified group at exact IP", qualifiedSRE, allAssets, exactIP, []string{"org1/gateway"}},
		{"nonmatching client IP", shortSRE, allAssets, "192.0.2.41", nil},
		{"nonmatching group", otherGroup, allAssets, "10.2.3.4", nil},
		{"wildcard applies only to own org", operator, allAssets, "", []string{"org1/disabled-account", "org1/tagged", "org1/wrong-tag", "org1/gateway", "org1/copy-only"}},
		{"other org has a separate grant", foreignSRE, allAssets, "10.2.3.4", []string{"org2/tagged"}},
		{"nil user", nil, allAssets, "10.2.3.4", nil},
		{"empty asset slice", qualifiedSRE, []*Asset{}, "10.2.3.4", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assets, err := FilterAllowedAssets(tt.user, tt.assets, tt.clientIP)
			if err != nil {
				t.Fatalf("FilterAllowedAssets: %v", err)
			}
			gotIDs := make([]string, 0, len(assets))
			for _, asset := range assets {
				gotIDs = append(gotIDs, asset.Owner+"/"+asset.Name)
			}
			if !slices.Equal(gotIDs, tt.wantIDs) {
				t.Errorf("FilterAllowedAssets = %v, want %v", gotIDs, tt.wantIDs)
			}
		})
	}
}
