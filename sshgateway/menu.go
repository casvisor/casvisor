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

package sshgateway

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/casvisor/casvisor/object"
)

// runMenu lists the SSH assets the user may connect to and reads a choice, for
// "ssh <user>@<casvisor host>" and terminal tools like Xshell that save one host.
// It returns false when the user quits.
func (s *sessionChannel) runMenu() bool {
	assets, err := s.g.getAllowedAssets()
	if err != nil {
		writeError(s.down, err.Error())
		return false
	}

	s.mu.Lock()
	echo := s.pty != nil
	s.mu.Unlock()

	write := func(text string) {
		_, _ = s.down.Write([]byte(strings.ReplaceAll(text, "\n", "\r\n")))
	}

	write(fmt.Sprintf("\nCasvisor SSH gateway, signed in as %s/%s\n\n", s.g.user.Owner, s.g.user.Name))
	if len(assets) == 0 {
		write("You have no SSH assets to connect to.\n")
		return false
	}

	for i, asset := range assets {
		name := asset.Name
		if asset.Owner != s.g.user.Owner {
			name = asset.GetId()
		}
		write(fmt.Sprintf("  %3d  %-24s %-24s %s\n", i+1, name, fmt.Sprintf("%s:%d", asset.Host, asset.Port), asset.DisplayName))
	}

	for {
		write("\nEnter a number or name (q to quit): ")
		line, ok := s.readLine(echo)
		if !ok {
			return false
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "q" || line == "quit" || line == "exit" {
			return false
		}

		asset := pickAsset(assets, line)
		if asset == nil {
			write(fmt.Sprintf("No asset matches: %s\n", line))
			continue
		}

		err = s.g.selectAsset(asset)
		if err != nil {
			write(fmt.Sprintf("%s\n", err.Error()))
			continue
		}

		write(fmt.Sprintf("Connecting to %s...\n", asset.Name))
		return true
	}
}

func pickAsset(assets []*object.Asset, choice string) *object.Asset {
	if i, err := strconv.Atoi(choice); err == nil && i >= 1 && i <= len(assets) {
		return assets[i-1]
	}
	for _, asset := range assets {
		if asset.Name == choice || asset.GetId() == choice {
			return asset
		}
	}
	return nil
}

// readLine reads a line from the client, echoing it when the client has a pty
// (its terminal is then in raw mode and shows nothing by itself).
func (s *sessionChannel) readLine(echo bool) (string, bool) {
	line := []byte{}
	buf := make([]byte, 256)
	for {
		n, err := s.down.Read(buf)
		if err != nil {
			return "", false
		}

		for _, b := range buf[:n] {
			switch b {
			case '\r', '\n':
				if echo {
					_, _ = s.down.Write([]byte("\r\n"))
				}
				return string(line), true
			case 3, 4:
				// Ctrl+C, Ctrl+D
				return "", false
			case 127, 8:
				if len(line) > 0 {
					line = line[:len(line)-1]
					if echo {
						_, _ = s.down.Write([]byte("\b \b"))
					}
				}
			default:
				if b >= 32 && b < 127 {
					line = append(line, b)
					if echo {
						_, _ = s.down.Write([]byte{b})
					}
				}
			}
		}
	}
}
