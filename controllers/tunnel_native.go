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
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
	"github.com/gorilla/websocket"
)

var nativeUpgrader = websocket.Upgrader{
	ReadBufferSize:  32 * 1024,
	WriteBufferSize: 32 * 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
	Subprotocols: []string{"binary"},
}

type wsStream struct {
	ws      *websocket.Conn
	reader  io.Reader
	writeMu sync.Mutex
}

func newWsStream(ws *websocket.Conn) *wsStream {
	return &wsStream{ws: ws}
}

func (s *wsStream) Read(p []byte) (int, error) {
	for {
		if s.reader == nil {
			messageType, reader, err := s.ws.NextReader()
			if err != nil {
				return 0, err
			}
			if messageType != websocket.BinaryMessage {
				continue
			}
			s.reader = reader
		}

		n, err := s.reader.Read(p)
		if err == io.EOF {
			s.reader = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (s *wsStream) Write(p []byte) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	err := s.ws.WriteMessage(websocket.BinaryMessage, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (s *wsStream) WriteJSON(v interface{}) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	return s.ws.WriteJSON(v)
}

func (s *wsStream) Close(code int, msg string) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	closeCode := websocket.CloseNormalClosure
	if code != Normal {
		closeCode = 4000
	}
	deadline := time.Now().Add(time.Second)
	_ = s.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(closeCode, truncateCloseReason(msg)), deadline)
	_ = s.ws.Close()
}

func truncateCloseReason(msg string) string {
	// A close frame's reason must fit in 123 bytes.
	if len(msg) <= 123 {
		return msg
	}
	return msg[:123]
}

func (c *ApiController) getTunnelSessionAndAsset(sessionId string, username string, password string) (*object.Session, *object.Asset, int, error) {
	session, err := object.GetConnSession(sessionId)
	if err != nil {
		return nil, nil, SessionNotFound, err
	}
	if session == nil {
		return nil, nil, SessionNotFound, fmt.Errorf("the session: %s does not exist", sessionId)
	}

	user := c.GetSessionUser()
	if user == nil || session.Creator != getUserId(user) {
		return nil, nil, PermissionDenied, fmt.Errorf("you have no permission to use this session")
	}

	asset, err := object.GetAsset(session.Asset)
	if err != nil {
		return nil, nil, AssetNotFound, err
	}
	if asset == nil {
		return nil, nil, AssetNotFound, fmt.Errorf("the asset: %s does not exist", session.Asset)
	}

	actions, err := object.GetAssetActions(user, asset, c.getAuthzClientIp())
	if err != nil {
		return nil, nil, PermissionDenied, err
	}
	if !slices.Contains(actions, object.ActionConnect) {
		return nil, nil, PermissionDenied, fmt.Errorf("you have no permission to connect to the asset: %s", session.Asset)
	}

	if asset.Username == "" {
		asset.Username = username
	}
	if asset.Password == "" {
		asset.Password = password
	}

	return session, asset, Normal, nil
}

func setSessionConnected(sessionId string, session *object.Session, width int, height int) error {
	session.Width = width
	session.Height = height
	session.Status = object.Connected
	session.StartTime = util.GetCurrentTime()
	// No audit is required when no screen is recorded
	session.Reviewed = true

	_, err := object.UpdateSession(sessionId, session)
	if err != nil {
		return err
	}

	if conf.GetConfigBool("isDemoMode") {
		time.AfterFunc(demoSessionDuration, func() {
			_ = object.CloseSession(sessionId, ForcedDisconnect, fmt.Sprintf("Demo sessions are limited to %d minutes", int(demoSessionDuration.Minutes())))
		})
	}
	return nil
}

func closeTunnelSession(sessionId string, msg string) {
	object.RemoveSessionCloser(sessionId)
	err := object.CloseSession(sessionId, Normal, msg)
	if err != nil {
		logs.Error(fmt.Sprintf("closeTunnelSession(): object.CloseSession() error: %s", err.Error()))
	}
}

const (
	demoSessionDuration  = 15 * time.Minute
	demoMaxSessionsPerIp = 2
)
