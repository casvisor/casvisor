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

// GetAccountTemplates
// @Title GetAccountTemplates
// @Tag Account API
// @Description get the account templates, with their secrets masked
// @Param   owner     query    string  true        "The owner of the account templates"
// @Success 200 {array} object.AccountTemplate The Response object
// @router /get-account-templates [get]
func (c *ApiController) GetAccountTemplates() {
	owner := c.Input().Get("owner")
	if !c.checkOwner(owner) {
		return
	}
	limit := util.ParseInt(c.Input().Get("pageSize"))
	if limit <= 0 {
		limit = 100
	}
	field, value := c.Input().Get("field"), c.Input().Get("value")

	count, err := object.GetAccountTemplateCount(owner, field, value)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	paginator := pagination.SetPaginator(c.Ctx, limit, count)
	templates, err := object.GetPaginationAccountTemplates(owner, paginator.Offset(), limit, field, value, c.Input().Get("sortField"), c.Input().Get("sortOrder"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(object.GetMaskedAccountTemplates(templates), paginator.Nums())
}

// GetAccountTemplate
// @Title GetAccountTemplate
// @Tag Account API
// @Description get an account template, with its secrets masked
// @Param   id     query    string  true        "The id ( owner/name ) of the account template"
// @Success 200 {object} object.AccountTemplate The Response object
// @router /get-account-template [get]
func (c *ApiController) GetAccountTemplate() {
	id := c.Input().Get("id")
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
	if !c.checkOwner(owner) {
		return
	}
	template, err := object.GetAccountTemplate(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(object.GetMaskedAccountTemplates([]*object.AccountTemplate{template})[0])
}

// UpdateAccountTemplate
// @Title UpdateAccountTemplate
// @Tag Account API
// @Description update an account template, keeping its secrets when they are sent masked; apply it to change the accounts
// @Param   id     query    string  true        "The id ( owner/name ) of the account template"
// @Param   body    body   object.AccountTemplate  true        "The details of the account template"
// @Success 200 {object} controllers.Response The Response object
// @router /update-account-template [post]
func (c *ApiController) UpdateAccountTemplate() {
	var template object.AccountTemplate
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &template)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	id := c.Input().Get("id")
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
	if !c.checkOwner(owner) || !c.checkOwner(template.Owner) {
		return
	}
	c.Data["json"] = wrapActionResponse(object.UpdateAccountTemplate(id, &template))
	c.ServeJSON()
}

// AddAccountTemplate
// @Title AddAccountTemplate
// @Tag Account API
// @Description add an account template
// @Param   body    body   object.AccountTemplate  true        "The details of the account template"
// @Success 200 {object} controllers.Response The Response object
// @router /add-account-template [post]
func (c *ApiController) AddAccountTemplate() {
	var template object.AccountTemplate
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &template)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(template.Owner) {
		return
	}
	c.Data["json"] = wrapActionResponse(object.AddAccountTemplate(&template))
	c.ServeJSON()
}

// DeleteAccountTemplate
// @Title DeleteAccountTemplate
// @Tag Account API
// @Description delete an account template; the accounts it created are kept
// @Param   body    body   object.AccountTemplate  true        "The account template"
// @Success 200 {object} controllers.Response The Response object
// @router /delete-account-template [post]
func (c *ApiController) DeleteAccountTemplate() {
	var template object.AccountTemplate
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &template)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(template.Owner) {
		return
	}
	c.Data["json"] = wrapActionResponse(object.DeleteAccountTemplate(&template))
	c.ServeJSON()
}

// ApplyAccountTemplate
// @Title ApplyAccountTemplate
// @Tag Account API
// @Description create the template's account on the assets it covers, or bring the accounts it created in line with it; with dryRun nothing is saved or pushed
// @Param   id     query    string  true        "The id ( owner/name ) of the account template"
// @Param   dryRun query    bool    false       "Only preview what would change"
// @Success 200 {object} object.TemplateApplyResult The Response object
// @router /apply-account-template [post]
func (c *ApiController) ApplyAccountTemplate() {
	id := c.Input().Get("id")
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
	if !c.checkOwner(owner) {
		return
	}
	res, err := object.ApplyAccountTemplate(id, c.Input().Get("dryRun") == "true")
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(res)
}

