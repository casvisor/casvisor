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
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	programFiles    = os.Getenv("ProgramFiles")
	programFilesX86 = os.Getenv("ProgramFiles(x86)")
	localAppData    = os.Getenv("LOCALAPPDATA")
	systemRoot      = os.Getenv("SystemRoot")
)

var clientCandidates = map[string][]string{
	"xshell":    {filepath.Join(programFilesX86, `NetSarang\Xshell*\Xshell.exe`), filepath.Join(programFiles, `NetSarang\Xshell*\Xshell.exe`)},
	"securecrt": {filepath.Join(programFiles, `VanDyke Software\SecureCRT\SecureCRT.exe`), filepath.Join(programFilesX86, `VanDyke Software\SecureCRT\SecureCRT.exe`)},
	"putty":     {filepath.Join(programFiles, `PuTTY\putty.exe`), filepath.Join(programFilesX86, `PuTTY\putty.exe`), "putty.exe"},
	"openssh":   {filepath.Join(systemRoot, `System32\OpenSSH\ssh.exe`), "ssh.exe"},
	"terminal":  {filepath.Join(localAppData, `Microsoft\WindowsApps\wt.exe`), "wt.exe"},
	"winscp":    {filepath.Join(programFilesX86, `WinSCP\WinSCP.exe`), filepath.Join(programFiles, `WinSCP\WinSCP.exe`), filepath.Join(localAppData, `Programs\WinSCP\WinSCP.exe`)},
	"filezilla": {filepath.Join(programFiles, `FileZilla FTP Client\filezilla.exe`), filepath.Join(programFilesX86, `FileZilla FTP Client\filezilla.exe`)},
	"mstsc":     {filepath.Join(systemRoot, `System32\mstsc.exe`)},
	"mysql":     {"mysql.exe", filepath.Join(programFiles, `MySQL\MySQL Server *\bin\mysql.exe`), filepath.Join(programFiles, `MariaDB *\bin\mysql.exe`)},
	"psql":      {"psql.exe", filepath.Join(programFiles, `PostgreSQL\*\bin\psql.exe`)},
	"dbeaver":   {filepath.Join(programFiles, `DBeaver\dbeaver.exe`), filepath.Join(localAppData, `DBeaver\dbeaver.exe`), "dbeaver.exe"},
}

var sshPreferences = []string{"xshell", "securecrt", "putty", "openssh"}

const sshClients = "Xshell, SecureCRT, PuTTY or OpenSSH"

const classesKey = `Software\Classes\` + scheme

func register(exe string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, classesKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	err = key.SetStringValue("", "URL:Casvisor Launcher")
	if err != nil {
		return err
	}
	err = key.SetStringValue("URL Protocol", "")
	if err != nil {
		return err
	}

	values := map[string]string{
		`DefaultIcon`:        `"` + exe + `",0`,
		`shell\open\command`: `"` + exe + `" "%1"`,
	}
	for path, value := range values {
		sub, _, err := registry.CreateKey(registry.CURRENT_USER, classesKey+`\`+path, registry.SET_VALUE)
		if err != nil {
			return err
		}
		err = sub.SetStringValue("", value)
		_ = sub.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func unregister() error {
	for _, path := range []string{`\shell\open\command`, `\shell\open`, `\shell`, `\DefaultIcon`, ``} {
		err := registry.DeleteKey(registry.CURRENT_USER, classesKey+path)
		if err != nil && err != registry.ErrNotExist {
			return err
		}
	}
	return nil
}

// startInTerminal runs a command line client in a tab of Windows Terminal, or in a console window of its own.
func startInTerminal(config *Config, configPath string, title string, path string, args ...string) error {
	if wt := findClient(config, "terminal"); wt != "" {
		title = strings.NewReplacer(";", "_", " ", "_").Replace(title)
		return start(wt, append([]string{"new-tab", "--title", title, path}, args...)...)
	}

	cmd := exec.Command(path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE}
	err := cmd.Start()
	if err != nil {
		return err
	}
	return cmd.Process.Release()
}

func launchRdp(config *Config, configPath string, r *request) error {
	_, path := pickClient(config, config.Rdp, []string{"mstsc"})
	if path == "" {
		return missingClient("Remote Desktop", "Remote Desktop Connection (mstsc)", configPath)
	}
	if r.certHash != "" {
		err := trustRdpCertificate(getRdpAddress(r), r.certHash)
		if err != nil {
			return err
		}
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
	caption, _ := windows.UTF16PtrFromString("Casvisor Launcher")
	message, _ := windows.UTF16PtrFromString(text)
	_, _ = windows.MessageBox(0, message, caption, windows.MB_OK|windows.MB_ICONINFORMATION)
}

// trustRdpCertificate is what mstsc saves when the user checks "Don't ask me again" for the certificate of a
// remote computer: here the certificate of Casvisor's gateway, which mstsc sees when Casvisor signs in for it.
func trustRdpCertificate(address string, certHash string) error {
	hash, err := hex.DecodeString(certHash)
	if err != nil {
		return err
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Microsoft\Terminal Server Client\Servers\`+address, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetBinaryValue("CertHash", hash)
}
