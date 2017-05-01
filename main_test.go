package main

import (
	"testing"
	"net/http"
	"net/http/httptest"
	"github.com/astaxie/beego"
	"github.com/hsluoyz/beeauthz/authn"
	"github.com/hsluoyz/beeauthz/authz"
)

func testEnforce(t *testing.T, user string, path string, method string, code int) {
	r, _ := http.NewRequest(method, path, nil)
	r.SetBasicAuth(user,"123")
	w := httptest.NewRecorder()
	beego.BeeApp.Handlers.ServeHTTP(w, r)

	if w.Code != code {
		t.Errorf("%s, %s, %s: %d, supposed to be %d", user, path, method, w.Code, code)
	}
}

func TestAuthzModel(t *testing.T) {
	beego.InsertFilter("*", beego.BeforeRouter,authn.NewAuthenticator("alice:123","bob:123"))
	beego.InsertFilter("*", beego.BeforeRouter, authz.NewBasicAuthorizer())
	beego.Router("*", &Controller{})

	testEnforce(t, "alice", "/dataset1/resource1", "GET", 200)
	testEnforce(t, "alice", "/dataset1/resource1", "POST", 200)
	testEnforce(t, "alice", "/dataset1/resource2", "GET", 200)
	testEnforce(t, "alice", "/dataset1/resource2", "POST", 403)
	testEnforce(t, "bob", "/dataset2/resource1", "GET", 403)
	testEnforce(t, "bob", "/dataset2/resource1", "POST", 200)
	testEnforce(t, "bob", "/dataset2/resource2", "GET", 200)
	testEnforce(t, "bob", "/dataset2/resource2", "POST", 200)
}
