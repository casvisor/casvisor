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

// GetCommandRules
// @Title GetCommandRules
// @Tag Command Rule API
// @Description get all command rules
// @Param   owner     query    string  true        "The owner of command rules"
// @Success 200 {object} object.CommandRule The Response object
// @router /get-command-rules [get]
func (c *ApiController) GetCommandRules() {
	owner := c.Input().Get("owner")
	limit := c.Input().Get("pageSize")
	page := c.Input().Get("p")
	field := c.Input().Get("field")
	value := c.Input().Get("value")
	sortField := c.Input().Get("sortField")
	sortOrder := c.Input().Get("sortOrder")

	if limit == "" || page == "" {
		rules, err := object.GetCommandRules(owner)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}

		c.ResponseOk(rules)
	} else {
		limit := util.ParseInt(limit)
		count, err := object.GetCommandRuleCount(owner, field, value)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}

		paginator := pagination.SetPaginator(c.Ctx, limit, count)
		rules, err := object.GetPaginationCommandRules(owner, paginator.Offset(), limit, field, value, sortField, sortOrder)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}

		c.ResponseOk(rules, paginator.Nums())
	}
}

// GetCommandRule
// @Title GetCommandRule
// @Tag Command Rule API
// @Description get command rule
// @Param   id     query    string  true        "The id ( owner/name ) of the command rule"
// @Success 200 {object} object.CommandRule The Response object
// @router /get-command-rule [get]
func (c *ApiController) GetCommandRule() {
	id := c.Input().Get("id")

	rule, err := object.GetCommandRule(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk(rule)
}

// UpdateCommandRule
// @Title UpdateCommandRule
// @Tag Command Rule API
// @Description update command rule
// @Param   id     query    string  true        "The id ( owner/name ) of the command rule"
// @Param   body    body   object.CommandRule  true        "The details of the command rule"
// @Success 200 {object} controllers.Response The Response object
// @router /update-command-rule [post]
func (c *ApiController) UpdateCommandRule() {
	id := c.Input().Get("id")

	var rule object.CommandRule
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &rule)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	if !c.checkOwner(rule.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.UpdateCommandRule(id, &rule))
	c.ServeJSON()
}

// AddCommandRule
// @Title AddCommandRule
// @Tag Command Rule API
// @Description add a command rule
// @Param   body    body   object.CommandRule  true        "The details of the command rule"
// @Success 200 {object} controllers.Response The Response object
// @router /add-command-rule [post]
func (c *ApiController) AddCommandRule() {
	var rule object.CommandRule
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &rule)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	if !c.checkOwner(rule.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.AddCommandRule(&rule))
	c.ServeJSON()
}

// DeleteCommandRule
// @Title DeleteCommandRule
// @Tag Command Rule API
// @Description delete a command rule
// @Param   body    body   object.CommandRule  true        "The details of the command rule"
// @Success 200 {object} controllers.Response The Response object
// @router /delete-command-rule [post]
func (c *ApiController) DeleteCommandRule() {
	var rule object.CommandRule
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &rule)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.Data["json"] = wrapActionResponse(object.DeleteCommandRule(&rule))
	c.ServeJSON()
}
