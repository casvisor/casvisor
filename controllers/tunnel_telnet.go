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
	"encoding/json"
	"net"
	"regexp"
	"strconv"
	"sync"

	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/terminal"
	"github.com/gorilla/websocket"
)

const (
	telnetSe   = 240
	telnetSb   = 250
	telnetWill = 251
	telnetWont = 252
	telnetDo   = 253
	telnetDont = 254
	telnetIac  = 255

	telnetOptBinary = 0
	telnetOptEcho   = 1
	telnetOptSga    = 3
	telnetOptTtype  = 24
	telnetOptNaws   = 31
)

var (
	telnetLoginPrompt    = regexp.MustCompile(`(?i)(login|username|user name)\s*:\s*$`)
	telnetPasswordPrompt = regexp.MustCompile(`(?i)password\s*:\s*$`)
)

// telnetConn speaks the Telnet protocol to the asset: it answers option negotiation,
// strips commands out of the output and escapes the user's input.
type telnetConn struct {
	conn    net.Conn
	writeMu sync.Mutex

	// mu guards the negotiated options, which the reader and the browser's resize both touch
	mu         sync.Mutex
	answered   map[[2]byte]bool
	nawsOn     bool
	binaryOn   bool
	cols, rows int

	state int
	verb  byte
	sb    []byte
}

func dialTelnet(asset *object.Asset, cols int, rows int) (*telnetConn, error) {
	conn, err := object.DialAsset(asset)
	if err != nil {
		return nil, err
	}
	return &telnetConn{conn: conn, answered: map[[2]byte]bool{}, cols: cols, rows: rows}, nil
}

func (t *telnetConn) writeRaw(p []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	_, err := t.conn.Write(p)
	return err
}

// Write sends the user's input, doubling IAC bytes and ending lines the way NVT expects.
func (t *telnetConn) Write(p []byte) error {
	t.mu.Lock()
	binaryOn := t.binaryOn
	t.mu.Unlock()

	var buf bytes.Buffer
	for _, b := range p {
		switch {
		case b == telnetIac:
			buf.Write([]byte{telnetIac, telnetIac})
		case b == '\r' && !binaryOn:
			buf.Write([]byte{'\r', 0})
		default:
			buf.WriteByte(b)
		}
	}
	return t.writeRaw(buf.Bytes())
}

func (t *telnetConn) reply(verb byte, option byte) {
	key := [2]byte{verb, option}
	if t.answered[key] {
		return
	}
	t.answered[key] = true
	_ = t.writeRaw([]byte{telnetIac, verb, option})
}

func (t *telnetConn) sendNaws() {
	cols, rows := t.cols, t.rows
	payload := []byte{byte(cols >> 8), byte(cols), byte(rows >> 8), byte(rows)}
	msg := []byte{telnetIac, telnetSb, telnetOptNaws}
	for _, b := range payload {
		msg = append(msg, b)
		if b == telnetIac {
			msg = append(msg, telnetIac)
		}
	}
	msg = append(msg, telnetIac, telnetSe)
	_ = t.writeRaw(msg)
}

func (t *telnetConn) Resize(cols int, rows int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.cols, t.rows = cols, rows
	if t.nawsOn {
		t.sendNaws()
	}
}

func (t *telnetConn) negotiate(verb byte, option byte) {
	t.mu.Lock()
	defer t.mu.Unlock()

	switch verb {
	case telnetDo:
		switch option {
		case telnetOptTtype, telnetOptSga, telnetOptBinary:
			if option == telnetOptBinary {
				t.binaryOn = true
			}
			t.reply(telnetWill, option)
		case telnetOptNaws:
			t.reply(telnetWill, option)
			t.nawsOn = true
			t.sendNaws()
		default:
			t.reply(telnetWont, option)
		}
	case telnetWill:
		switch option {
		case telnetOptEcho, telnetOptSga, telnetOptBinary:
			t.reply(telnetDo, option)
		default:
			t.reply(telnetDont, option)
		}
	case telnetDont:
		if option == telnetOptNaws {
			t.nawsOn = false
		}
		if option == telnetOptBinary {
			t.binaryOn = false
		}
	}
}

func (t *telnetConn) subnegotiate(data []byte) {
	// TTYPE SEND
	if len(data) >= 2 && data[0] == telnetOptTtype && data[1] == 1 {
		msg := []byte{telnetIac, telnetSb, telnetOptTtype, 0}
		msg = append(msg, "XTERM-256COLOR"...)
		msg = append(msg, telnetIac, telnetSe)
		_ = t.writeRaw(msg)
	}
}

