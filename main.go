package main

import (
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/context"
	"github.com/astaxie/beego/plugins/auth"
	"strings"
	"net/http"
	"encoding/base64"
)


type Controller struct {
	beego.Controller
}

func (c *Controller) Get() {
	c.Ctx.WriteString("This is the content of the page.")
}

func (c *Controller) Post() {
	c.Ctx.WriteString("This is the content of the page.")
}

func (c *Controller) Delete() {
	c.Ctx.WriteString("This is the content of the page.")
}

func (c *Controller) Put() {
	c.Ctx.WriteString("This is the content of the page.")
}

func getUserName(r *http.Request) string {
	s := strings.SplitN(r.Header.Get("Authorization"), " ", 2)
	if len(s) != 2 || s[0] != "Basic" {
		return ""
	}

	b, err := base64.StdEncoding.DecodeString(s[1])
	if err != nil {
		return ""
	}
	pair := strings.SplitN(string(b), ":", 2)
	if len(pair) != 2 {
		return ""
	}

	return pair[0]
}

func main() {
	var HasPermission = func(ctx *context.Context) {
		user := getUserName(ctx.Request)
		method := ctx.Request.Method
		path := ctx.Request.RequestURI
		ctx.WriteString("Not authorized to access page, user: " + user + ", method: " + method + ", path: " + path)
	}

	// authenticate every request
	beego.InsertFilter("*", beego.BeforeRouter,auth.Basic("alice","123"))

	beego.InsertFilter("*", beego.BeforeRouter, HasPermission)

	beego.Router("*", &Controller{})
	beego.Run()
}
