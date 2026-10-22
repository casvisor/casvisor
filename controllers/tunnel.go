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
	"fmt"
	"slices"

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
)

// AddAssetTunnel
// @Title AddAssetTunnel
// @Tag Session API
// @Description add session
// @Param   assetId    query   string  true        "The id of asset"
// @Success 200 {object} Response
// @router /add-asset-tunnel [get]
func (c *ApiController) AddAssetTunnel() {
	assetId := c.Input().Get("assetId")

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

	actions, err := object.GetAssetActions(user, asset, c.getAuthzClientIp())
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !slices.Contains(actions, object.ActionConnect) {
		c.ResponseError(fmt.Sprintf("you have no permission to connect to the asset: %s", assetId))
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

	session := &object.Session{
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

	c.ResponseOk(session)
}
