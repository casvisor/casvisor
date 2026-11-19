// Copyright 2026 The casbin Authors. All Rights Reserved.
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
	"encoding/json"
	"fmt"

	"github.com/casvisor/casvisor/object"
)

const mfaSecretSessionKey = "mfaSecret"

type MfaForm struct {
	Code     string `json:"code"`
	Password string `json:"password"`
}

func (c *ApiController) getLocalSessionUser() *object.LocalUser {
	if !object.IsLocalAuth() {
		c.ResponseError("two-factor sign-in of Casdoor users is set up in Casdoor")
		return nil
	}

	user := c.GetSessionUser()
	if user == nil {
		c.ResponseError("please sign in first")
		return nil
	}

	localUser, err := object.GetLocalUser(fmt.Sprintf("%s/%s", user.Owner, user.Name))
	if err != nil {
		c.ResponseError(err.Error())
		return nil
	}
	if localUser == nil {
		c.ResponseError(fmt.Sprintf("the user: %s/%s does not exist", user.Owner, user.Name))
		return nil
	}
	return localUser
}

func (c *ApiController) getMfaForm() (*MfaForm, bool) {
	var form MfaForm
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &form)
	if err != nil {
		c.ResponseError(err.Error())
		return nil, false
	}
	return &form, true
}

// GetMfaSetup
// @Title GetMfaSetup
// @Tag Account API
// @Description generate a new TOTP secret for the signed-in user to add to an authenticator app, kept until enable-mfa confirms it
// @Success 200 {object} object.MfaSetup The Response object
// @router /get-mfa-setup [post]
func (c *ApiController) GetMfaSetup() {
	user := c.getLocalSessionUser()
	if user == nil {
		return
	}

	setup, err := object.GenerateMfaSetup(user)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.SetSession(mfaSecretSessionKey, setup.Secret)
	c.ResponseOk(setup)
}

// EnableMfa
// @Title EnableMfa
// @Tag Account API
// @Description turn on two-factor sign-in with a code from the secret of get-mfa-setup
// @Param   body    body   controllers.MfaForm  true        "The code from the authenticator app"
// @Success 200 {object} controllers.Response The Response object
// @router /enable-mfa [post]
func (c *ApiController) EnableMfa() {
	user := c.getLocalSessionUser()
	if user == nil {
		return
	}
	form, ok := c.getMfaForm()
	if !ok {
		return
	}

	secret, _ := c.GetSession(mfaSecretSessionKey).(string)
	err := object.EnableLocalUserMfa(user.Owner, user.Name, secret, form.Code)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.DelSession(mfaSecretSessionKey)
	c.ResponseOk()
}

// DisableMfa
// @Title DisableMfa
// @Tag Account API
// @Description turn off two-factor sign-in of the signed-in user, who confirms it with the password
// @Param   body    body   controllers.MfaForm  true        "The password"
// @Success 200 {object} controllers.Response The Response object
// @router /disable-mfa [post]
func (c *ApiController) DisableMfa() {
	user := c.getLocalSessionUser()
	if user == nil {
		return
	}
	form, ok := c.getMfaForm()
	if !ok {
		return
	}

	err := object.DisableLocalUserMfa(user.Owner, user.Name, form.Password)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk()
}

// ResetLocalUserMfa
// @Title ResetLocalUserMfa
// @Tag User API
// @Description turn off two-factor sign-in of a user who lost the authenticator app
// @Param   id     query    string  true        "The id ( owner/name ) of the user"
// @Success 200 {object} controllers.Response The Response object
// @router /reset-local-user-mfa [post]
func (c *ApiController) ResetLocalUserMfa() {
	id := c.Input().Get("id")
	user, err := object.GetLocalUser(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if user == nil {
		c.ResponseError(fmt.Sprintf("the user: %s does not exist", id))
		return
	}
	if !c.checkOwner(user.Owner) {
		return
	}

	err = object.ResetLocalUserMfa(user.Owner, user.Name)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk()
}
