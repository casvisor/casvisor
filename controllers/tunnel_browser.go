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
	"io"
	"net/url"
	"os"
	"strconv"

	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/rbrowser"
	"github.com/casvisor/casvisor/recording"
)

// browserViewer sends the browser of a session to the page over its WebSocket.
type browserViewer struct {
	stream *wsStream
}

func (v *browserViewer) WriteFrame(data []byte) error {
	_, err := v.stream.Write(data)
	return err
}

func (v *browserViewer) WriteRecorded(msg interface{}) error {
	return v.stream.WriteJSON(msg)
}

func (v *browserViewer) WriteMessage(msg interface{}) error {
	return v.stream.WriteJSONUnrecorded(msg)
}

func (v *browserViewer) Record(msg interface{}) {
	v.stream.record(recording.KindText, msg)
}

// GetRemoteBrowser
// @Title GetRemoteBrowser
// @Tag Session API
// @Description tell whether Web assets can be opened in Casvisor's browser, which needs Chrome, Chromium or Edge on the server
// @Success 200 {object} Response true when they can
// @router /get-remote-browser [get]
func (c *ApiController) GetRemoteBrowser() {
	c.ResponseOk(rbrowser.IsAvailable())
}

// GetAssetBrowser
// @Title GetAssetBrowser
// @Tag Session API
// @Description open a Web asset in a browser that Casvisor runs, as a WebSocket that streams its screen and takes the user's mouse, keyboard and clipboard
// @Param   sessionId    query    string  true     "The id of the session, from add-asset-tunnel"
// @Param   width    query    int  false     "The width of the screen, in CSS pixels"
// @Param   height    query    int  false     "The height of the screen, in CSS pixels"
// @router /get-asset-browser [get]
func (c *ApiController) GetAssetBrowser() {
	c.EnableRender = false
	ws, err := c.upgradeWebSocket(wsMaxRdpMessage)
	if err != nil {
		c.ResponseError("WebSocket upgrade failed:", err)
		return
	}
	stream := newWsStream(ws)

	fail := func(code int, err error) {
		_ = stream.WriteJSONUnrecorded(map[string]string{"t": "error", "message": err.Error()})
		stream.Close(code, err.Error())
	}

	sessionId := c.Input().Get("sessionId")
	width, _ := strconv.Atoi(c.Input().Get("width"))
	height, _ := strconv.Atoi(c.Input().Get("height"))

	session, asset, code, err := c.getTunnelSessionAndAsset(sessionId)
	if err != nil {
		fail(code, err)
		return
	}
	if asset.Protocol != object.ProtocolWeb || asset.WebMode != object.WebModeBrowser {
		closeTunnelSession(sessionId, "Not a website opened in Casvisor's browser")
		fail(ParametersError, fmt.Errorf("the asset: %s is not a website opened in Casvisor's browser", asset.GetId()))
		return
	}

	user := c.GetSessionUser()
	clientIp := c.getAuthzClientIp()
	actions, err := object.GetAssetAccountActions(user, asset, &session.Account, clientIp)
	if err != nil {
		closeTunnelSession(sessionId, err.Error())
		fail(PermissionDenied, err)
		return
	}

	recorded := *asset
	recorded.Protocol = object.ProtocolBrowser
	stream.startRecording(c, sessionId, &recorded, width, height)
	defer stream.stopRecording()

	browser, err := rbrowser.Start(&rbrowser.Options{
		SessionId: sessionId,
		User:      user,
		Asset:     asset,
		Actions:   actions,
		ClientIp:  c.getClientIp(),
		Width:     width,
		Height:    height,
		Viewer:    &browserViewer{stream: stream},
	})
	if err != nil {
		closeTunnelSession(sessionId, err.Error())
		fail(NewTunnelError, err)
		return
	}
	defer browser.Close()

	session.Protocol = object.ProtocolBrowser
	err = setSessionConnected(sessionId, session, width, height)
	if err != nil {
		closeTunnelSession(sessionId, err.Error())
		fail(SessionUpdateError, err)
		return
	}
	object.AddSessionCloser(sessionId, func(code int, msg string) {
		stream.Close(code, msg)
	})
	object.AddLiveSession(sessionId, &object.LiveSession{
		User:      user,
		AssetId:   asset.GetId(),
		Account:   session.Account,
		ClientIp:  clientIp,
		OnActions: browser.SetActions,
	})

	go func() {
		<-browser.Done()
		stream.Close(Normal, "The browser has closed")
	}()

	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			break
		}
		msg := &rbrowser.Message{}
		if json.Unmarshal(data, msg) == nil {
			browser.Handle(msg)
		}
	}

	closeTunnelSession(sessionId, "Normal user exit")
}

// GetBrowserDownload
// @Title GetBrowserDownload
// @Tag Session API
// @Description download a file that the browser of a connected session has downloaded from its website, with the Download action
// @Param   sessionId    query    string  true     "The id of the session"
// @Param   download    query    string  true     "The id of the download"
// @router /get-browser-download [get]
func (c *ApiController) GetBrowserDownload() {
	sessionId := c.Input().Get("sessionId")
	id := c.Input().Get("download")

	user := c.GetSessionUser()
	session, err := object.GetConnSession(sessionId)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if user == nil || session == nil || session.Creator != getUserId(user) {
		c.ResponseError("you have no permission to use this session")
		return
	}
	browser := rbrowser.Get(sessionId)
	if browser == nil {
		c.ResponseError("the session has ended")
		return
	}

	path, name, err := browser.GetDownload(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	file, err := os.Open(path)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.EnableRender = false
	header := c.Ctx.ResponseWriter.Header()
	header.Set("Content-Type", "application/octet-stream")
	header.Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	header.Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(name)))
	c.Ctx.ResponseWriter.WriteHeader(200)
	_, _ = io.Copy(c.Ctx.ResponseWriter, file)
}
