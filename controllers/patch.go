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

type patchJobRequest struct {
	Owner   string   `json:"owner"`
	Assets  []string `json:"assets"`
	Type    string   `json:"type"`
	Updates []string `json:"updates"`
}

// GetPatchStatuses
// @Title GetPatchStatuses
// @Tag Patch API
// @Description get the pending updates of every SSH asset, from the last scan
// @Param   owner     query    string  true        "The owner of the assets"
// @Success 200 {object} object.PatchStatus The Response object
// @router /get-patch-statuses [get]
func (c *ApiController) GetPatchStatuses() {
	owner := c.Input().Get("owner")

	statuses, err := object.GetPatchStatuses(owner)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk(statuses)
}

// GetPatchStatus
// @Title GetPatchStatus
// @Tag Patch API
// @Description get the pending updates of an SSH asset, from the last scan
// @Param   id     query    string  true        "The id ( owner/name ) of the asset"
// @Success 200 {object} object.PatchStatus The Response object
// @router /get-patch-status [get]
func (c *ApiController) GetPatchStatus() {
	id := c.Input().Get("id")

	status, err := object.GetPatchStatus(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk(status)
}

// GetPatchJobs
// @Title GetPatchJobs
// @Tag Patch API
// @Description get the scan, install and restart jobs, by page
// @Param   owner     query    string  true        "The owner of the jobs"
// @Param   pageSize  query    string  true        "The size of each page"
// @Param   p         query    string  true        "The number of the page"
// @Success 200 {object} object.PatchJob The Response object
// @router /get-patch-jobs [get]
func (c *ApiController) GetPatchJobs() {
	owner := c.Input().Get("owner")
	field := c.Input().Get("field")
	value := c.Input().Get("value")
	sortField := c.Input().Get("sortField")
	sortOrder := c.Input().Get("sortOrder")
	limit := util.ParseInt(c.Input().Get("pageSize"))
	if limit <= 0 {
		limit = 10
	}

	count, err := object.GetPatchJobCount(owner, field, value)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	paginator := pagination.SetPaginator(c.Ctx, limit, count)
	jobs, err := object.GetPaginationPatchJobs(owner, paginator.Offset(), limit, field, value, sortField, sortOrder)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk(jobs, paginator.Nums())
}

// GetPatchJob
// @Title GetPatchJob
// @Tag Patch API
// @Description get a job with its output, live while it runs
// @Param   id     query    string  true        "The id ( owner/name ) of the job"
// @Success 200 {object} object.PatchJob The Response object
// @router /get-patch-job [get]
func (c *ApiController) GetPatchJob() {
	id := c.Input().Get("id")

	job, err := object.GetPatchJob(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk(job)
}

// StartPatchJob
// @Title StartPatchJob
// @Tag Patch API
// @Description scan for updates, install updates or restart, on one or more SSH assets
// @Param   body    body   controllers.patchJobRequest  true        "The owner, the asset names, the type (Scan, Install or Reboot) and, to install some of the updates of one asset, their ids"
// @Success 200 {object} object.PatchJob The Response object
// @router /start-patch-job [post]
func (c *ApiController) StartPatchJob() {
	var request patchJobRequest
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &request)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	if !c.checkOwner(request.Owner) {
		return
	}
	if len(request.Assets) == 0 {
		c.ResponseError("no assets are given")
		return
	}
	if len(request.Updates) > 0 && (request.Type != object.PatchJobInstall || len(request.Assets) != 1) {
		c.ResponseError("updates can only be picked when installing on one asset")
		return
	}

	user := getUserId(c.GetSessionUser())
	jobs := []*object.PatchJob{}
	errors := []string{}
	for _, name := range request.Assets {
		job, err := object.StartPatchJob(util.GetIdFromOwnerAndName(request.Owner, name), request.Type, request.Updates, user)
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}
		jobs = append(jobs, job)
	}

	if len(jobs) == 0 {
		c.ResponseError(fmt.Sprintf("%v", errors[0]))
		return
	}

	c.ResponseOk(jobs, errors)
}
