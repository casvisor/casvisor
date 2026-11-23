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
	"crypto/rand"
	"encoding/base32"
	"strings"
	"sync"
	"time"
)

const (
	// ClientTokenSsh signs in to the SSH gateway on an SSH asset, for ssh, Xshell, WinSCP and the like
	ClientTokenSsh = "ssh"
	// ClientTokenTunnel signs in to the SSH gateway to forward a local port to an asset
	ClientTokenTunnel = "tunnel"
	// ClientTokenRdp lets mstsc reach an RDP asset through Casvisor's Remote Desktop Gateway
	ClientTokenRdp = "rdp"

	ClientTokenTimeout = 2 * time.Minute
	clientTokenPrefix  = "cv"
)

// ClientToken lets a client on the user's machine open one connection to an asset, with the
// permissions checked when the user asked for it in the browser. It is used once, within minutes.
type ClientToken struct {
	Token      string
	Kind       string
	UserId     string
	AssetId    string
	Account    string
	ClientIp   string
	ExpireTime time.Time
}

var clientTokens sync.Map

// AddClientToken issues a token of letters and digits, which fits in an SSH username and a URL.
func AddClientToken(kind string, userId string, assetId string, account string, clientIp string) (*ClientToken, error) {
	b := make([]byte, 15)
	_, err := rand.Read(b)
	if err != nil {
		return nil, err
	}

	token := &ClientToken{
		Token:      clientTokenPrefix + strings.ToLower(base32.StdEncoding.EncodeToString(b)),
		Kind:       kind,
		UserId:     userId,
		AssetId:    assetId,
		Account:    account,
		ClientIp:   clientIp,
		ExpireTime: time.Now().Add(ClientTokenTimeout),
	}
	clientTokens.Store(token.Token, token)

	clientTokens.Range(func(key, value interface{}) bool {
		if time.Now().After(value.(*ClientToken).ExpireTime) {
			clientTokens.Delete(key)
		}
		return true
	})
	return token, nil
}

// IsClientToken tells whether a name looks like a token, so that the SSH gateway can tell it from
// an asset name before looking it up.
func IsClientToken(name string) bool {
	return len(name) == len(clientTokenPrefix)+24 && strings.HasPrefix(name, clientTokenPrefix)
}

// ConsumeClientToken returns the token of the kind once, and nil when it is unknown, used or expired.
func ConsumeClientToken(token string, kinds ...string) *ClientToken {
	value, ok := clientTokens.LoadAndDelete(token)
	if !ok {
		return nil
	}
	res := value.(*ClientToken)
	if time.Now().After(res.ExpireTime) {
		return nil
	}
	for _, kind := range kinds {
		if res.Kind == kind {
			return res
		}
	}
	return nil
}
