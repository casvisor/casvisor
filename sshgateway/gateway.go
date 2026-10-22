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

// Package sshgateway lets ssh, scp, sftp, Xshell and VS Code Remote reach SSH assets
// through Casvisor: "ssh -p 2222 <asset>@<casvisor host>", signed in with a public key
// the user added in the web console, and held to the same permissions as the web terminal.
package sshgateway

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/object"
	"golang.org/x/crypto/ssh"
)

const sshKeyExtension = "casvisor-ssh-key"

func IsEnabled() bool {
	return conf.GetConfigString("sshGatewayPort") != "" && !conf.GetConfigBool("isDemoMode")
}

func Start() {
	if !IsEnabled() {
		return
	}

	go func() {
		for {
			err := listen(conf.GetConfigString("sshGatewayPort"))
			logs.Error(fmt.Sprintf("SSH gateway stopped, restarting in 10 seconds: %s", err.Error()))
			time.Sleep(10 * time.Second)
		}
	}()
}

func listen(port string) error {
	signer, err := object.GetSshHostKeySigner()
	if err != nil {
		return err
	}

	config := &ssh.ServerConfig{
		PublicKeyCallback: checkPublicKey,
		ServerVersion:     "SSH-2.0-Casvisor",
	}
	config.AddHostKey(signer)

	listener, err := net.Listen("tcp", net.JoinHostPort("", port))
	if err != nil {
		return err
	}
	defer listener.Close()

	logs.Info(fmt.Sprintf("SSH gateway listening on port %s, host key %s", port, ssh.FingerprintSHA256(signer.PublicKey())))
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go handleConn(conn, config)
	}
}

func checkPublicKey(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	sshKey, err := object.GetSshKeyByFingerprint(ssh.FingerprintSHA256(key))
	if err != nil {
		return nil, err
	}
	if sshKey == nil {
		return nil, fmt.Errorf("unknown public key")
	}

	publicKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(sshKey.PublicKey))
	if err != nil || !bytes.Equal(publicKey.Marshal(), key.Marshal()) {
		return nil, fmt.Errorf("unknown public key")
	}

	return &ssh.Permissions{Extensions: map[string]string{sshKeyExtension: sshKey.GetId()}}, nil
}

func handleConn(conn net.Conn, config *ssh.ServerConfig) {
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	serverConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		_ = conn.Close()
		return
	}
	_ = conn.SetDeadline(time.Time{})
	defer serverConn.Close()

	go rejectRequests(reqs)

	g := &gatewayConn{
		conn:     serverConn,
		clientIp: getHost(conn.RemoteAddr()),
	}
	defer g.close()

	err = g.signIn()
	if err != nil {
		refuse(chans, err.Error())
		return
	}

	for newChannel := range chans {
		switch newChannel.ChannelType() {
		case "session":
			go g.handleSession(newChannel)
		case "direct-tcpip", "direct-streamlocal@openssh.com":
			go g.handleForward(newChannel)
		default:
			_ = newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
		}
	}
}

func rejectRequests(reqs <-chan *ssh.Request) {
	for req := range reqs {
		if req.WantReply {
			_ = req.Reply(false, nil)
		}
	}
}

// refuse tells the user why the connection can't go on: ssh only shows what comes
// over a session channel, an error during the handshake would read "Permission denied".
func refuse(chans <-chan ssh.NewChannel, message string) {
	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.Prohibited, message)
			continue
		}

		channel, reqs, err := newChannel.Accept()
		if err != nil {
			return
		}
		go func() {
			for req := range reqs {
				switch req.Type {
				case "shell", "exec", "subsystem":
					_ = req.Reply(true, nil)
					writeError(channel, message)
					exit(channel, 1)
				default:
					if req.WantReply {
						_ = req.Reply(true, nil)
					}
				}
			}
		}()
	}
}

func writeError(channel ssh.Channel, message string) {
	_, _ = channel.Stderr().Write([]byte("casvisor: " + strings.ReplaceAll(message, "\n", "\r\n") + "\r\n"))
}

func exit(channel ssh.Channel, status uint32) {
	_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
	_ = channel.Close()
}

func getHost(addr net.Addr) string {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}
