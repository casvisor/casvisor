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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf16"

	"golang.org/x/crypto/ssh"
)

// The scripts come in on stdin, so they are not limited by the length of the command line.
// Their output is JSON on a line starting with "CASVISOR:", escaped to ASCII so the console code page does not matter.
const windowsBootstrap = `$ProgressPreference='SilentlyContinue';$s=[Console]::In.ReadToEnd();& ([ScriptBlock]::Create($s))`

const windowsScriptHeader = `
$ErrorActionPreference = 'Stop'
function Out-Result($o) {
  $j = ConvertTo-Json -InputObject $o -Depth 5 -Compress
  'CASVISOR:' + [regex]::Replace($j, '[^\x00-\x7F]', { param($m) '\u{0:x4}' -f [int][char]$m.Value })
}
try {
`

const windowsScriptFooter = `
} catch {
  Out-Result @{ error = $_.Exception.Message }
  exit 1
}
`

const windowsScanScript = `
$os = Get-CimInstance Win32_OperatingSystem
$session = New-Object -ComObject Microsoft.Update.Session
$session.ClientApplicationID = 'Casvisor'
$result = $session.CreateUpdateSearcher().Search("IsInstalled=0 and IsHidden=0 and Type='Software'")
$updates = @()
foreach ($u in $result.Updates) {
  $classification = ''
  foreach ($c in $u.Categories) { if ($c.Type -eq 'UpdateClassification') { $classification = $c.CategoryID } }
  $updates += @{
    id = $u.Identity.UpdateID
    title = $u.Title
    kb = (@($u.KBArticleIDs) | ForEach-Object { 'KB' + $_ }) -join ','
    severity = [string]$u.MsrcSeverity
    classification = $classification
    rebootBehavior = [int]$u.InstallationBehavior.RebootBehavior
  }
}
$base = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion'
$reboot = (Test-Path "$base\WindowsUpdate\Auto Update\RebootRequired") -or (Test-Path "$base\Component Based Servicing\RebootPending")
Out-Result @{ os = "$($os.Caption) $($os.Version)"; updates = $updates; reboot = $reboot }
`

const windowsBootIdScript = `
Out-Result @{ boot = (Get-CimInstance Win32_OperatingSystem).LastBootUpTime.ToUniversalTime().ToString('o') }
`

const windowsRebootScript = `
shutdown.exe /r /t 5 /d p:2:17 /c "Restarted by Casvisor to finish installing updates" | Out-Null
Out-Result @{ ok = $true }
`

// The install runs as a SYSTEM scheduled task: Windows Update refuses to download or install from an SSH logon.
const windowsInstallTaskScript = `param([string]$Job)
$dir = Split-Path -Parent $MyInvocation.MyCommand.Path
$log = Join-Path $dir "$Job.log"
$done = Join-Path $dir "$Job.done"
function Log($m) { Add-Content -Path $log -Value ((Get-Date -Format 'HH:mm:ss') + ' ' + $m) -Encoding UTF8 }
function Finish($o) { Set-Content -Path $done -Value (ConvertTo-Json -InputObject $o -Depth 5 -Compress) -Encoding UTF8 }
try {
  $ids = @(Get-Content (Join-Path $dir "$Job.ids") | Where-Object { $_ })
  $session = New-Object -ComObject Microsoft.Update.Session
  $session.ClientApplicationID = 'Casvisor'
  Log 'Searching for updates'
  $result = $session.CreateUpdateSearcher().Search("IsInstalled=0 and IsHidden=0 and Type='Software'")
  $selected = @()
  foreach ($u in $result.Updates) {
    if ($ids.Count -eq 0 -or $ids -contains $u.Identity.UpdateID) { $selected += $u }
  }
  if ($selected.Count -eq 0) { Log 'No updates to install'; Finish @{ installed = 0; failed = 0; reboot = $false }; exit }

  $downloaded = New-Object -ComObject Microsoft.Update.UpdateColl
  $i = 0
  foreach ($u in $selected) {
    $i++
    if (-not $u.EulaAccepted) { $u.AcceptEula() }
    if ($u.IsDownloaded) { [void]$downloaded.Add($u); continue }
    Log "Downloading ($i/$($selected.Count)) $($u.Title)"
    $coll = New-Object -ComObject Microsoft.Update.UpdateColl
    [void]$coll.Add($u)
    $downloader = $session.CreateUpdateDownloader()
    $downloader.Updates = $coll
    $r = $downloader.Download()
    if ($u.IsDownloaded) { [void]$downloaded.Add($u) } else { Log ("Download failed, result code {0}, 0x{1:X8}" -f [int]$r.ResultCode, $r.HResult) }
  }
  if ($downloaded.Count -eq 0) { Finish @{ installed = 0; failed = $selected.Count; reboot = $false }; exit }

  Log "Installing $($downloaded.Count) updates"
  $installer = $session.CreateUpdateInstaller()
  $installer.Updates = $downloaded
  $r = $installer.Install()
  $installed = 0
  $failed = $selected.Count - $downloaded.Count
  for ($j = 0; $j -lt $downloaded.Count; $j++) {
    $ur = $r.GetUpdateResult($j)
    $code = [int]$ur.ResultCode
    if ($code -eq 2 -or $code -eq 3) { $installed++; $state = 'Installed' } else { $failed++; $state = 'Failed' }
    Log ("{0}: {1} (result code {2}, 0x{3:X8})" -f $state, $downloaded.Item($j).Title, $code, $ur.HResult)
  }
  Finish @{ installed = $installed; failed = $failed; reboot = [bool]$r.RebootRequired }
} catch {
  Log ('Error: ' + $_.Exception.Message)
  Finish @{ error = $_.Exception.Message }
}
`

