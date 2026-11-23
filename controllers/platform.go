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
	"strconv"

	"github.com/beego/beego/utils/pagination"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
)

// GetPlatforms
// @Title GetPlatforms
// @Tag Platform API
// @Description get the built-in platforms and the organization's own, by page when pageSize is given
// @Param   owner     query    string  true        "The owner of the platforms"
// @Param   value     query    string  false       "Only the platforms whose name, display name, category or method contains it"
// @Success 200 {array} object.Platform The Response object
// @router /get-platforms [get]
func (c *ApiController) GetPlatforms() {
	owner := c.Input().Get("owner")
	if !c.checkOwner(owner) {
		return
	}
	value := c.Input().Get("value")
	limit, _ := strconv.Atoi(c.Input().Get("pageSize"))
	if limit <= 0 {
		platforms, _, err := object.GetFilteredPlatforms(owner, value, -1, -1)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}
		c.ResponseOk(platforms)
		return
	}

	count, err := object.GetPlatformCount(owner, value)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	paginator := pagination.SetPaginator(c.Ctx, limit, int64(count))
	platforms, _, err := object.GetFilteredPlatforms(owner, value, paginator.Offset(), limit)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(platforms, paginator.Nums())
}

// GetPlatform
// @Title GetPlatform
// @Tag Platform API
// @Description get a platform, built-in or the organization's own
// @Param   id     query    string  true        "The id ( owner/name ) of the platform"
// @Success 200 {object} object.Platform The Response object
// @router /get-platform [get]
func (c *ApiController) GetPlatform() {
	id := c.Input().Get("id")
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
	if !c.checkOwner(owner) {
		return
	}
	platform, err := object.GetPlatform(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(platform)
}

// UpdatePlatform
// @Title UpdatePlatform
// @Tag Platform API
// @Description update a platform of the organization; a built-in one can't be changed, copy it instead. Renaming it renames it in its assets
// @Param   id     query    string  true        "The id ( owner/name ) of the platform"
// @Param   body    body   object.Platform  true        "The details of the platform"
// @Success 200 {object} controllers.Response The Response object
// @router /update-platform [post]
func (c *ApiController) UpdatePlatform() {
	var platform object.Platform
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &platform)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	id := c.Input().Get("id")
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
	if !c.checkOwner(owner) || !c.checkOwner(platform.Owner) {
		return
	}
	c.Data["json"] = wrapActionResponse(object.UpdatePlatform(id, &platform))
	c.ServeJSON()
}

// AddPlatform
// @Title AddPlatform
// @Tag Platform API
// @Description add a platform to the organization, such as a copy of a built-in one
// @Param   body    body   object.Platform  true        "The details of the platform"
// @Success 200 {object} controllers.Response The Response object
// @router /add-platform [post]
func (c *ApiController) AddPlatform() {
	var platform object.Platform
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &platform)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(platform.Owner) {
		return
	}
	c.Data["json"] = wrapActionResponse(object.AddPlatform(&platform))
	c.ServeJSON()
}

// DeletePlatform
// @Title DeletePlatform
// @Tag Platform API
// @Description delete a platform of the organization that no asset uses
// @Param   body    body   object.Platform  true        "The platform"
// @Success 200 {object} controllers.Response The Response object
// @router /delete-platform [post]
func (c *ApiController) DeletePlatform() {
	var platform object.Platform
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &platform)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(platform.Owner) {
		return
	}
	c.Data["json"] = wrapActionResponse(object.DeletePlatform(&platform))
	c.ServeJSON()
}
