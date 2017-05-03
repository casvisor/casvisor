package main

import (
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/plugins/auth"
	"github.com/hsluoyz/beego-authz/authz"
)

func main() {
	// authenticate every request.
	beego.InsertFilter("*", beego.BeforeRouter, auth.Basic("alice", "123"))

	// authorize every request.
	beego.InsertFilter("*", beego.BeforeRouter, authz.NewBasicAuthorizer("authz_model.conf", "authz_policy.csv"))

	//beego.Router("*", &TestController{})
	beego.Run()
}
