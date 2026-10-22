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
	"github.com/casvisor/casvisor/util"
)

// AuditEvent is something done inside a session, like a command run through the
// SSH gateway or a file moved over SFTP, which no API call records by itself.
type AuditEvent struct {
	Organization string
	User         string
	ClientIp     string
	UserAgent    string
	Method       string
	Action       string
	Asset        string
	Detail       string
}

func AddAuditRecord(event *AuditEvent) {
	record := &Record{
		Name:         util.GenerateId(),
		CreatedTime:  util.GetCurrentTime(),
		Organization: event.Organization,
		ClientIp:     event.ClientIp,
		UserAgent:    truncate(event.UserAgent, 100),
		User:         event.User,
		Method:       event.Method,
		RequestUri:   truncate(event.Detail, 1000),
		Action:       event.Action,
		Object:       event.Asset,
		Response:     "{\"status\":\"ok\",\"msg\":\"\"}",
	}
	AddRecord(record)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
