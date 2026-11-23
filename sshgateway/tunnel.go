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
	"net"
	"slices"

	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
	"golang.org/x/crypto/ssh"
)

// handleTunnel forwards a port to an asset itself, for clients of any protocol:
// "ssh -p 2222 -N -L 13389:win-01:3389 alice@casvisor", then mstsc connects to
// localhost:13389. Casvisor opens the connection, through the asset's network
// gateway when it has one, and passes the bytes through as they are.
func (g *gatewayConn) handleTunnel(newChannel ssh.NewChannel) {
	if newChannel.ChannelType() != "direct-tcpip" {
		_ = newChannel.Reject(ssh.Prohibited, "only TCP ports can be forwarded to assets")
		return
	}

	var payload struct {
		Host       string
		Port       uint32
		OriginHost string
		OriginPort uint32
	}
	err := ssh.Unmarshal(newChannel.ExtraData(), &payload)
	if err != nil {
		_ = newChannel.Reject(ssh.ConnectionFailed, err.Error())
		return
	}

	asset, err := g.findAsset(payload.Host)
	if err != nil {
		_ = newChannel.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	if asset == nil {
		_ = newChannel.Reject(ssh.ConnectionFailed, fmt.Sprintf("the asset: %s does not exist, forward ports to assets with \"ssh -p <port> -N -L <local port>:<asset>:<asset port> %s@<host>\"", payload.Host, g.user.Name))
		return
	}
	if g.tunnelAssetId != "" && asset.GetId() != g.tunnelAssetId {
		_ = newChannel.Reject(ssh.Prohibited, fmt.Sprintf("this token only forwards ports to the asset: %s", g.tunnelAssetId))
		return
	}
	if int(payload.Port) != asset.Port {
		_ = newChannel.Reject(ssh.ConnectionFailed, fmt.Sprintf("the asset: %s can only be reached on its port %d", asset.Name, asset.Port))
		return
	}

	actions, err := object.GetAssetActions(g.user, asset, g.clientIp)
	if err != nil {
		_ = newChannel.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	if !slices.Contains(actions, object.ActionConnect) {
		_ = newChannel.Reject(ssh.Prohibited, fmt.Sprintf("you have no permission to connect to the asset: %s", asset.Name))
		return
	}
	if !slices.Contains(actions, object.ActionForward) {
		_ = newChannel.Reject(ssh.Prohibited, fmt.Sprintf("you have no permission to forward ports to the asset: %s", asset.Name))
		return
	}

	conn, err := object.DialAsset(asset)
	if err != nil {
		_ = newChannel.Reject(ssh.ConnectionFailed, err.Error())
		return
	}

	err = g.openTunnel(asset)
	if err != nil {
		_ = conn.Close()
		_ = newChannel.Reject(ssh.ConnectionFailed, err.Error())
		return
	}

	channel, reqs, err := newChannel.Accept()
	if err != nil {
		_ = conn.Close()
		return
	}
	go ssh.DiscardRequests(reqs)

	go func() {
		_, _ = io.Copy(conn, channel)
		if tcpConn, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = tcpConn.CloseWrite()
		} else {
			_ = conn.Close()
		}
	}()
	_, _ = io.Copy(channel, conn)
	_ = channel.Close()
	_ = conn.Close()
}

// openTunnel opens the session of the asset's port forward the first time it is used
// in this connection, listed in the web console where an administrator can stop it.
func (g *gatewayConn) openTunnel(asset *object.Asset) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.tunnels == nil {
		g.tunnels = map[string]string{}
	}
	if g.tunnels[asset.GetId()] != "" {
		return nil
	}

	userAgent := string(g.conn.ClientVersion())
	session := &object.Session{
		Creator:       util.GetIdFromOwnerAndName(g.user.Owner, g.user.Name),
		ClientIp:      g.clientIp,
		UserAgent:     userAgent,
		ClientIpDesc:  util.GetDescFromIP(g.clientIp),
		UserAgentDesc: userAgent,
	}
	_, err := object.CreateSession(session, asset.GetId(), "tunnel", []string{})
	if err != nil {
		return err
	}

	session.Status = object.Connected
	session.StartTime = util.GetCurrentTime()
	session.Reviewed = true
	_, err = object.UpdateSession(session.GetId(), session)
	if err != nil {
		return err
	}

	sessionId := session.GetId()
	g.tunnels[asset.GetId()] = sessionId
	object.AddSessionCloser(sessionId, func(code int, msg string) {
		_ = g.conn.Close()
	})
	object.AddLiveSession(sessionId, &object.LiveSession{
		User:     g.user,
		AssetId:  asset.GetId(),
		ClientIp: g.clientIp,
		OnActions: func(actions []string) {
			if !slices.Contains(actions, object.ActionForward) {
				go func() {
					_ = object.CloseSession(sessionId, object.PermissionRevokedCode, fmt.Sprintf("The permission to forward ports to the asset: %s has been revoked", asset.Name))
				}()
			}
		},
	})

	g.auditAsset(asset, "ssh-tunnel", net.JoinHostPort(asset.Host, fmt.Sprintf("%d", asset.Port)))
	return nil
}
