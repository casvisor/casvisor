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
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/util"
)

const (
	// ClientTokenSsh signs in to the SSH gateway on an SSH asset, for ssh, Xshell, WinSCP and the like
	ClientTokenSsh = "ssh"
	// ClientTokenTunnel signs in to the SSH gateway to forward a local port to an asset
	ClientTokenTunnel = "tunnel"
	// ClientTokenRdp lets mstsc reach an RDP asset through Casvisor's Remote Desktop Gateway
	ClientTokenRdp = "rdp"
	// ClientTokenDb is the user and the password of a database client such as DBeaver, Navicat, mysql or psql
	// through the database proxy; database clients connect again and again, so it is used many times until it expires
	ClientTokenDb = "db"

	ClientTokenTimeout     = 2 * time.Minute
	KubernetesTokenTimeout = 8 * time.Hour
	clientTokenPrefix      = "cv"
	// dbTokenDefaultHours is how long a database token lasts unless dbProxyTokenHours says otherwise
	dbTokenDefaultHours = 8
)

// ClientToken lets a client on the user's machine open one connection to an asset, with the
// permissions checked when the user asked for it in the browser. It is used once, within minutes,
// except a database token, which lasts hours and opens a session for each connection.
type ClientToken struct {
	Id          string
	Token       string
	Kind        string
	UserId      string
	AssetId     string
	Account     string
	ClientIp    string
	CreatedTime time.Time
	ExpireTime  time.Time

	mu           sync.Mutex
	lastUsedTime time.Time
	lastUsedIp   string
	sessionIds   []string
	// approvedIps are the client IPs whose sign-in a login review approved for the token
	approvedIps []string
}

// ClientTokenInfo is a database token as the web console lists it, without the token itself.
type ClientTokenInfo struct {
	Id           string `json:"id"`
	Kind         string `json:"kind"`
	User         string `json:"user"`
	Asset        string `json:"asset"`
	Account      string `json:"account"`
	ClientIp     string `json:"clientIp"`
	Hint         string `json:"hint"`
	CreatedTime  string `json:"createdTime"`
	ExpireTime   string `json:"expireTime"`
	LastUsedTime string `json:"lastUsedTime"`
	LastUsedIp   string `json:"lastUsedIp"`
	Sessions     int    `json:"sessions"`
}

