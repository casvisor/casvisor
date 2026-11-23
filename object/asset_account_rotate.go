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
	"strings"
	"sync"
	"time"

	"github.com/beego/beego/logs"
)

var rotatingAccounts sync.Map

func quotePowerShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// connectAccountHost signs in to the asset over SSH with its own account, made privileged by the
// elevation of its platform, to manage the accounts of the system.
func connectAccountHost(asset *Asset, platform *Platform) (patchHost, error) {
	if !strings.EqualFold(asset.Protocol, "SSH") {
		return nil, fmt.Errorf("the accounts of %s are managed over SSH, the asset: %s uses %s", platform.getDisplayName(), asset.Name, asset.Protocol)
	}
	client, err := DialSsh(asset)
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	code, err := runSshCommand(client, "uname -s", "", &out, io.Discard)
	if err == nil && code == 0 && strings.TrimSpace(out.String()) != "" {
		host, err := newLinuxHost(client)
		if err == nil {
			err = host.elevate(asset, platform)
		}
		if err != nil {
			client.Close()
			return nil, err
		}
		return host, nil
	}

	out.Reset()
	_, err = runSshCommand(client, "cmd /c ver", "", &out, io.Discard)
	if err == nil && strings.Contains(out.String(), "Windows") {
		return newWindowsHost(client), nil
	}
	client.Close()
	return nil, fmt.Errorf("the SSH server of the asset: %s runs neither a Unix shell nor Windows", asset.GetId())
}

const shellPasswordScript = `u=%s
IFS= read -r p <<'CASVISOR_EOF'
%s
CASVISOR_EOF
create=%s
if [ "$(uname -s)" = Darwin ]; then
  if ! id -u "$u" >/dev/null 2>&1; then
    [ "$create" = 1 ] || { echo "the account: $u does not exist" >&2; exit 1; }
    sysadminctl -addUser "$u" -password "$p" || exit 1
    exit 0
  fi
  dscl . -passwd "/Users/$u" "$p" || exit 1
elif command -v chpasswd >/dev/null 2>&1; then
  if [ "$create" = 1 ]; then id -u "$u" >/dev/null 2>&1 || useradd -m "$u" || exit 1; fi
  printf '%%s:%%s\n' "$u" "$p" | chpasswd || exit 1
elif command -v pw >/dev/null 2>&1; then
  if [ "$create" = 1 ]; then id -u "$u" >/dev/null 2>&1 || pw useradd "$u" -m || exit 1; fi
  printf '%%s\n' "$p" | pw usermod "$u" -h 0 || exit 1
else
  echo "neither chpasswd nor pw is found" >&2
  exit 1
fi
`

func setHostAccountPassword(asset *Asset, platform *Platform, username string, password string, create bool) error {
	host, err := connectAccountHost(asset, platform)
	if err != nil {
		return err
	}
	defer host.close()

	switch h := host.(type) {
	case *linuxHost:
		err = h.requireRoot()
		if err != nil {
			return err
		}
		flag := "0"
		if create {
			flag = "1"
		}
		var stderr bytes.Buffer
		code, err := h.runScript(fmt.Sprintf(shellPasswordScript, shellQuote(username), password, flag), io.Discard, &stderr)
		if err != nil {
			return err
		}
		if code != 0 {
			return fmt.Errorf("failed to set the password of %s (exit code %d): %s", username, code, truncate(strings.TrimSpace(maskSecret(stderr.String(), password)), 500))
		}
		return nil
	case *windowsHost:
		script := fmt.Sprintf("$name = %s\n$password = ConvertTo-SecureString %s -AsPlainText -Force\n", quotePowerShell(username), quotePowerShell(password))
		if create {
			script += "if (Get-LocalUser -Name $name -ErrorAction SilentlyContinue) { Set-LocalUser -Name $name -Password $password } else { New-LocalUser -Name $name -Password $password -PasswordNeverExpires | Out-Null; Add-LocalGroupMember -Group (Get-LocalGroup -SID 'S-1-5-32-545').Name -Member $name }\n"
		} else {
			script += "Set-LocalUser -Name $name -Password $password\n"
		}
		script += "Out-Result @{ ok = $true }\n"

		var res struct {
			Ok bool `json:"ok"`
		}
		return h.runScript(script, &res)
	}
	return fmt.Errorf("the asset: %s is neither Unix nor Windows", asset.GetId())
}

func maskSecret(text string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "***")
		}
	}
	return text
}

// setAccountPassword sets the password of the account on the asset by the method of its platform,
// through the asset's own account; a database account may also change its own password with its
// current one, oldPassword. It returns a note on a change that is made but may not last.
func setAccountPassword(asset *Asset, platform *Platform, username string, password string, oldPassword string, create bool) (string, error) {
	if create && !platform.canPush() {
		return "", fmt.Errorf("the platform: %s can't create accounts on the assets", platform.getDisplayName())
	}
	if !create && !platform.canChange() {
		return "", fmt.Errorf("the platform: %s can't change the passwords of accounts", platform.getDisplayName())
	}
	err := platform.checkPassword(password)
	if err != nil {
		return "", err
	}

	switch {
	case platform.Method == PlatformMethodShell || platform.Method == PlatformMethodPowerShell:
		return "", setHostAccountPassword(asset, platform, username, password, create)
	case platform.Method == PlatformMethodCli:
		return "", setCliAccountPassword(asset, platform, username, password, create)
	case IsDatabaseProtocol(platform.Method):
		return setDatabaseAccountPassword(asset, platform, username, password, oldPassword, create)
	}
	return "", fmt.Errorf("the platform: %s has no way to change passwords", platform.getDisplayName())
}

