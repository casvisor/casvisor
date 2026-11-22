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
	"fmt"
	"strings"
	"time"

	"github.com/casvisor/casvisor/util"
)

type AssetProbeResult struct {
	Latency     int64  `json:"latency"`
	Version     string `json:"version"`
	Fingerprint string `json:"fingerprint"`
}

// ProbeAsset connects to the asset as it is on the edit page, before it is saved, and saves
// nothing: a host key or API server key seen for the first time is only returned. id is the
// saved asset the page edits, empty for a new one.
func ProbeAsset(id string, asset *Asset) (*AssetProbeResult, error) {
	target := *asset
	target.Host = strings.TrimSpace(target.Host)

	err := target.normalizeAddress()
	if err != nil {
		return nil, err
	}
	err = target.checkNetworkGateway()
	if err != nil {
		return nil, err
	}
	err = target.fillSavedSecrets(id)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	res := &AssetProbeResult{}
	switch target.Protocol {
	case ProtocolDesktop:
		gateway, err := getNetworkGateway(target.Owner, target.NetworkGateway)
		if err != nil {
			return nil, err
		}
		if !gateway.IsEnabled {
			return nil, fmt.Errorf("the network gateway: %s is disabled", gateway.GetId())
		}
		res.Version, err = PingAgent(gateway)
		if err != nil {
			return nil, err
		}
	case "SSH":
		client, err := dialSsh(&target, func(fingerprint string) error { return nil })
		if err != nil {
			return nil, err
		}
		res.Version = string(client.ServerVersion())
		_ = client.Close()
	case ProtocolKubernetes:
		client, err := NewKubernetesClient(&target)
		if err != nil {
			return nil, err
		}
		client.noSave = true
		var version struct {
			GitVersion string `json:"gitVersion"`
		}
		_, err = client.get("/version", &version)
		if err != nil {
			return nil, err
		}
		res.Version = version.GitVersion
	case ProtocolWeb:
		res.Version, err = probeWeb(&target)
		if err != nil {
			return nil, err
		}
	case ProtocolMysql, ProtocolPostgresql, ProtocolSqlServer, ProtocolOracle, ProtocolRedis, ProtocolMongodb:
		client, err := dialDatabase(&target, true)
		if err != nil {
			return nil, err
		}
		res.Version = client.Version
		client.Close()
	default:
		if target.Host == "" {
			return nil, fmt.Errorf("the host of the asset is empty")
		}
		conn, err := DialAsset(&target)
		if err != nil {
			return nil, err
		}
		_ = conn.Close()
	}

	res.Latency = time.Since(start).Milliseconds()
	if target.Fingerprint != asset.Fingerprint {
		res.Fingerprint = target.Fingerprint
	}
	return res, nil
}

// fillSavedSecrets replaces the masked password and private key with the saved ones, but only
// when the asset still points where it was saved to, so that a saved secret cannot be sent to
// another host by testing an edited asset.
func (asset *Asset) fillSavedSecrets(id string) error {
	if asset.Password != "***" && asset.PrivateKey != "***" {
		return nil
	}

	var saved *Asset
	if id != "" {
		owner, name := util.GetOwnerAndNameFromIdNoCheck(id)
		var err error
		saved, err = getAsset(owner, name)
		if err != nil {
			return err
		}
	}
	if saved == nil || saved.Owner != asset.Owner || saved.Protocol != asset.Protocol || saved.Host != asset.Host || saved.Port != asset.Port || saved.Username != asset.Username || saved.NetworkGateway != asset.NetworkGateway {
		return fmt.Errorf("the saved password and private key are only used for the saved host, port, username and network gateway, enter them again to test the changed asset")
	}

	if asset.Password == "***" {
		asset.Password = saved.Password
	}
	if asset.PrivateKey == "***" {
		asset.PrivateKey = saved.PrivateKey
	}
	return nil
}
