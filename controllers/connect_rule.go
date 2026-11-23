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

// GetConnectRules
// @Title GetConnectRules
// @Tag Connect Rule API
// @Description get the connect rules, by priority
// @Param   owner     query    string  true        "The owner of the connect rules"
// @Success 200 {array} object.ConnectRule The Response object
// @router /get-connect-rules [get]
func (c *ApiController) GetConnectRules() {
	owner := c.Input().Get("owner")
	if !c.checkOwner(owner) {
		return
	}
	limit := c.Input().Get("pageSize")
	page := c.Input().Get("p")
	field := c.Input().Get("field")
	value := c.Input().Get("value")
	sortField := c.Input().Get("sortField")
	sortOrder := c.Input().Get("sortOrder")

	if limit == "" || page == "" {
		rules, err := object.GetConnectRules(owner)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}
		c.ResponseOk(rules)
		return
	}

	count, err := object.GetConnectRuleCount(owner, field, value)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	paginator := pagination.SetPaginator(c.Ctx, util.ParseInt(limit), count)
	rules, err := object.GetPaginationConnectRules(owner, paginator.Offset(), util.ParseInt(limit), field, value, sortField, sortOrder)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(rules, paginator.Nums())
}

// GetConnectRule
// @Title GetConnectRule
// @Tag Connect Rule API
// @Description get a connect rule
// @Param   id     query    string  true        "The id ( owner/name ) of the connect rule"
// @Success 200 {object} object.ConnectRule The Response object
// @router /get-connect-rule [get]
func (c *ApiController) GetConnectRule() {
	id := c.Input().Get("id")
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
	if !c.checkOwner(owner) {
		return
	}
	rule, err := object.GetConnectRule(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(rule)
}

// UpdateConnectRule
// @Title UpdateConnectRule
// @Tag Connect Rule API
// @Description update a connect rule
// @Param   id     query    string  true        "The id ( owner/name ) of the connect rule"
// @Param   body    body   object.ConnectRule  true        "The details of the connect rule"
// @Success 200 {object} controllers.Response The Response object
// @router /update-connect-rule [post]
func (c *ApiController) UpdateConnectRule() {
	var rule object.ConnectRule
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &rule)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(rule.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.UpdateConnectRule(c.Input().Get("id"), &rule))
	c.ServeJSON()
}

// AddConnectRule
// @Title AddConnectRule
// @Tag Connect Rule API
// @Description add a connect rule
// @Param   body    body   object.ConnectRule  true        "The details of the connect rule"
// @Success 200 {object} controllers.Response The Response object
// @router /add-connect-rule [post]
func (c *ApiController) AddConnectRule() {
	var rule object.ConnectRule
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &rule)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(rule.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.AddConnectRule(&rule))
	c.ServeJSON()
}

// DeleteConnectRule
// @Title DeleteConnectRule
// @Tag Connect Rule API
// @Description delete a connect rule
// @Param   body    body   object.ConnectRule  true        "The connect rule"
// @Success 200 {object} controllers.Response The Response object
// @router /delete-connect-rule [post]
func (c *ApiController) DeleteConnectRule() {
	var rule object.ConnectRule
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &rule)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(rule.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.DeleteConnectRule(&rule))
	c.ServeJSON()
}
