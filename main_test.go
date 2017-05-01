package main

import (
	"github.com/hsluoyz/casbin/api"
	"testing"
)

func testEnforce(t *testing.T, e *api.Enforcer, sub string, obj string, act string, res bool) {
	if e.Enforce(sub, obj, act) != res {
		t.Errorf("%s, %s, %s: %t, supposed to be %t", sub, obj, act, !res, res)
	}
}

func TestAuthzModel(t *testing.T) {
	e := &api.Enforcer{}
	e.InitWithFile("authz_model.conf", "authz_policy.csv")

	testEnforce(t, e, "alice", "/dataset1/resource1", "GET", true)
	testEnforce(t, e, "alice", "/dataset1/resource1", "POST", true)
	testEnforce(t, e, "alice", "/dataset1/resource2", "GET", true)
	testEnforce(t, e, "alice", "/dataset1/resource2", "POST", false)
	testEnforce(t, e, "bob", "/dataset2/resource1", "GET", false)
	testEnforce(t, e, "bob", "/dataset2/resource1", "POST", true)
	testEnforce(t, e, "bob", "/dataset2/resource2", "GET", true)
	testEnforce(t, e, "bob", "/dataset2/resource2", "POST", true)
}
