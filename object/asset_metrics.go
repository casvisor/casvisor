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
	"errors"
	"fmt"
	"sync"

	"github.com/casvisor/casvisor/util/taskrunner"
	"github.com/casvisor/casvisor/util/term"
	"golang.org/x/crypto/ssh"
	"xorm.io/core"
)

var (
	sshClients      sync.Map
	sshClientsMutex sync.RWMutex
)

func RunUpdateAssetMetrics() {
	assets := []*Asset{}
	err := adapter.Engine.Where("enable_ssh = ? or type = ?", true, "SSH").Find(&assets)
	if err != nil {
		return
	}

	runner := taskrunner.Runner{}
	for _, asset := range assets {
		// use a copy of asset to avoid data race
		a := asset
		runner.Add(func() error {
			return UpdateAssetMetric(a)
		})
	}
	runner.Wait()
}

func UpdateAssetMetric(asset *Asset) error {
	client, err := GetSshClient(asset)
	if err != nil {
		asset.Active = false
		return errors.New(fmt.Sprintf("%s: %s", asset.GetId(), err.Error()))
	}
	asset.Active = true

	stats := &Stats{}
	stats, err = GetAllStats(client, stats)
	if err != nil {
		return err
	}

	LoadAssetState(asset, stats)
	_, err = adapter.Engine.ID(core.PK{asset.Owner, asset.Name}).Update(asset)
	if err != nil {
		return err
	}

	return err
}

func GetAllStats(client *ssh.Client, stats *Stats) (*Stats, error) {
	runner := taskrunner.Runner{}

	runner.Add(func() error {
		return getMemInfo(client, stats)
	})
	runner.Add(func() error {
		return getFSInfo(client, stats)
	})

	runner.Add(func() error {
		return getCPU(client, stats)
	})

	runner.Wait()
	return stats, nil
}

func GetSshClient(asset *Asset) (*ssh.Client, error) {
	sshClientsMutex.RLock()
	if client, ok := sshClients.Load(asset.GetId()); ok {
		sshClientsMutex.RUnlock()
		return client.(*ssh.Client), nil
	}
	sshClientsMutex.RUnlock()

	sshClientsMutex.Lock()
	defer sshClientsMutex.Unlock()

	client, err := term.NewSshClient(asset.GetAddr(), asset.Username, asset.Password)
	if err != nil {
		return nil, err
	}

	sshClients.Store(asset.GetId(), client)
	return client, nil
}

func LoadAssetState(asset *Asset, stats *Stats) {
	if len(stats.FSInfos) > 0 {
		fsInfo := stats.FSInfos[0]
		asset.FsTotal = fsInfo.Used + fsInfo.Free
		asset.FsCurrent = fsInfo.Used
	}

	asset.MemTotal = stats.MemTotal
	asset.MemCurrent = stats.MemTotal - stats.MemAvailable
	asset.CpuTotal = stats.CPU.CoreNum
	asset.CpuCurrent = 100 - stats.CPU.Idle
}

func CloseSshClients() {
	sshClientsMutex.Lock()
	defer sshClientsMutex.Unlock()

	sshClients.Range(func(key, value interface{}) bool {
		client := value.(*ssh.Client)
		client.Close()
		sshClients.Delete(key)
		return true
	})

	cleanupPreCPUMap()
}
