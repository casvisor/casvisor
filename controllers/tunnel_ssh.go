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
	"net"
	"strconv"
	"time"

	"github.com/casvisor/casvisor/object"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

type terminalMessage struct {
	Type    string `json:"type"`
	Data    string `json:"data,omitempty"`
	Cols    int    `json:"cols,omitempty"`
	Rows    int    `json:"rows,omitempty"`
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

func (c *ApiController) GetAssetSsh() {
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

	session, asset, code, err := getTunnelSessionAndAsset(sessionId, c.Input().Get("username"), c.Input().Get("password"))
	if err != nil {
		fail(code, err)
		return
	}

	client, err := dialSsh(asset)
	if err != nil {
		fail(NewTunnelError, err)
		return
	}
	defer client.Close()

	sshSession, err := client.NewSession()
	if err != nil {
		fail(NewTunnelError, err)
		return
	}
	defer sshSession.Close()

	stdin, err := sshSession.StdinPipe()
	if err != nil {
		fail(NewTunnelError, err)
		return
	}
	sshSession.Stdout = stream
	sshSession.Stderr = stream

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	err = sshSession.RequestPty("xterm-256color", rows, cols, modes)
	if err != nil {
		fail(NewTunnelError, err)
		return
	}
	err = sshSession.Shell()
	if err != nil {
		fail(NewTunnelError, err)
		return
	}

	err = setSessionConnected(sessionId, session, cols, rows)
	if err != nil {
		fail(SessionUpdateError, err)
		return
	}
	object.AddSessionCloser(sessionId, func(code int, msg string) {
		_ = stream.WriteJSON(terminalMessage{Type: "error", Code: code, Message: msg})
		stream.Close(code, msg)
		_ = client.Close()
	})
	_ = stream.WriteJSON(terminalMessage{Type: "connected"})

	go func() {
		_ = sshSession.Wait()
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
			_, err = io.WriteString(stdin, message.Data)
		case "resize":
			if message.Cols > 0 && message.Rows > 0 {
				err = sshSession.WindowChange(message.Rows, message.Cols)
			}
		}
		if err != nil {
			break
		}
	}

	closeTunnelSession(sessionId, "Normal user exit")
}

func dialSsh(asset *object.Asset) (*ssh.Client, error) {
	if asset.Username == "" {
		return nil, fmt.Errorf("the username of the asset: %s is empty", asset.GetId())
	}

	password := asset.Password
	config := &ssh.ClientConfig{
		User: asset.Username,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = password
				}
				return answers, nil
			}),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	address := net.JoinHostPort(asset.Host, strconv.Itoa(asset.Port))
	return ssh.Dial("tcp", address, config)
}
