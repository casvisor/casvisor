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

package object

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/casvisor/casvisor/syslog"
)

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Now()
	}
	return t
}

func getMillis(s string) string {
	return strconv.FormatInt(parseTime(s).UnixMilli(), 10)
}

func forwardRecord(record *Record) {
	var resp Response
	_ = json.Unmarshal([]byte(record.Response), &resp)

	severity := syslog.SeverityInfo
	if resp.Status == "error" {
		severity = syslog.SeverityNotice
	}

	syslog.Send(syslog.Event{
		Type:     "record",
		Name:     record.Action,
		Severity: severity,
		Time:     parseTime(record.CreatedTime),
		Fields: []syslog.Field{
			{Key: "id", CefKey: "externalId", Value: strconv.Itoa(record.Id)},
			{Key: "organization", CefKey: "cs1", Value: record.Organization},
			{Key: "user", CefKey: "suser", Value: record.User},
			{Key: "clientIp", CefKey: "src", Value: record.ClientIp},
			{Key: "userAgent", CefKey: "requestClientApplication", Value: record.UserAgent},
			{Key: "method", CefKey: "requestMethod", Value: record.Method},
			{Key: "requestUri", CefKey: "request", Value: record.RequestUri},
			{Key: "action", CefKey: "act", Value: record.Action},
			{Key: "object", CefKey: "cs2", Value: record.Object},
			{Key: "status", CefKey: "outcome", Value: resp.Status},
			{Key: "message", CefKey: "msg", Value: resp.Msg},
		},
	})
}

func forwardCommand(command *Command) {
	severity := syslog.SeverityInfo
	name := "Command"
	if command.Action == CommandActionBlock {
		severity = syslog.SeverityWarning
		name = "Command blocked"
	} else if command.Action == CommandActionAlert {
		severity = syslog.SeverityNotice
		name = "Command alerted"
	} else if command.Action == CommandActionReview {
		severity = syslog.SeverityNotice
		name = "Command reviewed"
		if command.Error != "" {
			severity = syslog.SeverityWarning
			name = "Command not approved"
		}
	}

	syslog.Send(syslog.Event{
		Type:     "command",
		Name:     name,
		Severity: severity,
		Time:     parseTime(command.CreatedTime),
		Fields: []syslog.Field{
			{Key: "organization", CefKey: "cs1", Value: command.Owner},
			{Key: "user", CefKey: "suser", Value: command.User},
			{Key: "clientIp", CefKey: "src", Value: command.ClientIp},
			{Key: "asset", CefKey: "dhost", Value: command.Asset},
			{Key: "session", CefKey: "cs2", Value: command.Session},
			{Key: "command", CefKey: "msg", Value: command.Text},
			{Key: "action", CefKey: "act", Value: command.Action},
			{Key: "rule", CefKey: "cs3", Value: command.Rule},
			{Key: "reviewer", CefKey: "duser", Value: command.Reviewer},
		},
	})
}

func forwardSession(session *Session, started bool) {
	event := syslog.Event{
		Type:     "session-start",
		Name:     "Session started",
		Severity: syslog.SeverityInfo,
		Time:     parseTime(session.StartTime),
		Fields: []syslog.Field{
			{Key: "organization", CefKey: "cs1", Value: session.Owner},
			{Key: "session", CefKey: "cs2", Value: session.GetId()},
			{Key: "user", CefKey: "suser", Value: session.Creator},
			{Key: "clientIp", CefKey: "src", Value: session.ClientIp},
			{Key: "userAgent", CefKey: "requestClientApplication", Value: session.UserAgent},
			{Key: "asset", CefKey: "dhost", Value: session.Asset},
			{Key: "protocol", CefKey: "app", Value: session.Protocol},
			{Key: "mode", CefKey: "cs3", Value: session.Mode},
			{Key: "startTime", CefKey: "start", Value: session.StartTime, CefValue: getMillis(session.StartTime)},
		},
	}

	if !started {
		event.Type = "session-end"
		event.Name = "Session ended"
		event.Time = parseTime(session.EndTime)
		duration := int64(parseTime(session.EndTime).Sub(parseTime(session.StartTime)).Seconds())
		event.Fields = append(event.Fields,
			syslog.Field{Key: "endTime", CefKey: "end", Value: session.EndTime, CefValue: getMillis(session.EndTime)},
			syslog.Field{Key: "duration", CefKey: "cn1", Value: strconv.FormatInt(duration, 10)},
			syslog.Field{Key: "commandCount", CefKey: "cn2", Value: strconv.FormatInt(session.CommandCount, 10)},
			syslog.Field{Key: "code", CefKey: "cn3", Value: strconv.Itoa(session.Code)},
			syslog.Field{Key: "message", CefKey: "msg", Value: session.Message},
		)
	}

	syslog.Send(event)
}
