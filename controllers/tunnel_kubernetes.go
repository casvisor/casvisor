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
	"slices"
	"strconv"
	"time"

	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/terminal"
	"github.com/gorilla/websocket"
)

const (
	kubernetesShellWait    = 2 * time.Second
	kubernetesPingInterval = 30 * time.Second
)

// the shells tried in turn, looked up in the PATH of the container, as many images have no bash
var kubernetesShells = [][]string{{"bash"}, {"sh"}}

// GetKubernetesPods
// @Title GetKubernetesPods
// @Tag Session API
// @Description list the namespaces and the pods of a namespace of a Kubernetes asset, with the Connect permission; the token typed for an asset that keeps none goes in the body
// @Param   assetId      query    string  true     "The id of the asset"
// @Param   account      query    string  false    "The username of the account to connect as, the asset's own account when empty"
// @Param   namespace    query    string  false    "The namespace, the namespace of the asset when empty"
// @Param   body         body     object.TunnelCredential  false  "The token in password, when the asset keeps none"
// @Success 200 {object} object.KubernetesPods
// @router /get-kubernetes-pods [post]
func (c *ApiController) GetKubernetesPods() {
	assetId := c.Input().Get("assetId")
	account := c.Input().Get("account")
	namespace := c.Input().Get("namespace")
	var credential object.TunnelCredential
	if len(c.Ctx.Input.RequestBody) > 0 {
		err := json.Unmarshal(c.Ctx.Input.RequestBody, &credential)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}
	}

	asset, _, scope, err := c.getKubernetesAsset(assetId, account, credential.Password, false)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	pods, err := object.GetKubernetesPods(asset, namespace, scope)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(pods)
}

