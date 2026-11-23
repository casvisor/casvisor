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

package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

// Config picks the client of each type, by the names in clientCandidates, and where the clients are.
// An empty choice takes the first client found in the order of preferences.
type Config struct {
	Ssh        string            `json:"ssh"`
	Sftp       string            `json:"sftp"`
	Rdp        string            `json:"rdp"`
	Mysql      string            `json:"mysql"`
	Postgresql string            `json:"postgresql"`
	Paths      map[string]string `json:"paths"`
}

func loadConfig() (*Config, string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, "", err
	}
	path := filepath.Join(dir, "casvisor-launcher", "config.json")

	config := &Config{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return config, path, nil
	}
	if err != nil {
		return nil, "", err
	}
	err = json.Unmarshal(data, config)
	if err != nil {
		return nil, "", err
	}
	return config, path, nil
}

func saveConfig(config *Config, path string) error {
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// findClient returns the path of a client: the one in the config file, or the first that exists of
// the usual places and the PATH.
func findClient(config *Config, name string) string {
	if path := config.Paths[name]; path != "" {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		if res, err := exec.LookPath(path); err == nil {
			return res
		}
	}
	for _, pattern := range clientCandidates[name] {
		if !filepath.IsAbs(pattern) {
			if res, err := exec.LookPath(pattern); err == nil {
				return res
			}
			continue
		}
		matches, _ := filepath.Glob(pattern)
		for i := len(matches) - 1; i >= 0; i-- {
			if info, err := os.Stat(matches[i]); err == nil && !info.IsDir() {
				return matches[i]
			}
		}
	}
	return ""
}

func discoverClients(config *Config) map[string]string {
	res := map[string]string{}
	for name := range clientCandidates {
		if path := findClient(config, name); path != "" {
			res[name] = path
		}
	}
	return res
}

// pickClient returns the client chosen in the config file for a type, or the first one found.
func pickClient(config *Config, choice string, preferences []string) (string, string) {
	if choice != "" {
		return choice, findClient(config, choice)
	}
	for _, name := range preferences {
		if path := findClient(config, name); path != "" {
			return name, path
		}
	}
	return "", ""
}
