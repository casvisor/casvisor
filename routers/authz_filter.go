// Copyright 2024 The Casbin Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package routers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/beego/beego/context"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/authz"
	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/controllers"
	"github.com/casvisor/casvisor/util"
)

type Object struct {
	Owner        string `json:"owner"`
	Name         string `json:"name"`
	AccessKey    string `json:"accessKey"`
	AccessSecret string `json:"accessSecret"`
}

func getSubject(ctx *context.Context) (string, string) {
	username := getUsername(ctx)
	if username == "" {
		return "anonymous", "anonymous"
	}

	// username == "built-in/admin"
	return util.GetOwnerAndNameFromId(username)
}

func getObject(ctx *context.Context) (string, string) {
	method := ctx.Request.Method

	if method == http.MethodGet {
		// query == "?id=built-in/admin"
		id := ctx.Input.Query("id")
		if id != "" {
			return util.GetOwnerAndNameFromIdNoCheck(id)
		}

		owner := ctx.Input.Query("owner")
		if owner != "" {
			return owner, ""
		}

		return "", ""
	} else {
		id := ctx.Input.Query("id")
		if id != "" {
			return util.GetOwnerAndNameFromIdNoCheck(id)
		}

		body := ctx.Input.RequestBody
		if len(body) == 0 {
			id := ctx.Request.Form.Get("id")
			if id != "" {
				return util.GetOwnerAndNameFromIdNoCheck(id)
			}

			return ctx.Request.Form.Get("owner"), ctx.Request.Form.Get("name")
		}

		var obj Object
		err := json.Unmarshal(body, &obj)
		if err != nil {
			return "", ""
		}

		return obj.Owner, obj.Name
	}
}

func willLog(subOwner string, subName string, method string, urlPath string, objOwner string, objName string) bool {
	if subOwner == "anonymous" && subName == "anonymous" && method == "GET" && (urlPath == "/api/get-account") && objOwner == "" && objName == "" {
		return false
	}
	return true
}

func getUrlPath(urlPath string) string {
	return urlPath
}

func ApiFilter(ctx *context.Context) {
	if strings.HasPrefix(ctx.Request.URL.Path, "/api/") && ctx.Request.Method != http.MethodGet && ctx.Request.Method != http.MethodHead && ctx.Request.Method != http.MethodOptions &&
		!util.IsAllowedOrigin(ctx.Request, conf.GetConfigAllowedOrigins()) {
		requestDeny(ctx)
		return
	}

	subOwner, subName := getSubject(ctx)
	method := ctx.Request.Method
	urlPath := getUrlPath(ctx.Request.URL.Path)
	objOwner, objName := getObject(ctx)

	user := GetSessionUser(ctx)
	isAllowed := authz.IsAllowed(user, subOwner, subName, method, urlPath, objOwner, objName) && isInUserOrganization(ctx, user)

	result := "deny"
	if isAllowed {
		result = "allow"
	}

	if willLog(subOwner, subName, method, urlPath, objOwner, objName) {
		logLine := fmt.Sprintf("subOwner = %s, subName = %s, method = %s, urlPath = %s, obj.Owner = %s, obj.Name = %s, result = %s",
			subOwner, subName, method, urlPath, objOwner, objName, result)
		fmt.Println(logLine)
		util.LogInfo(ctx, logLine)
	}

	if !isAllowed {
		requestDeny(ctx)
		return
	}

	if user != nil && !mfaSetupApis[urlPath] {
		if required, _ := ctx.Input.Session(controllers.MfaSetupRequiredSessionKey).(bool); required {
			ctx.ResponseWriter.WriteHeader(http.StatusForbidden)
			_ = ctx.Output.JSON(&Response{Status: "error", Msg: "turn on two-step verification in your account first", Data: "mfa-setup-required"}, false, false)
			return
		}
	}

	scopeOwnerToUserOrganization(ctx, user)
}

// mfaSetupApis are what a user whom a login rule asks to turn on two-step verification can do before that.
var mfaSetupApis = map[string]bool{
	"/api/get-account":   true,
	"/api/get-mfa-setup": true,
	"/api/enable-mfa":    true,
	"/api/signout":       true,
	"/api/signin":        true,
}

// isInUserOrganization keeps users outside built-in to their own organization,
// including ids without an owner and bodies naming another organization than the id.
func isInUserOrganization(ctx *context.Context, user *casdoorsdk.User) bool {
	if user == nil || user.Owner == "built-in" {
		return true
	}

	if id := ctx.Input.Query("id"); id != "" {
		owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
		if owner != user.Owner {
			return false
		}
	}

	if ctx.Request.Method != http.MethodGet && len(ctx.Input.RequestBody) != 0 {
		var obj Object
		if err := json.Unmarshal(ctx.Input.RequestBody, &obj); err == nil && obj.Owner != "" && obj.Owner != user.Owner {
			return false
		}
	}

	return true
}

// scopeOwnerToUserOrganization makes a list query without an owner return only
// the user's organization, so that only built-in users see every organization.
func scopeOwnerToUserOrganization(ctx *context.Context, user *casdoorsdk.User) {
	if user == nil || user.Owner == "built-in" || ctx.Request.Method != http.MethodGet {
		return
	}

	if ctx.Input.Query("id") == "" && ctx.Input.Query("owner") == "" {
		ctx.Request.Form.Set("owner", user.Owner)
	}
}
