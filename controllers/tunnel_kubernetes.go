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

	user := c.GetSessionUser()
	if user == nil {
		c.ResponseError("please sign in first")
		return
	}

	asset, err := object.GetAsset(assetId)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if asset == nil {
		c.ResponseError(fmt.Sprintf("the asset: %s does not exist", assetId))
		return
	}
	if asset.Protocol != object.ProtocolKubernetes {
		c.ResponseError(fmt.Sprintf("the asset: %s is not a Kubernetes cluster", assetId))
		return
	}

	actions, err := object.GetAssetAccountActions(user, asset, &account, c.getAuthzClientIp())
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !slices.Contains(actions, object.ActionConnect) {
		c.ResponseError(fmt.Sprintf("you have no permission to connect to the asset: %s as the account: %s", assetId, account))
		return
	}

	if account != "" && account != asset.Username {
		asset, err = asset.WithAccount(account)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}
	} else if asset.Password == "" {
		asset.Password = credential.Password
	}

	pods, err := object.GetKubernetesPods(asset, namespace)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(pods)
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
		User:      c.GetSessionUser(),
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
	c.addLiveSession(sessionId, asset)
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
