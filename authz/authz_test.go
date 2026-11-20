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

package authz

import (
	"net/http"
	"testing"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	stringadapter "github.com/qiangmzsx/string-adapter/v2"
)

func TestIsAllowedAuthenticated(t *testing.T) {
	t.Setenv("IsDemoMode", "false")

	globalAdmin := &casdoorsdk.User{Owner: "built-in", Name: "root"}
	orgAdmin := &casdoorsdk.User{Owner: "org1", Name: "admin", IsAdmin: true}
	regular := &casdoorsdk.User{Owner: "org1", Name: "alice"}
	apis := []struct {
		method    string
		path      string
		adminOnly bool
	}{
		{http.MethodGet, "/api/get-assets", false},
		{http.MethodPost, "/api/start-session", false},
		{http.MethodGet, "/api/get-permissions", true},
		{http.MethodPost, "/api/delete-asset", true},
	}
	tests := []struct {
		name      string
		user      *casdoorsdk.User
		objOwner  string
		wantUser  bool
		wantAdmin bool
	}{
		{"global admin own org", globalAdmin, "built-in", true, true},
		{"global admin other org", globalAdmin, "org2", true, true},
		{"org admin own org", orgAdmin, "org1", true, true},
		{"org admin unscoped", orgAdmin, "", true, true},
		{"org admin other org", orgAdmin, "org2", false, false},
		{"regular user own org", regular, "org1", true, false},
		{"regular user unscoped", regular, "", true, false},
		{"regular user other org", regular, "org2", false, false},
		{"deleted global admin", &casdoorsdk.User{Owner: "built-in", Name: "root", IsDeleted: true}, "org2", false, false},
		{"deleted org admin", &casdoorsdk.User{Owner: "org1", Name: "admin", IsAdmin: true, IsDeleted: true}, "org1", false, false},
		{"deleted regular user", &casdoorsdk.User{Owner: "org1", Name: "alice", IsDeleted: true}, "org1", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, api := range apis {
				want := tt.wantUser
				if api.adminOnly {
					want = tt.wantAdmin
				}
				if got := IsAllowed(tt.user, tt.user.Owner, tt.user.Name, api.method, api.path, tt.objOwner, "resource"); got != want {
					t.Errorf("IsAllowed for %s %s in %q = %t, want %t", api.method, api.path, tt.objOwner, got, want)
				}
			}
		})
	}
}

func TestIsAllowedApp(t *testing.T) {
	t.Setenv("IsDemoMode", "false")

	tests := []struct {
		name string
		user *casdoorsdk.User
	}{
		{"without user", nil},
		{"with regular user from another org", &casdoorsdk.User{Owner: "org1", Name: "alice"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !IsAllowed(tt.user, "app", "service", http.MethodPost, "/api/add-permission", "org2", "resource") {
				t.Error("app subject was denied a management API in another org")
			}
		})
	}
}

func TestIsAllowedDemoMode(t *testing.T) {
	t.Setenv("IsDemoMode", "true")

	globalAdmin := &casdoorsdk.User{Owner: "built-in", Name: "root"}
	regular := &casdoorsdk.User{Owner: "org1", Name: "alice"}
	gets := []struct {
		name     string
		user     *casdoorsdk.User
		path     string
		objOwner string
		want     bool
	}{
		{"global admin management GET", globalAdmin, "/api/get-permissions", "org2", true},
		{"regular user own org GET", regular, "/api/get-assets", "org1", true},
		{"regular user other org GET", regular, "/api/get-assets", "org2", false},
		{"regular user management GET", regular, "/api/get-permissions", "org1", false},
	}
	for _, tt := range gets {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAllowed(tt.user, tt.user.Owner, tt.user.Name, http.MethodGet, tt.path, tt.objOwner, "resource"); got != tt.want {
				t.Errorf("IsAllowed for GET %s in %q = %t, want %t", tt.path, tt.objOwner, got, tt.want)
			}
		})
	}

	posts := []struct {
		path string
		want bool
	}{
		{"/api/signin", true},
		{"/api/signin-oauth", true},
		{"/api/signout", true},
		{"/api/add-asset-tunnel", true},
		{"/api/start-session", true},
		{"/api/stop-session", true},
		{"/api/delete-asset", false},
		{"/api/update-asset", false},
		{"/api/add-permission", false},
	}
	for _, tt := range posts {
		t.Run("POST"+tt.path, func(t *testing.T) {
			if got := IsAllowed(globalAdmin, globalAdmin.Owner, globalAdmin.Name, http.MethodPost, tt.path, "org2", "resource"); got != tt.want {
				t.Errorf("IsAllowed for global admin on POST %s = %t, want %t", tt.path, got, tt.want)
			}
			if got := IsAllowed(nil, "app", "service", http.MethodPost, tt.path, "org2", "resource"); got != tt.want {
				t.Errorf("IsAllowed for app subject on POST %s = %t, want %t", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsAllowedAnonymous(t *testing.T) {
	t.Setenv("IsDemoMode", "false")

	m, err := model.NewModelFromString(authzModelText)
	if err != nil {
		t.Fatal(err)
	}
	enforcer, err := casbin.NewEnforcer(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := stringadapter.NewAdapter(defaultPolicyText).LoadPolicy(enforcer.GetModel()); err != nil {
		t.Fatal(err)
	}
	previous := Enforcer
	Enforcer = enforcer
	t.Cleanup(func() { Enforcer = previous })

	tests := []struct {
		name     string
		subOwner string
		method   string
		path     string
		want     bool
	}{
		{"anonymous signin", "", http.MethodPost, "/api/signin", true},
		{"anonymous signout", "", http.MethodPost, "/api/signout", true},
		{"anonymous account", "", http.MethodGet, "/api/get-account", true},
		{"anonymous agent", "", http.MethodGet, "/api/connect-agent", true},
		{"anonymous signin with wrong method", "", http.MethodGet, "/api/signin", false},
		{"anonymous asset list", "", http.MethodGet, "/api/get-assets", false},
		{"anonymous create asset", "", http.MethodPost, "/api/add-asset", false},
		{"built-in subject", "built-in", http.MethodPost, "/api/add-asset", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAllowed(nil, tt.subOwner, "", tt.method, tt.path, "", ""); got != tt.want {
				t.Errorf("IsAllowed for subject owner %q on %s %s = %t, want %t", tt.subOwner, tt.method, tt.path, got, tt.want)
			}
		})
	}
}