// GetAccountRisks
// @Title GetAccountRisks
// @Tag Account API
// @Description get the risks found in the accounts of the assets, by page
// @Param   owner     query    string  true        "The owner of the risks"
// @Param   status    query    string  false       "Open, Ignored or Resolved"
// @Success 200 {array} object.AccountRisk The Response object
// @router /get-account-risks [get]
func (c *ApiController) GetAccountRisks() {
	owner := c.Input().Get("owner")
	if !c.checkOwner(owner) {
		return
	}
	status := c.Input().Get("status")
	limit := util.ParseInt(c.Input().Get("pageSize"))
	if limit <= 0 {
		limit = 100
	}
	field, value := c.Input().Get("field"), c.Input().Get("value")

	count, err := object.GetAccountRiskCount(owner, status, field, value)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	paginator := pagination.SetPaginator(c.Ctx, limit, count)
	risks, err := object.GetPaginationAccountRisks(owner, status, paginator.Offset(), limit, field, value, c.Input().Get("sortField"), c.Input().Get("sortOrder"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(risks, paginator.Nums())
}

// StartAccountRiskScan
// @Title StartAccountRiskScan
// @Tag Account API
// @Description check the passwords of the accounts and read the accounts of the SSH assets, in the background
// @Param   owner     query    string  true        "The organization"
// @Success 200 {object} controllers.Response The Response object
// @router /start-account-risk-scan [post]
func (c *ApiController) StartAccountRiskScan() {
	owner := c.Input().Get("owner")
	if !c.checkOwner(owner) {
		return
	}
	err := object.StartAccountRiskScan(owner)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk()
}

// GetAccountRiskScan
// @Title GetAccountRiskScan
// @Tag Account API
// @Description get how the last scan of the accounts of the organization went
// @Param   owner     query    string  true        "The organization"
// @Success 200 {object} object.AccountRiskScan The Response object
// @router /get-account-risk-scan [get]
func (c *ApiController) GetAccountRiskScan() {
	owner := c.Input().Get("owner")
	if !c.checkOwner(owner) {
		return
	}
	c.ResponseOk(object.GetAccountRiskScan(owner))
}

type accountRiskStatusForm struct {
	Id      string `json:"id"`
	Status  string `json:"status"`
	Comment string `json:"comment"`
}

// SetAccountRiskStatus
// @Title SetAccountRiskStatus
// @Tag Account API
// @Description ignore a risk with a comment, or open it again
// @Param   body    body   controllers.accountRiskStatusForm  true        "The risk, Open or Ignored, and a comment"
// @Success 200 {object} controllers.Response The Response object
// @router /set-account-risk-status [post]
func (c *ApiController) SetAccountRiskStatus() {
	var form accountRiskStatusForm
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &form)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(form.Id)
	if !c.checkOwner(owner) {
		return
	}
	c.Data["json"] = wrapActionResponse(object.SetAccountRiskStatus(form.Id, form.Status, form.Comment))
	c.ServeJSON()
}

type accountRiskHandleForm struct {
	Ids    []string `json:"ids"`
	Action string   `json:"action"`
}

// HandleAccountRisks
// @Title HandleAccountRisks
// @Tag Account API
// @Description fix risks on the assets: Rotate sets a new random password for a managed account, Manage makes Casvisor manage an account found on the asset with a random password, Lock locks an account found on the asset
// @Param   body    body   controllers.accountRiskHandleForm  true        "The risks and the action: Rotate, Manage or Lock"
// @Success 200 {array} object.RiskHandleResult The Response object
// @router /handle-account-risks [post]
func (c *ApiController) HandleAccountRisks() {
	var form accountRiskHandleForm
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &form)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	for _, id := range form.Ids {
		owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
		if !c.checkOwner(owner) {
			return
		}
	}

	user := c.GetSessionUser()
	res, err := object.HandleAccountRisks(form.Ids, form.Action, util.GetIdFromOwnerAndName(user.Owner, user.Name))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(res)
}
