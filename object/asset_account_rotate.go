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
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/beego/beego/logs"
)

const accountPasswordChars = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!#%+-=_."

var rotatingAccounts sync.Map

func generateAccountPassword() (string, error) {
	groups := []string{"ABCDEFGHJKLMNPQRSTUVWXYZ", "abcdefghijkmnopqrstuvwxyz", "23456789", "!#%+-=_."}
	res := []byte{}
	for _, group := range groups {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(group))))
		if err != nil {
			return "", err
		}
		res = append(res, group[n.Int64()])
	}
	for len(res) < 24 {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(accountPasswordChars))))
		if err != nil {
			return "", err
		}
		res = append(res, accountPasswordChars[n.Int64()])
	}
	for i := len(res) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", err
		}
		j := n.Int64()
		res[i], res[j] = res[j], res[i]
	}
	return string(res), nil
}

func quotePowerShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// setAccountPassword sets the password of the account on the asset through the asset's own
// account, which must be root, able to sudo without a password, or a Windows administrator.
func setAccountPassword(asset *Asset, username string, password string, create bool) error {
	if !strings.EqualFold(asset.Protocol, "SSH") {
		return fmt.Errorf("the passwords of accounts can only be changed on SSH assets, the asset: %s uses %s", asset.Name, asset.Protocol)
	}
	if strings.ContainsAny(password, "\r\n") {
		return fmt.Errorf("the password cannot contain line breaks")
	}

	host, err := connectPatchHost(asset)
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

		script := ""
		if create {
			script += fmt.Sprintf("id -u '%s' >/dev/null 2>&1 || useradd -m '%s' || exit 1\n", username, username)
		}
		script += "chpasswd <<'CASVISOR_EOF'\n" + username + ":" + password + "\nCASVISOR_EOF\n"

		var stderr bytes.Buffer
		code, err := h.runScript(script, io.Discard, &stderr)
		if err != nil {
			return err
		}
		if code != 0 {
			return fmt.Errorf("failed to set the password of %s (exit code %d): %s", username, code, truncate(strings.TrimSpace(stderr.String()), 500))
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
	default:
		return fmt.Errorf("the asset: %s is neither Linux nor Windows", asset.GetId())
	}
}

func verifyAccountPassword(asset *Asset, username string, password string) error {
	target := *asset
	target.Username = username
	target.Password = password
	target.PrivateKey = ""
	client, err := DialSsh(&target)
	if err != nil {
		return err
	}
	return client.Close()
}

// RotateAssetAccount sets a new random password for the account on the asset, or the account's
// saved password when isPush is true, creating the account there if it does not exist.
func RotateAssetAccount(id string, isPush bool) (*AssetAccount, error) {
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

	password := account.Password
	if !isPush {
		password, err = generateAccountPassword()
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

	err = setAccountPassword(asset, account.Username, password, isPush)
	if err != nil {
		_ = updateAssetAccountRotation(account, "", AccountRotateFailed, truncate(err.Error(), 1000))
		auditAccountRotation(account, asset, isPush, AccountRotateFailed, err.Error())
		return nil, err
	}

	status := AccountRotateSucceeded
	message := ""
	err = verifyAccountPassword(asset, account.Username, password)
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
