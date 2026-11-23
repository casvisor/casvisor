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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var clientCandidates = map[string][]string{
	"openssh":   {"/usr/bin/ssh"},
	"filezilla": {"/Applications/FileZilla.app/Contents/MacOS/filezilla"},
	"rdp":       {"/usr/bin/open"},
	"mysql":     {"mysql", "/opt/homebrew/bin/mysql", "/usr/local/bin/mysql", "/usr/local/mysql/bin/mysql"},
	"psql":      {"psql", "/opt/homebrew/bin/psql", "/usr/local/bin/psql", "/Applications/Postgres.app/Contents/Versions/latest/bin/psql"},
	"dbeaver":   {"/Applications/DBeaver.app/Contents/MacOS/dbeaver"},
}

var sshPreferences = []string{"openssh"}

const sshClients = "OpenSSH"

const appleEventHandler = `on open location theUrl
	do shell script quoted form of "%s" & " " & quoted form of theUrl & " > /dev/null 2>&1 &"
end open location`

func getAppDir() (string, error) {
	dir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Applications", "Casvisor Launcher.app"), nil
}

const lsregister = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"

// register makes a small AppleScript app that receives casvisor:// links, as macOS hands URLs to apps
// as Apple Events rather than arguments, and passes them to this program.
func register(exe string) error {
	app, err := getAppDir()
	if err != nil {
		return err
	}
	_ = os.RemoveAll(app)
	err = os.MkdirAll(filepath.Dir(app), 0o755)
	if err != nil {
		return err
	}
	quoted := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(exe)
	out, err := exec.Command("osacompile", "-o", app, "-e", fmt.Sprintf(appleEventHandler, quoted)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("osacompile: %s", out)
	}

	plist := filepath.Join(app, "Contents", "Info.plist")
	for _, args := range [][]string{
		{"-replace", "CFBundleIdentifier", "-string", "org.casvisor.launcher"},
		{"-replace", "LSBackgroundOnly", "-bool", "true"},
		{"-replace", "CFBundleURLTypes", "-json", fmt.Sprintf(`[{"CFBundleURLName":"Casvisor","CFBundleURLSchemes":["%s"]}]`, scheme)},
	} {
		out, err = exec.Command("plutil", append(args, plist)...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("plutil: %s", out)
		}
	}
	out, err = exec.Command(lsregister, "-f", app).CombinedOutput()
	if err != nil {
		return fmt.Errorf("lsregister: %s", out)
	}
	return nil
}

func unregister() error {
	app, err := getAppDir()
	if err != nil {
		return err
	}
	_ = exec.Command(lsregister, "-u", app).Run()
	return os.RemoveAll(app)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func appleScriptQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// startInTerminal runs a command line client in a window of Terminal.
func startInTerminal(config *Config, configPath string, title string, path string, args ...string) error {
	words := []string{shellQuote(path)}
	for _, arg := range args {
		words = append(words, shellQuote(arg))
	}
	script := fmt.Sprintf("tell application \"Terminal\"\nactivate\ndo script %s\nend tell", appleScriptQuote(strings.Join(words, " ")))
	return start("osascript", "-e", script)
}

// launchRdp opens the .rdp file in the app that handles them, Windows App or Microsoft Remote Desktop.
func launchRdp(config *Config, configPath string, r *request) error {
	_, path := pickClient(config, config.Rdp, []string{"rdp"})
	if path == "" {
		return missingClient("Remote Desktop", "Windows App", configPath)
	}
	file, cleanup, err := writeRdpFile(r)
	if err != nil {
		return err
	}
	err = start(path, file)
	if err != nil {
		return err
	}
	cleanup()
	return nil
}

func showMessage(text string) {
	fmt.Fprintln(os.Stderr, text)
	_ = exec.Command("osascript", "-e", fmt.Sprintf("display alert \"Casvisor Launcher\" message %s", appleScriptQuote(text))).Run()
}
