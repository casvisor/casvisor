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
	"github.com/beego/beego/utils/pagination"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/recording"
	"github.com/casvisor/casvisor/terminal"
	"github.com/casvisor/casvisor/util"
)

// GetCommands
// @Title GetCommands
// @Tag Command API
// @Description get the commands run in SSH sessions, by page
// @Param   owner     query    string  true        "The owner of the commands"
// @Param   pageSize  query    string  true        "The size of each page"
// @Param   p         query    string  true        "The number of the page"
// @Success 200 {object} object.Command The Response object
// @router /get-commands [get]
func (c *ApiController) GetCommands() {
	owner := c.Input().Get("owner")
	field := c.Input().Get("field")
	value := c.Input().Get("value")
	sortField := c.Input().Get("sortField")
	sortOrder := c.Input().Get("sortOrder")
	limit := util.ParseInt(c.Input().Get("pageSize"))
	if limit <= 0 {
		limit = 10
	}

	count, err := object.GetCommandCount(owner, field, value)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	paginator := pagination.SetPaginator(c.Ctx, limit, count)
	commands, err := object.GetPaginationCommands(owner, paginator.Offset(), limit, field, value, sortField, sortOrder)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk(commands, paginator.Nums())
}

// GetSessionMonitor
// @Title GetSessionMonitor
// @Tag Session API
// @Description watch a connected session live over a websocket, read only
// @Param   id     query    string  true        "The id of the session"
// @router /get-session-monitor [get]
func (c *ApiController) GetSessionMonitor() {
	c.EnableRender = false
	ws, err := nativeUpgrader.Upgrade(c.Ctx.ResponseWriter, c.Ctx.Request, nil)
	if err != nil {
		c.ResponseError("WebSocket upgrade failed:", err)
		return
	}
	stream := newWsStream(ws)

	sessionId := c.Input().Get("id")
	recorder := terminal.GetRecorder(sessionId)
	if recorder == nil {
		if rec := recording.Get(sessionId); rec != nil {
			c.auditMonitor(sessionId, rec.Header.Protocol)
			c.monitorRecording(ws, stream, rec)
			return
		}

		message := "the session is not connected, or it is not an interactive session"
		_ = stream.WriteJSON(terminalMessage{Type: "error", Code: SessionNotFound, Message: message})
		stream.Close(SessionNotFound, message)
		return
	}

	history, cols, rows, viewer := recorder.Watch()
	defer recorder.Unwatch(viewer)

	c.auditMonitor(sessionId, "SSH")

	go func() {
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				recorder.Unwatch(viewer)
				return
			}
		}
	}()

	_ = stream.WriteJSON(terminalMessage{Type: "resize", Cols: cols, Rows: rows})
	_, _ = stream.Write(history)
	_ = stream.WriteJSON(terminalMessage{Type: "connected"})

	for event := range viewer.Events {
		if event.Data != nil {
			_, err = stream.Write(event.Data)
		} else {
			err = stream.WriteJSON(terminalMessage{Type: "resize", Cols: event.Cols, Rows: event.Rows})
		}
		if err != nil {
			return
		}
	}

	stream.Close(Normal, "The session has ended")
}

func (c *ApiController) auditMonitor(sessionId string, method string) {
	user := c.GetSessionUser()
	object.AddAuditRecord(&object.AuditEvent{
		Organization: user.Owner,
		User:         user.Name,
		ClientIp:     c.getClientIp(),
		UserAgent:    c.getUserAgent(),
		Method:       method,
		Action:       "session-monitor",
		Detail:       sessionId,
	})
}