// filter handles the commands in what the asset sent and returns the terminal output.
func (t *telnetConn) filter(p []byte) []byte {
	const (
		stData = iota
		stIac
		stVerb
		stSb
		stSbIac
	)

	out := make([]byte, 0, len(p))
	for _, b := range p {
		switch t.state {
		case stData:
			if b == telnetIac {
				t.state = stIac
			} else {
				out = append(out, b)
			}
		case stIac:
			switch b {
			case telnetIac:
				out = append(out, b)
				t.state = stData
			case telnetWill, telnetWont, telnetDo, telnetDont:
				t.verb = b
				t.state = stVerb
			case telnetSb:
				t.sb = t.sb[:0]
				t.state = stSb
			default:
				t.state = stData
			}
		case stVerb:
			t.negotiate(t.verb, b)
			t.state = stData
		case stSb:
			if b == telnetIac {
				t.state = stSbIac
			} else {
				t.sb = append(t.sb, b)
			}
		case stSbIac:
			switch b {
			case telnetSe:
				t.subnegotiate(t.sb)
				t.state = stData
			case telnetIac:
				t.sb = append(t.sb, b)
				t.state = stSb
			default:
				t.state = stSb
			}
		}
	}
	return out
}

// telnetAutoLogin types the asset's username and password at the first login and password prompts.
type telnetAutoLogin struct {
	username, password     string
	userSent, passwordSent bool
	tail                   []byte
}

func (a *telnetAutoLogin) answer(output []byte) string {
	if (a.userSent || a.username == "") && (a.passwordSent || a.password == "") {
		return ""
	}

	a.tail = append(a.tail, output...)
	if len(a.tail) > 256 {
		a.tail = a.tail[len(a.tail)-256:]
	}

	if !a.passwordSent && a.password != "" && telnetPasswordPrompt.Match(a.tail) {
		a.passwordSent = true
		a.userSent = true
		a.tail = nil
		return a.password + "\r"
	}
	if !a.userSent && a.username != "" && telnetLoginPrompt.Match(a.tail) {
		a.userSent = true
		a.tail = nil
		return a.username + "\r"
	}
	return ""
}

func (c *ApiController) GetAssetTelnet() {
	c.EnableRender = false
	ws, err := nativeUpgrader.Upgrade(c.Ctx.ResponseWriter, c.Ctx.Request, nil)
	if err != nil {
		c.ResponseError("WebSocket upgrade failed:", err)
		return
	}
	stream := newWsStream(ws)

	fail := func(code int, err error) {
		_ = stream.WriteJSON(terminalMessage{Type: "error", Code: code, Message: err.Error()})
		stream.Close(code, err.Error())
	}

	sessionId := c.Input().Get("sessionId")
	cols, err := strconv.Atoi(c.Input().Get("cols"))
	if err != nil {
		fail(ParametersError, err)
		return
	}
	rows, err := strconv.Atoi(c.Input().Get("rows"))
	if err != nil {
		fail(ParametersError, err)
		return
	}

	session, asset, code, err := c.getTunnelSessionAndAsset(sessionId, c.Input().Get("username"), c.Input().Get("password"))
	if err != nil {
		fail(code, err)
		return
	}

	conn, err := dialTelnet(asset, cols, rows)
	if err != nil {
		fail(NewTunnelError, err)
		return
	}
	defer conn.conn.Close()

	recorder := terminal.NewRecorder(terminal.Options{
		SessionId: sessionId,
		User:      c.GetSessionUser(),
		Asset:     asset,
		ClientIp:  c.getAuthzClientIp(),
		Cols:      cols,
		Rows:      rows,
		Send:      conn.Write,
		Notify: func(p []byte) {
			_, _ = stream.Write(p)
		},
	})
	defer recorder.Close()
	output := recordedWriter{stream: stream, recorder: recorder}

	err = setSessionConnected(sessionId, session, cols, rows)
	if err != nil {
		fail(SessionUpdateError, err)
		return
	}
	c.addLiveSession(sessionId, asset)
	object.AddSessionCloser(sessionId, func(code int, msg string) {
		_ = stream.WriteJSON(terminalMessage{Type: "error", Code: code, Message: msg})
		stream.Close(code, msg)
		_ = conn.conn.Close()
	})
	_ = stream.WriteJSON(terminalMessage{Type: "connected"})

	go func() {
		login := &telnetAutoLogin{username: asset.Username, password: asset.Password}
		buf := make([]byte, 32*1024)
		for {
			n, err := conn.conn.Read(buf)
			if n > 0 {
				data := conn.filter(buf[:n])
				if len(data) > 0 {
					_, _ = output.Write(data)
					if answer := login.answer(data); answer != "" {
						_ = conn.Write([]byte(answer))
					}
				}
			}
			if err != nil {
				break
			}
		}
		stream.Close(Normal, "Remote connection shut down")
	}()

	for {
		messageType, data, err := ws.ReadMessage()
		if err != nil {
			break
		}
		if messageType != websocket.TextMessage {
			continue
		}

		var message terminalMessage
		err = json.Unmarshal(data, &message)
		if err != nil {
			continue
		}

		switch message.Type {
		case "data":
			recorder.Input([]byte(message.Data))
		case "resize":
			if message.Cols > 0 && message.Rows > 0 {
				conn.Resize(message.Cols, message.Rows)
				recorder.Resize(message.Cols, message.Rows)
			}
		}
	}

	closeTunnelSession(sessionId, "Normal user exit")
}
