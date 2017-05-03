// Copyright 2014 beego Author. All Rights Reserved.
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
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/plugins/auth"
	"net/http"
	"net/http/httptest"
	"testing"
	"github.com/hsluoyz/casbin/api"
)

const (
	PermitString = "This is the content of the page."
)

type TestController struct {
	beego.Controller
}

func (c *TestController) Get() {
	c.Ctx.WriteString(PermitString)
}

func (c *TestController) Post() {
	c.Ctx.WriteString(PermitString)
}

func (c *TestController) Delete() {
	c.Ctx.WriteString(PermitString)
}

func (c *TestController) Put() {
	c.Ctx.WriteString(PermitString)
}

func testRequest(t *testing.T, user string, path string, method string, code int) {
	r, _ := http.NewRequest(method, path, nil)
	r.SetBasicAuth(user, "123")
	w := httptest.NewRecorder()
	beego.BeeApp.Handlers.ServeHTTP(w, r)

	if w.Code != code {
		t.Errorf("%s, %s, %s: %d, supposed to be %d", user, path, method, w.Code, code)
	}
}

func TestAuthorizer(t *testing.T) {
	beego.InsertFilter("*", beego.BeforeRouter, auth.Basic("alice", "123"))
	beego.InsertFilter("*", beego.BeforeRouter, NewAuthorizer(api.NewEnforcer("authz_model.conf", "authz_policy.csv")))
	beego.Router("*", &TestController{})

	testRequest(t, "alice", "/dataset1/resource1", "GET", 200)
	testRequest(t, "alice", "/dataset1/resource1", "POST", 200)
	testRequest(t, "alice", "/dataset1/resource2", "GET", 200)
	testRequest(t, "alice", "/dataset1/resource2", "POST", 403)
}

func TestWildcard(t *testing.T) {
	beego.InsertFilter("*", beego.BeforeRouter, auth.Basic("bob", "123"))
	beego.InsertFilter("*", beego.BeforeRouter, NewAuthorizer(api.NewEnforcer("authz_model.conf", "authz_policy.csv")))
	beego.Router("*", &TestController{})

	testRequest(t, "bob", "/dataset2/resource1", "GET", 200)
	testRequest(t, "bob", "/dataset2/resource1", "POST", 200)
	testRequest(t, "bob", "/dataset2/resource1", "DELETE", 200)
	testRequest(t, "bob", "/dataset2/resource2", "GET", 200)
	testRequest(t, "bob", "/dataset2/resource2", "POST", 403)
	testRequest(t, "bob", "/dataset2/resource2", "DELETE", 403)

	testRequest(t, "bob", "/dataset2/folder1/item1", "GET", 403)
	testRequest(t, "bob", "/dataset2/folder1/item1", "POST", 200)
	testRequest(t, "bob", "/dataset2/folder1/item1", "DELETE", 403)
	testRequest(t, "bob", "/dataset2/folder1/item2", "GET", 403)
	testRequest(t, "bob", "/dataset2/folder1/item2", "POST", 200)
	testRequest(t, "bob", "/dataset2/folder1/item2", "DELETE", 403)
}
