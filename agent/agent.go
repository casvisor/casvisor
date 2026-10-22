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
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	dialTimeout       = 10 * time.Second
	keepaliveInterval = 15 * time.Second
	keepaliveTimeout  = 30 * time.Second
	maxRetryDelay     = 30 * time.Second
)

type client struct {
	server      string
	fingerprint string
	signer      ssh.Signer
	local       []*net.IPNet

	mu     sync.Mutex
	remote []*net.IPNet
}

// Main runs "casvisor agent --server <host:port> --token <token> --fingerprint <SHA256:...>".
func Main(args []string) {
	flags := flag.NewFlagSet("casvisor agent", flag.ExitOnError)
	server := flags.String("server", "", "address of Casvisor's SSH gateway, host:port")
	token := flags.String("token", "", "token of the network gateway, or set CASVISOR_AGENT_TOKEN")
	fingerprint := flags.String("fingerprint", "", "host key fingerprint of Casvisor's SSH gateway, SHA256:...")
	allow := flags.String("allow", "", "comma separated networks the agent may reach, checked on top of the ones set on the network gateway")
	version := flags.Bool("version", false, "print the version")
	_ = flags.Parse(args)

	if *version {
		fmt.Println(GetVersion())
		return
	}

	if *token == "" {
		*token = os.Getenv("CASVISOR_AGENT_TOKEN")
	}
	if *server == "" || *token == "" || *fingerprint == "" {
		flags.Usage()
		os.Exit(2)
	}
	if _, _, err := net.SplitHostPort(*server); err != nil {
		log.Fatalf("invalid --server: %s, it should be host:port", *server)
	}

	signer, err := GetSigner(*token)
	if err != nil {
		log.Fatal(err)
	}
	local, err := ParseNetworks(strings.Split(*allow, ","))
	if err != nil {
		log.Fatalf("invalid --allow: %s", err.Error())
	}

	c := &client{
		server:      *server,
		fingerprint: *fingerprint,
		signer:      signer,
		local:       local,
	}
	c.run()
}

func (c *client) run() {
	log.Printf("Casvisor agent %s connecting to %s", GetVersion(), c.server)
	delay := time.Second
	for {
		start := time.Now()
		err := c.connect()
		if time.Since(start) > 10*time.Second {
			delay = time.Second
		}
		log.Printf("disconnected from %s: %s, reconnecting in %s", c.server, err.Error(), delay)
		time.Sleep(delay)
		delay = min(delay*2, maxRetryDelay)
	}
}

func (c *client) connect() error {
	conn, err := net.DialTimeout("tcp", c.server, dialTimeout)
	if err != nil {
		return err
	}

	config := &ssh.ClientConfig{
		User:            Username,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(c.signer)},
		HostKeyCallback: c.checkHostKey,
		ClientVersion:   "SSH-2.0-CasvisorAgent_" + strings.NewReplacer("-", ".", " ", ".").Replace(GetVersion()),
		Timeout:         dialTimeout,
	}

	_ = conn.SetDeadline(time.Now().Add(keepaliveTimeout))
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, c.server, config)
	if err != nil {
		_ = conn.Close()
		if strings.Contains(err.Error(), "unable to authenticate") {
			return fmt.Errorf("the token was rejected, it may have been revoked or replaced, or the network gateway is disabled")
		}
		return err
	}
	_ = conn.SetDeadline(time.Time{})
	defer sshConn.Close()

	log.Printf("connected to %s", c.server)
	go c.handleRequests(reqs)
	go keepalive(sshConn)

	for newChannel := range chans {
		if newChannel.ChannelType() != DialChannelType {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}
		go c.handleDial(newChannel)
	}

	err = sshConn.Wait()
	if err == nil {
		err = io.EOF
	}
	return err
}

func (c *client) checkHostKey(hostname string, remote net.Addr, key ssh.PublicKey) error {
	fingerprint := ssh.FingerprintSHA256(key)
	if fingerprint != c.fingerprint {
		return fmt.Errorf("the host key of %s is %s, not %s given by --fingerprint", c.server, fingerprint, c.fingerprint)
	}
	return nil
}

func (c *client) handleRequests(reqs <-chan *ssh.Request) {
	for req := range reqs {
		if req.Type != ConfigRequestType {
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
			continue
		}

		var config Config
		err := json.Unmarshal(req.Payload, &config)
		var networks []*net.IPNet
		if err == nil {
			networks, err = ParseNetworks(config.AllowedNetworks)
		}
		if err != nil {
			log.Printf("invalid config from %s: %s", c.server, err.Error())
			_ = req.Reply(false, nil)
			continue
		}

		c.mu.Lock()
		c.remote = networks
		c.mu.Unlock()
		if len(networks) == 0 {
			log.Printf("allowed networks: all")
		} else {
			log.Printf("allowed networks: %s", strings.Join(config.AllowedNetworks, ", "))
		}
		_ = req.Reply(true, nil)
	}
}

func keepalive(conn ssh.Conn) {
	ticker := time.NewTicker(keepaliveInterval)
	defer ticker.Stop()
	for range ticker.C {
		timer := time.AfterFunc(keepaliveTimeout, func() {
			_ = conn.Close()
		})
		_, _, err := conn.SendRequest(KeepaliveRequest, true, nil)
		timer.Stop()
		if err != nil {
			return
		}
	}
}

func (c *client) isAllowed(ip net.IP) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return IsAllowed(c.local, ip) && IsAllowed(c.remote, ip)
}

func (c *client) handleDial(newChannel ssh.NewChannel) {
	var req DialRequest
	err := ssh.Unmarshal(newChannel.ExtraData(), &req)
	if err != nil {
		_ = newChannel.Reject(ssh.ConnectionFailed, "invalid dial request")
		return
	}
	address := net.JoinHostPort(req.Host, strconv.Itoa(int(req.Port)))

	ips, err := resolve(req.Host)
	if err != nil {
		_ = newChannel.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	allowed := []net.IP{}
	for _, ip := range ips {
		if c.isAllowed(ip) {
			allowed = append(allowed, ip)
		}
	}
	if len(allowed) == 0 {
		log.Printf("refused to connect to %s: outside the allowed networks", address)
		_ = newChannel.Reject(ssh.Prohibited, fmt.Sprintf("%s is outside the networks the agent may reach", address))
		return
	}

	var target net.Conn
	for _, ip := range allowed {
		target, err = net.DialTimeout("tcp", net.JoinHostPort(ip.String(), strconv.Itoa(int(req.Port))), dialTimeout)
		if err == nil {
			break
		}
	}
	if err != nil {
		log.Printf("failed to connect to %s: %s", address, err.Error())
		_ = newChannel.Reject(ssh.ConnectionFailed, err.Error())
		return
	}

	channel, reqs, err := newChannel.Accept()
	if err != nil {
		_ = target.Close()
		return
	}
	go ssh.DiscardRequests(reqs)

	log.Printf("connected to %s", address)
	pipe(channel, target)
	log.Printf("closed the connection to %s", address)
}

func resolve(host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

func pipe(a io.ReadWriteCloser, b io.ReadWriteCloser) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(a, b)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(b, a)
		done <- struct{}{}
	}()
	<-done
	_ = a.Close()
	_ = b.Close()
	<-done
}
