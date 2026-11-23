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

// GetCommandReviews
// @Title GetCommandReviews
// @Tag Command Rule API
// @Description get the commands that Review rules hold, or held, for review
// @Param   owner     query    string  true        "The owner of the reviews"
// @Param   status    query    string  false       "Pending, Approved, Used, Rejected, Canceled or Expired"
// @Success 200 {array} object.CommandReview The Response object
// @router /get-command-reviews [get]
func (c *ApiController) GetCommandReviews() {
	owner := c.Input().Get("owner")
	if !c.checkOwner(owner) {
		return
	}
	status := c.Input().Get("status")
	limit, err := strconv.Atoi(c.Input().Get("pageSize"))
	if err != nil || limit <= 0 {
		limit = 100
	}

	count, err := object.GetCommandReviewCount(owner, status, c.Input().Get("field"), c.Input().Get("value"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	paginator := pagination.SetPaginator(c.Ctx, limit, count)
	reviews, err := object.GetPaginationCommandReviews(owner, status, paginator.Offset(), limit, c.Input().Get("field"), c.Input().Get("value"), c.Input().Get("sortField"), c.Input().Get("sortOrder"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(reviews, paginator.Nums())
}

type reviewCommandForm struct {
	Id      string `json:"id"`
	Approve bool   `json:"approve"`
	Comment string `json:"comment"`
}

// ReviewCommand
// @Title ReviewCommand
// @Tag Command Rule API
// @Description approve or reject a command held for review; an approved command runs right away in the session that waits for it
// @Param   body    body   controllers.reviewCommandForm  true        "The review id, approve or not, and a comment shown to the user"
// @Success 200 {object} object.CommandReview The Response object
// @router /review-command [post]
func (c *ApiController) ReviewCommand() {
	var form reviewCommandForm
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &form)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(form.Id)
	if !c.checkOwner(owner) {
		return
	}

	review, err := object.ReviewCommand(form.Id, c.GetSessionUser(), form.Approve, form.Comment)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(review)
}
