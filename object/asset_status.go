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

package object

import (
	"fmt"
	"net"
	"time"

	"github.com/casvisor/casvisor/metric"
	"xorm.io/core"
)

func RunCheckAssetStatus() error {
	assets := []*Asset{}
	err := adapter.Engine.Where("category = ?", "Machine").Find(&assets)
	if err != nil {
		return err
	}

	runner := metric.Runner{}
	for _, asset := range assets {
		// use a copy of asset to avoid data race
		a := asset
		runner.Add(func() error {
			return CheckAssetStatus(a)
		})
	}
	runner.Wait()
	return nil
}

func CheckAssetStatus(asset *Asset) error {
	err := ICMPPingHost(asset.Endpoint)
	if err != nil {
		asset.Status = AssetStatusStopped
		asset.SshStatus = AssetStatusStopped
		_, _ = adapter.Engine.ID(core.PK{asset.Owner, asset.Name}).Cols("status", "ssh_status").Update(asset)
		return fmt.Errorf("host %s is offline, %v", asset.Endpoint, err)
	}

	err = PortStatus(asset.Endpoint, asset.Port)
	if err != nil {
		asset.Status = AssetStatusStopped
		asset.SshStatus = AssetStatusStopped
		_, updateErr := adapter.Engine.ID(core.PK{asset.Owner, asset.Name}).Cols("status", "ssh_status").Update(asset)
		if updateErr != nil {
			return updateErr
		}
		return err
	}

	asset.Status = AssetStatusRunning
	_, err = adapter.Engine.ID(core.PK{asset.Owner, asset.Name}).Cols("status").Update(asset)
	if err != nil {
		return err
	}

	if asset.EnableSsh || asset.Type == "SSH" {
		var sshPort int
		if asset.Type == "SSH" {
			sshPort = asset.Port
		} else {
			sshPort = asset.SshPort
		}

		err = PortStatus(asset.Endpoint, sshPort)
		if err != nil {
			asset.SshStatus = AssetStatusStopped
			_, updateErr := adapter.Engine.ID(core.PK{asset.Owner, asset.Name}).Cols("ssh_status").Update(asset)
			if updateErr != nil {
				return updateErr
			}
			return fmt.Errorf("ssh connection to %s:%d failed, %v", asset.Endpoint, asset.Port, err)
		}

		asset.SshStatus = AssetStatusRunning
		_, err = adapter.Engine.ID(core.PK{asset.Owner, asset.Name}).Cols("ssh_status").Update(asset)
		if err != nil {
			return err
		}
	}

	return nil
}

func ICMPPingHost(host string) error {
	conn, err := net.DialTimeout("ip4:icmp", host, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	return nil
}

func PortStatus(host string, port int) error {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	return nil
}
