package main

import (
	"github.com/astaxie/beego"
	"github.com/hsluoyz/beego-authz/authn"
	"github.com/hsluoyz/beego-authz/authz"
)

const (
	PermitString = "This is the content of the page."
)

type Controller struct {
	beego.Controller
}

func (c *Controller) Get() {
	c.Ctx.WriteString(PermitString)
}

func (c *Controller) Post() {
	c.Ctx.WriteString(PermitString)
}

func (c *Controller) Delete() {
	c.Ctx.WriteString(PermitString)
}

func (c *Controller) Put() {
	c.Ctx.WriteString(PermitString)
}

func main() {
	// authenticate every request.
	beego.InsertFilter("*", beego.BeforeRouter, authn.NewAuthenticator("alice:123", "bob:123"))

	// authorize every request.
	beego.InsertFilter("*", beego.BeforeRouter, authz.NewBasicAuthorizer())

	beego.Router("*", &Controller{})
	beego.Run()
}
