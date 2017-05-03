package main

import (
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/plugins/auth"
	"github.com/hsluoyz/beego-authz/authz"
	"net/http"
	"net/http/httptest"
	"testing"
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
	beego.InsertFilter("*", beego.BeforeRouter, authz.NewBasicAuthorizer("authz_model.conf", "authz_policy.csv"))
	beego.Router("*", &TestController{})

	testRequest(t, "alice", "/dataset1/resource1", "GET", 200)
	testRequest(t, "alice", "/dataset1/resource1", "POST", 200)
	testRequest(t, "alice", "/dataset1/resource2", "GET", 200)
	testRequest(t, "alice", "/dataset1/resource2", "POST", 403)
}
