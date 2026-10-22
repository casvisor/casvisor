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
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
)

type Principal struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

// GetPermissions
// @Title GetPermissions
// @Tag Permission API
// @Description get all permissions
// @Param   owner     query    string  true        "The owner of permissions"
// @Success 200 {object} object.Permission The Response object
// @router /get-permissions [get]
func (c *ApiController) GetPermissions() {
	owner := c.Input().Get("owner")
	limit := c.Input().Get("pageSize")
	page := c.Input().Get("p")
	field := c.Input().Get("field")
	value := c.Input().Get("value")
	sortField := c.Input().Get("sortField")
	sortOrder := c.Input().Get("sortOrder")

	if limit == "" || page == "" {
		permissions, err := object.GetPermissions(owner)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}

		c.ResponseOk(permissions)
	} else {
		limit := util.ParseInt(limit)
		count, err := object.GetPermissionCount(owner, field, value)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}

		paginator := pagination.SetPaginator(c.Ctx, limit, count)
		permissions, err := object.GetPaginationPermissions(owner, paginator.Offset(), limit, field, value, sortField, sortOrder)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}

		c.ResponseOk(permissions, paginator.Nums())
	}
}

// GetPermission
// @Title GetPermission
// @Tag Permission API
// @Description get permission
// @Param   id     query    string  true        "The id ( owner/name ) of the permission"
// @Success 200 {object} object.Permission The Response object
// @router /get-permission [get]
func (c *ApiController) GetPermission() {
	id := c.Input().Get("id")

	permission, err := object.GetPermission(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk(permission)
}

// UpdatePermission
// @Title UpdatePermission
// @Tag Permission API
// @Description update permission
// @Param   id     query    string  true        "The id ( owner/name ) of the permission"
// @Param   body    body   object.Permission  true        "The details of the permission"
// @Success 200 {object} controllers.Response The Response object
// @router /update-permission [post]
func (c *ApiController) UpdatePermission() {
	id := c.Input().Get("id")

	var permission object.Permission
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &permission)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	if !c.checkOwner(permission.Owner) {
		return
	}

	affected, err := object.UpdatePermission(id, &permission)
	if err == nil {
		go object.CheckLiveSessions()
	}
	c.Data["json"] = wrapActionResponse(affected, err)
	c.ServeJSON()
}

// AddPermission
// @Title AddPermission
// @Tag Permission API
// @Description add a permission
// @Param   body    body   object.Permission  true        "The details of the permission"
// @Success 200 {object} controllers.Response The Response object
// @router /add-permission [post]
func (c *ApiController) AddPermission() {
	var permission object.Permission
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &permission)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	if !c.checkOwner(permission.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.AddPermission(&permission))
	c.ServeJSON()
}

// DeletePermission
// @Title DeletePermission
// @Tag Permission API
// @Description delete a permission
// @Param   body    body   object.Permission  true        "The details of the permission"
// @Success 200 {object} controllers.Response The Response object
// @router /delete-permission [post]
func (c *ApiController) DeletePermission() {
	var permission object.Permission
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &permission)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	affected, err := object.DeletePermission(&permission)
	if err == nil {
		go object.CheckLiveSessions()
	}
	c.Data["json"] = wrapActionResponse(affected, err)
	c.ServeJSON()
}

// GetUsers
// @Title GetUsers
// @Tag Permission API
// @Description get the users of the Casdoor organization, for choosing permission subjects
// @Success 200 {object} controllers.Principal The Response object
// @router /get-users [get]
func (c *ApiController) GetUsers() {
	if object.IsLocalAuth() {
		users, err := object.GetLocalUsers(c.GetSessionUser().Owner)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}

		res := []*Principal{}
		for _, user := range users {
			res = append(res, &Principal{Name: user.Name, DisplayName: user.DisplayName})
		}
		c.ResponseOk(res)
		return
	}

	if c.GetSessionUser().Owner != conf.GetConfigString("casdoorOrganization") {
		c.ResponseOk([]*Principal{})
		return
	}

	users, err := casdoorsdk.GetUsers()
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	res := []*Principal{}
	for _, user := range users {
		res = append(res, &Principal{Name: user.Name, DisplayName: user.DisplayName})
	}
	c.ResponseOk(res)
}

// GetGroups
// @Title GetGroups
// @Tag Permission API
// @Description get the groups of the Casdoor organization, for choosing permission subjects
// @Success 200 {object} controllers.Principal The Response object
// @router /get-groups [get]
func (c *ApiController) GetGroups() {
	if object.IsLocalAuth() {
		groups, err := object.GetLocalGroups(c.GetSessionUser().Owner)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}

		res := []*Principal{}
		for _, group := range groups {
			res = append(res, &Principal{Name: group, DisplayName: group})
		}
		c.ResponseOk(res)
		return
	}

	if c.GetSessionUser().Owner != conf.GetConfigString("casdoorOrganization") {
		c.ResponseOk([]*Principal{})
		return
	}

	groups, err := casdoorsdk.GetGroups()
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	res := []*Principal{}
	for _, group := range groups {
		res = append(res, &Principal{Name: group.Name, DisplayName: group.DisplayName})
	}
	c.ResponseOk(res)
}
