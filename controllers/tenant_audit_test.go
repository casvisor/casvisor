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

package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	beegocontext "github.com/beego/beego/context"
	beegosession "github.com/beego/beego/session"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/object"
)

const (
	tenantSignInError       = "please sign in first"
	tenantUnauthorizedError = "unauthorized"
	tenantOtherOrgError     = "you cannot access the resources of the organization: org-b"
	tenantInvalidIDError    = "invalid ID: expected owner/name"
)

func TestIsGlobalAdminUser(t *testing.T) {
	for _, tc := range []struct {
		name string
		user *casdoorsdk.User
		want bool
	}{
		{"nil user", nil, false},
		{"deleted built-in user", &casdoorsdk.User{Owner: "built-in", IsAdmin: true, IsDeleted: true}, false},
		{"forbidden built-in user", &casdoorsdk.User{Owner: "built-in", IsAdmin: true, IsForbidden: true}, false},
		{"empty owner", &casdoorsdk.User{IsAdmin: true}, false},
		{"org user", &casdoorsdk.User{Owner: "org-a"}, false},
		{"org admin", &casdoorsdk.User{Owner: "org-a", IsAdmin: true}, false},
		{"built-in user without org admin flag", &casdoorsdk.User{Owner: "built-in"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isGlobalAdminUser(tc.user); got != tc.want {
				t.Errorf("isGlobalAdminUser() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResolveTenantOwner(t *testing.T) {
	orgAdmin := &casdoorsdk.User{Owner: "org-a", IsAdmin: true}
	globalAdmin := &casdoorsdk.User{Owner: "built-in"}
	for _, tc := range []struct {
		name    string
		user    *casdoorsdk.User
		owner   string
		want    string
		wantErr string
	}{
		{"nil user", nil, "org-a", "", tenantSignInError},
		{"deleted admin", &casdoorsdk.User{Owner: "org-a", IsAdmin: true, IsDeleted: true}, "org-a", "", tenantUnauthorizedError},
		{"forbidden built-in admin", &casdoorsdk.User{Owner: "built-in", IsForbidden: true}, "", "", tenantUnauthorizedError},
		{"empty user owner", &casdoorsdk.User{IsAdmin: true}, "org-a", "", tenantUnauthorizedError},
		{"org user", &casdoorsdk.User{Owner: "org-a"}, "org-a", "", tenantUnauthorizedError},
		{"org admin with empty owner", orgAdmin, "", "org-a", ""},
		{"org admin with whitespace owner", orgAdmin, " \t ", "org-a", ""},
		{"org admin with own owner", orgAdmin, "org-a", "org-a", ""},
		{"org admin with padded own owner", orgAdmin, " org-a ", "org-a", ""},
		{"org admin with other owner", orgAdmin, " org-b ", "", tenantOtherOrgError},
		{"global admin with empty owner", globalAdmin, "", "", ""},
		{"global admin with other owner", globalAdmin, "org-b", "org-b", ""},
		{"global admin with own owner", globalAdmin, "built-in", "built-in", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner, err := resolveTenantOwner(tc.user, tc.owner)
			if owner != tc.want || tenantAuditErrorMessage(err) != tc.wantErr {
				t.Errorf("resolveTenantOwner(%q) = (%q, %v), want (%q, %q)", tc.owner, owner, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestResolveTenantId(t *testing.T) {
	orgAdmin := &casdoorsdk.User{Owner: "org-a", IsAdmin: true}
	globalAdmin := &casdoorsdk.User{Owner: "built-in"}
	for _, tc := range []struct {
		name      string
		user      *casdoorsdk.User
		id        string
		wantOwner string
		wantName  string
		wantErr   string
	}{
		{"nil user before parsing", nil, "bad-id", "", "", tenantSignInError},
		{"deleted admin", &casdoorsdk.User{Owner: "built-in", IsDeleted: true}, "org-a/item", "", "", tenantUnauthorizedError},
		{"forbidden admin", &casdoorsdk.User{Owner: "org-a", IsAdmin: true, IsForbidden: true}, "org-a/item", "", "", tenantUnauthorizedError},
		{"empty user owner", &casdoorsdk.User{IsAdmin: true}, "org-a/item", "", "", tenantUnauthorizedError},
		{"org user before parsing", &casdoorsdk.User{Owner: "org-a"}, "bad-id", "", "", tenantUnauthorizedError},
		{"no slash", orgAdmin, "bad-id", "", "", tenantInvalidIDError},
		{"missing owner", orgAdmin, "/item", "", "", tenantInvalidIDError},
		{"missing name", orgAdmin, "org-a/", "", "", tenantInvalidIDError},
		{"whitespace-only owner", orgAdmin, "   /item", "", "", tenantInvalidIDError},
		{"whitespace-only owner as global admin", globalAdmin, "   /item", "", "", tenantInvalidIDError},
		{"whitespace-only name", orgAdmin, "org-a/   ", "", "", tenantInvalidIDError},
		{"extra slash", orgAdmin, "org-a/item/extra", "", "", tenantInvalidIDError},
		{"empty ID", globalAdmin, "", "", "", tenantInvalidIDError},
		{"org admin with other org's ID", orgAdmin, "org-b/item", "", "", tenantOtherOrgError},
		{"org admin with own org's ID", orgAdmin, "org-a/item", "org-a", "item", ""},
		{"global admin with other org's ID", globalAdmin, "org-b/item", "org-b", "item", ""},
		{"global admin with own org's ID", globalAdmin, "built-in/item", "built-in", "item", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner, name, err := resolveTenantId(tc.user, tc.id)
			if owner != tc.wantOwner || name != tc.wantName || tenantAuditErrorMessage(err) != tc.wantErr {
				t.Errorf("resolveTenantId(%q) = (%q, %q, %v), want (%q, %q, %q)", tc.id, owner, name, err, tc.wantOwner, tc.wantName, tc.wantErr)
			}
		})
	}
}

func tenantAuditErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

type fakeSessionStore map[interface{}]interface{}

var _ beegosession.Store = fakeSessionStore(nil)

func (s fakeSessionStore) Set(key, value interface{}) error {
	s[key] = value
	return nil
}

func (s fakeSessionStore) Get(key interface{}) interface{} { return s[key] }

func (s fakeSessionStore) Delete(key interface{}) error {
	delete(s, key)
	return nil
}

func (s fakeSessionStore) SessionID() string { return "tenant-audit-test" }

func (s fakeSessionStore) SessionRelease(http.ResponseWriter) {}

func (s fakeSessionStore) Flush() error {
	clear(s)
	return nil
}

func newTenantAuditController(action, method, target, body string, user *casdoorsdk.User) (*ApiController, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	store := make(fakeSessionStore)
	if user != nil {
		store["user"] = casdoorsdk.Claims{User: *user}
	}
	ctx := beegocontext.NewContext()
	ctx.Reset(rec, req)
	ctx.Input.CruSession = store
	ctx.Input.RequestBody = []byte(body)
	c := &ApiController{}
	c.Init(ctx, "ApiController", action, c)
	c.CruSession = store
	return c, rec
}

func assertTenantAuditError(t *testing.T, rec *httptest.ResponseRecorder, wantMsg string) {
	t.Helper()
	var resp Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not JSON: %v, body = %q", err, rec.Body.String())
	}
	if resp.Status != "error" || resp.Msg != wantMsg {
		t.Errorf("response = (%q, %q), want (%q, %q)", resp.Status, resp.Msg, "error", wantMsg)
	}
}

func TestGetExportQueryTenantScope(t *testing.T) {
	orgAdmin := &casdoorsdk.User{Owner: "org-a", IsAdmin: true}
	globalAdmin := &casdoorsdk.User{Owner: "built-in"}
	for _, tc := range []struct {
		name    string
		user    *casdoorsdk.User
		owner   string
		want    string
		wantErr string
	}{
		{"org admin with no owner", orgAdmin, "", "org-a", ""},
		{"org admin with cross-org owner", orgAdmin, "org-b", "", tenantOtherOrgError},
		{"global admin with no owner", globalAdmin, "", "", ""},
		{"global admin with specific owner", globalAdmin, "org-b", "org-b", ""},
		{"unauthenticated", nil, "", "", tenantSignInError},
		{"non-admin", &casdoorsdk.User{Owner: "org-a"}, "org-a", "", tenantUnauthorizedError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := "/api/export-records?owner=" + url.QueryEscape(tc.owner)
			c, rec := newTenantAuditController("ExportRecords", http.MethodGet, target, "", tc.user)
			query, ok := c.getExportQuery()
			if tc.wantErr != "" {
				if query != nil || ok {
					t.Errorf("getExportQuery() = (%+v, %v), want (nil, false)", query, ok)
				}
				assertTenantAuditError(t, rec, tc.wantErr)
				return
			}
			if !ok || query == nil || query.owner != tc.want {
				t.Errorf("getExportQuery() = (%+v, %v), want owner %q and ok", query, ok, tc.want)
			}
		})
	}
}

func TestTenantAuditQueryHandlersRejectInvalidRequests(t *testing.T) {
	orgAdmin := &casdoorsdk.User{Owner: "org-a", IsAdmin: true}
	for _, endpoint := range []struct {
		action string
		path   string
		method string
		param  string
		call   func(*ApiController)
	}{
		{"GetRecords", "/api/get-records", http.MethodGet, "owner", (*ApiController).GetRecords},
		{"GetRecord", "/api/get-record", http.MethodGet, "id", (*ApiController).GetRecord},
		{"ExportSessions", "/api/export-sessions", http.MethodGet, "owner", (*ApiController).ExportSessions},
		{"ExportCommands", "/api/export-commands", http.MethodGet, "owner", (*ApiController).ExportCommands},
		{"ExportRecords", "/api/export-records", http.MethodGet, "owner", (*ApiController).ExportRecords},
		{"GetJobs", "/api/get-jobs", http.MethodGet, "owner", (*ApiController).GetJobs},
		{"GetJob", "/api/get-job", http.MethodGet, "id", (*ApiController).GetJob},
		{"RerunJob", "/api/rerun-job", http.MethodPost, "id", (*ApiController).RerunJob},
		{"CancelJob", "/api/cancel-job", http.MethodPost, "id", (*ApiController).CancelJob},
	} {
		t.Run(endpoint.action, func(t *testing.T) {
			check := func(t *testing.T, user *casdoorsdk.User, value, wantMsg string) {
				t.Helper()
				target := endpoint.path + "?" + endpoint.param + "=" + url.QueryEscape(value)
				c, rec := newTenantAuditController(endpoint.action, endpoint.method, target, "", user)
				endpoint.call(c)
				assertTenantAuditError(t, rec, wantMsg)
			}
			own, other := "org-a", "org-b"
			if endpoint.param == "id" {
				own, other = "org-a/item", "org-b/item"
				t.Run("malformed ID", func(t *testing.T) { check(t, orgAdmin, "bad-id", tenantInvalidIDError) })
			}
			t.Run("cross-org", func(t *testing.T) { check(t, orgAdmin, other, tenantOtherOrgError) })
			t.Run("unauthenticated", func(t *testing.T) { check(t, nil, own, tenantSignInError) })
			t.Run("non-admin", func(t *testing.T) {
				check(t, &casdoorsdk.User{Owner: "org-a"}, own, tenantUnauthorizedError)
			})
		})
	}
}

func TestAddRecordRejectsInvalidTenant(t *testing.T) {
	orgAdmin := &casdoorsdk.User{Owner: "org-a", IsAdmin: true}
	for _, tc := range []struct {
		name    string
		user    *casdoorsdk.User
		body    string
		wantErr string
	}{
		{"unauthenticated", nil, `{}`, tenantSignInError},
		{"unauthenticated malformed JSON", nil, `{`, tenantSignInError},
		{"non-admin", &casdoorsdk.User{Owner: "org-a"}, `{}`, tenantUnauthorizedError},
		{"cross-org owner", orgAdmin, `{"owner":"org-b"}`, tenantOtherOrgError},
		{"cross-org organization", orgAdmin, `{"organization":"org-b"}`, tenantOtherOrgError},
		{"cross-org organization with whitespace-only owner", orgAdmin, `{"owner":"   ","organization":"org-b"}`, tenantOtherOrgError},
		{"mismatched tenant", orgAdmin, `{"owner":"org-a","organization":"org-b"}`, "owner and organization must match"},
		{"mismatched tenant as global admin", &casdoorsdk.User{Owner: "built-in"}, `{"owner":"org-a","organization":"org-b"}`, "owner and organization must match"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := newTenantAuditController("AddRecord", http.MethodPost, "/api/add-record", tc.body, tc.user)
			c.AddRecord()
			assertTenantAuditError(t, rec, tc.wantErr)
		})
	}
}

func TestAuditMutationHandlersRejectGlobalAdmin(t *testing.T) {
	globalAdmin := &casdoorsdk.User{Owner: "built-in"}
	for _, endpoint := range []struct {
		action string
		path   string
		call   func(*ApiController)
	}{
		{"UpdateRecord", "/api/update-record", (*ApiController).UpdateRecord},
		{"DeleteRecord", "/api/delete-record", (*ApiController).DeleteRecord},
	} {
		t.Run(endpoint.action, func(t *testing.T) {
			c, rec := newTenantAuditController(endpoint.action, http.MethodPost, endpoint.path, `{`, globalAdmin)
			endpoint.call(c)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("HTTP status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
			}
			assertTenantAuditError(t, rec, object.ErrAuditRecordImmutable.Error())
		})
	}
}

func TestExportCsvWithoutSession(t *testing.T) {
	c, rec := newTenantAuditController("ExportRecords", http.MethodGet, "/api/export-records", "", nil)
	c.exportCsv("records", []string{"Name"}, func(write func([]string) error) error {
		return write([]string{"example"})
	})
	if rec.Code != http.StatusOK || rec.Body.String() != "\xEF\xBB\xBFName\nexample\n" {
		t.Errorf("unexpected CSV response: HTTP %d, body = %q", rec.Code, rec.Body.String())
	}
}
