// Copyright 2023 The casbin Authors. All Rights Reserved.
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

package controllers

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/beego/beego"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/object"
)

//go:embed token_jwt_key.pem
var JwtPublicKey string

func init() {
	InitAuthConfig()
}

func tryInitAuthConfig() error {
	casdoorEndpoint := conf.GetConfigString("casdoorEndpoint")
	clientId := conf.GetConfigString("clientId")
	clientSecret := conf.GetConfigString("clientSecret")
	casdoorOrganization := conf.GetConfigString("casdoorOrganization")
	casdoorApplication := conf.GetConfigString("casdoorApplication")

	casdoorsdk.InitConfig(casdoorEndpoint, clientId, clientSecret, JwtPublicKey, casdoorOrganization, casdoorApplication)

	application, err := casdoorsdk.GetApplication(casdoorApplication)
	if err != nil {
		return err
	}
	if application == nil {
		return fmt.Errorf("the application %q does not exist", casdoorApplication)
	}

	cert, err := casdoorsdk.GetCert(application.Cert)
	if err != nil {
		return err
	}

	certificate := ""
	if cert != nil {
		certificate = cert.Certificate
	} else {
		certificate, err = getJwksCertificate(casdoorEndpoint, application.Cert)
		if err != nil {
			return err
		}
	}

	casdoorsdk.InitConfig(casdoorEndpoint, clientId, clientSecret, certificate, casdoorOrganization, casdoorApplication)
	return nil
}

func getJwksCertificate(casdoorEndpoint string, certName string) (string, error) {
	resp, err := http.Get(strings.TrimSuffix(casdoorEndpoint, "/") + "/.well-known/jwks")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var jwks struct {
		Keys []struct {
			Kid string   `json:"kid"`
			X5c []string `json:"x5c"`
		} `json:"keys"`
	}
	err = json.NewDecoder(resp.Body).Decode(&jwks)
	if err != nil {
		return "", err
	}

	for _, key := range jwks.Keys {
		if key.Kid == certName && len(key.X5c) > 0 {
			return fmt.Sprintf("-----BEGIN CERTIFICATE-----\n%s\n-----END CERTIFICATE-----\n", key.X5c[0]), nil
		}
	}

	return "", fmt.Errorf("the cert %q does not exist", certName)
}

func InitAuthConfig() {
	if object.IsLocalAuth() {
		return
	}

	if err := tryInitAuthConfig(); err != nil {
		beego.Warning("InitAuthConfig: casdoor unreachable, will retry in background:", err)
		go func() {
			for {
				time.Sleep(10 * time.Second)
				if err := tryInitAuthConfig(); err != nil {
					beego.Warning("InitAuthConfig: retry failed:", err)
				} else {
					beego.Info("InitAuthConfig: casdoor connected successfully")
					return
				}
			}
		}()
	}
}

func (c *ApiController) Signin() {
	if object.IsLocalAuth() {
		c.signinLocal()
		return
	}

	code := c.Input().Get("code")
	state := c.Input().Get("state")

	token, err := casdoorsdk.GetOAuthToken(code, state)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	claims, err := casdoorsdk.ParseJwtToken(token.AccessToken)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	userId := claims.User.Owner + "/" + claims.User.Name
	if !object.IsUserAllowedToSignin(userId) {
		c.ResponseError(fmt.Sprintf("the user: %s is not allowed to sign in", userId))
		return
	}

	claims.AccessToken = token.AccessToken
	c.SetSessionClaims(claims)
	c.Ctx.Input.SetParam("recordUserId", userId)

	c.ResponseOk(claims)
}

type LocalSigninForm struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (c *ApiController) signinLocal() {
	var form LocalSigninForm
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &form)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	organization := object.GetLocalOrganization()
	user, err := object.SigninLocalUser(organization, strings.TrimSpace(form.Username), form.Password, c.getAuthzClientIp())
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	userId := user.GetId()
	if !object.IsUserAllowedToSignin(userId) {
		c.ResponseError(fmt.Sprintf("the user: %s is not allowed to sign in", userId))
		return
	}

	c.SessionRegenerateID()
	claims := &casdoorsdk.Claims{User: *user.ToCasdoorUser()}
	c.SetSessionClaims(claims)
	c.Ctx.Input.SetParam("recordUserId", userId)

	c.ResponseOk(claims)
}

// ChangePassword
// @Title ChangePassword
// @Tag Account API
// @Description change the password of the signed-in local user
// @Param   body    body   controllers.PasswordForm  true        "The old and the new password"
// @Success 200 {object} controllers.Response The Response object
// @router /change-password [post]
func (c *ApiController) ChangePassword() {
	user := c.GetSessionUser()
	if user == nil {
		c.ResponseError("please sign in first")
		return
	}
	if !object.IsLocalAuth() {
		c.ResponseError("passwords are managed in Casdoor")
		return
	}

	var form PasswordForm
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &form)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	err = object.ChangeLocalUserPassword(user.Owner, user.Name, form.OldPassword, form.NewPassword)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk()
}

type PasswordForm struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

func (c *ApiController) Signout() {
	c.SetSessionClaims(nil)

	c.ResponseOk()
}

func (c *ApiController) GetAccount() {
	if c.RequireSignedIn() {
		return
	}

	claims := c.GetSessionClaims()

	c.ResponseOk(claims)
}
