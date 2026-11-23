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
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/casvisor/casvisor/util"
)

const (
	// ClientTokenSsh signs in to the SSH gateway on an SSH asset, for ssh, Xshell, WinSCP and the like
	ClientTokenSsh = "ssh"
	// ClientTokenTunnel signs in to the SSH gateway to forward a local port to an asset
	ClientTokenTunnel = "tunnel"
	// ClientTokenRdp lets mstsc reach an RDP asset through Casvisor's Remote Desktop Gateway
	ClientTokenRdp = "rdp"

	ClientTokenTimeout     = 2 * time.Minute
	KubernetesTokenTimeout = 8 * time.Hour
	clientTokenPrefix      = "cv"
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

func newClientToken() (string, error) {
	b := make([]byte, 15)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return clientTokenPrefix + strings.ToLower(base32.StdEncoding.EncodeToString(b)), nil
}

// AddClientToken issues a token of letters and digits, which fits in an SSH username and a URL.
func AddClientToken(kind string, userId string, assetId string, account string, clientIp string) (*ClientToken, error) {
	value, err := newClientToken()
	if err != nil {
		return nil, err
	}

	token := &ClientToken{
		Token:      value,
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

const SessionModeKubeconfig = "kubeconfig"

// KubernetesToken signs kubectl in to the Kubernetes proxy of an asset from a kubeconfig, for
// many requests until it expires. Only a hash of the token is kept. Its requests are listed in a
// session of its own, which ends when it expires, and ending the session revokes it.
type KubernetesToken struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`

	Hash       string `xorm:"varchar(100) index" json:"-"`
	User       string `xorm:"varchar(200) index" json:"user"`
	Asset      string `xorm:"varchar(200) index" json:"asset"`
	Account    string `xorm:"varchar(100)" json:"account"`
	Session    string `xorm:"varchar(200) index" json:"session"`
	ClientIp   string `xorm:"varchar(100)" json:"clientIp"`
	ExpireTime string `xorm:"varchar(100)" json:"expireTime"`
	IsRevoked  bool   `json:"isRevoked"`
}

func (token *KubernetesToken) GetId() string {
	return util.GetIdFromOwnerAndName(token.Owner, token.Name)
}

func (token *KubernetesToken) IsActive(now time.Time) bool {
	expireTime, err := time.Parse(time.RFC3339, token.ExpireTime)
	return !token.IsRevoked && err == nil && now.Before(expireTime)
}

func getClientTokenHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// AddKubernetesToken issues a token for kubectl, with the session its requests go to, and
// returns the token itself, which is not kept.
func AddKubernetesToken(userId string, asset *Asset, account string, clientIp string, userAgent string) (*KubernetesToken, string, error) {
	value, err := newClientToken()
	if err != nil {
		return nil, "", err
	}

	session := &Session{
		Account:       account,
		Creator:       userId,
		ClientIp:      clientIp,
		UserAgent:     userAgent,
		ClientIpDesc:  util.GetDescFromIP(clientIp),
		UserAgentDesc: userAgent,
	}
	_, err = CreateSession(session, asset.GetId(), SessionModeKubeconfig, []string{})
	if err != nil {
		return nil, "", err
	}
	session.Target = "kubectl"
	session.Status = Connected
	session.StartTime = util.GetCurrentTime()
	session.Reviewed = true
	_, err = UpdateSession(session.GetId(), session)
	if err != nil {
		return nil, "", err
	}

	token := &KubernetesToken{
		Owner:       asset.Owner,
		Name:        util.GenerateId(),
		CreatedTime: util.GetCurrentTime(),
		Hash:        getClientTokenHash(value),
		User:        userId,
		Asset:       asset.GetId(),
		Account:     account,
		Session:     session.GetId(),
		ClientIp:    clientIp,
		ExpireTime:  time.Now().Add(KubernetesTokenTimeout).Format(time.RFC3339),
	}
	_, err = adapter.engine.Insert(token)
	if err != nil {
		return nil, "", err
	}
	return token, value, nil
}

// GetKubernetesTokenByValue returns the active token, nil when it is unknown, revoked or expired.
func GetKubernetesTokenByValue(value string) (*KubernetesToken, error) {
	if !IsClientToken(value) {
		return nil, nil
	}

	token := &KubernetesToken{}
	existed, err := adapter.engine.Where("hash = ?", getClientTokenHash(value)).Get(token)
	if err != nil {
		return nil, err
	}
	if !existed || !token.IsActive(time.Now()) {
		return nil, nil
	}
	return token, nil
}

func GetKubernetesToken(id string) (*KubernetesToken, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	token := &KubernetesToken{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(token)
	if err != nil {
		return nil, err
	}
	if !existed {
		return nil, nil
	}
	return token, nil
}

// GetKubernetesTokens returns the active tokens of the asset, of all users when userId is empty.
func GetKubernetesTokens(assetId string, userId string) ([]*KubernetesToken, error) {
	tokens := []*KubernetesToken{}
	err := adapter.engine.Desc("created_time").Find(&tokens, &KubernetesToken{Asset: assetId, User: userId})
	if err != nil {
		return nil, err
	}

	res := []*KubernetesToken{}
	now := time.Now()
	for _, token := range tokens {
		if token.IsActive(now) {
			res = append(res, token)
		}
	}
	return res, nil
}

// GetExpiredKubernetesTokens returns the tokens that have expired without being revoked.
func GetExpiredKubernetesTokens() ([]*KubernetesToken, error) {
	tokens := []*KubernetesToken{}
	err := adapter.engine.Where("is_revoked = ?", false).Find(&tokens)
	if err != nil {
		return nil, err
	}

	res := []*KubernetesToken{}
	now := time.Now()
	for _, token := range tokens {
		if !token.IsActive(now) {
			res = append(res, token)
		}
	}
	return res, nil
}

func RevokeKubernetesTokens(sessionId string) error {
	_, err := adapter.engine.Where("session = ?", sessionId).Cols("is_revoked").Update(&KubernetesToken{IsRevoked: true})
	return err
}
