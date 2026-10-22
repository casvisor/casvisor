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
	"io"
	"net"
	"strconv"
	"time"

	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util/vnc"
)

func (c *ApiController) GetAssetVnc() {
	c.EnableRender = false
	ws, err := nativeUpgrader.Upgrade(c.Ctx.ResponseWriter, c.Ctx.Request, nil)
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
	session, asset, code, err := c.getTunnelSessionAndAsset(sessionId, c.Input().Get("username"), c.Input().Get("password"))
	if err != nil {
		fail(code, err)
		return
	}

	address := net.JoinHostPort(asset.Host, strconv.Itoa(asset.Port))
	conn, err := vnc.DialServer(address, asset.Password, 10*time.Second)
	if err != nil {
		fail(NewTunnelError, err)
		return
	}
	defer conn.Close()

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

	go func() {
		_, _ = io.Copy(stream, conn)
		stream.Close(Normal, "Remote connection shut down")
	}()

	_, _ = io.Copy(conn, stream)

	closeTunnelSession(sessionId, "Normal user exit")
}
