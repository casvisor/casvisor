// Copyright 2024 The casbin Authors. All Rights Reserved.
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

package proxy

import (
	"time"

	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/proxy/client"
	"github.com/casvisor/casvisor/proxy/server"
	"github.com/casvisor/casvisor/proxy/tunnel"
	"github.com/casvisor/casvisor/util"
)

func StartProxyServer() {
	if conf.GatewayAddr == nil {
		return
	}

	proxyServer, err := server.NewProxyServer("Casvisor Proxy Server", conf.GatewayAddr.Port)
	if err != nil {
		panic(err)
		return
	}
	proxyServer.Serve()
}

func StartProxyClient() {
	if conf.GatewayAddr == nil {
		return
	}

	asset, err := object.GetAsset(util.GetIdFromOwnerAndName(conf.GetConfigString("casdoorOrganization"), util.GetHostname()))
	if err != nil {
		panic(err)
	}
	if asset == nil {
		panic("asset not found")
	}

	count := 0
	for {
		println("Connecting to proxy server...")
		client.NewClient(asset.Name,
			conf.GatewayAddr.IP.String(),
			conf.GatewayAddr.Port,
			tunnel.AssetToAppInfo(asset),
		).Run()

		time.Sleep(5 * time.Second)
		count++
		if count >= tunnel.RetryTimes {
			panic("failed to connect to proxy server,  no additional retries will be attempted")
			return
		}
	}
}
