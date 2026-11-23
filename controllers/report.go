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
	"strconv"
	"strings"
	"time"

	"github.com/beego/beego/utils/pagination"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
)

// GetReports
// @Title GetReports
// @Tag Report API
// @Description get the saved reports, by page
// @Param   owner     query    string  true        "The owner of the reports"
// @Success 200 {array} object.Report The Response object
// @router /get-reports [get]
func (c *ApiController) GetReports() {
	owner := c.Input().Get("owner")
	if !c.checkOwner(owner) {
		return
	}
	limit := util.ParseInt(c.Input().Get("pageSize"))
	if limit <= 0 {
		limit = 100
	}
	field, value := c.Input().Get("field"), c.Input().Get("value")

	count, err := object.GetReportCount(owner, field, value)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	paginator := pagination.SetPaginator(c.Ctx, limit, count)
	reports, err := object.GetPaginationReports(owner, paginator.Offset(), limit, field, value, c.Input().Get("sortField"), c.Input().Get("sortOrder"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(reports, paginator.Nums())
}

// GetReport
// @Title GetReport
// @Tag Report API
// @Description get a saved report
// @Param   id     query    string  true        "The id ( owner/name ) of the report"
// @Success 200 {object} object.Report The Response object
// @router /get-report [get]
func (c *ApiController) GetReport() {
	id := c.Input().Get("id")
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
	if !c.checkOwner(owner) {
		return
	}

	report, err := object.GetReport(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(report)
}

// UpdateReport
// @Title UpdateReport
// @Tag Report API
// @Description update a saved report
// @Param   id     query    string  true        "The id ( owner/name ) of the report"
// @Param   body    body   object.Report  true        "The details of the report"
// @Success 200 {object} controllers.Response The Response object
// @router /update-report [post]
func (c *ApiController) UpdateReport() {
	var report object.Report
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &report)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	id := c.Input().Get("id")
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
	if !c.checkOwner(owner) || !c.checkOwner(report.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.UpdateReport(id, &report))
	c.ServeJSON()
}

// AddReport
// @Title AddReport
// @Tag Report API
// @Description add a saved report
// @Param   body    body   object.Report  true        "The details of the report"
// @Success 200 {object} controllers.Response The Response object
// @router /add-report [post]
func (c *ApiController) AddReport() {
	var report object.Report
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &report)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(report.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.AddReport(&report))
	c.ServeJSON()
}

// DeleteReport
// @Title DeleteReport
// @Tag Report API
// @Description delete a saved report
// @Param   body    body   object.Report  true        "The report"
// @Success 200 {object} controllers.Response The Response object
// @router /delete-report [post]
func (c *ApiController) DeleteReport() {
	var report object.Report
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &report)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(report.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.DeleteReport(&report))
	c.ServeJSON()
}

