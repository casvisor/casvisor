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
	"fmt"
	"net/http"
	"strings"

	"github.com/beego/beego/context"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
)

type Response struct {
	Status string      `json:"status"`
	Msg    string      `json:"msg"`
	Data   interface{} `json:"data"`
	Data2  interface{} `json:"data2"`
}

func GetSessionUser(ctx *context.Context) *casdoorsdk.User {
	s := ctx.Input.Session("user")
	if s == nil {
		return nil
	}

	claims := s.(casdoorsdk.Claims)
	return &claims.User
}

func getUsername(ctx *context.Context) (username string) {
	user := GetSessionUser(ctx)
	if user != nil {
		username = util.GetIdFromOwnerAndName(user.Owner, user.Name)
	} else {
		username, _ = getUsernameByClientIdSecret(ctx)
	}
	return
}

func requestDeny(ctx *context.Context) {
	ctx.ResponseWriter.WriteHeader(http.StatusForbidden)

	response := &Response{
		Status: "error",
		Msg:    "Unauthorized operation",
	}

	err := ctx.Output.JSON(response, false, false)
	if err != nil {
		return
	}
}

func getUsernameByClientIdSecret(ctx *context.Context) (string, error) {
	clientId, clientSecret, ok := ctx.Request.BasicAuth()
	if !ok {
		clientId = ctx.Input.Query("clientId")
		clientSecret = ctx.Input.Query("clientSecret")
	}

	if clientId == "" || clientSecret == "" {
		return "", nil
	}

	applicationName := conf.GetConfigString("casdoorApplication")
	if clientSecret != conf.GetConfigString("clientSecret") {
		return "", fmt.Errorf("Incorrect client secret for application: %s", applicationName)
	}

	return fmt.Sprintf("app/%s", applicationName), nil
}

// RefreshLocalUser keeps the signed-in local user in step with the user table,
// so that a forbidden or deleted user is signed out and new groups apply at once.
func RefreshLocalUser(ctx *context.Context) {
	if !object.IsLocalAuth() || !strings.HasPrefix(ctx.Request.URL.Path, "/api/") {
		return
	}

	s := ctx.Input.Session("user")
	if s == nil {
		return
	}
	claims := s.(casdoorsdk.Claims)

	user, err := object.GetLocalUser(util.GetIdFromOwnerAndName(claims.User.Owner, claims.User.Name))
	if err != nil {
		return
	}
	if user == nil || user.IsForbidden {
		ctx.Input.CruSession.Delete("user")
		return
	}

	claims.User = *user.ToCasdoorUser()
	ctx.Input.CruSession.Set("user", claims)
}
