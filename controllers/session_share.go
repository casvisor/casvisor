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
	"time"

	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/recording"
	"github.com/casvisor/casvisor/terminal"
	"github.com/casvisor/casvisor/util"
)

type sessionShareForm struct {
	Session       string   `json:"session"`
	Users         []string `json:"users"`
	Mode          string   `json:"mode"`
	ExpireMinutes int      `json:"expireMinutes"`
}

// sessionShareInfo is what a user the share is for sees before joining: no code.
type sessionShareInfo struct {
	Owner      string `json:"owner"`
	Name       string `json:"name"`
	Asset      string `json:"asset"`
	Creator    string `json:"creator"`
	Mode       string `json:"mode"`
	ExpireTime string `json:"expireTime"`
}

// canShareSession tells whether the signed-in user may share the session: its own, or as an
// administrator of its organization.
func (c *ApiController) canShareSession(session *object.Session) bool {
	user := c.GetSessionUser()
	if user == nil || session == nil {
		return false
	}
	if session.Creator == getUserId(user) {
		return true
	}
	return object.IsAdminUser(user) && (user.Owner == "built-in" || user.Owner == session.Owner)
}

// AddSessionShare
// @Title AddSessionShare
// @Tag Session API
// @Description share a connected session of yours with other users of the organization, to watch it or, for a terminal, to type in it too; returns the share with its code
// @Param   body    body   controllers.sessionShareForm  true        "The session, the names of the users it is for (anyone of the organization when empty), View or Control, and how many minutes it lasts"
// @Success 200 {object} object.SessionShare The Response object
// @router /add-session-share [post]
func (c *ApiController) AddSessionShare() {
	var form sessionShareForm
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &form)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	session, err := object.GetConnSession(form.Session)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.canShareSession(session) {
		c.ResponseError("you can only share your own sessions")
		return
	}
	isTerminal := terminal.GetRecorder(form.Session) != nil
	if !isTerminal && recording.Get(form.Session) == nil {
		c.ResponseError("only terminal and remote desktop sessions can be shared")
		return
	}
	if form.Mode == object.ShareModeControl && !isTerminal {
		c.ResponseError("only a terminal session can be shared for typing in it, a remote desktop session only for watching")
		return
	}

	share, err := object.AddSessionShare(session, c.GetSessionUser(), form.Users, form.Mode, form.ExpireMinutes)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.auditMonitorAction(form.Session, form.Mode, "session-share")
	c.ResponseOk(share)
}

// GetSessionShares
// @Title GetSessionShares
// @Tag Session API
// @Description get the open shares of a session of yours, with their codes
// @Param   session     query    string  true        "The id of the session"
// @Success 200 {array} object.SessionShare The Response object
// @router /get-session-shares [get]
func (c *ApiController) GetSessionShares() {
	sessionId := c.Input().Get("session")
	session, err := object.GetConnSession(sessionId)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.canShareSession(session) {
		c.ResponseError("you can only see the shares of your own sessions")
		return
	}

	shares, err := object.GetSessionShares(sessionId)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(shares)
}

// RevokeSessionShare
// @Title RevokeSessionShare
// @Tag Session API
// @Description revoke a share of a session of yours; the users who joined through it are disconnected
// @Param   id     query    string  true        "The id of the share"
// @Success 200 {object} controllers.Response The Response object
// @router /revoke-session-share [post]
func (c *ApiController) RevokeSessionShare() {
	id := c.Input().Get("id")
	share, err := object.GetSessionShare(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if share == nil || !share.CanManage(c.GetSessionUser()) {
		c.ResponseError("the share does not exist, or it is not yours")
		return
	}

	err = object.RevokeSessionShare(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk()
}

// GetSessionShare
// @Title GetSessionShare
// @Tag Session API
// @Description get what a share is, for a user it is for, without its code
// @Param   id     query    string  true        "The id of the share"
// @Success 200 {object} controllers.sessionShareInfo The Response object
// @router /get-session-share [get]
func (c *ApiController) GetSessionShare() {
	share, err := object.GetSessionShare(c.Input().Get("id"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if share == nil || !share.CanJoin(c.GetSessionUser()) {
		c.ResponseError("the share does not exist, or it is not for you")
		return
	}

	c.ResponseOk(sessionShareInfo{
		Owner:      share.Owner,
		Name:       share.Name,
		Asset:      share.Asset,
		Creator:    share.Creator,
		Mode:       share.Mode,
		ExpireTime: share.ExpireTime,
	})
}

// JoinSessionShare
// @Title JoinSessionShare
// @Tag Session API
// @Description join a shared session over a websocket with the code of the share, to watch it or, when the share allows it and control is true, to type in it too
// @Param   id     query    string  true        "The id of the share"
// @Param   code     query    string  true        "The code of the share"
// @Param   control     query    bool  false        "true to type in the session"
// @router /join-session-share [get]
func (c *ApiController) JoinSessionShare() {
	c.EnableRender = false
	ws, err := c.upgradeWebSocket(wsMaxMessage)
	if err != nil {
		c.ResponseError("WebSocket upgrade failed:", err)
		return
	}
	stream := newWsStream(ws)
	fail := func(code int, message string) {
		_ = stream.WriteJSON(terminalMessage{Type: "error", Code: code, Message: message})
		stream.Close(code, message)
	}

	id := c.Input().Get("id")
	user := c.GetSessionUser()
	share, err := object.CheckSessionShare(id, user, c.Input().Get("code"))
	if err != nil {
		fail(SessionNotFound, err.Error())
		return
	}

	// the users who joined leave as soon as the share is revoked, expires or the session ends
	stop := make(chan struct{})
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if !object.IsSessionShareActive(id) {
					close(stop)
					return
				}
			}
		}
	}()

	userId := util.GetIdFromOwnerAndName(user.Owner, user.Name)
	recorder := terminal.GetRecorder(share.Session)
	if recorder == nil {
		rec := recording.Get(share.Session)
		if rec == nil {
			fail(SessionNotFound, "the shared session has ended")
			return
		}
		c.auditMonitorAction(share.Session, rec.Header.Protocol, "session-share-join")
		go func() {
			select {
			case <-stop:
				stream.Close(Normal, "The share has ended")
				_ = ws.Close()
			case <-done:
			}
		}()
		c.monitorRecording(ws, stream, rec)
		return
	}

	isControl := share.Mode == object.ShareModeControl && c.Input().Get("control") == "true"
	if isControl {
		c.auditMonitorAction(share.Session, "SSH", "session-share-control")
		recorder.Announce(fmt.Sprintf("%s has joined this session through a share and can type in it", userId))
	} else {
		c.auditMonitorAction(share.Session, "SSH", "session-share-join")
		recorder.Announce(fmt.Sprintf("%s is watching this session through a share", userId))
	}
	defer recorder.Announce(fmt.Sprintf("%s has left this session", userId))

	streamTerminal(ws, stream, recorder, user, isControl, stop)
}
