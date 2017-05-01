package authz

import (
	"encoding/base64"
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/context"
	"github.com/hsluoyz/casbin/api"
	"net/http"
	"strings"
)

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

// NewBasicAuthorizer returns the casbin authorizer.
func NewBasicAuthorizer() beego.FilterFunc {
	e := &api.Enforcer{}
	e.InitWithFile("authz_model.conf", "authz_policy.csv")

	return func(ctx *context.Context) {
		user := getUserName(ctx.Request)
		method := ctx.Request.Method
		path := ctx.Request.URL.Path

		if !e.Enforce(user, path, method) {
			ctx.ResponseWriter.WriteHeader(403)
			ctx.WriteString("Not authorized to access page, user: " + user + ", method: " + method + ", path: " + path)
		}
	}
}

// NewAuthorizer returns the casbin authorizer.
func NewAuthorizer(e *api.Enforcer) beego.FilterFunc {
	return func(ctx *context.Context) {
		user := getUserName(ctx.Request)
		method := ctx.Request.Method
		path := ctx.Request.RequestURI

		if !e.Enforce(user, path, method) {
			ctx.WriteString("Not authorized to access page, user: " + user + ", method: " + method + ", path: " + path)
		}
	}
}