func verifyAccountPassword(asset *Asset, platform *Platform, username string, password string) error {
	target := *asset
	target.Username = username
	target.Password = password
	target.PrivateKey = ""
	if IsDatabaseProtocol(platform.Method) {
		return verifyDatabaseLogin(&target)
	}
	client, err := DialSsh(&target)
	if err != nil {
		return err
	}
	return client.Close()
}

// RotateAssetAccount sets a new random password for the account on the asset, or the account's
// saved password when isPush is true, creating the account there if it does not exist.
func RotateAssetAccount(id string, isPush bool) (*AssetAccount, error) {
	return rotateAssetAccount(id, isPush, isPush)
}

// rotateAssetAccount sets a new random password, or the saved one when isPush is true; create
// makes the account on the asset when it is not there.
func rotateAssetAccount(id string, isPush bool, create bool) (*AssetAccount, error) {
	if _, loaded := rotatingAccounts.LoadOrStore(id, true); loaded {
		return nil, fmt.Errorf("the password of the account: %s is being changed", id)
	}
	defer rotatingAccounts.Delete(id)

	account, err := GetAssetAccount(id)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, fmt.Errorf("the account: %s does not exist", id)
	}

	asset, err := getAsset(account.Owner, account.Asset)
	if err != nil {
		return nil, err
	}
	if asset == nil {
		return nil, fmt.Errorf("the asset: %s does not exist", account.Asset)
	}
	platform, err := getAssetPlatform(asset)
	if err != nil {
		return nil, err
	}

	password := account.Password
	if !isPush {
		password, err = platform.generatePassword()
		if err != nil {
			return nil, err
		}
	} else if password == "" {
		return nil, fmt.Errorf("the account: %s has no password to push", account.Username)
	}

	err = updateAssetAccountRotation(account, "", AccountRotateRunning, "")
	if err != nil {
		return nil, err
	}

	note, err := setAccountPassword(asset, platform, account.Username, password, account.Password, create)
	if err != nil {
		_ = updateAssetAccountRotation(account, "", AccountRotateFailed, truncate(err.Error(), 1000))
		auditAccountRotation(account, asset, isPush, AccountRotateFailed, err.Error())
		return nil, err
	}

	status := AccountRotateSucceeded
	message := note
	err = verifyAccountPassword(asset, platform, account.Username, password)
	if err != nil {
		status = AccountRotateFailed
		message = truncate(fmt.Sprintf("the password was changed, but signing in with it failed: %s", err.Error()), 1000)
	}

	saved := password
	if isPush {
		saved = ""
		if status == AccountRotateSucceeded {
			// a push counts as a rotation, the saved password is now the one on the asset
			saved = password
		}
	}
	err = updateAssetAccountRotation(account, saved, status, message)
	if err != nil {
		return nil, err
	}

	auditAccountRotation(account, asset, isPush, status, message)

	if status == AccountRotateFailed {
		return nil, fmt.Errorf("%s", message)
	}
	return GetMaskedAssetAccount(GetAssetAccount(id))
}

func auditAccountRotation(account *AssetAccount, asset *Asset, isPush bool, status string, message string) {
	action := "rotate-asset-account"
	if isPush {
		action = "push-asset-account"
	}
	AddAuditRecord(&AuditEvent{
		Organization: account.Owner,
		User:         "casvisor",
		Method:       "Account",
		Action:       action,
		Asset:        asset.GetId(),
		Detail:       fmt.Sprintf("%s: %s %s", account.Username, status, message),
	})
}

func isAccountRotationDue(account *AssetAccount, now time.Time) bool {
	if !account.AutoRotate || !account.IsEnabled || account.Password == "" || account.PrivateKey != "" {
		return false
	}
	if account.LastRotatedTime == "" {
		return true
	}

	last, err := time.Parse(time.RFC3339, account.LastRotatedTime)
	if err != nil {
		return true
	}
	return now.Sub(last) >= time.Duration(account.RotateIntervalDays)*24*time.Hour
}

// rotateDueAccounts changes the passwords of the accounts whose rotation interval has passed.
// A failed rotation is tried again an hour later.
func rotateDueAccounts() {
	accounts := []*AssetAccount{}
	err := adapter.engine.Where("auto_rotate = ?", true).Find(&accounts)
	if err != nil {
		logs.Error(fmt.Sprintf("rotateDueAccounts(): %s", err.Error()))
		return
	}

	now := time.Now()
	for _, account := range accounts {
		if !isAccountRotationDue(account, now) {
			continue
		}
		if account.RotateStatus == AccountRotateFailed && account.UpdatedTime != "" {
			updated, err := time.Parse(time.RFC3339, account.UpdatedTime)
			if err == nil && now.Sub(updated) < time.Hour {
				continue
			}
		}

		_, err = RotateAssetAccount(account.GetId(), false)
		if err != nil {
			logs.Error(fmt.Sprintf("rotateDueAccounts(): failed to rotate the password of the account: %s/%s of the asset: %s: %s", account.Owner, account.Username, account.Asset, err.Error()))
		}
	}
}

func StartAccountRotator() {
	_, err := adapter.engine.Where("rotate_status = ?", AccountRotateRunning).Cols("rotate_status", "rotate_message").Update(&AssetAccount{RotateStatus: AccountRotateFailed, RotateMessage: "Casvisor restarted while the password was being changed"})
	if err != nil {
		logs.Error(fmt.Sprintf("StartAccountRotator(): %s", err.Error()))
	}

	go func() {
		time.Sleep(time.Minute)
		rotateDueAccounts()
		for range time.Tick(10 * time.Minute) {
			rotateDueAccounts()
		}
	}()
}
