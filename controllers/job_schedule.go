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

// GetJobSchedules
// @Title GetJobSchedules
// @Tag Job API
// @Description get the job schedules, by page
// @Param   owner     query    string  true        "The owner of the job schedules"
// @Success 200 {array} object.JobSchedule The Response object
// @router /get-job-schedules [get]
func (c *ApiController) GetJobSchedules() {
	owner := c.Input().Get("owner")
	if !c.checkOwner(owner) {
		return
	}
	limit := util.ParseInt(c.Input().Get("pageSize"))
	if limit <= 0 {
		limit = 100
	}
	field, value := c.Input().Get("field"), c.Input().Get("value")

	count, err := object.GetJobScheduleCount(owner, field, value)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	paginator := pagination.SetPaginator(c.Ctx, limit, count)
	schedules, err := object.GetPaginationJobSchedules(owner, paginator.Offset(), limit, field, value, c.Input().Get("sortField"), c.Input().Get("sortOrder"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(schedules, paginator.Nums())
}

// GetJobSchedule
// @Title GetJobSchedule
// @Tag Job API
// @Description get a job schedule
// @Param   id     query    string  true        "The id ( owner/name ) of the job schedule"
// @Success 200 {object} object.JobSchedule The Response object
// @router /get-job-schedule [get]
func (c *ApiController) GetJobSchedule() {
	id := c.Input().Get("id")
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
	if !c.checkOwner(owner) {
		return
	}

	schedule, err := object.GetJobSchedule(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(schedule)
}

// UpdateJobSchedule
// @Title UpdateJobSchedule
// @Tag Job API
// @Description update a job schedule; its jobs run as the administrator who saves it
// @Param   id     query    string  true        "The id ( owner/name ) of the job schedule"
// @Param   body    body   object.JobSchedule  true        "The details of the job schedule"
// @Success 200 {object} controllers.Response The Response object
// @router /update-job-schedule [post]
func (c *ApiController) UpdateJobSchedule() {
	var schedule object.JobSchedule
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &schedule)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	id := c.Input().Get("id")
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
	if !c.checkOwner(owner) || !c.checkOwner(schedule.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.UpdateJobSchedule(id, &schedule, c.GetSessionUser()))
	c.ServeJSON()
}

// AddJobSchedule
// @Title AddJobSchedule
// @Tag Job API
// @Description add a job schedule; its jobs run as the administrator who saves it
// @Param   body    body   object.JobSchedule  true        "The details of the job schedule"
// @Success 200 {object} controllers.Response The Response object
// @router /add-job-schedule [post]
func (c *ApiController) AddJobSchedule() {
	var schedule object.JobSchedule
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &schedule)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(schedule.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.AddJobSchedule(&schedule, c.GetSessionUser()))
	c.ServeJSON()
}

// DeleteJobSchedule
// @Title DeleteJobSchedule
// @Tag Job API
// @Description delete a job schedule; the jobs it started are kept
// @Param   body    body   object.JobSchedule  true        "The job schedule"
// @Success 200 {object} controllers.Response The Response object
// @router /delete-job-schedule [post]
func (c *ApiController) DeleteJobSchedule() {
	var schedule object.JobSchedule
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &schedule)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(schedule.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.DeleteJobSchedule(&schedule))
	c.ServeJSON()
}

// RunJobSchedule
// @Title RunJobSchedule
// @Tag Job API
// @Description start a job of the schedule now, unless its last job is still running
// @Param   id     query    string  true        "The id ( owner/name ) of the job schedule"
// @Success 200 {object} object.Job The Response object
// @router /run-job-schedule [post]
func (c *ApiController) RunJobSchedule() {
	id := c.Input().Get("id")
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
	if !c.checkOwner(owner) {
		return
	}

	job, err := object.RunJobSchedule(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(job)
}

// GetJobScheduleRunTimes
// @Title GetJobScheduleRunTimes
// @Tag Job API
// @Description get the next five times a cron expression fires in a time zone, to check it
// @Param   cron     query    string  true        "A cron expression of five fields, such as 0 3 * * *"
// @Param   timeZone query    string  false       "An IANA time zone, the server's when empty"
// @Success 200 {array} string The Response object
// @router /get-job-schedule-run-times [get]
func (c *ApiController) GetJobScheduleRunTimes() {
	times, err := object.GetJobScheduleRunTimes(c.Input().Get("cron"), c.Input().Get("timeZone"), 5)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(times)
}
