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
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/casvisor/casvisor/object"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

// The messages of a Casvisor Desktop session, see docs/protocol.md of
// casvisor/casvisor-desktop: 1 byte type, 4 bytes big endian length, payload.
const (
	desktopHello       = 1
	desktopAccept      = 2
	desktopVideoConfig = 16
	desktopVideo       = 17
	desktopCursor      = 18
	desktopDisplays    = 19
	desktopMouse       = 32
	desktopKey         = 33
	desktopControl     = 34
	desktopClipboard   = 48
	desktopNotice      = 64

	desktopMaxMessage     = 64 * 1024 * 1024
	desktopConsentTimeout = 45 * time.Second
)

type desktopHelloMessage struct {
	Version         int      `json:"version"`
	Session         string   `json:"session"`
	User            string   `json:"user"`
	UserDisplayName string   `json:"userDisplayName"`
	ClientIp        string   `json:"clientIp"`
	Permissions     []string `json:"permissions"`
}

type desktopAcceptMessage struct {
	Accepted bool   `json:"accepted"`
	Message  string `json:"message"`
}

type desktopNoticeMessage struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func encodeDesktopMessage(kind byte, payload []byte) []byte {
	msg := make([]byte, 5+len(payload))
	msg[0] = kind
	binary.BigEndian.PutUint32(msg[1:5], uint32(len(payload)))
	copy(msg[5:], payload)
	return msg
}

func encodeDesktopJson(kind byte, v interface{}) []byte {
	payload, _ := json.Marshal(v)
	return encodeDesktopMessage(kind, payload)
}

func readDesktopMessage(r io.Reader) ([]byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(header[1:5])
	if length > desktopMaxMessage {
		return nil, fmt.Errorf("message too large: %d bytes", length)
	}
	msg := make([]byte, 5+int(length))
	copy(msg, header)
	if _, err := io.ReadFull(r, msg[5:]); err != nil {
		return nil, err
	}
	return msg, nil
}

// getDesktopPermissions is what the device is told the user may do.
func getDesktopPermissions(session *object.Session) []string {
	permissions := []string{object.ActionConnect}
	for _, item := range []struct {
		operation string
		action    string
	}{
		{"copy", object.ActionCopy},
		{"paste", object.ActionPaste},
		{"upload", object.ActionUpload},
		{"download", object.ActionDownload},
	} {
		if slices.Contains(session.Operations, item.operation) {
			permissions = append(permissions, item.action)
		}
	}
	return permissions
}

