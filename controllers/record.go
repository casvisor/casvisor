// Copyright 2023 The casbin Authors. All Rights Reserved.
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
	"net/http"
	"strings"

	"github.com/beego/beego/utils/pagination"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
)

func isGlobalAdminUser(user *casdoorsdk.User) bool {
	return user != nil && !user.IsDeleted && !user.IsForbidden && user.Owner == "built-in"
}

func requireTenantAdmin(user *casdoorsdk.User) error {
	if user == nil {
		return fmt.Errorf("please sign in first")
	}
	if user.IsDeleted || user.IsForbidden || !object.IsAdminUser(user) || user.Owner == "" {
		return fmt.Errorf("unauthorized")
	}
	return nil
}

func resolveTenantOwner(user *casdoorsdk.User, requestedOwner string) (string, error) {
	if err := requireTenantAdmin(user); err != nil {
		return "", err
	}

	owner := strings.TrimSpace(requestedOwner)
	if isGlobalAdminUser(user) {
		return owner, nil
	}
	if owner == "" {
		return user.Owner, nil
	}
	if owner != user.Owner {
		return "", fmt.Errorf("you cannot access the resources of the organization: %s", owner)
	}
	return owner, nil
}

func splitTenantId(id string) (string, string, bool) {
	tokens := strings.Split(id, "/")
	if len(tokens) != 2 || strings.TrimSpace(tokens[0]) == "" || strings.TrimSpace(tokens[1]) == "" {
		return "", "", false
	}
	return tokens[0], tokens[1], true
}

func resolveTenantId(user *casdoorsdk.User, id string) (string, string, error) {
	if err := requireTenantAdmin(user); err != nil {
		return "", "", err
	}

	owner, name, ok := splitTenantId(id)
	if !ok {
		return "", "", fmt.Errorf("invalid ID: expected owner/name")
	}
	owner, err := resolveTenantOwner(user, owner)
	if err != nil {
		return "", "", err
	}
	return owner, name, nil
}

func (c *ApiController) resolveQueryOwner(requestedOwner string) (string, bool) {
	owner, err := resolveTenantOwner(c.GetSessionUser(), requestedOwner)
	if err != nil {
		c.ResponseError(err.Error())
		return "", false
	}
	return owner, true
}

func (c *ApiController) resolveTenantId(id string) (string, string, bool) {
	owner, name, err := resolveTenantId(c.GetSessionUser(), id)
	if err != nil {
		c.ResponseError(err.Error())
		return "", "", false
	}
	return owner, name, true
}

// GetRecords
// @Title GetRecords
// @Tag Record API
// @Description get all records
// @Param   pageSize     query    string  true        "The size of each page"
// @Param   p     query    string  true        "The number of the page"
// @Success 200 {object} object.Record The Response object
// @router /get-records [get]
func (c *ApiController) GetRecords() {
	owner, ok := c.resolveQueryOwner(c.Input().Get("owner"))
	if !ok {
		return
	}
	limit := c.Input().Get("pageSize")
	page := c.Input().Get("p")
	field := c.Input().Get("field")
	value := c.Input().Get("value")
	sortField := c.Input().Get("sortField")
	sortOrder := c.Input().Get("sortOrder")

	if limit == "" || page == "" {
		records, err := object.GetRecords(owner)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}

		c.ResponseOk(records)
	} else {
		limit := util.ParseInt(limit)

		count, err := object.GetRecordCount(owner, field, value)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}

		paginator := pagination.SetPaginator(c.Ctx, limit, count)
		records, err := object.GetPaginationRecords(owner, paginator.Offset(), limit, field, value, sortField, sortOrder)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}

		c.ResponseOk(records, paginator.Nums())
	}
}

// GetRecord
// @Title GetRecord
// @Tag Record API
// @Description get record
// @Param   id     query    string  true        "The id ( owner/name ) of the record"
// @Success 200 {object} object.Record The Response object
// @router /get-record [get]
func (c *ApiController) GetRecord() {
	owner, name, ok := c.resolveTenantId(c.Input().Get("id"))
	if !ok {
		return
	}

	record, err := object.GetRecord(util.GetIdFromOwnerAndName(owner, name))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk(record)
}

// UpdateRecord rejects attempts to update an audit record.
func (c *ApiController) UpdateRecord() {
	c.Ctx.Output.SetStatus(http.StatusMethodNotAllowed)
	c.ResponseError(object.ErrAuditRecordImmutable.Error())
}

// AddRecord
// @Title AddRecord
// @Tag Record API
// @Description add a record
// @Param   body    body   object.Record  true        "The details of the record"
// @Success 200 {object} controllers.Response The Response object
// @router /add-record [post]
func (c *ApiController) AddRecord() {
	if err := requireTenantAdmin(c.GetSessionUser()); err != nil {
		c.ResponseError(err.Error())
		return
	}

	var record object.Record
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &record)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	owner := strings.TrimSpace(record.Owner)
	organization := strings.TrimSpace(record.Organization)
	if owner != "" && organization != "" && owner != organization {
		c.ResponseError("owner and organization must match")
		return
	}
	requestedOwner := owner
	if requestedOwner == "" {
		requestedOwner = organization
	}
	scopedOwner, ok := c.resolveQueryOwner(requestedOwner)
	if !ok {
		return
	}
	if scopedOwner == "" {
		scopedOwner = c.GetSessionUser().Owner
	}
	record.Owner = scopedOwner
	record.Organization = scopedOwner
	record.Id = 0
	if record.Name == "" {
		record.Name = util.GenerateId()
	}
	if record.CreatedTime == "" {
		record.CreatedTime = util.GetCurrentTime()
	}

	if record.ClientIp == "" {
		record.ClientIp = c.getClientIp()
	}
	if record.UserAgent == "" {
		record.UserAgent = c.getUserAgent()
	}

	c.Data["json"] = wrapActionResponse(object.AddRecord(&record))
	c.ServeJSON()
}

// DeleteRecord rejects attempts to delete an audit record.
func (c *ApiController) DeleteRecord() {
	c.Ctx.Output.SetStatus(http.StatusMethodNotAllowed)
	c.ResponseError(object.ErrAuditRecordImmutable.Error())
}
