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

package rbrowser

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/casvisor/casvisor/conf"
)

var (
	pathOnce  sync.Once
	pathValue string
)

func getCandidates() []string {
	switch runtime.GOOS {
	case "windows":
		res := []string{}
		for _, dir := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LocalAppData")} {
			if dir == "" {
				continue
			}
			res = append(res,
				filepath.Join(dir, `Google\Chrome\Application\chrome.exe`),
				filepath.Join(dir, `Chromium\Application\chrome.exe`),
				filepath.Join(dir, `Microsoft\Edge\Application\msedge.exe`),
			)
		}
		return append(res, "chrome.exe", "msedge.exe")
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		}
	default:
		return []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "chrome-headless-shell", "headless_shell", "microsoft-edge", "microsoft-edge-stable", "/snap/bin/chromium"}
	}
}

// GetPath returns the Chrome, Chromium or Edge that runs the remote browsers: remoteBrowserPath
// when it is set, or the first one found on the server. It is empty when there is none.
func GetPath() string {
	pathOnce.Do(func() {
		configured := strings.TrimSpace(conf.GetConfigString("remoteBrowserPath"))
		candidates := getCandidates()
		if configured != "" {
			candidates = []string{configured}
		}
		for _, candidate := range candidates {
			path, err := exec.LookPath(candidate)
			if err == nil {
				pathValue = path
				return
			}
		}
	})
	return pathValue
}

func IsAvailable() bool {
	return GetPath() != ""
}
