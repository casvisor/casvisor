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
	"bytes"
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
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

// getBodyOwners returns every value the JSON body gives for owner, matching keys without
// regard to case like encoding/json does, so that a field of another type elsewhere in the
// body cannot hide the owner. ok is false when the body is JSON that cannot be read this way.
func getBodyOwners(body []byte) (owners []string, ok bool) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
		return nil, true
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return nil, false
	}

	for key, value := range fields {
		if !strings.EqualFold(key, "owner") {
			continue
		}
		var owner *string
		if err := json.Unmarshal(value, &owner); err != nil {
			return nil, false
		}
		if owner != nil {
			owners = append(owners, *owner)
		}
	}
	return owners, true
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

		owners, ok := getBodyOwners(body)
		if !ok || len(owners) == 0 {
			return "", ""
		}

		var obj Object
		_ = json.Unmarshal(body, &obj)
		return owners[len(owners)-1], obj.Name
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

// isInUserOrganization keeps users outside built-in to their own organization: every id
// and owner in the query, the form and the body must be of it, including ids without an owner.
func isInUserOrganization(ctx *context.Context, user *casdoorsdk.User) bool {
	if user == nil || user.Owner == "built-in" {
		return true
	}

	_ = ctx.Input.Query("id")
	for _, id := range ctx.Request.Form["id"] {
		if id == "" {
			continue
		}
		owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
		if owner != user.Owner {
			return false
		}
	}

	for _, owner := range ctx.Request.Form["owner"] {
		if owner != "" && owner != user.Owner {
			return false
		}
	}

	if ctx.Request.Method != http.MethodGet && len(ctx.Input.RequestBody) != 0 {
		owners, ok := getBodyOwners(ctx.Input.RequestBody)
		if !ok {
			return false
		}
		for _, owner := range owners {
			if owner != "" && owner != user.Owner {
				return false
			}
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
