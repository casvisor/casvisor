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
	"cmp"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/beego/beego/utils/pagination"
	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/recording"
	"github.com/casvisor/casvisor/terminal"
	"github.com/casvisor/casvisor/util"
	"github.com/gorilla/websocket"
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
// @Description watch a connected session live over a websocket; with control, also type into an SSH, Telnet or Kubernetes terminal
// @Param   id     query    string  true        "The id of the session"
// @Param   control     query    bool  false        "true to take part in the session"
// @router /get-session-monitor [get]
func (c *ApiController) GetSessionMonitor() {
	c.serveSessionMonitor(c.Input().Get("id"), c.Input().Get("control") == "true", nil)
}

func (c *ApiController) serveSessionMonitor(sessionId string, isControl bool, ctx context.Context) {
	c.EnableRender = false
	ws, err := nativeUpgrader.Upgrade(c.Ctx.ResponseWriter, c.Ctx.Request, nil)
	if err != nil {
		c.ResponseError("WebSocket upgrade failed:", err)
		return
	}
	stream := newWsStream(ws)
	if ctx != nil {
		defer context.AfterFunc(ctx, func() {
			stream.Close(ForcedDisconnect, "The shared link has expired or been revoked")
		})()
	}

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

	user := c.GetSessionUser()
	if isControl && user != nil {
		c.auditMonitorAction(sessionId, "SSH", "session-control")
		userId := util.GetIdFromOwnerAndName(user.Owner, user.Name)
		recorder.Announce(fmt.Sprintf("The administrator %s has joined this session and can type in it", userId))
		defer recorder.Announce(fmt.Sprintf("The administrator %s has left this session", userId))
	} else {
		isControl = false
		c.auditMonitor(sessionId, "SSH")
	}

	go func() {
		for {
			messageType, data, err := ws.ReadMessage()
			if err != nil {
				recorder.Unwatch(viewer)
				return
			}
			if !isControl || messageType != websocket.TextMessage {
				continue
			}

			var message terminalMessage
			if json.Unmarshal(data, &message) == nil && message.Type == "data" {
				recorder.InputAs(user, []byte(message.Data))
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
	c.auditMonitorAction(sessionId, method, "session-monitor")
}

func (c *ApiController) auditMonitorAction(sessionId string, method string, action string) {
	user := c.GetSessionUser()
	if user == nil {
		return
	}
	object.AddAuditRecord(&object.AuditEvent{
		Organization: user.Owner,
		User:         user.Name,
		ClientIp:     c.getClientIp(),
		UserAgent:    c.getUserAgent(),
		Method:       method,
		Action:       action,
		Detail:       sessionId,
	})
}

func (c *ApiController) shareAuthErr(owner, creator string) string {
	if user := c.GetSessionUser(); user != nil {
		if user.Owner != "built-in" && user.Owner != owner {
			return fmt.Sprintf("you cannot manage the resources of the organization: %s", owner)
		}
		if !c.isAdmin() && getUserId(user) != creator {
			return "you have no permission to share this session"
		}
		return ""
	}
	id, sec, ok := c.Ctx.Request.BasicAuth()
	wId, wSec := conf.GetConfigString("clientId"), conf.GetConfigString("clientSecret")
	if ok && wId != "" && wSec != "" && subtle.ConstantTimeCompare([]byte(id), []byte(wId)) == 1 && subtle.ConstantTimeCompare([]byte(sec), []byte(wSec)) == 1 {
		return ""
	}
	return "please sign in first"
}

func (c *ApiController) verifySharedViewer() (*object.SessionShare, context.Context, string) {
	user := c.GetSessionUser()
	if user == nil {
		return nil, nil, "please sign in first"
	}
	share, ctx, err := object.VerifySessionShare(c.Input().Get("token"), c.Input().Get("code"), user.Owner)
	if err != nil {
		return nil, nil, err.Error()
	}
	if terminal.GetRecorder(share.SessionId) == nil && recording.Get(share.SessionId) == nil {
		object.CloseSessionShareBySession(share.SessionId)
		return nil, nil, "the session is not connected, or it is not an interactive session"
	}
	return share, ctx, ""
}

func (c *ApiController) CreateSessionShare() {
	s, err := object.GetConnSession(c.Input().Get("id"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if s == nil {
		c.ResponseError("the session does not exist")
		return
	}
	if msg := c.shareAuthErr(s.Owner, s.Creator); msg != "" {
		c.ResponseError(msg)
		return
	}
	if terminal.GetRecorder(s.GetId()) == nil && recording.Get(s.GetId()) == nil {
		c.ResponseError("the session is not connected, or it is not an interactive session")
		return
	}
	if share, err := object.CreateSessionShare(s, time.Duration(util.ParseInt(c.Input().Get("ttl")))*time.Second); err != nil {
		c.ResponseError(err.Error())
	} else {
		c.ResponseOk(share)
	}
}

func (c *ApiController) RevokeSessionShare() {
	var req map[string]string
	_ = json.Unmarshal(c.Ctx.Input.RequestBody, &req)
	msg := "invalid or expired session share"
	if object.RevokeSessionShare(cmp.Or(req["token"], c.Input().Get("token")), func(owner, creator string) bool {
		msg = c.shareAuthErr(owner, creator)
		return msg == ""
	}) {
		c.ResponseOk()
	} else {
		c.ResponseError(msg)
	}
}

func (c *ApiController) GetSessionShare() {
	if share, _, msg := c.verifySharedViewer(); msg != "" {
		c.ResponseError(msg)
	} else {
		c.ResponseOk(share)
	}
}

func (c *ApiController) WatchSharedSession() {
	if share, ctx, msg := c.verifySharedViewer(); msg != "" {
		http.Error(c.Ctx.ResponseWriter, msg, http.StatusForbidden)
	} else {
		c.serveSessionMonitor(share.SessionId, false, ctx)
	}
}
