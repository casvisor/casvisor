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
	"encoding/json"
	"fmt"

	"github.com/beego/beego/logs"
	"github.com/beego/beego/utils/pagination"
	"github.com/casvisor/casvisor/agent"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/sshgateway"
	"github.com/casvisor/casvisor/util"
)

// GetNetworkGateways
// @Title GetNetworkGateways
// @Tag Network Gateway API
// @Description get all network gateways
// @Param   owner     query    string  true        "The owner of network gateways"
// @Success 200 {object} object.NetworkGateway The Response object
// @router /get-network-gateways [get]
func (c *ApiController) GetNetworkGateways() {
	owner := c.Input().Get("owner")
	limit := c.Input().Get("pageSize")
	page := c.Input().Get("p")
	field := c.Input().Get("field")
	value := c.Input().Get("value")
	sortField := c.Input().Get("sortField")
	sortOrder := c.Input().Get("sortOrder")

	if limit == "" || page == "" {
		gateways, err := object.GetMaskedNetworkGateways(object.GetNetworkGateways(owner))
		if err != nil {
			c.ResponseError(err.Error())
			return
		}

		c.ResponseOk(gateways)
	} else {
		limit := util.ParseInt(limit)
		count, err := object.GetNetworkGatewayCount(owner, field, value)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}

		paginator := pagination.SetPaginator(c.Ctx, limit, count)
		gateways, err := object.GetMaskedNetworkGateways(object.GetPaginationNetworkGateways(owner, paginator.Offset(), limit, field, value, sortField, sortOrder))
		if err != nil {
			c.ResponseError(err.Error())
			return
		}

		c.ResponseOk(gateways, paginator.Nums())
	}
}

// GetNetworkGateway
// @Title GetNetworkGateway
// @Tag Network Gateway API
// @Description get network gateway
// @Param   id     query    string  true        "The id ( owner/name ) of the network gateway"
// @Success 200 {object} object.NetworkGateway The Response object
// @router /get-network-gateway [get]
func (c *ApiController) GetNetworkGateway() {
	id := c.Input().Get("id")

	gateway, err := object.GetMaskedNetworkGateway(object.GetNetworkGateway(id))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk(gateway)
}

// UpdateNetworkGateway
// @Title UpdateNetworkGateway
// @Tag Network Gateway API
// @Description update network gateway
// @Param   id     query    string  true        "The id ( owner/name ) of the network gateway"
// @Param   body    body   object.NetworkGateway  true        "The details of the network gateway"
// @Success 200 {object} controllers.Response The Response object
// @router /update-network-gateway [post]
func (c *ApiController) UpdateNetworkGateway() {
	id := c.Input().Get("id")

	var gateway object.NetworkGateway
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &gateway)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	if !c.checkOwner(gateway.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.UpdateNetworkGateway(id, &gateway))
	c.ServeJSON()
}

// AddNetworkGateway
// @Title AddNetworkGateway
// @Tag Network Gateway API
// @Description add a network gateway
// @Param   body    body   object.NetworkGateway  true        "The details of the network gateway"
// @Success 200 {object} controllers.Response The Response object
// @router /add-network-gateway [post]
func (c *ApiController) AddNetworkGateway() {
	var gateway object.NetworkGateway
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &gateway)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	if !c.checkOwner(gateway.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.AddNetworkGateway(&gateway))
	c.ServeJSON()
}

// DeleteNetworkGateway
// @Title DeleteNetworkGateway
// @Tag Network Gateway API
// @Description delete a network gateway
// @Param   body    body   object.NetworkGateway  true        "The details of the network gateway"
// @Success 200 {object} controllers.Response The Response object
// @router /delete-network-gateway [post]
func (c *ApiController) DeleteNetworkGateway() {
	var gateway object.NetworkGateway
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &gateway)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	if !c.checkOwner(gateway.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.DeleteNetworkGateway(&gateway))
	c.ServeJSON()
}

// TestNetworkGateway
// @Title TestNetworkGateway
// @Tag Network Gateway API
// @Description connect to a network gateway with its saved settings, recording its host key on the first connection
// @Param   id     query    string  true        "The id ( owner/name ) of the network gateway"
// @Success 200 {object} controllers.Response The Response object
// @router /test-network-gateway [post]
func (c *ApiController) TestNetworkGateway() {
	id := c.Input().Get("id")

	gateway, err := object.GetNetworkGateway(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if gateway == nil {
		c.ResponseError(fmt.Sprintf("the network gateway: %s does not exist", id))
		return
	}
	if !c.checkOwner(gateway.Owner) {
		return
	}

	if gateway.IsAgent() {
		version, err := object.PingAgent(gateway)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}
		c.ResponseOk(gateway.Fingerprint, version)
		return
	}

	client, err := gateway.DialSsh()
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	version := string(client.ServerVersion())
	_ = client.Close()

	c.ResponseOk(gateway.Fingerprint, version)
}

func (c *ApiController) getOwnedNetworkGateway() *object.NetworkGateway {
	id := c.Input().Get("id")

	gateway, err := object.GetNetworkGateway(id)
	if err != nil {
		c.ResponseError(err.Error())
		return nil
	}
	if gateway == nil {
		c.ResponseError(fmt.Sprintf("the network gateway: %s does not exist", id))
		return nil
	}
	if !c.checkOwner(gateway.Owner) {
		return nil
	}
	return gateway
}

// GenerateNetworkGatewayToken
// @Title GenerateNetworkGatewayToken
// @Tag Network Gateway API
// @Description generate a new token for the agent of an agent network gateway, the agent with the old token is disconnected. The token is only returned this once
// @Param   id     query    string  true        "The id ( owner/name ) of the network gateway"
// @Success 200 {object} controllers.Response The Response object
// @router /generate-network-gateway-token [post]
func (c *ApiController) GenerateNetworkGatewayToken() {
	gateway := c.getOwnedNetworkGateway()
	if gateway == nil {
		return
	}

	token, err := object.GenerateNetworkGatewayToken(gateway)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk(token)
}

// RevokeNetworkGatewayToken
// @Title RevokeNetworkGatewayToken
// @Tag Network Gateway API
// @Description revoke the token of an agent network gateway and disconnect its agent
// @Param   id     query    string  true        "The id ( owner/name ) of the network gateway"
// @Success 200 {object} controllers.Response The Response object
// @router /revoke-network-gateway-token [post]
func (c *ApiController) RevokeNetworkGatewayToken() {
	gateway := c.getOwnedNetworkGateway()
	if gateway == nil {
		return
	}

	err := object.RevokeNetworkGatewayToken(gateway)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk()
}

// ConnectAgent
// @Title ConnectAgent
// @Tag Network Gateway API
// @Description the WebSocket endpoint that network gateway agents connect to, carrying an SSH connection signed in with the agent token
// @Success 101 {string} Switching Protocols
// @router /connect-agent [get]
func (c *ApiController) ConnectAgent() {
	c.EnableRender = false
	ws, err := nativeUpgrader.Upgrade(c.Ctx.ResponseWriter, c.Ctx.Request, nil)
	if err != nil {
		return
	}

	err = sshgateway.ServeAgentConn(agent.NewWebSocketConn(ws), c.getAuthzClientIp())
	if err != nil {
		logs.Info(fmt.Sprintf("agent from %s refused: %s", c.getAuthzClientIp(), err.Error()))
	}
}
