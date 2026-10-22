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

package agent

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ConnectPath is where Casvisor's web server accepts agents over WebSocket, for
// networks that only let HTTPS out or deployments behind a reverse proxy.
const ConnectPath = "/api/connect-agent"

// webSocketConn carries the agent's SSH connection in binary WebSocket messages.
type webSocketConn struct {
	ws     *websocket.Conn
	reader io.Reader

	writeMu sync.Mutex
}

func NewWebSocketConn(ws *websocket.Conn) net.Conn {
	return &webSocketConn{ws: ws}
}

func (c *webSocketConn) Read(p []byte) (int, error) {
	for {
		if c.reader == nil {
			messageType, reader, err := c.ws.NextReader()
			if err != nil {
				return 0, err
			}
			if messageType != websocket.BinaryMessage {
				continue
			}
			c.reader = reader
		}

		n, err := c.reader.Read(p)
		if err == io.EOF {
			c.reader = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (c *webSocketConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	err := c.ws.WriteMessage(websocket.BinaryMessage, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *webSocketConn) Close() error {
	return c.ws.Close()
}

func (c *webSocketConn) LocalAddr() net.Addr {
	return c.ws.LocalAddr()
}

func (c *webSocketConn) RemoteAddr() net.Addr {
	return c.ws.RemoteAddr()
}

func (c *webSocketConn) SetDeadline(t time.Time) error {
	err := c.ws.SetReadDeadline(t)
	if err != nil {
		return err
	}
	return c.ws.SetWriteDeadline(t)
}

func (c *webSocketConn) SetReadDeadline(t time.Time) error {
	return c.ws.SetReadDeadline(t)
}

func (c *webSocketConn) SetWriteDeadline(t time.Time) error {
	return c.ws.SetWriteDeadline(t)
}

// getWebSocketUrl turns "https://casvisor.example.com" into the agent endpoint
// "wss://casvisor.example.com/api/connect-agent", or returns "" for "host:port".
func getWebSocketUrl(server string) (string, error) {
	if !strings.Contains(server, "://") {
		return "", nil
	}

	u, err := url.Parse(server)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https", "wss":
		u.Scheme = "wss"
	case "http", "ws":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("no host in %s", server)
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + ConnectPath
	return u.String(), nil
}

func dialWebSocket(webSocketUrl string) (net.Conn, error) {
	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: dialTimeout,
	}
	ws, resp, err := dialer.Dial(webSocketUrl, nil)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("%s: %s", err.Error(), resp.Status)
		}
		return nil, err
	}
	return NewWebSocketConn(ws), nil
}
