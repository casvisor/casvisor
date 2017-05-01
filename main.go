package main

import "github.com/astaxie/beego"

type Controller struct {
	beego.Controller
}

func (c *Controller) Get() {
	method := c.Ctx.Request.Method
	url := c.Ctx.Request.RequestURI
	c.Ctx.WriteString("method: " + method + ", path: " + url)
}

func main() {
	beego.Router("*", &Controller{})
	beego.Run()
}
