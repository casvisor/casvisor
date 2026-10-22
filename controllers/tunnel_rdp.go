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
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/casvisor/casvisor/object"
	gordp "github.com/casvisor/gordp"
	"github.com/casvisor/gordp/bitmap"
	"github.com/gorilla/websocket"
)

// GetAssetRdp connects to an RDP asset with the native gordp client (no guacd),
// decodes the screen on the gateway and streams changed rectangles to the
// browser as PNG tiles, while relaying the browser's mouse and keyboard input.
func (c *ApiController) GetAssetRdp() {
	c.EnableRender = false
	ws, err := nativeUpgrader.Upgrade(c.Ctx.ResponseWriter, c.Ctx.Request, nil)
	if err != nil {
		c.ResponseError("WebSocket upgrade failed:", err)
		return
	}
	stream := newWsStream(ws)

	fail := func(code int, err error) {
		_ = stream.WriteJSON(rdpMessage{Type: "error", Message: err.Error()})
		stream.Close(code, err.Error())
	}

	sessionId := c.Input().Get("sessionId")
	width, err := strconv.Atoi(c.Input().Get("width"))
	if err != nil || width <= 0 {
		width = 1024
	}
	height, err := strconv.Atoi(c.Input().Get("height"))
	if err != nil || height <= 0 {
		height = 768
	}

	session, asset, code, err := c.getTunnelSessionAndAsset(sessionId, c.Input().Get("username"), c.Input().Get("password"))
	if err != nil {
		fail(code, err)
		return
	}

	addr := net.JoinHostPort(asset.Host, strconv.Itoa(asset.Port))
	client := gordp.NewClient(addr, "", asset.Username, asset.Password,
		gordp.WithDesktopSize(uint16(width), uint16(height)),
		gordp.WithServerCertFingerprint(asset.Fingerprint))

	// Clipboard and resize calls are queued to run between pumps, on the
	// goroutine that owns the client state.
	ops := make(chan func(), 16)
	lastW, lastH := 0, 0
	syncSize := func() {
		w, h := client.DesktopSize()
		if w != lastW || h != lastH {
			lastW, lastH = w, h
			_ = stream.WriteJSON(rdpMessage{Type: "size", Width: w, Height: h})
		}
	}

	client.OnBitmap = func(rects []bitmap.Rect) {
		syncSize()
		for _, rect := range rects {
			frame, err := encodeTile(rect)
			if err != nil {
				continue
			}
			if _, err = stream.Write(frame); err != nil {
				return
			}
		}
	}

	if slices.Contains(session.Operations, "copy") {
		client.OnClipboard = func(text string) {
			_ = stream.WriteJSON(rdpMessage{Type: "clipboard", Text: text})
			c.auditRdpClipboard(asset, "clipboard-copy", text)
		}
	}

	if err := client.Connect(); err != nil {
		if asset.Fingerprint != "" && strings.Contains(err.Error(), "does not match pinned") {
			err = fmt.Errorf("%s, clear the fingerprint of the asset if the server certificate was renewed", err.Error())
		}
		fail(NewTunnelError, err)
		return
	}
	defer client.Close()

	if asset.Fingerprint == "" {
		if err := object.UpdateAssetFingerprint(asset.GetId(), client.ServerCertFingerprint()); err != nil {
			fail(NewTunnelError, err)
			return
		}
	}

	if err := setSessionConnected(sessionId, session, width, height); err != nil {
		fail(SessionUpdateError, err)
		return
	}
	object.AddSessionCloser(sessionId, func(code int, msg string) {
		_ = client.Close()
		stream.Close(code, msg)
	})
	c.addLiveSession(sessionId, asset)

	lastW, lastH = client.DesktopSize()
	_ = stream.WriteJSON(rdpMessage{Type: "init", Width: lastW, Height: lastH})

	go c.readRdpInput(ws, client, session, asset, ops)

	for {
		if _, err := client.PumpUpdates(100 * time.Millisecond); err != nil {
			break
		}
		for pending := true; pending; {
			select {
			case op := <-ops:
				op()
			default:
				pending = false
			}
		}
		syncSize()
	}

	closeTunnelSession(sessionId, "Normal user exit")
}

