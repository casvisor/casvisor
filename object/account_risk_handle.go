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
	"slices"
	"strings"

	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

const (
	// RiskActionRotate sets a new random password for a managed account
	RiskActionRotate = "Rotate"
	// RiskActionManage makes Casvisor manage an account found on the asset, with a random password
	RiskActionManage = "Manage"
	// RiskActionLock locks an account found on the asset, so that it can't sign in at all
	RiskActionLock = "Lock"
)

var passwordRisks = []string{RiskWeakPassword, RiskStalePassword, RiskReusedPassword, RiskEmptyPassword}

type RiskHandleResult struct {
	Id       string `json:"id"`
	Asset    string `json:"asset"`
	Username string `json:"username"`
	Ok       bool   `json:"ok"`
	Message  string `json:"message"`
}

// HandleAccountRisks fixes the risks with the action, one by one, and resolves the ones it fixed;
// the next scan opens them again if they are still there.
func HandleAccountRisks(ids []string, action string, operator string) ([]*RiskHandleResult, error) {
	if action != RiskActionRotate && action != RiskActionManage && action != RiskActionLock {
		return nil, fmt.Errorf("unknown action: %s", action)
	}

	res := []*RiskHandleResult{}
	done := map[string]bool{}
	for _, id := range ids {
		owner, name := util.GetOwnerAndNameFromId(id)
		risk := &AccountRisk{Owner: owner, Name: name}
		existed, err := adapter.engine.Get(risk)
		if err != nil {
			return nil, err
		}
		result := &RiskHandleResult{Id: id}
		res = append(res, result)
		if !existed {
			result.Message = "the risk does not exist"
			continue
		}
		result.Asset, result.Username = risk.Asset, risk.Username

		// one account with several risks is handled once
		key := risk.Asset + "\x00" + risk.Username
		if !done[key] {
			err = handleAccountRisk(risk, action)
			if err != nil {
				result.Message = err.Error()
				continue
			}
			done[key] = true
			AddAuditRecord(&AuditEvent{
				Organization: risk.Owner,
				User:         operator,
				Method:       "Account",
				Action:       "handle-account-risk",
				Asset:        util.GetIdFromOwnerAndName(risk.Owner, risk.Asset),
				Detail:       fmt.Sprintf("%s %s: %s", strings.ToLower(action), risk.Username, risk.Kind),
			})
		}

		result.Ok = true
		comment := truncate(fmt.Sprintf("%s by %s on %s", getRiskActionName(action), operator, util.GetCurrentTime()[:10]), 500)
		_, err = adapter.engine.ID(core.PK{risk.Owner, risk.Name}).Cols("status", "resolved_time", "comment").
			Update(&AccountRisk{Status: RiskResolved, ResolvedTime: util.GetCurrentTime(), Comment: comment})
		if err != nil {
			return nil, err
		}
	}
	return res, nil
}

func getRiskActionName(action string) string {
	switch action {
	case RiskActionRotate:
		return "password changed"
	case RiskActionManage:
		return "managed"
	case RiskActionLock:
		return "locked"
	}
	return action
}

func handleAccountRisk(risk *AccountRisk, action string) error {
	asset, err := getAsset(risk.Owner, risk.Asset)
	if err != nil {
		return err
	}
	if asset == nil {
		return fmt.Errorf("the asset: %s does not exist", risk.Asset)
	}
	platform, err := getAssetPlatform(asset)
	if err != nil {
		return err
	}

	switch action {
	case RiskActionRotate:
		if !slices.Contains(passwordRisks, risk.Kind) || !risk.Managed {
			return fmt.Errorf("only the password of an account that Casvisor manages can be changed, manage the account first")
		}
		if risk.Username == asset.Username || asset.Protocol == ProtocolRedis && asset.Username == "" && risk.Username == "default" {
			return rotateAssetOwnPassword(asset, platform)
		}
		account, err := getAssetAccountByUsername(risk.Owner, risk.Asset, risk.Username)
		if err != nil {
			return err
		}
		if account == nil {
			return fmt.Errorf("the account: %s is no longer a system account of the asset", risk.Username)
		}
		if account.PrivateKey != "" {
			return fmt.Errorf("the account: %s signs in with a private key, which Casvisor does not change", risk.Username)
		}
		_, err = RotateAssetAccount(account.GetId(), false)
		return err
	case RiskActionManage:
		if risk.Managed {
			return fmt.Errorf("Casvisor already manages the account: %s", risk.Username)
		}
		return manageAssetAccount(asset, platform, risk.Username)
	case RiskActionLock:
		if risk.Managed {
			return fmt.Errorf("the account: %s is managed by Casvisor, delete or disable it there instead", risk.Username)
		}
		return lockAssetAccount(asset, platform, risk.Username)
	}
	return fmt.Errorf("unknown action: %s", action)
}