// GetDbTokenTimeout is how long a database token lasts, dbProxyTokenHours or 8 hours.
func GetDbTokenTimeout() time.Duration {
	hours, err := strconv.ParseFloat(conf.GetConfigString("dbProxyTokenHours"), 64)
	if err != nil || hours <= 0 {
		hours = dbTokenDefaultHours
	}
	return time.Duration(hours * float64(time.Hour))
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

	timeout := ClientTokenTimeout
	if kind == ClientTokenDb {
		timeout = GetDbTokenTimeout()
	}
	now := time.Now()
	token := &ClientToken{
		Id:          util.GenerateId(),
		Token:       value,
		Kind:        kind,
		UserId:      userId,
		AssetId:     assetId,
		Account:     account,
		ClientIp:    clientIp,
		CreatedTime: now,
		ExpireTime:  now.Add(timeout),
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
	value, ok := clientTokens.Load(token)
	if !ok || value.(*ClientToken).Kind == ClientTokenDb {
		return nil
	}
	value, ok = clientTokens.LoadAndDelete(token)
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

// GetDbToken returns the database token while it is valid, without using it up; nil means it is unknown, revoked or expired.
func GetDbToken(token string) *ClientToken {
	value, ok := clientTokens.Load(token)
	if !ok {
		return nil
	}
	res := value.(*ClientToken)
	if res.Kind != ClientTokenDb || time.Now().After(res.ExpireTime) {
		return nil
	}
	return res
}

// Use records a connection made with the token and the session it opened.
func (token *ClientToken) Use(clientIp string, sessionId string) {
	token.mu.Lock()
	defer token.mu.Unlock()
	token.lastUsedTime = time.Now()
	token.lastUsedIp = clientIp
	if sessionId != "" {
		token.sessionIds = append(token.sessionIds, sessionId)
	}
}

// EndSession forgets a session of the token once it has ended.
func (token *ClientToken) EndSession(sessionId string) {
	token.mu.Lock()
	defer token.mu.Unlock()
	token.sessionIds = slices.DeleteFunc(token.sessionIds, func(id string) bool { return id == sessionId })
}

// IsApproved tells whether a login review approved a sign-in with the token from the client IP.
func (token *ClientToken) IsApproved(clientIp string) bool {
	token.mu.Lock()
	defer token.mu.Unlock()
	return slices.Contains(token.approvedIps, clientIp)
}

func (token *ClientToken) Approve(clientIp string) {
	token.mu.Lock()
	defer token.mu.Unlock()
	if !slices.Contains(token.approvedIps, clientIp) {
		token.approvedIps = append(token.approvedIps, clientIp)
	}
}

func (token *ClientToken) getInfo() *ClientTokenInfo {
	token.mu.Lock()
	defer token.mu.Unlock()
	res := &ClientTokenInfo{
		Id:          token.Id,
		Kind:        token.Kind,
		User:        token.UserId,
		Asset:       token.AssetId,
		Account:     token.Account,
		ClientIp:    token.ClientIp,
		Hint:        token.Token[:6] + "...",
		CreatedTime: token.CreatedTime.Format(time.RFC3339),
		ExpireTime:  token.ExpireTime.Format(time.RFC3339),
		LastUsedIp:  token.lastUsedIp,
		Sessions:    len(token.sessionIds),
	}
	if !token.lastUsedTime.IsZero() {
		res.LastUsedTime = token.lastUsedTime.Format(time.RFC3339)
	}
	return res
}

// GetDbTokens lists the valid database tokens of a user, or of every user of an organization when
// userId is empty, and of everyone when owner is empty too, the newest first.
func GetDbTokens(owner string, userId string) []*ClientTokenInfo {
	res := []*ClientTokenInfo{}
	now := time.Now()
	clientTokens.Range(func(key, value interface{}) bool {
		token := value.(*ClientToken)
		if token.Kind != ClientTokenDb || now.After(token.ExpireTime) {
			return true
		}
		tokenOwner, _ := util.GetOwnerAndNameFromIdNoCheck(token.UserId)
		if (userId != "" && token.UserId != userId) || (owner != "" && tokenOwner != owner) {
			return true
		}
		res = append(res, token.getInfo())
		return true
	})
	slices.SortFunc(res, func(a, b *ClientTokenInfo) int { return strings.Compare(b.CreatedTime, a.CreatedTime) })
	return res
}

// GetDbTokenById finds a valid database token by its id, which the web console uses instead of the token.
func GetDbTokenById(id string) *ClientToken {
	var res *ClientToken
	clientTokens.Range(func(key, value interface{}) bool {
		token := value.(*ClientToken)
		if token.Kind == ClientTokenDb && token.Id == id {
			res = token
			return false
		}
		return true
	})
	return res
}

// RevokeDbToken removes a database token and ends the sessions opened with it.
func RevokeDbToken(token *ClientToken, message string) {
	clientTokens.Delete(token.Token)

	token.mu.Lock()
	sessionIds := slices.Clone(token.sessionIds)
	token.mu.Unlock()
	for _, sessionId := range sessionIds {
		err := CloseSession(sessionId, PermissionRevokedCode, message)
		if err != nil {
			logs.Error(fmt.Sprintf("RevokeDbToken(): failed to close the session: %s: %s", sessionId, err.Error()))
		}
	}
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

// RestoreClientToken puts back a token whose connection ended before it was of any use, as mstsc connects
// again at once after it checks the certificate of the remote computer.
func RestoreClientToken(token *ClientToken) {
	if time.Now().Before(token.ExpireTime) {
		clientTokens.Store(token.Token, token)
	}
}
