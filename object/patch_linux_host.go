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

package object

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	"golang.org/x/crypto/ssh"
)

// The scripts are fed to "sh -s" on stdin, so every command in them that could read stdin reads /dev/null instead.
const linuxScanScript = `
export LANG=C LC_ALL=C
. /etc/os-release 2>/dev/null
echo "OS|${PRETTY_NAME:-$(uname -sr)}"
if command -v apt-get >/dev/null 2>&1; then
  echo "MANAGER|apt"
  if [ "$(id -u)" = 0 ]; then
    apt-get update -q -o DPkg::Lock::Timeout=300 </dev/null >&2 || echo "WARN|apt-get update failed"
  else
    echo "WARN|not root, the package lists were not refreshed"
  fi
  apt list --upgradable </dev/null 2>/dev/null | grep '/' | sed 's/^/APT|/'
  if [ -f /var/run/reboot-required ]; then echo "REBOOT|1"; fi
elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
  if command -v dnf >/dev/null 2>&1; then
    echo "MANAGER|dnf"
    dnf -q repoquery --upgrades --latest-limit=1 --qf '%{name}.%{arch}|%{evr}|%{repoid}\n' </dev/null 2>/dev/null | grep '|' | sed 's/^/RPM|/'
    dnf -q updateinfo list --security </dev/null 2>/dev/null | sed 's/^/SEC|/'
  else
    echo "MANAGER|yum"
    yum -q check-update </dev/null 2>/dev/null | awk 'NF==1 {p=$1; next} p!="" {$0=p" "$0; p=""} NF>=3 && $1 ~ /\./ {print "RPM|"$1"|"$2"|"$3}'
    yum -q updateinfo list security </dev/null 2>/dev/null | sed 's/^/SEC|/'
  fi
  rpm -qa --qf 'INST|%{NAME}.%{ARCH}|%{VERSION}-%{RELEASE}\n' </dev/null
  if command -v needs-restarting >/dev/null 2>&1; then
    needs-restarting -r </dev/null >/dev/null 2>&1
    if [ $? -eq 1 ]; then echo "REBOOT|1"; fi
  else
    latest=$(rpm -q --last kernel kernel-core </dev/null 2>/dev/null | grep -v 'not installed' | head -n 1 | awk '{print $1}')
    case "$latest" in
      ""|*"$(uname -r)"*) ;;
      *) echo "REBOOT|1" ;;
    esac
  fi
else
  echo "MANAGER|"
fi
exit 0
`

const linuxInstallScript = `
export LANG=C LC_ALL=C DEBIAN_FRONTEND=noninteractive
set -e
PACKAGES="%s"
if command -v apt-get >/dev/null 2>&1; then
  apt-get update -q -o DPkg::Lock::Timeout=600 </dev/null
  OPTIONS="-y -q -o DPkg::Lock::Timeout=600 -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold"
  if [ -z "$PACKAGES" ]; then
    apt-get $OPTIONS upgrade --with-new-pkgs </dev/null
  else
    apt-get $OPTIONS install --only-upgrade $PACKAGES </dev/null
  fi
elif command -v dnf >/dev/null 2>&1; then
  dnf -y upgrade $PACKAGES </dev/null
else
  yum -y update $PACKAGES </dev/null
fi
`

const linuxRebootScript = `
nohup sh -c 'sleep 3; systemctl reboot || reboot' </dev/null >/dev/null 2>&1 &
echo ok
`

type linuxHost struct {
	client *ssh.Client
	sudo   bool
	root   bool
}

func newLinuxHost(client *ssh.Client) (*linuxHost, error) {
	var out bytes.Buffer
	_, err := runSshCommand(client, "id -u", "", &out, io.Discard)
	if err != nil {
		return nil, err
	}

	host := &linuxHost{client: client}
	if strings.TrimSpace(out.String()) == "0" {
		host.root = true
		return host, nil
	}

	code, err := runSshCommand(client, "sudo -n true", "", io.Discard, io.Discard)
	if err == nil && code == 0 {
		host.sudo = true
		host.root = true
	}
	return host, nil
}

func (h *linuxHost) runScript(script string, stdout io.Writer, stderr io.Writer) (int, error) {
	command := "sh -s"
	if h.sudo {
		command = "sudo -n sh -s"
	}
	return runSshCommand(h.client, command, script, stdout, stderr)
}

func (h *linuxHost) requireRoot() error {
	if !h.root {
		return fmt.Errorf("the SSH account of the asset must be root or be able to run sudo without a password")
	}
	return nil
}

func (h *linuxHost) scan(r *patchRun) (*PatchStatus, error) {
	var stdout bytes.Buffer
	code, err := h.runScript(linuxScanScript, &stdout, r)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("the scan exited with code %d", code)
	}

	status, warnings := parseLinuxScan(stdout.String())
	for _, warning := range warnings {
		r.logf("Warning: %s", warning)
	}
	if status.Manager == "" {
		return nil, fmt.Errorf("no supported package manager (apt, dnf or yum) on %s", status.Os)
	}
	return status, nil
}

