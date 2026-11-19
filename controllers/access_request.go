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

	"github.com/beego/beego/utils/pagination"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
)

type AccessReviewForm struct {
	Comment string   `json:"comment"`
	Actions []string `json:"actions"`
	EndTime string   `json:"endTime"`
}

// GetAccessRequests
// @Title GetAccessRequests
// @Tag Access Request API
// @Description get the access requests of the organization, or only the signed-in user's own requests for a user who is not an administrator
// @Param   owner     query    string  true        "The owner of access requests"
// @Param   pageSize     query    string  false        "The size of each page"
// @Param   p     query    string  false        "The number of the page"
// @Success 200 {object} object.AccessRequest The Response object
// @router /get-access-requests [get]
func (c *ApiController) GetAccessRequests() {
	user := c.GetSessionUser()
	if user == nil {
		c.ResponseError("please sign in first")
		return
	}

	owner := c.Input().Get("owner")
	requester := ""
	if !c.isAdmin() {
		owner = user.Owner
		requester = user.Name
	}

	limit := c.Input().Get("pageSize")
	page := c.Input().Get("p")
	field := c.Input().Get("field")
	value := c.Input().Get("value")
	sortField := c.Input().Get("sortField")
	sortOrder := c.Input().Get("sortOrder")

	if limit == "" || page == "" {
		requests, err := object.GetAccessRequests(owner, requester)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}
		c.ResponseOk(requests)
		return
	}

	count, err := object.GetAccessRequestCount(owner, requester, field, value)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	paginator := pagination.SetPaginator(c.Ctx, util.ParseInt(limit), count)
	requests, err := object.GetPaginationAccessRequests(owner, requester, paginator.Offset(), util.ParseInt(limit), field, value, sortField, sortOrder)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(requests, paginator.Nums())
}

// GetRequestableAssets
// @Title GetRequestableAssets
// @Tag Access Request API
// @Description get the assets of the signed-in user's organization that access can be requested for, without their addresses or credentials
// @Success 200 {object} object.RequestableAsset The Response object
// @router /get-requestable-assets [get]
func (c *ApiController) GetRequestableAssets() {
	user := c.GetSessionUser()
	if user == nil {
		c.ResponseError("please sign in first")
		return
	}

	assets, err := object.GetRequestableAssets(user.Owner)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(assets)
}

// AddAccessRequest
// @Title AddAccessRequest
// @Tag Access Request API
// @Description request access to assets for the signed-in user
// @Param   body    body   object.AccessRequest  true        "The details of the request"
// @Success 200 {object} controllers.Response The Response object
// @router /add-access-request [post]
func (c *ApiController) AddAccessRequest() {
	user := c.GetSessionUser()
	if user == nil {
		c.ResponseError("please sign in first")
		return
	}

	var request object.AccessRequest
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &request)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	request.Owner = user.Owner
	request.User = user.Name
	c.Data["json"] = wrapActionResponse(object.AddAccessRequest(&request))
	c.ServeJSON()
}

func (c *ApiController) getAccessRequestToReview() *object.AccessRequest {
	id := c.Input().Get("id")
	request, err := object.GetAccessRequest(id)
	if err != nil {
		c.ResponseError(err.Error())
		return nil
	}
	if request == nil {
		c.ResponseError(fmt.Sprintf("the access request: %s does not exist", id))
		return nil
	}
	if !c.checkOwner(request.Owner) {
		return nil
	}
	return request
}

func (c *ApiController) getAccessReviewForm() (*AccessReviewForm, bool) {
	form := &AccessReviewForm{}
	if len(c.Ctx.Input.RequestBody) == 0 {
		return form, true
	}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, form)
	if err != nil {
		c.ResponseError(err.Error())
		return nil, false
	}
	return form, true
}

// CancelAccessRequest
// @Title CancelAccessRequest
// @Tag Access Request API
// @Description cancel a pending access request of the signed-in user
// @Param   id     query    string  true        "The id ( owner/name ) of the access request"
// @Success 200 {object} controllers.Response The Response object
// @router /cancel-access-request [post]
func (c *ApiController) CancelAccessRequest() {
	user := c.GetSessionUser()
	request := c.getAccessRequestToReview()
	if request == nil {
		return
	}
	if !c.isAdmin() && request.User != user.Name {
		c.ResponseError("you can only cancel your own access requests")
		return
	}

	err := object.CancelAccessRequest(request)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(request)
}

// ApproveAccessRequest
// @Title ApproveAccessRequest
// @Tag Access Request API
// @Description approve a pending access request, which adds a permission for it
// @Param   id     query    string  true        "The id ( owner/name ) of the access request"
// @Param   body    body   controllers.AccessReviewForm  false        "The comment, and the actions and end time when they are narrowed"
// @Success 200 {object} object.Permission The Response object
// @router /approve-access-request [post]
func (c *ApiController) ApproveAccessRequest() {
	request := c.getAccessRequestToReview()
	if request == nil {
		return
	}
	form, ok := c.getAccessReviewForm()
	if !ok {
		return
	}

	permission, err := object.ApproveAccessRequest(request, getUserId(c.GetSessionUser()), form.Comment, form.Actions, form.EndTime)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(permission)
}

// RejectAccessRequest
// @Title RejectAccessRequest
// @Tag Access Request API
// @Description reject a pending access request
// @Param   id     query    string  true        "The id ( owner/name ) of the access request"
// @Param   body    body   controllers.AccessReviewForm  false        "The comment"
// @Success 200 {object} controllers.Response The Response object
// @router /reject-access-request [post]
func (c *ApiController) RejectAccessRequest() {
	request := c.getAccessRequestToReview()
	if request == nil {
		return
	}
	form, ok := c.getAccessReviewForm()
	if !ok {
		return
	}

	err := object.RejectAccessRequest(request, getUserId(c.GetSessionUser()), form.Comment)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(request)
}

// RevokeAccessRequest
// @Title RevokeAccessRequest
// @Tag Access Request API
// @Description revoke an approved access request by deleting its permission, which disconnects its sessions
// @Param   id     query    string  true        "The id ( owner/name ) of the access request"
// @Param   body    body   controllers.AccessReviewForm  false        "The comment"
// @Success 200 {object} controllers.Response The Response object
// @router /revoke-access-request [post]
func (c *ApiController) RevokeAccessRequest() {
	request := c.getAccessRequestToReview()
	if request == nil {
		return
	}
	form, ok := c.getAccessReviewForm()
	if !ok {
		return
	}

	err := object.RevokeAccessRequest(request, getUserId(c.GetSessionUser()), form.Comment)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	go object.CheckLiveSessions()
	c.ResponseOk(request)
}

// DeleteAccessRequest
// @Title DeleteAccessRequest
// @Tag Access Request API
// @Description delete an access request, leaving the permission of an approved one in place
// @Param   body    body   object.AccessRequest  true        "The access request"
// @Success 200 {object} controllers.Response The Response object
// @router /delete-access-request [post]
func (c *ApiController) DeleteAccessRequest() {
	var request object.AccessRequest
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &request)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(request.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.DeleteAccessRequest(&request))
	c.ServeJSON()
}
