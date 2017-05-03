package authz

import (
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/context"
	"github.com/hsluoyz/casbin/api"
	"net/http"
)

func getUserName(r *http.Request) string {
	username, _, _ := r.BasicAuth()
	return username
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
			requirePermission(ctx.ResponseWriter)
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
			requirePermission(ctx.ResponseWriter)
		}
	}
}

func requirePermission(w http.ResponseWriter) {
	w.WriteHeader(403)
	w.Write([]byte("403 Forbidden\n"))
}
