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