const windowsStartInstallScript = `
$existing = Get-ScheduledTask -TaskName 'Casvisor Patch' -ErrorAction SilentlyContinue
if ($existing -and $existing.State -eq 'Running') { throw 'another Casvisor update task is still running on this machine' }
$dir = Join-Path $env:ProgramData 'Casvisor\patch'
New-Item -ItemType Directory -Force -Path $dir | Out-Null
Get-ChildItem -Path $dir -File | Where-Object { $_.LastWriteTime -lt (Get-Date).AddDays(-7) } | Remove-Item -Force
$job = '%s'
Set-Content -Path (Join-Path $dir 'install.ps1') -Value $taskScript -Encoding UTF8
Set-Content -Path (Join-Path $dir "$job.ids") -Value (@(%s) -join [Environment]::NewLine) -Encoding ASCII
$action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument "-NoProfile -ExecutionPolicy Bypass -File ""$dir\install.ps1"" -Job $job"
$principal = New-ScheduledTaskPrincipal -UserId 'SYSTEM' -LogonType ServiceAccount -RunLevel Highest
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit (New-TimeSpan -Hours 6)
Register-ScheduledTask -TaskName 'Casvisor Patch' -Action $action -Principal $principal -Settings $settings -Force | Out-Null
Start-ScheduledTask -TaskName 'Casvisor Patch'
Out-Result @{ ok = $true }
`

const windowsPollInstallScript = `
$dir = Join-Path $env:ProgramData 'Casvisor\patch'
$job = '%s'
$from = %d
$lines = @()
$log = Join-Path $dir "$job.log"
if (Test-Path $log) { $lines = @(Get-Content -Path $log -Encoding UTF8 | ForEach-Object { [string]$_ }) }
$new = @()
if ($lines.Count -gt $from) { $new = @($lines[$from..($lines.Count - 1)]) }
$done = ''
if (Test-Path (Join-Path $dir "$job.done")) { $done = ([string](Get-Content -Path (Join-Path $dir "$job.done") -Raw -Encoding UTF8)).Trim() }
$task = Get-ScheduledTask -TaskName 'Casvisor Patch' -ErrorAction SilentlyContinue
$state = ''
if ($task) { $state = [string]$task.State }
if ($done -ne '') {
  Unregister-ScheduledTask -TaskName 'Casvisor Patch' -Confirm:$false -ErrorAction SilentlyContinue
  Remove-Item -Path (Join-Path $dir "$job.*") -Force -ErrorAction SilentlyContinue
}
Out-Result @{ lines = $new; count = $lines.Count; done = $done; state = $state }
`

// classifications of Windows Update, by category id
var windowsClassifications = map[string]string{
	"0fa1201d-4330-4fa8-8ae9-b877473b6441": "Security Updates",
	"e6cf1350-c01b-414d-a61f-263d14d133b4": "Critical Updates",
	"e0789628-ce08-4437-be74-2495b842f43b": "Definition Updates",
	"28bc880e-0592-4cbf-8f95-c79b17911d5f": "Update Rollups",
	"cd5ffd1e-e932-4e3a-bf74-18bf0b1bbd83": "Updates",
	"b54e7d24-7add-428f-8b75-90a396fa584f": "Feature Packs",
	"68c5b0a3-d1a6-4553-ae49-01d3a7827828": "Service Packs",
	"b4832bd8-e735-4761-8daf-37f882276dab": "Tools",
	"ebfc1fc5-71a4-4f7b-9aca-3b9a503104a0": "Drivers",
	"3689bdc8-b205-4af4-8d4a-a63924c5e9d5": "Upgrades",
}

var windowsBootstrapCommand = func() string {
	encoded := utf16.Encode([]rune(windowsBootstrap))
	b := make([]byte, len(encoded)*2)
	for i, c := range encoded {
		b[i*2] = byte(c)
		b[i*2+1] = byte(c >> 8)
	}
	return "powershell -NoProfile -NonInteractive -EncodedCommand " + base64.StdEncoding.EncodeToString(b)
}()

type windowsHost struct {
	client *ssh.Client
}

