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
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
)

type exportQuery struct {
	owner     string
	field     string
	value     string
	sortField string
	sortOrder string
}

func (c *ApiController) getExportQuery() (*exportQuery, bool) {
	query := &exportQuery{
		owner:     c.Input().Get("owner"),
		field:     c.Input().Get("field"),
		value:     c.Input().Get("value"),
		sortField: c.Input().Get("sortField"),
		sortOrder: c.Input().Get("sortOrder"),
	}
	if query.sortField != "" && !util.FilterField(query.sortField) {
		c.ResponseError(fmt.Sprintf("invalid sort field: %s", query.sortField))
		return nil, false
	}
	return query, true
}

// csvCell keeps a spreadsheet from running a cell as a formula.
func csvCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// exportTable streams the rows as a CSV file, with a BOM so that Excel reads it as UTF-8, or as an
// Excel workbook when the query has format=xlsx, and records the export.
func (c *ApiController) exportTable(name string, header []string, iterate func(write func(row []string) error) error) {
	c.EnableRender = false
	isXlsx := c.Input().Get("format") == "xlsx"
	fileName := fmt.Sprintf("%s-%s.csv", name, time.Now().Format("20060102-150405"))
	var write func(row []string) error
	var finish func() error
	if isXlsx {
		fileName = strings.TrimSuffix(fileName, ".csv") + ".xlsx"
		stream, err := object.NewXlsxStream(name, header)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}
		write = stream.WriteRow
		finish = func() error {
			return stream.Close(c.Ctx.ResponseWriter)
		}
		c.Ctx.Output.Header("Content-Type", object.XlsxContentType)
	} else {
		c.Ctx.Output.Header("Content-Type", "text/csv; charset=utf-8")
	}
	c.Ctx.Output.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))

	if !isXlsx {
		c.Ctx.ResponseWriter.WriteHeader(200)
		_, _ = c.Ctx.ResponseWriter.Write([]byte("\xEF\xBB\xBF"))
		writer := csv.NewWriter(c.Ctx.ResponseWriter)
		_ = writer.Write(header)
		write = func(row []string) error {
			for i := range row {
				row[i] = csvCell(row[i])
			}
			return writer.Write(row)
		}
		finish = func() error {
			writer.Flush()
			return writer.Error()
		}
	}

	count := 0
	err := iterate(func(row []string) error {
		count++
		return write(row)
	})
	if err != nil {
		logs.Error("failed to export the %s: %s", name, err.Error())
	}
	if isXlsx {
		c.Ctx.ResponseWriter.WriteHeader(200)
	}
	err = finish()
	if err != nil {
		logs.Error("failed to export the %s: %s", name, err.Error())
	}

	organization, userName := c.Input().Get("owner"), ""
	if user := c.GetSessionUser(); user != nil {
		organization, userName = user.Owner, user.Name
	}
	detail := fmt.Sprintf("Exported %d rows to %s", count, fileName)
	if query := c.Ctx.Request.URL.RawQuery; query != "" {
		detail += ", filter: " + query
	}
	object.AddAuditRecord(&object.AuditEvent{
		Organization: organization,
		User:         userName,
		ClientIp:     c.getClientIp(),
		UserAgent:    c.getUserAgent(),
		Method:       "Export",
		Action:       "export-" + name,
		Detail:       detail,
	})
}

// ExportSessions
// @Title ExportSessions
// @Tag Session API
// @Description download the sessions that match the filter of the session list as CSV, or as an Excel workbook with format=xlsx, 100,000 at most
// @Param   owner     query    string  true        "The owner of the sessions"
// @Param   status    query    string  false       "connected or disconnected"
// @Param   field     query    string  false       "The field to filter on"
// @Param   value     query    string  false       "The value of the filter"
// @Param   format    query    string  false       "csv (default) or xlsx"
// @Success 200 {string} string The CSV or xlsx file
// @router /export-sessions [get]
func (c *ApiController) ExportSessions() {
	query, ok := c.getExportQuery()
	if !ok {
		return
	}
	status := c.Input().Get("status")

	header := []string{"ID", "Asset", "Protocol", "Mode", "User", "Client IP", "Client IP location", "User agent", "Status", "Start time", "End time", "Duration (s)", "Commands", "Code", "Message"}
	c.exportTable("sessions", header, func(write func(row []string) error) error {
		return object.IterateSessions(query.owner, status, query.field, query.value, query.sortField, query.sortOrder, func(s *object.Session) error {
			duration := ""
			if start, err := time.Parse(time.RFC3339, s.StartTime); err == nil {
				if end, err := time.Parse(time.RFC3339, s.EndTime); err == nil {
					duration = strconv.FormatInt(int64(end.Sub(start).Seconds()), 10)
				}
			}
			return write([]string{
				s.GetId(), s.Asset, s.Protocol, s.Mode, s.Creator, s.ClientIp, s.ClientIpDesc, s.UserAgent, s.Status,
				s.StartTime, s.EndTime, duration, strconv.FormatInt(s.CommandCount, 10), strconv.Itoa(s.Code), s.Message,
			})
		})
	})
}

// ExportCommands
// @Title ExportCommands
// @Tag Command API
// @Description download the commands that match the filter of the command list as CSV, or as an Excel workbook with format=xlsx, 100,000 at most
// @Param   owner     query    string  true        "The owner of the commands"
// @Param   field     query    string  false       "The field to filter on"
// @Param   value     query    string  false       "The value of the filter"
// @Param   format    query    string  false       "csv (default) or xlsx"
// @Success 200 {string} string The CSV or xlsx file
// @router /export-commands [get]
func (c *ApiController) ExportCommands() {
	query, ok := c.getExportQuery()
	if !ok {
		return
	}

	header := []string{"Time", "Command", "Action", "Rule", "Reviewer", "Error", "User", "Operator", "Asset", "Client IP", "Session"}
	c.exportTable("commands", header, func(write func(row []string) error) error {
		return object.IterateCommands(query.owner, query.field, query.value, query.sortField, query.sortOrder, func(command *object.Command) error {
			return write([]string{command.CreatedTime, command.Text, command.Action, command.Rule, command.Reviewer, command.Error, command.User, command.Operator, command.Asset, command.ClientIp, command.Session})
		})
	})
}

// ExportRecords
// @Title ExportRecords
// @Tag Record API
// @Description download the records that match the filter of the record list as CSV, or as an Excel workbook with format=xlsx, 100,000 at most
// @Param   owner     query    string  true        "The owner of the records"
// @Param   field     query    string  false       "The field to filter on"
// @Param   value     query    string  false       "The value of the filter"
// @Param   format    query    string  false       "csv (default) or xlsx"
// @Success 200 {string} string The CSV or xlsx file
// @router /export-records [get]
func (c *ApiController) ExportRecords() {
	query, ok := c.getExportQuery()
	if !ok {
		return
	}

	header := []string{"ID", "Name", "Time", "Organization", "User", "Client IP", "User agent", "Method", "Request URI", "Action", "Language", "Object", "Response"}
	c.exportTable("records", header, func(write func(row []string) error) error {
		return object.IterateRecords(query.owner, query.field, query.value, query.sortField, query.sortOrder, func(record *object.Record) error {
			return write([]string{
				strconv.Itoa(record.Id), record.Name, record.CreatedTime, record.Organization, record.User, record.ClientIp, record.UserAgent,
				record.Method, record.RequestUri, record.Action, record.Language, record.Object, record.Response,
			})
		})
	})
}
