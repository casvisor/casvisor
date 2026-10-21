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

package demo

import (
	"net"
	"strconv"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
)

// StartDemoTargets runs the fake SSH and VNC servers that the demo site connects to,
// and creates their assets and a permission letting every signed-in user connect.
func StartDemoTargets() {
	if !conf.GetConfigBool("demoTargets") {
		return
	}

	sshAddress := getAddress("demoSshAddress", "127.0.0.1:19122")
	vncAddress := getAddress("demoVncAddress", "127.0.0.1:19159")

	err := startSshServer(sshAddress)
	if err != nil {
		panic(err)
	}
	err = startVncServer(vncAddress)
	if err != nil {
		panic(err)
	}

	owner := conf.GetConfigString("casdoorOrganization")
	err = addDemoData(owner, sshAddress, vncAddress)
	if err != nil {
		panic(err)
	}
	logs.Info("demo targets: SSH on %s, VNC on %s", sshAddress, vncAddress)
}

func getAddress(key string, defaultAddress string) string {
	address := conf.GetConfigString(key)
	if address == "" {
		return defaultAddress
	}
	return address
}

func addDemoData(owner string, sshAddress string, vncAddress string) error {
	sshHost, sshPort, err := splitAddress(sshAddress)
	if err != nil {
		return err
	}
	vncHost, vncPort, err := splitAddress(vncAddress)
	if err != nil {
		return err
	}

	assets := []*object.Asset{
		{
			Owner: owner, Name: "demo-ssh", DisplayName: "Demo Linux server (SSH)",
			Description: "A sandboxed shell with an in-memory file system",
			Tag:         "demo", Os: "Linux", Protocol: "SSH",
			Host: sshHost, Port: sshPort, Username: Username, Password: Password,
		},
		{
			Owner: owner, Name: "demo-vnc", DisplayName: "Demo desktop (VNC)",
			Description: "A private desktop to type and draw on",
			Tag:         "demo", Os: "Linux", Protocol: "VNC",
			Host: vncHost, Port: vncPort, Password: Password,
		},
	}
	for _, asset := range assets {
		existing, err := object.GetAsset(asset.GetId())
		if err != nil {
			return err
		}
		if existing != nil {
			continue
		}

		asset.CreatedTime = util.GetCurrentTime()
		asset.UpdatedTime = asset.CreatedTime
		_, err = object.AddAsset(asset)
		if err != nil {
			return err
		}
	}

	permission, err := object.GetPermission(owner + "/demo-everyone")
	if err != nil {
		return err
	}
	if permission == nil {
		_, err = object.AddPermission(&object.Permission{
			Owner: owner, Name: "demo-everyone", CreatedTime: util.GetCurrentTime(),
			DisplayName: "Everyone can use the demo assets",
			Users:       []string{"*"},
			AssetTags:   []string{"demo"},
			Actions:     []string{object.ActionConnect, object.ActionCopy, object.ActionPaste},
			IsEnabled:   true,
		})
	}
	return err
}

func splitAddress(address string) (string, int, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, err
	}
	portNumber, err := strconv.Atoi(port)
	return host, portNumber, err
}
