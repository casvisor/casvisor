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

// Casvisor Launcher opens casvisor:// links from the Casvisor web console in the clients on the user's
// machine: SSH and SFTP clients through the SSH gateway, Remote Desktop Connection through the Remote
// Desktop Gateway, and database clients through the database proxy, all with one-time tokens.
//
//	casvisor-launcher              register casvisor:// links and write the config file
//	casvisor-launcher uninstall    remove the registration
//	casvisor-launcher <link>       open a casvisor://launch?type=... link
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

const scheme = "casvisor"

func main() {
	arg := ""
	if len(os.Args) > 1 {
		arg = os.Args[1]
	}

	var err error
	switch {
	case strings.HasPrefix(strings.ToLower(arg), scheme+":"):
		err = launch(arg)
	case arg == "" || arg == "install":
		err = install()
	case arg == "uninstall":
		err = unregister()
		if err == nil {
			showMessage("casvisor:// links are no longer opened by Casvisor Launcher.")
		}
	default:
		err = fmt.Errorf("usage: casvisor-launcher [install | uninstall | casvisor://launch?type=...]")
	}
	if err != nil {
		showMessage("Error: " + err.Error())
		os.Exit(1)
	}
}

func install() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	err = register(exe)
	if err != nil {
		return err
	}

	config, path, err := loadConfig()
	if err != nil {
		return err
	}
	found := discoverClients(config)
	if len(config.Paths) == 0 {
		config.Paths = found
		err = saveConfig(config, path)
		if err != nil {
			return err
		}
	}

	names := []string{}
	for name := range found {
		names = append(names, name)
	}
	sort.Strings(names)
	clients := "none"
	if len(names) > 0 {
		clients = strings.Join(names, ", ")
	}
	showMessage(fmt.Sprintf("Casvisor Launcher now opens casvisor:// links from the Casvisor web console. Keep it at %s, or run it again after moving it.\n\nClients found: %s\n\nTo choose a client or set its path, edit %s", exe, clients, path))
	return nil
}
