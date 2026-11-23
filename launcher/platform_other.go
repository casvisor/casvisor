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

//go:build !windows && !linux && !darwin

package main

import (
	"fmt"
	"os"
	"runtime"
)

var clientCandidates = map[string][]string{}

var sshPreferences = []string{}

const sshClients = "an SSH client"

func register(exe string) error {
	return fmt.Errorf("casvisor:// links are not supported on %s", runtime.GOOS)
}

func unregister() error {
	return register("")
}

func startInTerminal(config *Config, configPath string, title string, path string, args ...string) error {
	return register("")
}

func launchRdp(config *Config, configPath string, r *request) error {
	return register("")
}

func showMessage(text string) {
	fmt.Fprintln(os.Stderr, text)
}