// GetAssetDesktop relays a remote session between the browser and a computer
// running Casvisor Desktop. The video is relayed and recorded as it is, the
// input and the clipboard are checked against the session's permissions.
func (c *ApiController) GetAssetDesktop() {
	c.EnableRender = false
	ws, err := nativeUpgrader.Upgrade(c.Ctx.ResponseWriter, c.Ctx.Request, nil)
	if err != nil {
		c.ResponseError("WebSocket upgrade failed:", err)
		return
	}
	stream := newWsStream(ws)

	fail := func(code int, err error) {
		_ = stream.writeBinary(encodeDesktopJson(desktopNotice, desktopNoticeMessage{Type: "error", Message: err.Error()}))
		stream.Close(code, err.Error())
	}

	sessionId := c.Input().Get("sessionId")
	session, asset, code, err := c.getTunnelSessionAndAsset(sessionId)
	if err != nil {
		fail(code, err)
		return
	}
	if asset.Protocol != object.ProtocolDesktop {
		fail(NewTunnelError, fmt.Errorf("the asset: %s is not a Desktop asset", asset.GetId()))
		return
	}

	gateway, err := object.GetNetworkGateway(fmt.Sprintf("%s/%s", asset.Owner, asset.NetworkGateway))
	if err != nil {
		fail(NewTunnelError, err)
		return
	}
	if gateway == nil {
		fail(NewTunnelError, fmt.Errorf("the device: %s of the asset: %s does not exist", asset.NetworkGateway, asset.GetId()))
		return
	}

	channel, err := object.OpenDesktopChannel(gateway)
	if err != nil {
		fail(NewTunnelError, err)
		return
	}
	defer channel.Close()
	reader := bufio.NewReaderSize(channel, 64*1024)

	user := c.GetSessionUser()
	hello := desktopHelloMessage{
		Version:         1,
		Session:         sessionId,
		User:            getUserId(user),
		UserDisplayName: user.DisplayName,
		ClientIp:        c.getClientIp(),
		Permissions:     getDesktopPermissions(session),
	}
	if _, err = channel.Write(encodeDesktopJson(desktopHello, hello)); err != nil {
		fail(NewTunnelError, err)
		return
	}
	_ = stream.writeBinary(encodeDesktopJson(desktopNotice, desktopNoticeMessage{Type: "waiting"}))

	accept, err := c.waitDesktopAccept(channel, reader)
	if err != nil {
		fail(NewTunnelError, err)
		return
	}
	if !accept.Accepted {
		c.auditDesktop(asset, "consent", "refused: "+accept.Message)
		message := accept.Message
		if message == "" {
			message = "the remote session was refused"
		}
		fail(PermissionDenied, fmt.Errorf("%s", message))
		return
	}
	c.auditDesktop(asset, "consent", "allowed")

	if err = setSessionConnected(sessionId, session, 0, 0); err != nil {
		fail(SessionUpdateError, err)
		return
	}
	object.AddSessionCloser(sessionId, func(code int, msg string) {
		_ = channel.Close()
		stream.Close(code, msg)
	})
	c.addLiveSession(sessionId, asset)
	stream.startRecording(c, sessionId, asset, 0, 0)
	defer stream.stopRecording()

	canCopy := slices.Contains(session.Operations, "copy")
	canPaste := slices.Contains(session.Operations, "paste")
	go c.readDesktopInput(ws, channel, asset, canPaste)

	for {
		msg, err := readDesktopMessage(reader)
		if err != nil {
			break
		}
		switch msg[0] {
		case desktopVideoConfig, desktopVideo, desktopCursor, desktopDisplays, desktopNotice:
			_, err = stream.Write(msg)
		case desktopClipboard:
			if canCopy {
				c.auditDesktop(asset, "clipboard-copy", truncateRunes(string(msg[5:]), 250))
				err = stream.writeBinary(msg)
			}
		}
		if err != nil {
			break
		}
	}

	closeTunnelSession(sessionId, "Normal user exit")
	stream.Close(Normal, "Remote connection shut down")
}

// waitDesktopAccept waits for the person at the remote computer to allow or refuse the session.
func (c *ApiController) waitDesktopAccept(channel ssh.Channel, reader *bufio.Reader) (*desktopAcceptMessage, error) {
	timer := time.AfterFunc(desktopConsentTimeout, func() {
		_ = channel.Close()
	})
	defer timer.Stop()

	msg, err := readDesktopMessage(reader)
	if err != nil {
		return nil, fmt.Errorf("the remote computer didn't answer: %s", err.Error())
	}
	if msg[0] != desktopAccept {
		return nil, fmt.Errorf("the remote computer answered with message type %d instead of accept", msg[0])
	}
	var accept desktopAcceptMessage
	if err = json.Unmarshal(msg[5:], &accept); err != nil {
		return nil, err
	}
	return &accept, nil
}

// readDesktopInput relays the viewer's input to the device until the viewer goes away.
func (c *ApiController) readDesktopInput(ws *websocket.Conn, channel ssh.Channel, asset *object.Asset, canPaste bool) {
	defer channel.Close()
	for {
		messageType, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.BinaryMessage || len(data) < 5 {
			continue
		}
		if int(binary.BigEndian.Uint32(data[1:5])) != len(data)-5 {
			continue
		}
		switch data[0] {
		case desktopMouse, desktopKey, desktopControl:
		case desktopClipboard:
			if !canPaste {
				continue
			}
			c.auditDesktop(asset, "clipboard-paste", truncateRunes(string(data[5:]), 250))
		default:
			continue
		}
		if _, err = channel.Write(data); err != nil {
			return
		}
	}
}

func (c *ApiController) auditDesktop(asset *object.Asset, action string, detail string) {
	user := c.GetSessionUser()
	if user == nil {
		return
	}
	object.AddAuditRecord(&object.AuditEvent{
		Organization: asset.Owner,
		User:         user.Name,
		ClientIp:     c.getClientIp(),
		UserAgent:    c.getUserAgent(),
		Method:       "Desktop",
		Action:       action,
		Asset:        asset.GetId(),
		Detail:       detail,
	})
}
