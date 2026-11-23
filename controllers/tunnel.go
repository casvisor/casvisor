// Copyright 2023 The casbin Authors. All Rights Reserved.
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

package controllers

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
)

const (
	Normal             int = 0
	SessionNotFound    int = 800
	NewTunnelError     int = 801
	ForcedDisconnect   int = 802
	ParametersError    int = 804
	AssetNotFound      int = 805
	SessionUpdateError int = 806
	PermissionDenied   int = 807
	InternalError      int = 808
)

const tunnelCredentialTtl = time.Minute

// tunnelCredential is what the user typed for an asset that keeps no credentials. It is held for
// the tunnel of the new session, so that it never goes into a WebSocket URL.
type tunnelCredential struct {
	username string
	password string
	expires  time.Time
}

var (
	tunnelCredentials   = map[string]tunnelCredential{}
	tunnelCredentialsMu sync.Mutex
)

func putTunnelCredential(sessionId string, username string, password string) {
	tunnelCredentialsMu.Lock()
	defer tunnelCredentialsMu.Unlock()

	now := time.Now()
	for id, credential := range tunnelCredentials {
		if now.After(credential.expires) {
			delete(tunnelCredentials, id)
		}
	}
	tunnelCredentials[sessionId] = tunnelCredential{username: username, password: password, expires: now.Add(tunnelCredentialTtl)}
}

// takeTunnelCredential returns the credentials typed for a session, only once.
func takeTunnelCredential(sessionId string) (string, string) {
	tunnelCredentialsMu.Lock()
	defer tunnelCredentialsMu.Unlock()

	credential, ok := tunnelCredentials[sessionId]
	delete(tunnelCredentials, sessionId)
	if !ok || time.Now().After(credential.expires) {
		return "", ""
	}
	return credential.username, credential.password
}

// AddAssetTunnel
// @Title AddAssetTunnel
// @Tag Session API
// @Description add a session to connect to an asset; the username and password typed for an asset that keeps none go in the body, and are used once by the session's tunnel within a minute
// @Param   assetId    query   string  true        "The id of asset"
// @Param   account    query   string  false       "The username of the account to connect as, the asset's own account when empty"
// @Param   body       body    object.TunnelCredential  false  "The username and password, when the asset keeps none"
// @Success 200 {object} Response
// @router /add-asset-tunnel [post]
func (c *ApiController) AddAssetTunnel() {
	assetId := c.Input().Get("assetId")
	account := c.Input().Get("account")
	var credential object.TunnelCredential
	if len(c.Ctx.Input.RequestBody) > 0 {
		err := json.Unmarshal(c.Ctx.Input.RequestBody, &credential)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}
	}

	user := c.GetSessionUser()
	if user == nil {
		c.ResponseError("please sign in first")
		return
	}

	asset, err := object.GetAsset(assetId)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if asset == nil {
		c.ResponseError(fmt.Sprintf("the asset: %s does not exist", assetId))
		return
	}

	actions, err := object.GetAssetAccountActions(user, asset, &account, c.getAuthzClientIp())
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !slices.Contains(actions, object.ActionConnect) {
		c.ResponseError(fmt.Sprintf("you have no permission to connect to the asset: %s as the account: %s", assetId, account))
		return
	}
	err = object.CheckConnectMethod(user, asset, account, object.ConnectMethodWeb)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	if conf.GetConfigBool("isDemoMode") {
		count, err := object.GetActiveSessionCountByClientIp(c.getClientIp(), demoSessionDuration)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}
		if count >= demoMaxSessionsPerIp {
			c.ResponseError(fmt.Sprintf("in demo mode, each client IP can open at most %d sessions at the same time", demoMaxSessionsPerIp))
			return
		}
	}

	if account == asset.Username {
		account = ""
	}
	watermark, err := object.GetSessionWatermark(user, asset, account, c.getAuthzClientIp())
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	session := &object.Session{
		Watermark:     watermark,
		Account:       account,
		Creator:       getUserId(user),
		ClientIp:      c.getClientIp(),
		UserAgent:     c.getUserAgent(),
		ClientIpDesc:  util.GetDescFromIP(c.getClientIp()),
		UserAgentDesc: util.GetDescFromUserAgent(c.getUserAgent()),
	}

	session, err = object.CreateSession(session, assetId, "native", object.GetSessionOperations(actions))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	if credential.Username != "" || credential.Password != "" {
		putTunnelCredential(session.GetId(), credential.Username, credential.Password)
	}
	c.ResponseOk(session)
}
