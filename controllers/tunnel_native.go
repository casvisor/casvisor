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
	"path"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/recording"
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
	// rec records what the viewer is sent, once the screen is up
	rec *recording.Recording
}

// wsWriteTimeout ends a session whose viewer stopped reading, instead of
// blocking it for good.
const wsWriteTimeout = time.Minute

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

	s.rec.Write(recording.KindData, p)
	_ = s.ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
	err := s.ws.WriteMessage(websocket.BinaryMessage, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (s *wsStream) WriteJSON(v interface{}) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	s.rec.WriteJson(recording.KindText, v)
	_ = s.ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
	return s.ws.WriteJSON(v)
}

// record adds a message to the recording without sending it.
func (s *wsStream) record(kind byte, v interface{}) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	s.rec.WriteJson(kind, v)
}

// writeBinary sends data that is left out of the recording.
func (s *wsStream) writeBinary(p []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	_ = s.ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
	return s.ws.WriteMessage(websocket.BinaryMessage, p)
}

// WriteJSONUnrecorded sends a message that is left out of the recording, like the clipboard.
func (s *wsStream) WriteJSONUnrecorded(v interface{}) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	_ = s.ws.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
	return s.ws.WriteJSON(v)
}

// startRecording records everything the viewer is sent from now on.
func (s *wsStream) startRecording(c *ApiController, sessionId string, asset *object.Asset, width int, height int) {
	rec, err := recording.Start(recording.Header{
		Protocol: asset.Protocol,
		Session:  sessionId,
		Asset:    asset.GetId(),
		User:     getUserId(c.GetSessionUser()),
		Width:    width,
		Height:   height,
	})
	if err != nil {
		logs.Error(fmt.Sprintf("startRecording(): %s", err.Error()))
		return
	}
	if rec == nil {
		return
	}

	err = object.AddSessionRecording(sessionId, rec.Name)
	if err != nil {
		logs.Error(fmt.Sprintf("startRecording(): %s", err.Error()))
	}

	s.writeMu.Lock()
	s.rec = rec
	s.writeMu.Unlock()
}

func (s *wsStream) stopRecording() {
	s.writeMu.Lock()
	rec := s.rec
	s.rec = nil
	s.writeMu.Unlock()

	rec.Close()
}

func (s *wsStream) Close(code int, msg string) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	closeCode := websocket.CloseNormalClosure
	if code == InternalError {
		// the viewer may connect again
		closeCode = websocket.CloseInternalServerErr
	} else if code != Normal {
		closeCode = 4000
	}
	deadline := time.Now().Add(time.Second)
	_ = s.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(closeCode, truncateCloseReason(msg)), deadline)
	_ = s.ws.Close()
}

// recoveredPanic is a panic that ended a session, with where it happened.
type recoveredPanic struct {
	value    interface{}
	stack    []byte
	location string
}

// newRecoveredPanic is called by the deferred function that recovered value.
func newRecoveredPanic(value interface{}) *recoveredPanic {
	p := &recoveredPanic{value: value, stack: debug.Stack()}
	pcs := make([]uintptr, 64)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs)])
	panicked := false
	for {
		frame, more := frames.Next()
		if strings.HasPrefix(frame.Function, "runtime.") {
			panicked = panicked || frame.Function == "runtime.gopanic"
		} else if panicked {
			p.location = fmt.Sprintf("%s (%s:%d)", path.Base(frame.Function), path.Base(frame.File), frame.Line)
			break
		}
		if !more {
			break
		}
	}
	return p
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

// addLiveSession lets the session be disconnected once the user loses the permission to the asset.
func (c *ApiController) addLiveSession(sessionId string, asset *object.Asset) {
	object.AddLiveSession(sessionId, &object.LiveSession{
		User:     c.GetSessionUser(),
		AssetId:  asset.GetId(),
		ClientIp: c.getAuthzClientIp(),
	})
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
