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

package sshgateway

import (
	"fmt"
	"io"
	"path"
	"strings"
	"sync"

	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/terminal"
	"golang.org/x/crypto/ssh"
)

type ptyRequest struct {
	Term   string
	Cols   uint32
	Rows   uint32
	Width  uint32
	Height uint32
	Modes  string
}

type windowChange struct {
	Cols   uint32
	Rows   uint32
	Width  uint32
	Height uint32
}

// sessionChannel is one "session" channel of the client. The pty and env requests
// that come before the shell, exec or subsystem are kept and replayed on the asset,
// since the asset may only be known once the user has picked it from the menu.
type sessionChannel struct {
	g    *gatewayConn
	down ssh.Channel

	mu       sync.Mutex
	up       ssh.Channel
	pty      *ptyRequest
	envs     [][]byte
	started  bool
	recorder *terminal.Recorder
}

func (g *gatewayConn) handleSession(newChannel ssh.NewChannel) {
	down, reqs, err := newChannel.Accept()
	if err != nil {
		return
	}

	s := &sessionChannel{g: g, down: down}
	for req := range reqs {
		s.mu.Lock()
		up := s.up
		recorder := s.recorder
		if up != nil {
			s.mu.Unlock()
			ok, _ := up.SendRequest(req.Type, req.WantReply, req.Payload)
			if req.WantReply {
				_ = req.Reply(ok, nil)
			}

			size := &windowChange{}
			if recorder != nil && req.Type == "window-change" && ssh.Unmarshal(req.Payload, size) == nil {
				recorder.Resize(int(size.Cols), int(size.Rows))
			}
			continue
		}

		switch req.Type {
		case "pty-req":
			pty := &ptyRequest{}
			err = ssh.Unmarshal(req.Payload, pty)
			if err == nil {
				s.pty = pty
			}
			_ = req.Reply(err == nil, nil)
		case "env":
			s.envs = append(s.envs, req.Payload)
			_ = req.Reply(true, nil)
		case "window-change":
			size := &windowChange{}
			if ssh.Unmarshal(req.Payload, size) == nil && s.pty != nil {
				s.pty.Cols, s.pty.Rows, s.pty.Width, s.pty.Height = size.Cols, size.Rows, size.Width, size.Height
			}
		case "shell", "exec", "subsystem":
			if s.started {
				_ = req.Reply(false, nil)
				break
			}
			s.started = true
			go s.start(req)
		default:
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
		s.mu.Unlock()
	}

	s.mu.Lock()
	up := s.up
	s.mu.Unlock()
	if up != nil {
		_ = up.Close()
	}
}

func (s *sessionChannel) fail(req *ssh.Request, message string) {
	if req != nil {
		_ = req.Reply(true, nil)
	}
	writeError(s.down, message)
	exit(s.down, 1)
}

func (s *sessionChannel) start(req *ssh.Request) {
	switch req.Type {
	case "subsystem":
		var payload struct{ Name string }
		err := ssh.Unmarshal(req.Payload, &payload)
		if err != nil || payload.Name != "sftp" {
			_ = req.Reply(false, nil)
			return
		}
		s.serveSftp(req)
	case "exec":
		var payload struct{ Command string }
		err := ssh.Unmarshal(req.Payload, &payload)
		if err != nil {
			_ = req.Reply(false, nil)
			return
		}
		if s.g.getAsset() == nil {
			s.fail(req, "no asset is selected, connect with \"ssh -p <port> <asset>@<host>\" to run commands")
			return
		}

		action := getTransferAction(payload.Command)
		if action != "" && !s.g.can(action) {
			s.fail(req, fmt.Sprintf("you have no permission to %s files on the asset: %s", strings.ToLower(action), s.g.getAsset().Name))
			return
		}

		_, err = s.g.getClient()
		if err != nil {
			s.fail(req, err.Error())
			return
		}
		rule, err := terminal.CheckCommand(s.g.getSessionId(), s.g.user, s.g.getAsset(), s.g.clientIp, payload.Command)
		if err != nil {
			s.fail(req, err.Error())
			return
		}
		if rule != nil {
			s.fail(req, terminal.GetBlockedMessage(rule))
			return
		}

		s.openUpstream(req.Type, req.Payload, req)
	case "shell":
		if s.g.getAsset() == nil {
			_ = req.Reply(true, nil)
			if !s.runMenu() {
				exit(s.down, 0)
				return
			}
			s.openUpstream("shell", nil, nil)
			return
		}
		s.openUpstream(req.Type, req.Payload, req)
	}
}

// openUpstream opens the same kind of session on the asset, replays the pty and env
// requests, sends the shell or exec request, and then pipes the two channels together.
func (s *sessionChannel) openUpstream(reqType string, payload []byte, req *ssh.Request) {
	client, err := s.g.getClient()
	if err != nil {
		s.fail(req, err.Error())
		return
	}

	up, upReqs, err := client.OpenChannel("session", nil)
	if err != nil {
		s.fail(req, err.Error())
		return
	}

	s.mu.Lock()
	if s.pty != nil {
		_, err = up.SendRequest("pty-req", true, ssh.Marshal(s.pty))
	}
	for _, env := range s.envs {
		// most servers refuse env variables outside of AcceptEnv, which is fine
		_, _ = up.SendRequest("env", true, env)
	}
	ok := false
	if err == nil {
		ok, err = up.SendRequest(reqType, true, payload)
	}
	if err != nil || !ok {
		s.mu.Unlock()
		_ = up.Close()
		message := fmt.Sprintf("the asset refused the %s request", reqType)
		if err != nil {
			message = err.Error()
		}
		s.fail(req, message)
		return
	}
	s.up = up
	// an interactive shell is followed on a virtual screen, for its commands and for live watching
	var recorder *terminal.Recorder
	if reqType == "shell" && s.pty != nil {
		recorder = terminal.NewRecorder(terminal.Options{
			SessionId: s.g.getSessionId(),
			User:      s.g.user,
			Asset:     s.g.getAsset(),
			ClientIp:  s.g.clientIp,
			Cols:      int(s.pty.Cols),
			Rows:      int(s.pty.Rows),
			Send: func(p []byte) error {
				_, err := up.Write(p)
				return err
			},
			Notify: func(p []byte) {
				_, _ = s.down.Write(p)
			},
		})
		defer recorder.Close()
		s.recorder = recorder
	}
	s.mu.Unlock()

	if req != nil {
		_ = req.Reply(true, nil)
	}

	var stdout, stderr io.Writer = s.down, s.down.Stderr()
	if recorder != nil {
		stdout = recordedWriter{w: s.down, recorder: recorder}
		stderr = recordedWriter{w: s.down.Stderr(), recorder: recorder}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		_, _ = io.Copy(stdout, up)
		wg.Done()
	}()
	go func() {
		_, _ = io.Copy(stderr, up.Stderr())
		wg.Done()
	}()
	go func() {
		if recorder == nil {
			_, _ = io.Copy(up, s.down)
		} else {
			buf := make([]byte, 32*1024)
			for {
				n, err := s.down.Read(buf)
				if n > 0 {
					recorder.Input(buf[:n])
				}
				if err != nil {
					break
				}
			}
		}
		_ = up.CloseWrite()
	}()

	for upReq := range upReqs {
		ok, _ := s.down.SendRequest(upReq.Type, upReq.WantReply, upReq.Payload)
		if upReq.WantReply {
			_ = upReq.Reply(ok, nil)
		}
	}
	wg.Wait()
	_ = s.down.Close()
}

// getTransferAction tells the permission scp and rsync need from their server-side
// command line: "scp -t" and "rsync --server" receive files, "scp -f" and
// "rsync --server --sender" send them. Newer scp uses SFTP instead.
func getTransferAction(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}

	switch path.Base(fields[0]) {
	case "scp":
		for _, field := range fields[1:] {
			if !strings.HasPrefix(field, "-") || strings.HasPrefix(field, "--") {
				continue
			}
			if strings.Contains(field, "f") {
				return object.ActionDownload
			}
			if strings.Contains(field, "t") {
				return object.ActionUpload
			}
		}
	case "rsync":
		if !strings.Contains(command, "--server") {
			return ""
		}
		if strings.Contains(command, "--sender") {
			return object.ActionDownload
		}
		return object.ActionUpload
	}
	return ""
}

