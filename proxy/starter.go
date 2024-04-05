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
	"fmt"
	"os"

	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/proxy/client"
	"github.com/casvisor/casvisor/proxy/server"
	"github.com/casvisor/casvisor/proxy/tunnel"
	"github.com/casvisor/casvisor/util"
)

func StartProxyServer() {
	proxyPort := conf.GetConfigInt("proxyPort")
	proxyServer, err := server.NewProxyServer("Casvisor Proxy Server", proxyPort)
	if err != nil {
		panic(fmt.Errorf("failed to create proxy server %s", err))
		return
	}
	proxyServer.Serve()
}

func StartProxyClient() {
	proxyPort := conf.GetConfigInt("proxyPort")
	remoteHost := conf.GetConfigString("remoteHost")

	asset, err := object.GetAssetByName(util.GetHostname())
	if err != nil {
		panic(fmt.Errorf("failed to get asset by hostname %s", err))
	}
	if asset == nil {
		panic("asset not found")
	}

	for {
		client.NewClient(asset.Name,
			remoteHost,
			proxyPort,
			tunnel.AssetToAppInfo(asset),
		).Run()
	}
}

func StartMode() string {
	if conf.GetConfigString("startMode") == "server" {
		return "server"
	}

	hostname, err := os.Hostname()
	if err != nil {
		return "server"
	}

	asset, err := object.GetAssetByName(hostname)
	if err != nil {
		return "server"
	}
	if asset == nil {
		return "server"
	} else {
		return "client"
	}
}
