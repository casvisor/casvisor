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

package kubeproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/terminal"
	"github.com/casvisor/casvisor/util"
	"github.com/gorilla/websocket"
)

const SessionModeKubectl = "kubectl"

var shells = []string{"sh", "bash", "ash", "dash", "zsh", "ksh", "fish", "busybox"}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  32 * 1024,
	WriteBufferSize: 32 * 1024,
	// kubectl sends no Origin; the token is what is checked
	CheckOrigin: func(r *http.Request) bool { return true },
}

func isTrue(value string) bool {
	return value == "true" || value == "1"
}

// getTarDirection tells the tar that kubectl cp runs: "upload" for "tar -xmf -", "download" for "tar cf -".
func getTarDirection(command []string) string {
	if len(command) < 2 || path.Base(command[0]) != "tar" {
		return ""
	}
	for _, arg := range command[1:] {
		if strings.HasPrefix(arg, "--") {
			continue
		}
		flags := strings.TrimPrefix(arg, "-")
		if strings.ContainsRune(flags, 'x') {
			return "upload"
		}
		if strings.ContainsRune(flags, 'c') {
			return "download"
		}
		return ""
	}
	return ""
}

// serveConnect opens exec, attach and portforward, which kubectl does over WebSockets.
func (c *call) serveConnect(w http.ResponseWriter, r *http.Request) {
	if c.req.Subresource == "portforward" {
		if c.isSpdy {
			c.refuseSpdy(w)
			return
		}
		c.forward(w, r)
		return
	}

	query := r.URL.Query()
	command := query["command"]
	stdin, tty := isTrue(query.Get("stdin")), isTrue(query.Get("tty"))
	target := c.req.Namespace + "/" + c.req.Name
	if container := query.Get("container"); container != "" {
		target += "/" + container
	}

	if direction := getTarDirection(command); c.req.Subresource == "exec" && direction != "" {
		action := object.ActionDownload
		if direction == "upload" {
			action = object.ActionUpload
		}
		if !slices.Contains(c.actions, action) {
			c.block(w, fmt.Sprintf("you have no permission to %s files on the asset: %s", direction, c.asset.Name))
			return
		}
		if c.isSpdy {
			c.refuseSpdy(w)
			return
		}
		c.audit("kubernetes-"+direction, fmt.Sprintf("%s: %s", target, strings.Join(command, " ")), r)
		c.forward(w, r)
		return
	}

	if stdin && !tty {
		if c.req.Subresource == "attach" || len(command) == 0 || (len(command) == 1 && slices.Contains(shells, path.Base(command[0]))) {
			c.block(w, "open a shell with a terminal (kubectl exec -it), so that its commands are recorded")
			return
		}
		// what is piped into a command reaches the container like an upload
		if !slices.Contains(c.actions, object.ActionUpload) {
			c.block(w, fmt.Sprintf("you have no permission to upload files to the asset: %s, which piping into a command needs", c.asset.Name))
			return
		}
	}
	if c.isSpdy {
		if len(command) > 0 {
			rule, err := object.MatchCommandRule(c.user, c.asset, strings.Join(command, " "))
			if err == nil && rule != nil && rule.Action == object.CommandActionBlock {
				writeForbidden(w, terminal.GetBlockedMessage(rule))
				return
			}
		}
		c.refuseSpdy(w)
		return
	}
	if stdin && !tty {
		c.audit("kubernetes-upload", fmt.Sprintf("%s: %s", target, strings.Join(command, " ")), r)
	}

	sessionId, err := c.openSession(r, target)
	if err != nil {
		logs.Error(fmt.Sprintf("kubeproxy.serveConnect(): %s", err.Error()))
		writeStatus(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	defer func() {
		c.ts.removeChild(sessionId)
		object.RemoveSessionCloser(sessionId)
		err := object.CloseSession(sessionId, 0, "Normal user exit")
		if err != nil {
			logs.Error(fmt.Sprintf("kubeproxy.serveConnect(): failed to close the session: %s: %s", sessionId, err.Error()))
		}
	}()

	if len(command) > 0 {
		message, err := terminal.CheckCommand(sessionId, c.user, c.asset, c.clientIp, strings.Join(command, " "), r.Context().Done(), nil)
		if err != nil {
			message = err.Error()
		}
		if message != "" {
			c.command.Error = message
			c.command.StatusCode = http.StatusForbidden
			c.save()
			writeForbidden(w, message)
			return
		}
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	var ended atomic.Pointer[string]
	object.AddSessionCloser(sessionId, func(code int, msg string) {
		ended.Store(&msg)
		cancel()
	})
	c.addLiveSession(sessionId)

	if stdin && tty {
		c.relay(w, r.WithContext(ctx), sessionId, &ended)
		return
	}
	c.forward(w, r.WithContext(ctx))
}

// openSession starts the session of a container opened with kubectl, listed with the others and
// ended with the kubeconfig.
func (c *call) openSession(r *http.Request, target string) (string, error) {
	session := &object.Session{
		Account:       c.ts.token.Account,
		Creator:       c.ts.token.User,
		ClientIp:      c.clientIp,
		UserAgent:     r.UserAgent(),
		ClientIpDesc:  util.GetDescFromIP(c.clientIp),
		UserAgentDesc: r.UserAgent(),
	}
	_, err := object.CreateSession(session, c.ts.token.Asset, SessionModeKubectl, object.GetSessionOperations(c.actions))
	if err != nil {
		return "", err
	}
	sessionId := session.GetId()
	session.Target = target
	session.Status = object.Connected
	session.StartTime = util.GetCurrentTime()
	session.Reviewed = true
	_, err = object.UpdateSession(sessionId, session)
	if err != nil {
		return "", err
	}

	if !c.ts.addChild(sessionId) {
		_ = object.CloseSession(sessionId, object.PermissionRevokedCode, "The kubeconfig has been revoked")
		return "", fmt.Errorf("the kubeconfig has been revoked")
	}
	return sessionId, nil
}

// addLiveSession ends the session when the user may no longer reach the namespace in it.
func (c *call) addLiveSession(sessionId string) {
	liveSession := &object.LiveSession{User: c.user, AssetId: c.ts.token.Asset, Account: c.ts.token.Account, ClientIp: c.clientIp}
	liveSession.OnActions = func(actions []string) {
		asset, err := object.GetAsset(c.ts.token.Asset)
		if err != nil || asset == nil {
			return
		}
		_, scope, err := object.GetKubernetesAccess(liveSession.User, asset, c.ts.token.Account, c.clientIp)
		if err == nil && !c.req.IsAllowed(scope) {
			go func() {
				_ = object.CloseSession(sessionId, object.PermissionRevokedCode, fmt.Sprintf("The permission to the namespace: %s has been revoked", c.req.Namespace))
			}()
		}
	}
	object.AddLiveSession(sessionId, liveSession)
}

func (c *call) audit(action string, detail string, r *http.Request) {
	object.AddAuditRecord(&object.AuditEvent{
		Organization: c.asset.Owner,
		User:         c.user.Name,
		ClientIp:     c.clientIp,
		UserAgent:    r.UserAgent(),
		Method:       "Kubernetes",
		Action:       action,
		Asset:        c.ts.token.Asset,
		Detail:       detail,
	})
}

// relay passes a shell opened with kubectl exec -it through the terminal recorder, which reads
// its commands, applies the command rules to them and records the terminal.
func (c *call) relay(w http.ResponseWriter, r *http.Request, sessionId string, ended *atomic.Pointer[string]) {
	ctx, done, ok := c.ts.begin(r.Context(), c.req)
	defer done()
	if !ok {
		writeStatus(w, http.StatusUnauthorized, "Unauthorized", "the kubeconfig has been revoked")
		return
	}

	subprotocols := []string{}
	for _, protocol := range websocket.Subprotocols(r) {
		if protocol == object.KubernetesExecSubprotocolV5 || protocol == object.KubernetesExecSubprotocol {
			subprotocols = append(subprotocols, protocol)
		}
	}
	if len(subprotocols) == 0 {
		c.command.Error = "unsupported subprotocols"
		c.command.StatusCode = http.StatusBadRequest
		c.save()
		writeStatus(w, http.StatusBadRequest, "BadRequest", fmt.Sprintf("Casvisor opens a terminal only with the subprotocol %s or %s", object.KubernetesExecSubprotocolV5, object.KubernetesExecSubprotocol))
		return
	}

	up, resp, err := c.client.DialWebSocket(c.req.Path, r.URL.RawQuery, subprotocols)
	if err != nil {
		c.command.Error = err.Error()
		if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
			c.command.StatusCode = resp.StatusCode
			c.save()
			for _, key := range []string{"Content-Type"} {
				w.Header().Set(key, resp.Header.Get(key))
			}
			w.WriteHeader(resp.StatusCode)
			_, _ = io.Copy(w, io.LimitReader(resp.Body, 64*1024))
			_ = resp.Body.Close()
			return
		}
		c.command.StatusCode = http.StatusBadGateway
		c.save()
		writeStatus(w, http.StatusBadGateway, "ServiceUnavailable", err.Error())
		return
	}
	defer up.Close()

	header := http.Header{}
	header.Set("Sec-WebSocket-Protocol", up.Subprotocol())
	down, err := upgrader.Upgrade(w, r, header)
	if err != nil {
		return
	}
	defer down.Close()
	c.command.StatusCode = http.StatusSwitchingProtocols
	c.save()

	var upMu, downMu sync.Mutex
	writeUp := func(messageType int, data []byte) error {
		upMu.Lock()
		defer upMu.Unlock()
		_ = up.SetWriteDeadline(time.Now().Add(time.Minute))
		return up.WriteMessage(messageType, data)
	}
	writeDown := func(messageType int, data []byte) error {
		downMu.Lock()
		defer downMu.Unlock()
		_ = down.SetWriteDeadline(time.Now().Add(time.Minute))
		return down.WriteMessage(messageType, data)
	}

	recorder := terminal.NewRecorder(terminal.Options{
		SessionId: sessionId,
		User:      c.user,
		Asset:     c.asset,
		ClientIp:  c.clientIp,
		Send: func(p []byte) error {
			return writeUp(websocket.BinaryMessage, append([]byte{object.KubernetesChannelStdin}, p...))
		},
		Notify: func(p []byte) {
			_ = writeDown(websocket.BinaryMessage, append([]byte{object.KubernetesChannelStdout}, p...))
		},
	})
	defer recorder.Close()

	// the end of the session is told to kubectl as the status of the command, which it shows
	go func() {
		<-ctx.Done()
		message := ended.Load()
		if cause := context.Cause(ctx); message == nil && (errors.Is(cause, errRevoked) || errors.Is(cause, errNotCovered)) {
			text := "The session has ended, " + cause.Error()
			message = &text
		}
		if message != nil {
			status, _ := json.Marshal(map[string]string{"status": "Failure", "message": *message, "reason": "Forbidden"})
			_ = writeDown(websocket.BinaryMessage, append([]byte{object.KubernetesChannelError}, status...))
			_ = writeDown(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		}
		_ = up.Close()
		_ = down.Close()
	}()

	go func() {
		defer down.Close()
		for {
			messageType, data, err := up.ReadMessage()
			if err != nil {
				_ = writeDown(websocket.CloseMessage, getCloseMessage(err))
				return
			}
			if messageType == websocket.BinaryMessage && len(data) > 1 && (data[0] == object.KubernetesChannelStdout || data[0] == object.KubernetesChannelStderr) {
				recorder.Output(data[1:])
			}
			if writeDown(messageType, data) != nil {
				return
			}
		}
	}()

	for {
		messageType, data, err := down.ReadMessage()
		if err != nil {
			_ = writeUp(websocket.CloseMessage, getCloseMessage(err))
			return
		}
		if messageType == websocket.BinaryMessage && len(data) > 0 {
			switch data[0] {
			case object.KubernetesChannelStdin:
				recorder.Input(data[1:])
				continue
			case object.KubernetesChannelResize:
				var size struct {
					Width  int
					Height int
				}
				if json.Unmarshal(data[1:], &size) == nil {
					recorder.Resize(size.Width, size.Height)
				}
			}
		}
		if writeUp(messageType, data) != nil {
			return
		}
	}
}

// getCloseMessage passes on how one side closed the WebSocket.
func getCloseMessage(err error) []byte {
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) && closeErr.Code != websocket.CloseNoStatusReceived && closeErr.Code != websocket.CloseAbnormalClosure {
		return websocket.FormatCloseMessage(closeErr.Code, closeErr.Text)
	}
	return websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
}

func (c *call) refuseSpdy(w http.ResponseWriter) {
	writeStatus(w, http.StatusBadRequest, "BadRequest", fmt.Sprintf("Casvisor opens %s only over WebSockets, not SPDY: use kubectl 1.31 or later", c.req.Subresource))
}
