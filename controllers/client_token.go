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
	"slices"
	"strings"
	"time"

	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/dbproxy"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/rdgateway"
	"github.com/casvisor/casvisor/sshgateway"
	"github.com/casvisor/casvisor/util"
)

// ClientToken is what the web console needs to launch a client on the user's machine.
type ClientToken struct {
	Token string `json:"token"`
	Kind  string `json:"kind"`
	// Username is the account on the asset, filled in for mstsc
	Username string `json:"username"`
	Expires  int    `json:"expires"`
}

// AddClientToken
// @Title AddClientToken
// @Tag Session API
// @Description issue a one-time token, valid for two minutes, that lets a client on the user's machine open one connection to an asset: kind "ssh" signs in to the SSH gateway on an SSH asset (ssh, Xshell, WinSCP), "tunnel" forwards a local port to an asset through the SSH gateway, and "rdp" lets mstsc reach an RDP asset through Casvisor's Remote Desktop Gateway; "tunnel" and "rdp" pass the traffic through without recording it and need the Forward action. Kind "db" is the user and the password of a database client (DBeaver, Navicat, mysql, psql) on a MySQL or PostgreSQL asset through the database proxy; it lasts dbProxyTokenHours (8 hours by default), opens a session for every connection and can be revoked
// @Param   assetId    query    string  true     "The id of the asset"
// @Param   account    query    string  false    "The system account of the asset to connect as"
// @Param   kind       query    string  true     "ssh, tunnel, rdp or db"
// @Success 200 {object} controllers.ClientToken The Response object
// @router /add-client-token [post]
func (c *ApiController) AddClientToken() {
	user := c.GetSessionUser()
	if user == nil {
		c.ResponseError("please sign in first")
		return
	}
	if conf.GetConfigBool("isDemoMode") {
		c.ResponseError("clients on your own machine are not available in demo mode")
		return
	}

	assetId := c.Input().Get("assetId")
	account := c.Input().Get("account")
	kind := c.Input().Get("kind")

	asset, err := object.GetAsset(assetId)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if asset == nil {
		c.ResponseError(fmt.Sprintf("the asset: %s does not exist", assetId))
		return
	}

	switch kind {
	case object.ClientTokenSsh:
		if !strings.EqualFold(asset.Protocol, "SSH") {
			c.ResponseError(fmt.Sprintf("the asset: %s uses %s, only SSH assets open in SSH clients", asset.Name, asset.Protocol))
			return
		}
		fallthrough
	case object.ClientTokenTunnel:
		if !sshgateway.IsEnabled() {
			c.ResponseError("the SSH gateway is off, set sshGatewayPort to turn it on")
			return
		}
	case object.ClientTokenDb:
		if !(asset.Protocol == object.ProtocolMysql && dbproxy.IsMysqlEnabled()) && !(asset.Protocol == object.ProtocolPostgresql && dbproxy.IsPostgresqlEnabled()) {
			c.ResponseError(fmt.Sprintf("the database proxy is off for %s assets, set dbProxyMysqlPort or dbProxyPostgresqlPort to turn it on", asset.Protocol))
			return
		}
	case object.ClientTokenRdp:
		if asset.Protocol != "RDP" {
			c.ResponseError(fmt.Sprintf("the asset: %s uses %s, only RDP assets open in Remote Desktop Connection", asset.Name, asset.Protocol))
			return
		}
		if !rdgateway.IsEnabled() {
			c.ResponseError("the Remote Desktop Gateway is off")
			return
		}
	default:
		c.ResponseError(fmt.Sprintf("unknown kind: %s", kind))
		return
	}

	actions, err := object.GetAssetAccountActions(user, asset, &account, c.getAuthzClientIp())
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !slices.Contains(actions, object.ActionConnect) {
		c.ResponseError(fmt.Sprintf("you have no permission to connect to the asset: %s", asset.Name))
		return
	}
	// the bytes of a forwarded port or of mstsc go through as they are, without a recording
	if kind != object.ClientTokenSsh && kind != object.ClientTokenDb && !slices.Contains(actions, object.ActionForward) {
		c.ResponseError(fmt.Sprintf("you have no permission to forward ports to the asset: %s, which a client on your own machine needs", asset.Name))
		return
	}
	method := object.ConnectMethodForward
	switch kind {
	case object.ClientTokenSsh:
		method = object.ConnectMethodSsh
	case object.ClientTokenRdp:
		method = object.ConnectMethodRdp
	case object.ClientTokenDb:
		method = object.ConnectMethodDatabase
	}
	err = object.CheckConnectMethod(user, asset, account, method)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	username := ""
	if kind == object.ClientTokenRdp || kind == object.ClientTokenDb {
		target, err := asset.WithAccount(account)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}
		username = target.Username
	}

	token, err := object.AddClientToken(kind, getUserId(user), asset.GetId(), account, c.getClientIp())
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	if kind == object.ClientTokenDb {
		object.AddAuditRecord(&object.AuditEvent{
			Organization: asset.Owner,
			User:         user.Name,
			ClientIp:     c.getClientIp(),
			UserAgent:    c.getUserAgent(),
			Method:       asset.Protocol,
			Action:       "add-database-token",
			Asset:        asset.GetId(),
			Detail:       token.Id,
		})
	}

	c.ResponseOk(ClientToken{
		Token:    token.Token,
		Kind:     kind,
		Username: username,
		Expires:  int(time.Until(token.ExpireTime).Seconds()),
	})
}

// GetDatabaseTokens
// @Title GetDatabaseTokens
// @Tag Session API
// @Description list the database tokens that are valid: the user's own, or those of every user of the organization for an administrator; the tokens themselves are not shown
// @Success 200 {array} object.ClientTokenInfo The Response object
// @router /get-database-tokens [get]
func (c *ApiController) GetDatabaseTokens() {
	user := c.GetSessionUser()
	if user == nil {
		c.ResponseError("please sign in first")
		return
	}

	if object.IsAdminUser(user) {
		owner := user.Owner
		if owner == "built-in" {
			owner = ""
		}
		c.ResponseOk(object.GetDbTokens(owner, ""))
		return
	}
	c.ResponseOk(object.GetDbTokens(user.Owner, getUserId(user)))
}

// RevokeDatabaseToken
// @Title RevokeDatabaseToken
// @Tag Session API
// @Description revoke a database token, which also disconnects the sessions opened with it; users revoke their own tokens, administrators those of their organization
// @Param   tokenId    query    string  true     "The id of the token, from get-database-tokens"
// @Success 200 {object} controllers.Response The Response object
// @router /revoke-database-token [post]
func (c *ApiController) RevokeDatabaseToken() {
	user := c.GetSessionUser()
	if user == nil {
		c.ResponseError("please sign in first")
		return
	}

	token := object.GetDbTokenById(c.Input().Get("tokenId"))
	if token == nil {
		c.ResponseError("the token does not exist or has expired")
		return
	}
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(token.UserId)
	isAdmin := object.IsAdminUser(user) && (user.Owner == "built-in" || user.Owner == owner)
	if token.UserId != getUserId(user) && !isAdmin {
		c.ResponseError("you have no permission to revoke the token")
		return
	}

	object.RevokeDbToken(token, fmt.Sprintf("The database token has been revoked by %s", user.Name))
	object.AddAuditRecord(&object.AuditEvent{
		Organization: owner,
		User:         user.Name,
		ClientIp:     c.getClientIp(),
		UserAgent:    c.getUserAgent(),
		Method:       "POST",
		Action:       "revoke-database-token",
		Asset:        token.AssetId,
		Detail:       token.Id,
	})
	c.ResponseOk()
}
