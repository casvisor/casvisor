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

// GetWebhooks
// @Title GetWebhooks
// @Tag Webhook API
// @Description get the webhooks, with their secrets masked
// @Param   owner     query    string  true        "The owner of the webhooks"
// @Success 200 {array} object.Webhook The Response object
// @router /get-webhooks [get]
func (c *ApiController) GetWebhooks() {
	owner := c.Input().Get("owner")
	limit := c.Input().Get("pageSize")
	page := c.Input().Get("p")
	field := c.Input().Get("field")
	value := c.Input().Get("value")
	sortField := c.Input().Get("sortField")
	sortOrder := c.Input().Get("sortOrder")

	if limit == "" || page == "" {
		webhooks, err := object.GetWebhooks(owner)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}
		c.ResponseOk(object.GetMaskedWebhooks(webhooks))
		return
	}

	count, err := object.GetWebhookCount(owner, field, value)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	paginator := pagination.SetPaginator(c.Ctx, util.ParseInt(limit), count)
	webhooks, err := object.GetPaginationWebhooks(owner, paginator.Offset(), util.ParseInt(limit), field, value, sortField, sortOrder)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(object.GetMaskedWebhooks(webhooks), paginator.Nums())
}

// GetWebhook
// @Title GetWebhook
// @Tag Webhook API
// @Description get a webhook, with its secret masked
// @Param   id     query    string  true        "The id ( owner/name ) of the webhook"
// @Success 200 {object} object.Webhook The Response object
// @router /get-webhook [get]
func (c *ApiController) GetWebhook() {
	webhook, err := object.GetWebhook(c.Input().Get("id"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(object.GetMaskedWebhooks([]*object.Webhook{webhook})[0])
}

// UpdateWebhook
// @Title UpdateWebhook
// @Tag Webhook API
// @Description update a webhook, keeping its secret when it is sent masked
// @Param   id     query    string  true        "The id ( owner/name ) of the webhook"
// @Param   body    body   object.Webhook  true        "The details of the webhook"
// @Success 200 {object} controllers.Response The Response object
// @router /update-webhook [post]
func (c *ApiController) UpdateWebhook() {
	var webhook object.Webhook
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &webhook)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(webhook.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.UpdateWebhook(c.Input().Get("id"), &webhook))
	c.ServeJSON()
}

// AddWebhook
// @Title AddWebhook
// @Tag Webhook API
// @Description add a webhook
// @Param   body    body   object.Webhook  true        "The details of the webhook"
// @Success 200 {object} controllers.Response The Response object
// @router /add-webhook [post]
func (c *ApiController) AddWebhook() {
	var webhook object.Webhook
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &webhook)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(webhook.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.AddWebhook(&webhook))
	c.ServeJSON()
}

// DeleteWebhook
// @Title DeleteWebhook
// @Tag Webhook API
// @Description delete a webhook
// @Param   body    body   object.Webhook  true        "The webhook"
// @Success 200 {object} controllers.Response The Response object
// @router /delete-webhook [post]
func (c *ApiController) DeleteWebhook() {
	var webhook object.Webhook
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &webhook)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(webhook.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.DeleteWebhook(&webhook))
	c.ServeJSON()
}

// TestWebhook
// @Title TestWebhook
// @Tag Webhook API
// @Description send a test message with the webhook as it is in the form, without saving it
// @Param   id     query    string  false       "The id ( owner/name ) of the saved webhook whose secret is used when masked"
// @Param   body    body   object.Webhook  true        "The details of the webhook"
// @Success 200 {object} controllers.Response The Response object
// @router /test-webhook [post]
func (c *ApiController) TestWebhook() {
	var webhook object.Webhook
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &webhook)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(webhook.Owner) {
		return
	}
	id := c.Input().Get("id")
	if id != "" {
		owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
		if !c.checkOwner(owner) {
			return
		}
	}

	err = object.TestWebhook(id, &webhook)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk()
}
