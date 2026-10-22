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
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const dialTimeout = 10 * time.Second

func (asset *Asset) getAddress() string {
	return net.JoinHostPort(asset.Host, strconv.Itoa(asset.Port))
}

// DialAsset opens a TCP connection to the asset, directly or through its network gateway.
func DialAsset(asset *Asset) (net.Conn, error) {
	address := asset.getAddress()
	if asset.NetworkGateway == "" {
		return net.DialTimeout("tcp", address, dialTimeout)
	}

	gateway, err := getNetworkGateway(asset.Owner, asset.NetworkGateway)
	if err != nil {
		return nil, err
	}
	if gateway == nil {
		return nil, fmt.Errorf("the network gateway: %s of the asset: %s does not exist", asset.NetworkGateway, asset.GetId())
	}
	if !gateway.IsEnabled {
		return nil, fmt.Errorf("the network gateway: %s of the asset: %s is disabled", asset.NetworkGateway, asset.GetId())
	}

	if gateway.IsAgent() {
		return gateway.dialAgent(address)
	}
	return gateway.Dial(address)
}

// Dial opens a TCP connection to address from the gateway. The connection has
// an SSH connection to the gateway of its own, closed together with it.
func (gateway *NetworkGateway) Dial(address string) (net.Conn, error) {
	client, err := gateway.DialSsh()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the network gateway: %s: %s", gateway.GetId(), err.Error())
	}

	conn, err := client.Dial("tcp", address)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("the network gateway: %s failed to connect to %s: %s", gateway.GetId(), address, err.Error())
	}

	return &forwardedConn{Conn: conn, client: client}, nil
}

func (gateway *NetworkGateway) DialSsh() (*ssh.Client, error) {
	auth, err := getSshAuthMethods(gateway.GetId(), gateway.Password, gateway.PrivateKey)
	if err != nil {
		return nil, err
	}

	config := &ssh.ClientConfig{
		User: gateway.Username,
		Auth: auth,
		HostKeyCallback: getHostKeyCallback("network gateway", gateway.GetId(), &gateway.Fingerprint, func(fingerprint string) error {
			return updateNetworkGatewayFingerprint(gateway.GetId(), fingerprint)
		}),
		Timeout: dialTimeout,
	}

	address := net.JoinHostPort(gateway.Host, strconv.Itoa(gateway.Port))
	conn, err := net.DialTimeout("tcp", address, dialTimeout)
	if err != nil {
		return nil, err
	}
	return newSshClient(conn, address, config)
}

// forwardedConn is a connection forwarded by a gateway or an agent. SSH channels have no
// deadlines, so a deadline closes the connection instead, which is enough for
// the handshake timeouts the connections are given.
type forwardedConn struct {
	net.Conn
	client *ssh.Client

	mu    sync.Mutex
	timer *time.Timer
}

func (c *forwardedConn) Close() error {
	c.mu.Lock()
	if c.timer != nil {
		c.timer.Stop()
	}
	c.mu.Unlock()

	err := c.Conn.Close()
	if c.client != nil {
		_ = c.client.Close()
	}
	return err
}

func (c *forwardedConn) SetDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	if !t.IsZero() {
		c.timer = time.AfterFunc(time.Until(t), func() {
			_ = c.Close()
		})
	}
	return nil
}

func (c *forwardedConn) SetReadDeadline(t time.Time) error {
	return c.SetDeadline(t)
}

func (c *forwardedConn) SetWriteDeadline(t time.Time) error {
	return c.SetDeadline(t)
}
