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

package object

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/agent"
	"github.com/casvisor/casvisor/util"
	"golang.org/x/crypto/ssh"
	"xorm.io/core"
)

const (
	agentKeepaliveInterval = 15 * time.Second
	agentKeepaliveTimeout  = 30 * time.Second
)

// agentConn is the connection of an online agent, which Casvisor opens a
// channel on for every connection to an asset behind it.
type agentConn struct {
	conn    *ssh.ServerConn
	version string
	ip      string
}

var (
	agentsMu sync.Mutex
	agents   = map[string]*agentConn{}
)

func getAgent(id string) *agentConn {
	agentsMu.Lock()
	defer agentsMu.Unlock()
	return agents[id]
}

func disconnectAgent(id string) {
	a := getAgent(id)
	if a != nil {
		_ = a.conn.Close()
	}
}

// GetNetworkGatewayByAgentKey finds the enabled agent gateway the key derived from a token belongs to.
func GetNetworkGatewayByAgentKey(key ssh.PublicKey) (*NetworkGateway, error) {
	fingerprint := ssh.FingerprintSHA256(key)
	gateway := NetworkGateway{AgentKey: fingerprint}
	existed, err := adapter.engine.Get(&gateway)
	if err != nil {
		return nil, err
	}
	if !existed || gateway.AgentKey != fingerprint {
		return nil, fmt.Errorf("unknown agent token")
	}
	if !gateway.IsAgent() {
		return nil, fmt.Errorf("the network gateway: %s is not an agent gateway", gateway.GetId())
	}
	if !gateway.IsEnabled {
		return nil, fmt.Errorf("the network gateway: %s is disabled", gateway.GetId())
	}
	return &gateway, nil
}

// GenerateNetworkGatewayToken replaces the token of an agent gateway, the agent
// with the old token is disconnected. Only the fingerprint of the key derived
// from the token is kept, so the token can't be shown again.
func GenerateNetworkGatewayToken(gateway *NetworkGateway) (string, error) {
	if !gateway.IsAgent() {
		return "", fmt.Errorf("the network gateway: %s is not an agent gateway", gateway.GetId())
	}

	token, err := agent.NewToken()
	if err != nil {
		return "", err
	}
	fingerprint, err := agent.GetTokenFingerprint(token)
	if err != nil {
		return "", err
	}

	_, err = adapter.engine.ID(core.PK{gateway.Owner, gateway.Name}).Cols("agent_key").Update(&NetworkGateway{AgentKey: fingerprint})
	if err != nil {
		return "", err
	}

	disconnectAgent(gateway.GetId())
	return token, nil
}

// RevokeNetworkGatewayToken removes the token of an agent gateway and disconnects its agent.
func RevokeNetworkGatewayToken(gateway *NetworkGateway) error {
	_, err := adapter.engine.ID(core.PK{gateway.Owner, gateway.Name}).Cols("agent_key").Update(&NetworkGateway{AgentKey: ""})
	if err != nil {
		return err
	}

	disconnectAgent(gateway.GetId())
	return nil
}

func sendAgentConfig(gateway *NetworkGateway) {
	a := getAgent(gateway.GetId())
	if a == nil {
		return
	}
	go func() {
		err := a.sendConfig(gateway)
		if err != nil {
			logs.Warning(fmt.Sprintf("failed to send the config to the agent of the network gateway: %s: %s", gateway.GetId(), err.Error()))
			_ = a.conn.Close()
		}
	}()
}

func (a *agentConn) sendConfig(gateway *NetworkGateway) error {
	allowedNetworks := gateway.AllowedNetworks
	if allowedNetworks == nil {
		allowedNetworks = []string{}
	}
	payload, err := json.Marshal(agent.Config{AllowedNetworks: allowedNetworks})
	if err != nil {
		return err
	}

	ok, _, err := a.conn.SendRequest(agent.ConfigRequestType, true, payload)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("the agent refused the config")
	}
	return nil
}

func (a *agentConn) ping() error {
	timer := time.AfterFunc(agentKeepaliveTimeout, func() {
		_ = a.conn.Close()
	})
	defer timer.Stop()
	_, _, err := a.conn.SendRequest(agent.KeepaliveRequest, true, nil)
	return err
}

func updateAgentStatus(gateway *NetworkGateway, cols ...string) {
	gateway.LastOnlineTime = util.GetCurrentTime()
	cols = append(cols, "last_online_time")
	_, err := adapter.engine.ID(core.PK{gateway.Owner, gateway.Name}).Cols(cols...).Update(gateway)
	if err != nil {
		logs.Warning(fmt.Sprintf("failed to update the agent status of the network gateway: %s: %s", gateway.GetId(), err.Error()))
	}
}