// getKubernetesAsset returns the Kubernetes asset with the token of the account, the token typed
// for an asset that keeps none, after checking that the user may connect to it, and in the browser
// when checkMethod is set, with what the user may do on it and the namespaces the user may reach.
func (c *ApiController) getKubernetesAsset(assetId string, account string, password string, checkMethod bool) (*object.Asset, []string, *object.KubernetesScope, error) {
	user := c.GetSessionUser()
	if user == nil {
		return nil, nil, nil, fmt.Errorf("please sign in first")
	}

	asset, err := object.GetAsset(assetId)
	if err != nil {
		return nil, nil, nil, err
	}
	if asset == nil {
		return nil, nil, nil, fmt.Errorf("the asset: %s does not exist", assetId)
	}
	if asset.Protocol != object.ProtocolKubernetes {
		return nil, nil, nil, fmt.Errorf("the asset: %s is not a Kubernetes cluster", assetId)
	}

	actions, scope, err := object.GetKubernetesAccess(user, asset, account, c.getAuthzClientIp())
	if err != nil {
		return nil, nil, nil, err
	}
	if !slices.Contains(actions, object.ActionConnect) {
		return nil, nil, nil, fmt.Errorf("you have no permission to connect to the asset: %s as the account: %s", assetId, account)
	}
	if checkMethod {
		err = object.CheckConnectMethod(user, asset, account, object.ConnectMethodWeb)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	if account != "" && account != asset.Username {
		asset, err = asset.WithAccount(account)
		if err != nil {
			return nil, nil, nil, err
		}
	} else if asset.Password == "" {
		asset.Password = password
	}
	return asset, actions, scope, nil
}

type kubernetesFrame struct {
	channel byte
	data    []byte
}

func readKubernetesFrames(exec *object.KubernetesExec) <-chan kubernetesFrame {
	frames := make(chan kubernetesFrame, 64)
	go func() {
		defer close(frames)
		for {
			channel, data, err := exec.Read()
			if err != nil {
				return
			}
			frames <- kubernetesFrame{channel: channel, data: data}
		}
	}()
	return frames
}

// openKubernetesShell starts the first shell the container has. A shell that is missing ends
// the command with an error status at once, before any output; one that starts may print
// nothing for a while, so it is kept when nothing came within kubernetesShellWait.
func openKubernetesShell(client *object.KubernetesClient, namespace string, pod string, container string, cols int, rows int) (*object.KubernetesExec, <-chan kubernetesFrame, *kubernetesFrame, error) {
	var lastErr error
	for _, shell := range kubernetesShells {
		exec, err := client.Exec(namespace, pod, container, shell)
		if err != nil {
			return nil, nil, nil, err
		}
		_ = exec.Resize(cols, rows)

		frames := readKubernetesFrames(exec)
		timer := time.NewTimer(kubernetesShellWait)
		select {
		case frame, ok := <-frames:
			timer.Stop()
			if ok && frame.channel != object.KubernetesChannelError {
				return exec, frames, &frame, nil
			}

			_ = exec.Close()
			lastErr = fmt.Errorf("the shell: %s ended at once", shell[0])
			if ok {
				if err := object.GetKubernetesExecError(frame.data); err != nil {
					lastErr = err
				}
			}
		case <-timer.C:
			return exec, frames, nil, nil
		}
	}
	return nil, nil, nil, fmt.Errorf("failed to start a shell in the container: %s, %s", container, lastErr.Error())
}

// GetAssetKubernetes
// @Title GetAssetKubernetes
// @Tag Session API
// @Description open a shell in a container of a Kubernetes asset over a websocket, with the same messages as get-asset-ssh; commands are checked against the command rules and recorded, and the terminal is recorded
// @Param   sessionId    query    string  true     "The id of the session, from add-asset-tunnel"
// @Param   cols         query    int     true     "The columns of the terminal"
// @Param   rows         query    int     true     "The rows of the terminal"
// @Param   namespace    query    string  true     "The namespace of the pod"
// @Param   pod          query    string  true     "The pod"
// @Param   container    query    string  true     "The container of the pod"
// @router /get-asset-kubernetes [get]
func (c *ApiController) GetAssetKubernetes() {
	c.EnableRender = false
	ws, err := c.upgradeWebSocket(wsMaxMessage)
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
	namespace := c.Input().Get("namespace")
	pod := c.Input().Get("pod")
	container := c.Input().Get("container")
	for _, name := range []string{namespace, pod, container} {
		if !object.IsKubernetesName(name) {
			fail(ParametersError, fmt.Errorf("the namespace, pod or container: %q is invalid", name))
			return
		}
	}

	session, asset, code, err := c.getTunnelSessionAndAsset(sessionId)
	if err != nil {
		fail(code, err)
		return
	}
	if asset.Protocol != object.ProtocolKubernetes {
		fail(ParametersError, fmt.Errorf("the asset: %s is not a Kubernetes cluster", asset.GetId()))
		return
	}

	user := c.GetSessionUser()
	_, scope, err := object.GetKubernetesAccess(user, asset, session.Account, c.getAuthzClientIp())
	if err != nil {
		fail(PermissionDenied, err)
		return
	}
	// a shell is created in the pod, which a read-only permission doesn't allow
	if !scope.CanWrite(namespace) {
		fail(PermissionDenied, fmt.Errorf("you have no permission to open a shell in the namespace: %s", namespace))
		return
	}

	client, err := object.NewKubernetesClient(asset)
	if err != nil {
		fail(NewTunnelError, err)
		return
	}
	exec, frames, first, err := openKubernetesShell(client, namespace, pod, container, cols, rows)
	if err != nil {
		fail(NewTunnelError, err)
		return
	}
	defer exec.Close()

	recorder := terminal.NewRecorder(terminal.Options{
		SessionId: sessionId,
		User:      user,
		Asset:     asset,
		ClientIp:  c.getAuthzClientIp(),
		Cols:      cols,
		Rows:      rows,
		Send:      exec.Write,
		Notify: func(p []byte) {
			_, _ = stream.Write(p)
		},
	})
	defer recorder.Close()
	output := recordedWriter{stream: stream, recorder: recorder}

	session.Target = fmt.Sprintf("%s/%s/%s", namespace, pod, container)
	err = setSessionConnected(sessionId, session, cols, rows)
	if err != nil {
		fail(SessionUpdateError, err)
		return
	}
	c.addKubernetesLiveSession(sessionId, session.Account, namespace)
	object.AddSessionCloser(sessionId, func(code int, msg string) {
		_ = stream.WriteJSON(terminalMessage{Type: "error", Code: code, Message: msg})
		stream.Close(code, msg)
		_ = exec.Close()
	})
	_ = stream.WriteJSON(terminalMessage{Type: "connected"})

	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(kubernetesPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				_ = exec.Ping()
			}
		}
	}()

	go func() {
		handle := func(frame kubernetesFrame) {
			switch frame.channel {
			case object.KubernetesChannelStdout, object.KubernetesChannelStderr:
				_, _ = output.Write(frame.data)
			case object.KubernetesChannelError:
				if err := object.GetKubernetesExecError(frame.data); err != nil {
					_, _ = fmt.Fprintf(output, "\r\n\x1b[33m[Casvisor] %s\x1b[0m\r\n", err.Error())
				}
			}
		}
		if first != nil {
			handle(*first)
		}
		for frame := range frames {
			handle(frame)
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
				_ = exec.Resize(message.Cols, message.Rows)
				recorder.Resize(message.Cols, message.Rows)
			}
		}
	}

	closeTunnelSession(sessionId, "Normal user exit")
}

// addKubernetesLiveSession ends a shell once the user may no longer connect to the asset, or no
// longer open a shell in the namespace.
func (c *ApiController) addKubernetesLiveSession(sessionId string, account string, namespace string) {
	session, err := object.GetConnSession(sessionId)
	if err != nil || session == nil {
		return
	}
	assetId := session.Asset
	clientIp := c.getAuthzClientIp()
	liveSession := &object.LiveSession{User: c.GetSessionUser(), AssetId: assetId, Account: account, ClientIp: clientIp}
	liveSession.OnActions = func(actions []string) {
		operations := object.GetSessionOperations(actions)
		for _, operation := range session.Operations {
			if !slices.Contains(operations, operation) {
				go func() {
					_ = object.CloseSession(sessionId, object.PermissionRevokedCode, fmt.Sprintf("The permission to %s on the asset: %s has been revoked, connect again", operation, assetId))
				}()
				return
			}
		}

		asset, err := object.GetAsset(assetId)
		if err != nil || asset == nil {
			return
		}
		_, scope, err := object.GetKubernetesAccess(liveSession.User, asset, account, clientIp)
		if err == nil && !scope.CanWrite(namespace) {
			go func() {
				_ = object.CloseSession(sessionId, object.PermissionRevokedCode, fmt.Sprintf("The permission to open a shell in the namespace: %s has been revoked", namespace))
			}()
		}
	}
	object.AddLiveSession(sessionId, liveSession)
}
