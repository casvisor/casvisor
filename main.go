package main

import (
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/context"
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

func main() {
	var HasPermission = func(ctx *context.Context) {
		method := ctx.Request.Method
		url := ctx.Request.RequestURI
		ctx.WriteString("Not authorized to access page, method: " + method + ", path: " + url)
	}

	beego.InsertFilter("*", beego.BeforeRouter, HasPermission)

	beego.Router("*", &Controller{})
	beego.Run()
}