func parseLinuxScan(output string) (*PatchStatus, []string) {
	status := &PatchStatus{Platform: "Linux", Updates: []*PatchUpdate{}}
	warnings := []string{}
	installed := map[string]string{}
	securityLines := []string{}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		kind, value, ok := strings.Cut(line, "|")
		if !ok {
			continue
		}

		switch kind {
		case "OS":
			status.Os = value
		case "MANAGER":
			status.Manager = value
		case "WARN":
			warnings = append(warnings, value)
		case "REBOOT":
			status.RebootRequired = true
		case "APT":
			// openssl/noble-updates,noble-security 3.0.13-0ubuntu3.5 amd64 [upgradable from: 3.0.13-0ubuntu3.4]
			fields := strings.Fields(value)
			if len(fields) < 3 {
				continue
			}
			name, suites, _ := strings.Cut(fields[0], "/")
			update := &PatchUpdate{Id: name, Title: name, Category: suites, NewVersion: fields[1]}
			if _, from, ok := strings.Cut(value, "[upgradable from: "); ok {
				update.CurrentVersion = strings.TrimSuffix(from, "]")
			}
			for _, suite := range strings.Split(suites, ",") {
				if strings.HasSuffix(suite, "-security") {
					update.IsSecurity = true
				}
			}
			status.Updates = append(status.Updates, update)
		case "RPM":
			// openssl.x86_64|1:3.0.7-27.el9|baseos
			fields := strings.Split(value, "|")
			if len(fields) < 3 {
				continue
			}
			repo := strings.TrimSuffix(strings.TrimSpace(fields[2]), `\n`)
			status.Updates = append(status.Updates, &PatchUpdate{Id: fields[0], Title: fields[0], Category: repo, NewVersion: fields[1]})
		case "INST":
			name, version, _ := strings.Cut(value, "|")
			installed[name] = version
		case "SEC":
			securityLines = append(securityLines, value)
		}
	}

	if len(installed) > 0 || len(securityLines) > 0 {
		updates := map[string]*PatchUpdate{}
		for _, update := range status.Updates {
			update.CurrentVersion = installed[update.Id]
			updates[update.Id] = update
		}
		markRpmSecurityUpdates(updates, securityLines)
	}

	sort.SliceStable(status.Updates, func(i, j int) bool {
		if status.Updates[i].IsSecurity != status.Updates[j].IsSecurity {
			return status.Updates[i].IsSecurity
		}
		return status.Updates[i].Id < status.Updates[j].Id
	})
	return status, warnings
}

// markRpmSecurityUpdates finds the packages in the advisories of "updateinfo list", whose lines look like
// "RHSA-2024:1234 Important/Sec. openssl-libs-1:3.0.7-27.el9.x86_64" on dnf 4 and yum,
// or "RHSA-2024:1234 security Important openssl-libs-1:3.0.7-27.el9.x86_64 2024-03-01 00:00:00" on dnf 5.
func markRpmSecurityUpdates(updates map[string]*PatchUpdate, lines []string) {
	for _, line := range lines {
		fields := strings.Fields(line)
		severity := ""
		for i, field := range fields {
			if strings.HasSuffix(field, "/Sec.") {
				severity = strings.TrimSuffix(field, "/Sec.")
			} else if field == "security" && i+1 < len(fields) {
				severity = fields[i+1]
			}
		}

		for _, field := range fields {
			dot := strings.LastIndex(field, ".")
			if dot <= 0 {
				continue
			}
			nameVersion, arch := field[:dot], field[dot+1:]
			release := strings.LastIndex(nameVersion, "-")
			if release <= 0 {
				continue
			}
			version := strings.LastIndex(nameVersion[:release], "-")
			if version <= 0 {
				continue
			}
			update := updates[nameVersion[:version]+"."+arch]
			if update == nil {
				continue
			}
			update.IsSecurity = true
			if update.Severity == "" && severity != "" && severity != "None" {
				update.Severity = severity
			}
		}
	}
}

func (h *linuxHost) install(r *patchRun, ids []string) error {
	err := h.requireRoot()
	if err != nil {
		return err
	}

	code, err := h.runScript(fmt.Sprintf(linuxInstallScript, strings.Join(ids, " ")), r, r)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("the package manager exited with code %d", code)
	}
	return nil
}

func (h *linuxHost) reboot(r *patchRun) error {
	err := h.requireRoot()
	if err != nil {
		return err
	}

	code, err := h.runScript(linuxRebootScript, io.Discard, r)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("the restart command exited with code %d", code)
	}
	return nil
}

func (h *linuxHost) bootId() (string, error) {
	var out bytes.Buffer
	_, err := runSshCommand(h.client, "cat /proc/sys/kernel/random/boot_id", "", &out, io.Discard)
	return strings.TrimSpace(out.String()), err
}

func (h *linuxHost) close() {
	h.client.Close()
}