// rotateAssetOwnPassword sets a new random password for the asset's own account, signs in with
// it, and only then saves it.
func rotateAssetOwnPassword(asset *Asset, platform *Platform) error {
	if asset.Password == "" {
		return fmt.Errorf("the asset's own account signs in with a private key, which Casvisor does not change")
	}
	password, err := platform.generatePassword()
	if err != nil {
		return err
	}
	_, err = setAccountPassword(asset, platform, asset.Username, password, asset.Password, false)
	if err != nil {
		return err
	}
	err = verifyAccountPassword(asset, platform, asset.Username, password)
	if err != nil {
		return fmt.Errorf("the password was changed, but signing in with it failed, the asset now needs its password set by hand: %s", err.Error())
	}

	encrypted, err := encryptCredential(password)
	if err != nil {
		return err
	}
	_, err = adapter.engine.ID(core.PK{asset.Owner, asset.Name}).Cols("password", "updated_time").Update(&Asset{Password: encrypted, UpdatedTime: util.GetCurrentTime()})
	return err
}

// manageAssetAccount makes the account a system account of the asset with a random password,
// set on the asset, that is rotated every 30 days.
func manageAssetAccount(asset *Asset, platform *Platform, username string) error {
	if !platform.canChange() {
		return fmt.Errorf("the platform: %s can't change the passwords of accounts", platform.getDisplayName())
	}
	password, err := platform.generatePassword()
	if err != nil {
		return err
	}
	account := &AssetAccount{
		Owner:              asset.Owner,
		Asset:              asset.Name,
		Username:           username,
		Password:           password,
		IsEnabled:          true,
		AutoRotate:         true,
		RotateIntervalDays: 30,
		Description:        "managed from the account risks",
	}
	_, err = AddAssetAccount(account)
	if err != nil {
		return err
	}
	_, err = rotateAssetAccount(account.GetId(), true, false)
	if err != nil {
		_, _ = DeleteAssetAccount(account)
		return fmt.Errorf("setting a password on the asset failed, so the account is not managed: %s", err.Error())
	}
	return nil
}

// lockAssetAccount locks the password of the account and expires it, which keeps it from
// signing in with a key too; on Windows it disables the account.
func lockAssetAccount(asset *Asset, platform *Platform, username string) error {
	if !platform.canLock() {
		return fmt.Errorf("the accounts of %s can't be locked from Casvisor", platform.getDisplayName())
	}
	if IsDatabaseProtocol(platform.Method) {
		return lockDatabaseAccount(asset, username)
	}
	if username == "root" || username == asset.Username || strings.EqualFold(username, "Administrator") {
		return fmt.Errorf("the account: %s can't be locked from Casvisor", username)
	}
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
		var stderr bytes.Buffer
		code, err := h.runScript(fmt.Sprintf("usermod -L -e 1 %s\n", shellQuote(username)), io.Discard, &stderr)
		if err != nil {
			return err
		}
		if code != 0 {
			return fmt.Errorf("failed to lock %s (exit code %d): %s", username, code, truncate(strings.TrimSpace(stderr.String()), 300))
		}
		return nil
	case *windowsHost:
		var res struct {
			Ok bool `json:"ok"`
		}
		return h.runScript(fmt.Sprintf("Disable-LocalUser -Name %s\nOut-Result @{ ok = $true }\n", quotePowerShell(username)), &res)
	}
	return fmt.Errorf("the asset is neither Linux nor Windows")
}
