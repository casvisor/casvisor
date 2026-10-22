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

// Package agent is the network gateway agent: "casvisor agent" runs in a network
// that Casvisor can't reach, connects out to Casvisor's SSH gateway port and
// keeps the connection open, and Casvisor reaches the assets there through it.
package agent

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"runtime/debug"
	"strings"

	"golang.org/x/crypto/ssh"
)

const (
	Username          = "casvisor-agent"
	DialChannelType   = "dial@casvisor.org"
	ConfigRequestType = "config@casvisor.org"
	KeepaliveRequest  = "keepalive@openssh.com"
	TokenPrefix       = "cva_"
)

// Version is set at build time with -ldflags "-X github.com/casvisor/casvisor/agent.Version=v1.2.3".
var Version = ""

type DialRequest struct {
	Host string
	Port uint32
}

type Config struct {
	AllowedNetworks []string `json:"allowedNetworks"`
}

func GetVersion() string {
	if Version != "" {
		return Version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	revision, modified := "", false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if len(revision) > 7 {
		revision = revision[:7]
	}
	if revision == "" {
		return "dev"
	}
	if modified {
		revision += "+dirty"
	}
	return revision
}

func NewToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// GetSigner derives the agent's SSH key from its token, so Casvisor only keeps
// the fingerprint of the public key and the token never goes over the wire.
func GetSigner(token string) (ssh.Signer, error) {
	if !strings.HasPrefix(token, TokenPrefix) {
		return nil, fmt.Errorf("invalid agent token, it should start with %s", TokenPrefix)
	}
	seed := sha256.Sum256([]byte("casvisor-agent:" + token))
	return ssh.NewSignerFromKey(ed25519.NewKeyFromSeed(seed[:]))
}

func GetTokenFingerprint(token string) (string, error) {
	signer, err := GetSigner(token)
	if err != nil {
		return "", err
	}
	return ssh.FingerprintSHA256(signer.PublicKey()), nil
}

// ParseNetworks parses CIDRs and single IP addresses.
func ParseNetworks(networks []string) ([]*net.IPNet, error) {
	res := []*net.IPNet{}
	for _, network := range networks {
		network = strings.TrimSpace(network)
		if network == "" {
			continue
		}
		if !strings.Contains(network, "/") {
			ip := net.ParseIP(network)
			if ip == nil {
				return nil, fmt.Errorf("invalid network: %s", network)
			}
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			network = fmt.Sprintf("%s/%d", network, bits)
		}
		_, ipNet, err := net.ParseCIDR(network)
		if err != nil {
			return nil, fmt.Errorf("invalid network: %s", network)
		}
		res = append(res, ipNet)
	}
	return res, nil
}

// IsAllowed reports whether ip is in one of the networks, no networks allow every address.
func IsAllowed(networks []*net.IPNet, ip net.IP) bool {
	if len(networks) == 0 {
		return true
	}
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
