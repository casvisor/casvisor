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
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/beego/beego/logs"

	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util/vnc"
)

func (c *ApiController) GetAssetVnc() {
	c.EnableRender = false
	ws, err := c.upgradeWebSocket(wsMaxMessage)
	if err != nil {
		c.ResponseError("WebSocket upgrade failed:", err)
		return
	}
	stream := newWsStream(ws)

	fail := func(code int, err error) {
		_ = vnc.RejectClient(stream, err.Error())
		stream.Close(code, err.Error())
	}

	sessionId := c.Input().Get("sessionId")
	session, asset, code, err := c.getTunnelSessionAndAsset(sessionId)
	if err != nil {
		fail(code, err)
		return
	}

	conn, err := object.DialAsset(asset)
	if err != nil {
		fail(NewTunnelError, err)
		return
	}
	err = vnc.ConnectServer(conn, asset.Password, 10*time.Second)
	if err != nil {
		fail(NewTunnelError, err)
		return
	}
	defer conn.Close()

	stream.startRecording(c, sessionId, asset, 0, 0)
	defer stream.stopRecording()

	err = vnc.AcceptClient(stream)
	if err != nil {
		stream.Close(ParametersError, err.Error())
		return
	}

	err = setSessionConnected(sessionId, session, 0, 0)
	if err != nil {
		stream.Close(SessionUpdateError, err.Error())
		return
	}
	object.AddSessionCloser(sessionId, func(code int, msg string) {
		stream.Close(code, msg)
		_ = conn.Close()
	})
	c.addLiveSession(sessionId, asset)

	// The gateway, not only the browser, keeps the clipboard to what the
	// user may copy and paste.
	filter := &vnc.Filter{
		AllowCopy:  slices.Contains(session.Operations, "copy"),
		AllowPaste: slices.Contains(session.Operations, "paste"),
	}
	go func() {
		err := filter.RelayServer(stream, conn)
		if isProtocolError(err) {
			logs.Warning(fmt.Sprintf("GetAssetVnc(): %s: %s", asset.GetId(), err.Error()))
			stream.Close(InternalError, err.Error())
			return
		}
		stream.Close(Normal, "Remote connection shut down")
	}()

	err = filter.RelayClient(conn, stream)
	if isProtocolError(err) {
		logs.Warning(fmt.Sprintf("GetAssetVnc(): %s: %s", asset.GetId(), err.Error()))
		stream.Close(InternalError, err.Error())
		_ = conn.Close()
	}

	closeTunnelSession(sessionId, "Normal user exit")
}

// isProtocolError tells a message the relay could not read from the
// connection simply ending.
func isProtocolError(err error) bool {
	var protocolErr *vnc.ProtocolError
	return errors.As(err, &protocolErr)
}