// readRdpInput reads input events from the browser and drives the RDP client.
func (c *ApiController) readRdpInput(ws *websocket.Conn, client *gordp.Client, session *object.Session, asset *object.Asset, ops chan<- func()) {
	canPaste := slices.Contains(session.Operations, "paste")
	queue := func(op func()) {
		select {
		case ops <- op:
		default:
		}
	}
	for {
		messageType, data, err := ws.ReadMessage()
		if err != nil {
			_ = client.Close()
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}
		var msg rdpMessage
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "mm":
			_ = client.MouseMove(uint16(msg.X), uint16(msg.Y))
		case "mb":
			_ = client.MouseButton(mouseButton(msg.Button), msg.Down, uint16(msg.X), uint16(msg.Y))
		case "mw":
			_ = client.MouseWheel(uint16(msg.X), uint16(msg.Y), int16(msg.Delta))
		case "k":
			_ = client.KeyScancode(byte(msg.Code), msg.Down, msg.Extended)
		case "u":
			_ = client.KeyUnicode(rune(msg.Code))
		case "clipboard":
			if !canPaste {
				continue
			}
			text := msg.Text
			queue(func() { _ = client.SetClipboard(text) })
			c.auditRdpClipboard(asset, "clipboard-paste", text)
		case "resize":
			if msg.Width <= 0 || msg.Height <= 0 {
				continue
			}
			w, h := uint16(min(msg.Width, 8192)), uint16(min(msg.Height, 8192))
			queue(func() { _ = client.Resize(w, h) })
		}
	}
}

func (c *ApiController) auditRdpClipboard(asset *object.Asset, action string, text string) {
	user := c.GetSessionUser()
	if user == nil {
		return
	}
	object.AddAuditRecord(&object.AuditEvent{
		Organization: asset.Owner,
		User:         user.Name,
		ClientIp:     c.getClientIp(),
		UserAgent:    c.getUserAgent(),
		Method:       "RDP",
		Action:       action,
		Asset:        asset.GetId(),
		Detail:       truncateRunes(text, 1000),
	})
}

func truncateRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// rdpMessage is the JSON control message exchanged with the browser. Pixel data
// travels as binary frames instead.
type rdpMessage struct {
	Type     string `json:"t"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	Message  string `json:"message,omitempty"`
	X        int    `json:"x,omitempty"`
	Y        int    `json:"y,omitempty"`
	Button   int    `json:"button,omitempty"`
	Down     bool   `json:"down,omitempty"`
	Delta    int    `json:"delta,omitempty"`
	Code     int    `json:"code,omitempty"`
	Extended bool   `json:"extended,omitempty"`
	Text     string `json:"text,omitempty"`
}

func mouseButton(button int) gordp.MouseButton {
	switch button {
	case 1:
		return gordp.ButtonMiddle
	case 2:
		return gordp.ButtonRight
	default:
		return gordp.ButtonLeft
	}
}

// encodeTile builds a binary frame: an 8-byte header (x, y, width, height as
// little-endian uint16) followed by the PNG-encoded rectangle.
func encodeTile(rect bitmap.Rect) ([]byte, error) {
	img := &image.RGBA{
		Pix:    rect.RGBA,
		Stride: rect.Width * 4,
		Rect:   image.Rect(0, 0, rect.Width, rect.Height),
	}
	var buf bytes.Buffer
	var header [8]byte
	binary.LittleEndian.PutUint16(header[0:], uint16(rect.DestLeft))
	binary.LittleEndian.PutUint16(header[2:], uint16(rect.DestTop))
	binary.LittleEndian.PutUint16(header[4:], uint16(rect.Width))
	binary.LittleEndian.PutUint16(header[6:], uint16(rect.Height))
	buf.Write(header[:])
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
