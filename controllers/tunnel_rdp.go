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
	"strconv"
	"strings"
	"sync"
	"time"

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

	var writeMu sync.Mutex
	client.OnBitmap = func(rects []bitmap.Rect) {
		for _, rect := range rects {
			frame, err := encodeTile(rect)
			if err != nil {
				continue
			}
			writeMu.Lock()
			err = ws.WriteMessage(websocket.BinaryMessage, frame)
			writeMu.Unlock()
			if err != nil {
				return
			}
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

	w, h := client.DesktopSize()
	writeMu.Lock()
	_ = ws.WriteJSON(rdpMessage{Type: "init", Width: w, Height: h})
	writeMu.Unlock()

	go c.readRdpInput(ws, client)

	for {
		if _, err := client.PumpUpdates(1 * time.Second); err != nil {
			break
		}
	}

	closeTunnelSession(sessionId, "Normal user exit")
}

// readRdpInput reads input events from the browser and drives the RDP client.
func (c *ApiController) readRdpInput(ws *websocket.Conn, client *gordp.Client) {
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
		}
	}
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