// ServeAgent keeps the connection of the agent of gateway until it closes. The
// agent opens nothing on it, Casvisor opens a channel for each asset connection.
func ServeAgent(gateway *NetworkGateway, conn *ssh.ServerConn, chans <-chan ssh.NewChannel, reqs <-chan *ssh.Request, ip string) {
	defer conn.Close()
	go ssh.DiscardRequests(reqs)
	go func() {
		for newChannel := range chans {
			_ = newChannel.Reject(ssh.Prohibited, "the agent can't open channels")
		}
	}()

	a := &agentConn{
		conn:    conn,
		version: strings.TrimPrefix(string(conn.ClientVersion()), "SSH-2.0-CasvisorAgent_"),
		ip:      ip,
	}
	err := a.sendConfig(gateway)
	if err != nil {
		logs.Warning(fmt.Sprintf("failed to send the config to the agent of the network gateway: %s: %s", gateway.GetId(), err.Error()))
		return
	}

	id := gateway.GetId()
	agentsMu.Lock()
	old := agents[id]
	agents[id] = a
	agentsMu.Unlock()
	if old != nil {
		logs.Info(fmt.Sprintf("the agent of the network gateway: %s connected again from %s, closing the connection from %s", id, ip, old.ip))
		_ = old.conn.Close()
	}

	// The token may have been revoked or replaced while the agent was signing in.
	current, err := getNetworkGateway(gateway.Owner, gateway.Name)
	if err != nil || current == nil || current.AgentKey != gateway.AgentKey || !current.IsAgent() || !current.IsEnabled {
		_ = conn.Close()
	}

	gateway.AgentVersion = a.version
	gateway.AgentIp = ip
	updateAgentStatus(gateway, "agent_version", "agent_ip")
	logs.Info(fmt.Sprintf("the agent %s of the network gateway: %s connected from %s", a.version, id, ip))

	go func() {
		ticker := time.NewTicker(agentKeepaliveInterval)
		defer ticker.Stop()
		lastUpdate := time.Now()
		for range ticker.C {
			if a.ping() != nil {
				return
			}
			if time.Since(lastUpdate) >= time.Minute {
				updateAgentStatus(gateway)
				lastUpdate = time.Now()
			}
		}
	}()

	_ = conn.Wait()

	agentsMu.Lock()
	if agents[id] == a {
		delete(agents, id)
	}
	agentsMu.Unlock()
	updateAgentStatus(gateway)
	logs.Info(fmt.Sprintf("the agent of the network gateway: %s from %s disconnected", id, ip))
}

// PingAgent checks that the agent of gateway is online and answering.
func PingAgent(gateway *NetworkGateway) (string, error) {
	a := getAgent(gateway.GetId())
	if a == nil {
		return "", gateway.getOfflineError()
	}
	err := a.ping()
	if err != nil {
		return "", fmt.Errorf("the agent of the network gateway: %s did not answer: %s", gateway.GetId(), err.Error())
	}
	return a.version, nil
}

func (gateway *NetworkGateway) getOfflineError() error {
	if gateway.AgentKey == "" {
		return fmt.Errorf("the network gateway: %s has no agent token, generate one and start the agent with it", gateway.GetId())
	}
	if gateway.LastOnlineTime == "" {
		return fmt.Errorf("the agent of the network gateway: %s has never connected", gateway.GetId())
	}
	return fmt.Errorf("the agent of the network gateway: %s is offline, last online at %s", gateway.GetId(), gateway.LastOnlineTime)
}

func (gateway *NetworkGateway) dialAgent(address string) (net.Conn, error) {
	host, portString, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		return nil, err
	}

	if ip := net.ParseIP(host); ip != nil {
		networks, err := agent.ParseNetworks(gateway.AllowedNetworks)
		if err != nil {
			return nil, err
		}
		if !agent.IsAllowed(networks, ip) {
			return nil, fmt.Errorf("%s is outside the networks the network gateway: %s may reach: %s", address, gateway.GetId(), strings.Join(gateway.AllowedNetworks, ", "))
		}
	}

	a := getAgent(gateway.GetId())
	if a == nil {
		return nil, gateway.getOfflineError()
	}

	channel, reqs, err := a.conn.OpenChannel(agent.DialChannelType, ssh.Marshal(agent.DialRequest{Host: host, Port: uint32(port)}))
	if err != nil {
		if openErr, ok := err.(*ssh.OpenChannelError); ok {
			return nil, fmt.Errorf("the agent of the network gateway: %s failed to connect to %s: %s", gateway.GetId(), address, openErr.Message)
		}
		return nil, fmt.Errorf("the agent of the network gateway: %s failed to connect to %s: %s", gateway.GetId(), address, err.Error())
	}
	go ssh.DiscardRequests(reqs)

	conn := &channelConn{Channel: channel, local: a.conn.LocalAddr(), remote: &net.TCPAddr{IP: net.ParseIP(host), Port: port}}
	return &forwardedConn{Conn: conn}, nil
}

// channelConn is an SSH channel as a net.Conn, its deadlines are left to forwardedConn.
type channelConn struct {
	ssh.Channel
	local  net.Addr
	remote net.Addr
}

func (c *channelConn) LocalAddr() net.Addr {
	return c.local
}

func (c *channelConn) RemoteAddr() net.Addr {
	return c.remote
}

func (c *channelConn) SetDeadline(t time.Time) error {
	return nil
}

func (c *channelConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *channelConn) SetWriteDeadline(t time.Time) error {
	return nil
}