// runScript runs a PowerShell script and decodes its result into v.
func (h *windowsHost) runScript(script string, v interface{}) error {
	var stdout bytes.Buffer
	_, err := runSshCommand(h.client, windowsBootstrapCommand, windowsScriptHeader+script+windowsScriptFooter, &stdout, io.Discard)
	if err != nil {
		return err
	}

	result := ""
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "CASVISOR:") {
			result = strings.TrimPrefix(line, "CASVISOR:")
		}
	}
	if result == "" {
		return fmt.Errorf("PowerShell returned no result: %s", truncate(strings.TrimSpace(stdout.String()), 500))
	}

	var scriptErr struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(result), &scriptErr) == nil && scriptErr.Error != "" {
		return fmt.Errorf("%s", scriptErr.Error)
	}
	return json.Unmarshal([]byte(result), v)
}

func (h *windowsHost) scan(r *patchRun) (*PatchStatus, error) {
	var result struct {
		Os      string `json:"os"`
		Reboot  bool   `json:"reboot"`
		Updates []struct {
			Id             string `json:"id"`
			Title          string `json:"title"`
			Kb             string `json:"kb"`
			Severity       string `json:"severity"`
			Classification string `json:"classification"`
			RebootBehavior int    `json:"rebootBehavior"`
		} `json:"updates"`
	}
	err := h.runScript(windowsScanScript, &result)
	if err != nil {
		return nil, err
	}

	status := &PatchStatus{Platform: "Windows", Os: result.Os, Manager: "Windows Update", RebootRequired: result.Reboot, Updates: []*PatchUpdate{}}
	for _, u := range result.Updates {
		category := windowsClassifications[strings.ToLower(u.Classification)]
		status.Updates = append(status.Updates, &PatchUpdate{
			Id:             u.Id,
			Title:          u.Title,
			Kb:             u.Kb,
			Category:       category,
			Severity:       u.Severity,
			IsSecurity:     category == "Security Updates" || category == "Critical Updates" || u.Severity != "",
			RebootRequired: u.RebootBehavior != 0,
		})
	}
	return status, nil
}

func (h *windowsHost) startInstall(r *patchRun, ids []string) error {
	quoted := []string{}
	for _, id := range ids {
		quoted = append(quoted, "'"+id+"'")
	}
	script := "$taskScript = @'\n" + windowsInstallTaskScript + "\n'@\n" + fmt.Sprintf(windowsStartInstallScript, r.job.Name, strings.Join(quoted, ","))

	var started struct {
		Ok bool `json:"ok"`
	}
	err := h.runScript(script, &started)
	if err != nil {
		return fmt.Errorf("failed to start the update task: %s", err.Error())
	}
	r.logf("Started the scheduled task \"Casvisor Patch\" to install the updates as SYSTEM")
	return nil
}

// waitInstall reads the log of the scheduled task, job.RemoteOffset being the number of lines read so far.
func (h *windowsHost) waitInstall(r *patchRun) error {
	stopped := 0
	failures := 0
	deadline := time.Now().Add(6 * time.Hour)
	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Second)

		var poll struct {
			Lines []string `json:"lines"`
			Count int      `json:"count"`
			Done  string   `json:"done"`
			State string   `json:"state"`
		}
		err := h.runScript(fmt.Sprintf(windowsPollInstallScript, r.job.Name, r.remoteOffset()), &poll)
		if err != nil {
			r.logf("Failed to read the progress: %s", err.Error())
			failures++
			if failures >= 12 {
				return fmt.Errorf("lost the asset while installing updates: %s", err.Error())
			}
			h.reconnect(r)
			continue
		}
		failures = 0

		text := ""
		for _, line := range poll.Lines {
			text += line + "\n"
		}
		r.readRemote([]byte(text), int64(poll.Count))

		if poll.Done != "" {
			var done struct {
				Error     string `json:"error"`
				Installed int    `json:"installed"`
				Failed    int    `json:"failed"`
				Reboot    bool   `json:"reboot"`
			}
			err = json.Unmarshal([]byte(strings.TrimPrefix(poll.Done, "\ufeff")), &done)
			if err != nil {
				return fmt.Errorf("unexpected result of the update task: %s", poll.Done)
			}
			if done.Error != "" {
				return fmt.Errorf("%s", done.Error)
			}
			r.logf("Installed %d updates, %d failed, restart required: %t", done.Installed, done.Failed, done.Reboot)
			if done.Failed > 0 {
				return fmt.Errorf("%d updates failed to install", done.Failed)
			}
			return nil
		}

		if poll.State != "Running" && poll.State != "Queued" {
			stopped++
			if stopped >= 3 {
				return fmt.Errorf("the update task stopped without a result, state: %s", poll.State)
			}
		} else {
			stopped = 0
		}
	}

	return fmt.Errorf("the updates did not finish installing in 6 hours")
}

func (h *windowsHost) reconnect(r *patchRun) {
	client, err := DialSsh(r.asset)
	if err != nil {
		return
	}
	h.client.Close()
	h.client = client
}

func (h *windowsHost) reboot(r *patchRun) error {
	var result struct {
		Ok bool `json:"ok"`
	}
	return h.runScript(windowsRebootScript, &result)
}

func (h *windowsHost) bootId() (string, error) {
	var result struct {
		Boot string `json:"boot"`
	}
	err := h.runScript(windowsBootIdScript, &result)
	return result.Boot, err
}

func (h *windowsHost) close() {
	h.client.Close()
}
