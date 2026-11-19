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
	"github.com/casvisor/casvisor/util"
)

// GetAssetAccounts
// @Title GetAssetAccounts
// @Tag Asset Account API
// @Description get the system accounts of an asset, besides the asset's own account
// @Param   asset     query    string  true        "The id ( owner/name ) of the asset"
// @Success 200 {object} object.AssetAccount The Response object
// @router /get-asset-accounts [get]
func (c *ApiController) GetAssetAccounts() {
	owner, name := util.GetOwnerAndNameFromIdNoCheck(c.Input().Get("asset"))
	if !c.checkOwner(owner) {
		return
	}

	accounts, err := object.GetMaskedAssetAccounts(object.GetAssetAccounts(owner, name))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk(accounts)
}

// GetAssetAccount
// @Title GetAssetAccount
// @Tag Asset Account API
// @Description get a system account of an asset
// @Param   id     query    string  true        "The id ( owner/name ) of the account"
// @Success 200 {object} object.AssetAccount The Response object
// @router /get-asset-account [get]
func (c *ApiController) GetAssetAccount() {
	account, err := object.GetMaskedAssetAccount(object.GetAssetAccount(c.Input().Get("id")))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk(account)
}

// UpdateAssetAccount
// @Title UpdateAssetAccount
// @Tag Asset Account API
// @Description update a system account of an asset
// @Param   id     query    string  true        "The id ( owner/name ) of the account"
// @Param   body    body   object.AssetAccount  true        "The details of the account"
// @Success 200 {object} controllers.Response The Response object
// @router /update-asset-account [post]
func (c *ApiController) UpdateAssetAccount() {
	id := c.Input().Get("id")

	var account object.AssetAccount
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &account)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
	if !c.checkOwner(owner) {
		return
	}

	affected, err := object.UpdateAssetAccount(id, &account)
	if affected {
		go object.CheckLiveSessions()
	}
	c.Data["json"] = wrapActionResponse(affected, err)
	c.ServeJSON()
}

// AddAssetAccount
// @Title AddAssetAccount
// @Tag Asset Account API
// @Description add a system account to an asset
// @Param   body    body   object.AssetAccount  true        "The details of the account"
// @Success 200 {object} controllers.Response The Response object
// @router /add-asset-account [post]
func (c *ApiController) AddAssetAccount() {
	var account object.AssetAccount
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &account)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	if !c.checkOwner(account.Owner) {
		return
	}

	account.Name = ""
	account.LastRotatedTime = ""
	account.RotateStatus = ""
	account.RotateMessage = ""
	c.Data["json"] = wrapActionResponse(object.AddAssetAccount(&account))
	c.ServeJSON()
}

// DeleteAssetAccount
// @Title DeleteAssetAccount
// @Tag Asset Account API
// @Description delete a system account of an asset
// @Param   body    body   object.AssetAccount  true        "The details of the account"
// @Success 200 {object} controllers.Response The Response object
// @router /delete-asset-account [post]
func (c *ApiController) DeleteAssetAccount() {
	var account object.AssetAccount
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &account)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	if !c.checkOwner(account.Owner) {
		return
	}

	affected, err := object.DeleteAssetAccount(&account)
	if affected {
		go object.CheckLiveSessions()
	}
	c.Data["json"] = wrapActionResponse(affected, err)
	c.ServeJSON()
}

// RotateAssetAccount
// @Title RotateAssetAccount
// @Tag Asset Account API
// @Description set a new random password for the account on the asset through the asset's own account
// @Param   id     query    string  true        "The id ( owner/name ) of the account"
// @Success 200 {object} object.AssetAccount The Response object
// @router /rotate-asset-account [post]
func (c *ApiController) RotateAssetAccount() {
	c.rotateAssetAccount(false)
}

// PushAssetAccount
// @Title PushAssetAccount
// @Tag Asset Account API
// @Description create the account on the asset if it does not exist and set its saved password there
// @Param   id     query    string  true        "The id ( owner/name ) of the account"
// @Success 200 {object} object.AssetAccount The Response object
// @router /push-asset-account [post]
func (c *ApiController) PushAssetAccount() {
	c.rotateAssetAccount(true)
}

func (c *ApiController) rotateAssetAccount(isPush bool) {
	id := c.Input().Get("id")
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
	if !c.checkOwner(owner) {
		return
	}
	if owner == "" {
		c.ResponseError(fmt.Sprintf("invalid account id: %s", id))
		return
	}

	account, err := object.RotateAssetAccount(id, isPush)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk(account)
}
