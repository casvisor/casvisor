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

	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/sshgateway"
	"golang.org/x/crypto/ssh"
)

type SshGateway struct {
	Enabled     bool   `json:"enabled"`
	Host        string `json:"host"`
	Port        string `json:"port"`
	Fingerprint string `json:"fingerprint"`
}

// GetSshKeys
// @Title GetSshKeys
// @Tag SSH Key API
// @Description get the SSH public keys of the signed-in user
// @Success 200 {object} object.SshKey The Response object
// @router /get-ssh-keys [get]
func (c *ApiController) GetSshKeys() {
	user := c.GetSessionUser()
	if user == nil {
		c.ResponseError("please sign in first")
		return
	}

	sshKeys, err := object.GetUserSshKeys(user.Owner, user.Name)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk(sshKeys)
}

// AddSshKey
// @Title AddSshKey
// @Tag SSH Key API
// @Description add an SSH public key for the signed-in user to sign in to the SSH gateway with
// @Param   body    body   object.SshKey  true        "The SSH key object"
// @Success 200 {object} controllers.Response The Response object
// @router /add-ssh-key [post]
func (c *ApiController) AddSshKey() {
	user := c.GetSessionUser()
	if user == nil {
		c.ResponseError("please sign in first")
		return
	}

	var sshKey object.SshKey
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &sshKey)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	sshKey.Owner = user.Owner
	sshKey.User = user.Name
	c.Data["json"] = wrapActionResponse(object.AddSshKey(&sshKey))
	c.ServeJSON()
}

// DeleteSshKey
// @Title DeleteSshKey
// @Tag SSH Key API
// @Description delete an SSH public key of the signed-in user
// @Param   body    body   object.SshKey  true        "The SSH key object"
// @Success 200 {object} controllers.Response The Response object
// @router /delete-ssh-key [post]
func (c *ApiController) DeleteSshKey() {
	user := c.GetSessionUser()
	if user == nil {
		c.ResponseError("please sign in first")
		return
	}

	var sshKey object.SshKey
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &sshKey)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	existing, err := object.GetSshKey(sshKey.GetId())
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if existing == nil {
		c.ResponseError(fmt.Sprintf("the SSH key: %s does not exist", sshKey.GetId()))
		return
	}
	if existing.GetUserId() != getUserId(user) {
		c.ResponseError("you can only delete your own SSH keys")
		return
	}

	c.Data["json"] = wrapActionResponse(object.DeleteSshKey(existing))
	c.ServeJSON()
}

// GetSshGateway
// @Title GetSshGateway
// @Tag SSH Key API
// @Description get the address and host key fingerprint of the SSH gateway
// @Success 200 {object} controllers.SshGateway The Response object
// @router /get-ssh-gateway [get]
func (c *ApiController) GetSshGateway() {
	res := SshGateway{Enabled: sshgateway.IsEnabled()}
	if res.Enabled {
		res.Host = conf.GetConfigString("sshGatewayHost")
		res.Port = conf.GetConfigString("sshGatewayPort")

		signer, err := object.GetSshHostKeySigner()
		if err != nil {
			c.ResponseError(err.Error())
			return
		}
		res.Fingerprint = ssh.FingerprintSHA256(signer.PublicKey())
	}

	c.ResponseOk(res)
}
