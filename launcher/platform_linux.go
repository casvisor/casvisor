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
	"putty":     {"putty"},
	"openssh":   {"ssh"},
	"terminal":  {"x-terminal-emulator", "gnome-terminal", "konsole", "xfce4-terminal", "xterm"},
	"winscp":    {},
	"filezilla": {"filezilla"},
	"freerdp":   {"xfreerdp3", "xfreerdp", "sdl-freerdp3", "sdl-freerdp"},
	"mysql":     {"mysql"},
	"psql":      {"psql"},
	"dbeaver":   {"dbeaver", "/usr/share/dbeaver-ce/dbeaver", "/snap/bin/dbeaver-ce", "/var/lib/flatpak/exports/bin/io.dbeaver.DBeaverCommunity"},
}

var sshPreferences = []string{"openssh", "putty"}

const sshClients = "OpenSSH or PuTTY"

func getDesktopFile() (string, error) {
	dir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".local", "share", "applications", "casvisor-launcher.desktop"), nil
}

func register(exe string) error {
	path, err := getDesktopFile()
	if err != nil {
		return err
	}
	err = os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return err
	}
	quoted := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`).Replace(exe)
	text := fmt.Sprintf("[Desktop Entry]\nType=Application\nName=Casvisor Launcher\nExec=\"%s\" %%u\nTerminal=false\nNoDisplay=true\nMimeType=x-scheme-handler/%s;\n", quoted, scheme)
	err = os.WriteFile(path, []byte(text), 0o644)
	if err != nil {
		return err
	}
	_ = exec.Command("update-desktop-database", filepath.Dir(path)).Run()
	return exec.Command("xdg-mime", "default", filepath.Base(path), "x-scheme-handler/"+scheme).Run()
}

func unregister() error {
	path, err := getDesktopFile()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = exec.Command("update-desktop-database", filepath.Dir(path)).Run()
	return nil
}

// startInTerminal runs a command line client in a terminal window.
func startInTerminal(config *Config, configPath string, title string, path string, args ...string) error {
	terminal := findClient(config, "terminal")
	if terminal == "" {
		return missingClient("terminal", "a terminal emulator such as GNOME Terminal, Konsole or xterm", configPath)
	}
	option := "-e"
	switch filepath.Base(terminal) {
	case "gnome-terminal":
		option = "--"
	case "xfce4-terminal":
		option = "-x"
	}
	return start(terminal, append([]string{option, path}, args...)...)
}

// launchRdp runs FreeRDP in a terminal, where it asks to trust the certificate of the remote desktop.
func launchRdp(config *Config, configPath string, r *request) error {
	_, path := pickClient(config, config.Rdp, []string{"freerdp"})
	if path == "" {
		return missingClient("Remote Desktop", "FreeRDP", configPath)
	}
	args := []string{"/v:" + r.name, fmt.Sprintf("/gateway:g:%s:%s,type:http,access-token:%s", r.host, r.port, r.user), "/dynamic-resolution"}
	if r.username != "" {
		args = append(args, "/u:"+r.username)
	}
	if r.autoLogon {
		args = append(args, "/sec:tls")
	}
	return startInTerminal(config, configPath, r.name, path, args...)
}

func showMessage(text string) {
	fmt.Fprintln(os.Stderr, text)
	if notify, err := exec.LookPath("notify-send"); err == nil {
		_ = exec.Command(notify, "Casvisor Launcher", text).Run()
	}
}