// RunReport
// @Title RunReport
// @Tag Report API
// @Description make the report now and email it as an Excel workbook with its webhooks of the Email type
// @Param   id     query    string  true        "The id ( owner/name ) of the report"
// @Success 200 {object} controllers.Response The Response object
// @router /run-report [post]
func (c *ApiController) RunReport() {
	id := c.Input().Get("id")
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
	if !c.checkOwner(owner) {
		return
	}

	err := object.RunReport(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk()
}

// getReportOptions reads the report of the query: a saved report by its id, or the options in the query.
func (c *ApiController) getReportOptions() (*object.ReportOptions, bool) {
	if id := c.Input().Get("id"); id != "" {
		owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
		if !c.checkOwner(owner) {
			return nil, false
		}
		report, err := object.GetReport(id)
		if err != nil {
			c.ResponseError(err.Error())
			return nil, false
		}
		if report == nil {
			c.ResponseError(fmt.Sprintf("the report: %s does not exist", id))
			return nil, false
		}
		options := report.GetOptions()
		if lang := c.Input().Get("lang"); lang != "" {
			options.Language = lang
		}
		return options, true
	}

	options := &object.ReportOptions{
		Owner:         c.Input().Get("owner"),
		Period:        c.Input().Get("period"),
		StartDate:     c.Input().Get("startDate"),
		EndDate:       c.Input().Get("endDate"),
		TimeZone:      c.Input().Get("timeZone"),
		WorkStartTime: c.Input().Get("workStartTime"),
		WorkEndTime:   c.Input().Get("workEndTime"),
		Language:      c.Input().Get("lang"),
	}
	if !c.checkOwner(options.Owner) {
		return nil, false
	}
	for _, weekday := range strings.Split(c.Input().Get("weekdays"), ",") {
		if weekday = strings.TrimSpace(weekday); weekday != "" {
			n, err := strconv.Atoi(weekday)
			if err != nil {
				c.ResponseError(fmt.Sprintf("invalid weekday: %s", weekday))
				return nil, false
			}
			options.Weekdays = append(options.Weekdays, n)
		}
	}
	for _, section := range strings.Split(c.Input().Get("sections"), ",") {
		if section = strings.TrimSpace(section); section != "" {
			options.Sections = append(options.Sections, section)
		}
	}
	if options.Period == "" {
		options.Period = object.ReportPeriodThisWeek
	}
	return options, true
}

// GetReportData
// @Title GetReportData
// @Tag Report API
// @Description count the sign-ins, sessions, commands, file transfers, approvals, account changes and asset and permission changes of an organization in a period
// @Param   id             query    string  false       "The id ( owner/name ) of a saved report, instead of the options below"
// @Param   owner          query    string  false       "The organization"
// @Param   period         query    string  false       "today, yesterday, this-week (default), last-week, this-month, last-month, last-7-days, last-30-days or custom"
// @Param   startDate      query    string  false       "The first day of a custom period, such as 2026-11-01"
// @Param   endDate        query    string  false       "The last day of a custom period, such as 2026-11-30"
// @Param   timeZone       query    string  false       "The time zone of the period and the working hours, such as Asia/Shanghai; the server's by default"
// @Param   weekdays       query    string  false       "The working days, from 0 (Sunday) to 6, separated by commas; 1,2,3,4,5 by default"
// @Param   workStartTime  query    string  false       "The start of the working hours, 09:00 by default"
// @Param   workEndTime    query    string  false       "The end of the working hours, 18:00 by default"
// @Param   sections       query    string  false       "The parts to count, separated by commas: login, session, command, file, approval, account, asset; all by default"
// @Success 200 {object} object.ReportData The Response object
// @router /get-report-data [get]
func (c *ApiController) GetReportData() {
	options, ok := c.getReportOptions()
	if !ok {
		return
	}

	data, err := object.GetReportData(options, time.Now())
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(data)
}

// DownloadReport
// @Title DownloadReport
// @Tag Report API
// @Description download the report as an Excel workbook, with a sheet for each part; it takes the parameters of get-report-data
// @Param   id     query    string  false       "The id ( owner/name ) of a saved report, instead of the options of get-report-data"
// @Param   owner  query    string  false       "The organization"
// @Param   lang   query    string  false       "en or zh, the language of the workbook"
// @Success 200 {file} file The xlsx file
// @router /download-report [get]
func (c *ApiController) DownloadReport() {
	options, ok := c.getReportOptions()
	if !ok {
		return
	}
	if options.Language != "zh" {
		options.Language = "en"
	}

	data, fileName, _, err := object.GetReportFile(options, time.Now())
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	organization, userName := options.Owner, ""
	if user := c.GetSessionUser(); user != nil {
		organization, userName = user.Owner, user.Name
	}
	object.AddAuditRecord(&object.AuditEvent{
		Organization: organization,
		User:         userName,
		ClientIp:     c.getClientIp(),
		UserAgent:    c.getUserAgent(),
		Method:       "Export",
		Action:       "export-report",
		Detail:       fmt.Sprintf("Exported the report of %s to %s, filter: %s", options.Owner, fileName, c.Ctx.Request.URL.RawQuery),
	})

	c.EnableRender = false
	c.Ctx.Output.Header("Content-Type", object.XlsxContentType)
	c.Ctx.Output.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
	c.Ctx.Output.Header("Content-Length", strconv.Itoa(len(data)))
	c.Ctx.Output.Header("Cache-Control", "no-store")
	c.Ctx.ResponseWriter.WriteHeader(200)
	_, _ = c.Ctx.ResponseWriter.Write(data)
}