// handleForward passes a port forwarding channel ("ssh -L", VS Code Remote) through to
// the asset, so the connection is opened from the asset, as with a direct SSH login.
func (g *gatewayConn) handleForward(newChannel ssh.NewChannel) {
	if g.getAsset() == nil {
		_ = newChannel.Reject(ssh.Prohibited, "no asset is selected")
		return
	}
	if !g.can(object.ActionForward) {
		_ = newChannel.Reject(ssh.Prohibited, fmt.Sprintf("you have no permission to forward ports on the asset: %s", g.getAsset().Name))
		return
	}

	client, err := g.getClient()
	if err != nil {
		_ = newChannel.Reject(ssh.ConnectionFailed, err.Error())
		return
	}

	up, upReqs, err := client.OpenChannel(newChannel.ChannelType(), newChannel.ExtraData())
	if err != nil {
		if openErr, ok := err.(*ssh.OpenChannelError); ok {
			_ = newChannel.Reject(openErr.Reason, openErr.Message)
		} else {
			_ = newChannel.Reject(ssh.ConnectionFailed, err.Error())
		}
		return
	}

	down, downReqs, err := newChannel.Accept()
	if err != nil {
		_ = up.Close()
		return
	}
	go ssh.DiscardRequests(downReqs)
	go ssh.DiscardRequests(upReqs)

	destination := getForwardDestination(newChannel.ChannelType(), newChannel.ExtraData())
	g.mu.Lock()
	if g.forwards == nil {
		g.forwards = map[string]bool{}
	}
	isNew := !g.forwards[destination]
	g.forwards[destination] = true
	g.mu.Unlock()
	if isNew {
		g.audit("ssh-forward", destination)
	}

	go func() {
		_, _ = io.Copy(up, down)
		_ = up.CloseWrite()
	}()
	_, _ = io.Copy(down, up)
	_ = down.Close()
	_ = up.Close()
}

func getForwardDestination(channelType string, extraData []byte) string {
	if channelType == "direct-tcpip" {
		var payload struct {
			Host       string
			Port       uint32
			OriginHost string
			OriginPort uint32
		}
		if ssh.Unmarshal(extraData, &payload) == nil {
			return fmt.Sprintf("%s:%d", payload.Host, payload.Port)
		}
	} else {
		var payload struct {
			SocketPath string
			Reserved0  string
			Reserved1  uint32
		}
		if ssh.Unmarshal(extraData, &payload) == nil {
			return payload.SocketPath
		}
	}
	return channelType
}

// recordedWriter sends the asset's output to the client and to the recorder.
type recordedWriter struct {
	w        io.Writer
	recorder *terminal.Recorder
}

func (w recordedWriter) Write(p []byte) (int, error) {
	w.recorder.Output(p)
	return w.w.Write(p)
}
