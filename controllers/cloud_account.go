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

	"github.com/beego/beego/utils/pagination"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
)

// GetCloudAccounts
// @Title GetCloudAccounts
// @Tag Cloud Account API
// @Description get the cloud accounts, with their secrets masked
// @Param   owner     query    string  true        "The owner of the cloud accounts"
// @Success 200 {array} object.CloudAccount The Response object
// @router /get-cloud-accounts [get]
func (c *ApiController) GetCloudAccounts() {
	owner := c.Input().Get("owner")
	if !c.checkOwner(owner) {
		return
	}
	limit := c.Input().Get("pageSize")
	page := c.Input().Get("p")
	field := c.Input().Get("field")
	value := c.Input().Get("value")

	if limit == "" || page == "" {
		accounts, err := object.GetCloudAccounts(owner)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}
		c.ResponseOk(object.GetMaskedCloudAccounts(accounts))
		return
	}

	count, err := object.GetCloudAccountCount(owner, field, value)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	paginator := pagination.SetPaginator(c.Ctx, util.ParseInt(limit), count)
	accounts, err := object.GetPaginationCloudAccounts(owner, paginator.Offset(), util.ParseInt(limit), field, value, c.Input().Get("sortField"), c.Input().Get("sortOrder"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(object.GetMaskedCloudAccounts(accounts), paginator.Nums())
}

// GetCloudAccount
// @Title GetCloudAccount
// @Tag Cloud Account API
// @Description get a cloud account, with its secret masked
// @Param   id     query    string  true        "The id ( owner/name ) of the cloud account"
// @Success 200 {object} object.CloudAccount The Response object
// @router /get-cloud-account [get]
func (c *ApiController) GetCloudAccount() {
	id := c.Input().Get("id")
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
	if !c.checkOwner(owner) {
		return
	}

	account, err := object.GetCloudAccount(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(object.GetMaskedCloudAccounts([]*object.CloudAccount{account})[0])
}

// UpdateCloudAccount
// @Title UpdateCloudAccount
// @Tag Cloud Account API
// @Description update a cloud account, keeping its secret when it is sent masked
// @Param   id     query    string  true        "The id ( owner/name ) of the cloud account"
// @Param   body    body   object.CloudAccount  true        "The details of the cloud account"
// @Success 200 {object} controllers.Response The Response object
// @router /update-cloud-account [post]
func (c *ApiController) UpdateCloudAccount() {
	var account object.CloudAccount
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &account)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	id := c.Input().Get("id")
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
	if !c.checkOwner(owner) || !c.checkOwner(account.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.UpdateCloudAccount(id, &account))
	c.ServeJSON()
}

// AddCloudAccount
// @Title AddCloudAccount
// @Tag Cloud Account API
// @Description add a cloud account
// @Param   body    body   object.CloudAccount  true        "The details of the cloud account"
// @Success 200 {object} controllers.Response The Response object
// @router /add-cloud-account [post]
func (c *ApiController) AddCloudAccount() {
	var account object.CloudAccount
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &account)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(account.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.AddCloudAccount(&account))
	c.ServeJSON()
}

// DeleteCloudAccount
// @Title DeleteCloudAccount
// @Tag Cloud Account API
// @Description delete a cloud account; its assets are kept and no longer synced
// @Param   body    body   object.CloudAccount  true        "The cloud account"
// @Success 200 {object} controllers.Response The Response object
// @router /delete-cloud-account [post]
func (c *ApiController) DeleteCloudAccount() {
	var account object.CloudAccount
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &account)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(account.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.DeleteCloudAccount(&account))
	c.ServeJSON()
}

// GetCloudRegions
// @Title GetCloudRegions
// @Tag Cloud Account API
// @Description list the regions of a cloud account as it is in the form, without saving it; the secret of the saved account is used when the form has it masked
// @Param   id     query    string  false       "The id ( owner/name ) of the saved cloud account"
// @Param   body    body   object.CloudAccount  true        "The details of the cloud account"
// @Success 200 {array} string The Response object
// @router /get-cloud-regions [post]
func (c *ApiController) GetCloudRegions() {
	var account object.CloudAccount
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &account)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(account.Owner) {
		return
	}
	id := c.Input().Get("id")
	if id != "" {
		owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
		if !c.checkOwner(owner) {
			return
		}
	}

	regions, err := object.GetCloudRegions(id, &account)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(regions)
}

// SyncCloudAccount
// @Title SyncCloudAccount
// @Tag Cloud Account API
// @Description list the servers of a cloud account and create, update or report the assets for them; with dryRun nothing is saved
// @Param   id     query    string  true        "The id ( owner/name ) of the cloud account"
// @Param   dryRun query    bool    false       "Only preview what would change"
// @Success 200 {object} object.CloudSyncResult The Response object
// @router /sync-cloud-account [post]
func (c *ApiController) SyncCloudAccount() {
	id := c.Input().Get("id")
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
	if !c.checkOwner(owner) {
		return
	}

	res, err := object.SyncCloudAccount(id, c.Input().Get("dryRun") == "true")
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(res)
}
