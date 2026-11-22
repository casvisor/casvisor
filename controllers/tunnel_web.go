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

package controllers

import (
	"fmt"

	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/webproxy"
)

// GetWebAssetUrl
// @Title GetWebAssetUrl
// @Tag Session API
// @Description connect the session of a Web asset, and return the address that opens the website on its subdomain of webAssetDomain; the address signs the browser in to the session, once and within a minute
// @Param   sessionId    query    string  true     "The id of the session, from add-asset-tunnel"
// @Success 200 {object} Response The address of the website
// @router /get-web-asset-url [post]
func (c *ApiController) GetWebAssetUrl() {
	sessionId := c.Input().Get("sessionId")
	session, asset, _, err := c.getTunnelSessionAndAsset(sessionId)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if asset.Protocol != object.ProtocolWeb {
		closeTunnelSession(sessionId, "Not a website")
		c.ResponseError(fmt.Sprintf("the asset: %s is not a website", asset.GetId()))
		return
	}

	user := c.GetSessionUser()
	clientIp := c.getAuthzClientIp()
	actions, err := object.GetAssetAccountActions(user, asset, &session.Account, clientIp)
	if err != nil {
		closeTunnelSession(sessionId, err.Error())
		c.ResponseError(err.Error())
		return
	}

	webSession, url, err := webproxy.NewSession(&webproxy.SessionOptions{
		Id:      sessionId,
		User:    user,
		Asset:   asset,
		Actions: actions,
		Request: c.Ctx.Request,
	})
	if err != nil {
		closeTunnelSession(sessionId, err.Error())
		c.ResponseError(err.Error())
		return
	}

	err = setSessionConnected(sessionId, session, 0, 0)
	if err != nil {
		closeTunnelSession(sessionId, err.Error())
		c.ResponseError(err.Error())
		return
	}
	object.AddLiveSession(sessionId, &object.LiveSession{
		User:      user,
		AssetId:   asset.GetId(),
		Account:   session.Account,
		ClientIp:  clientIp,
		OnActions: webSession.SetActions,
	})

	c.ResponseOk(url)
}
