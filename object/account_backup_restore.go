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
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const maxRestoreAccounts = 20000

// AccountRestoreReport is what a restore did, or would do, to the assets and to the accounts.
type AccountRestoreReport struct {
	Assets         []*AssetImportResult `json:"assets"`
	AssetSummary   *AssetImportSummary  `json:"assetSummary"`
	Accounts       []*AssetImportResult `json:"accounts"`
	AccountSummary *AssetImportSummary  `json:"accountSummary"`
}

func readBackupCsv(data []byte) ([]string, [][]string, error) {
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\uFEFF")))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	if len(records) == 0 {
		return nil, nil, fmt.Errorf("the file is empty")
	}
	return records[0], records[1:], nil
}

// RestoreAccountBackup puts back the assets and the accounts of a backup's zip file: the missing
// ones, and when overwrite is true, the existing ones as they are in the backup; or only tells
// what it would do when dryRun is true.
func RestoreAccountBackup(owner string, data []byte, password string, overwrite bool, dryRun bool) (*AccountRestoreReport, error) {
	files, err := ReadAesZip(data, password)
	if err != nil {
		return nil, err
	}
	accountCsv, ok := files["accounts.csv"]
	if !ok {
		return nil, fmt.Errorf("the zip file has no accounts.csv, it is not a backup of Casvisor accounts")
	}

	report := &AccountRestoreReport{Assets: []*AssetImportResult{}, AssetSummary: &AssetImportSummary{}}
	pending := map[string]bool{}
	if assetCsv, ok := files["assets.csv"]; ok {
		columns, rows, err := ParseAssetCsv(string(assetCsv))
		if err != nil {
			return nil, fmt.Errorf("assets.csv: %s", err.Error())
		}
		report.Assets, report.AssetSummary, err = ImportAssets(owner, columns, rows, overwrite, dryRun)
		if err != nil {
			return nil, err
		}
		for _, result := range report.Assets {
			if result.Action == AssetImportCreate {
				pending[result.Name] = true
			}
		}
	}

	header, rows, err := readBackupCsv(accountCsv)
	if err != nil {
		return nil, fmt.Errorf("accounts.csv: %s", err.Error())
	}
	if len(rows) > maxRestoreAccounts {
		return nil, fmt.Errorf("accounts.csv has %d accounts, %d at most can be restored at a time", len(rows), maxRestoreAccounts)
	}
	report.Accounts, report.AccountSummary, err = restoreAccounts(owner, header, rows, overwrite, dryRun, pending)
	if err != nil {
		return nil, err
	}
	return report, nil
}

func restoreAccounts(owner string, header []string, rows [][]string, overwrite bool, dryRun bool, pending map[string]bool) ([]*AssetImportResult, *AssetImportSummary, error) {
	index := map[string]int{}
	for i, column := range header {
		index[strings.TrimSpace(column)] = i
	}
	for _, column := range []string{"asset", "username", "password", "privateKey"} {
		if _, ok := index[column]; !ok {
			return nil, nil, fmt.Errorf("accounts.csv has no column: %s", column)
		}
	}

	results := []*AssetImportResult{}
	summary := &AssetImportSummary{}
	seen := map[string]int{}
	for i, row := range rows {
		result := &AssetImportResult{Row: i + 2}
		results = append(results, result)
		get := func(column string) (string, bool) {
			j, ok := index[column]
			if !ok || j >= len(row) {
				return "", false
			}
			return row[j], true
		}
		text := func(column string) (string, bool) {
			value, ok := get(column)
			return unquoteBackupCell(strings.TrimSpace(value)), ok
		}
		fail := func(err error) {
			result.Action = AssetImportError
			result.Message = err.Error()
			summary.Failed++
		}

		assetName, _ := text("asset")
		username, _ := text("username")
		result.Name = assetName + "/" + username
		if assetName == "" || username == "" {
			fail(fmt.Errorf("the asset and the username can not be empty"))
			continue
		}
		if row, ok := seen[result.Name]; ok {
			fail(fmt.Errorf("the account is already in row %d", row))
			continue
		}
		seen[result.Name] = result.Row

		asset, err := getAsset(owner, assetName)
		if err != nil {
			return nil, nil, err
		}
		if asset == nil && !pending[assetName] {
			fail(fmt.Errorf("the asset: %s does not exist", assetName))
			continue
		}
		if asset != nil && asset.Username == username {
			result.Action = AssetImportSkip
			result.Message = "it is the asset's own account, which is restored with the asset"
			summary.Skipped++
			continue
		}

		account := &AssetAccount{Owner: owner, Asset: assetName, Username: username, IsEnabled: true}
		account.Password, _ = get("password")
		privateKey, _ := get("privateKey")
		account.PrivateKey = strings.ReplaceAll(strings.TrimSpace(privateKey), `\n`, "\n")
		account.DisplayName, _ = text("displayName")
		account.Description, _ = text("description")
		account.Template, _ = text("template")
		account.LastRotatedTime, _ = text("lastRotatedTime")
		if value, ok := text("isEnabled"); ok && value != "" {
			account.IsEnabled = value == "true"
		}
		if value, ok := text("autoRotate"); ok && value != "" {
			account.AutoRotate = value == "true"
		}
		if value, ok := text("rotateIntervalDays"); ok && value != "" {
			account.RotateIntervalDays, err = strconv.Atoi(value)
			if err != nil {
				fail(fmt.Errorf("invalid rotateIntervalDays: %s", value))
				continue
			}
		}

		var existing *AssetAccount
		if asset != nil {
			existing, err = getAssetAccountByUsername(owner, assetName, username)
			if err != nil {
				return nil, nil, err
			}
		}
		if existing != nil && !overwrite {
			result.Action = AssetImportSkip
			result.Message = "the account already exists"
			summary.Skipped++
			continue
		}

		if existing != nil {
			result.Changes = getAccountChanges(existing, account)
			if len(result.Changes) == 0 {
				result.Action = AssetImportSkip
				result.Message = "nothing to change"
				summary.Skipped++
				continue
			}
			result.Action = AssetImportUpdate
			if !dryRun {
				account.Name = existing.Name
				account.CreatedTime = existing.CreatedTime
				account.Template = existing.Template
				_, err = UpdateAssetAccount(existing.GetId(), account)
				if err == nil && account.LastRotatedTime != "" && (existing.Password != account.Password || existing.PrivateKey != account.PrivateKey) {
					err = updateRestoredRotation(existing, account.LastRotatedTime)
				}
			}
		} else {
			result.Action = AssetImportCreate
			if !dryRun {
				_, err = AddAssetAccount(account)
			} else if account.Password == "" && account.PrivateKey == "" {
				err = fmt.Errorf("the account: %s needs a password or a private key", username)
			}
		}
		if err != nil {
			fail(err)
			continue
		}
		if result.Action == AssetImportCreate {
			summary.Created++
		} else {
			summary.Updated++
		}
	}
	return results, summary, nil
}

// updateRestoredRotation keeps the time the restored password was set, so that rotation goes on from it.
func updateRestoredRotation(account *AssetAccount, lastRotatedTime string) error {
	_, err := adapter.engine.Where("owner = ? and name = ?", account.Owner, account.Name).Cols("last_rotated_time").Update(&AssetAccount{LastRotatedTime: lastRotatedTime})
	return err
}

func getAccountChanges(old *AssetAccount, account *AssetAccount) []string {
	res := []string{}
	if old.Password != account.Password {
		res = append(res, "password")
	}
	if old.PrivateKey != account.PrivateKey {
		res = append(res, "privateKey")
	}
	fields := []struct {
		name     string
		old, new string
	}{
		{"displayName", old.DisplayName, account.DisplayName},
		{"description", old.Description, account.Description},
		{"isEnabled", strconv.FormatBool(old.IsEnabled), strconv.FormatBool(account.IsEnabled)},
		{"autoRotate", strconv.FormatBool(old.AutoRotate), strconv.FormatBool(account.AutoRotate)},
		{"rotateIntervalDays", strconv.Itoa(old.RotateIntervalDays), strconv.Itoa(account.RotateIntervalDays)},
	}
	for _, field := range fields {
		if field.old != field.new {
			res = append(res, fmt.Sprintf("%s: %s → %s", field.name, field.old, field.new))
		}
	}
	return res
}

// GetAccountBackupFile is the backup of the organization's accounts made now, to download.
func GetAccountBackupFile(id string) ([]byte, string, int, error) {
	backup, err := GetAccountBackup(id)
	if err != nil {
		return nil, "", 0, err
	}
	if backup == nil {
		return nil, "", 0, fmt.Errorf("the account backup: %s does not exist", id)
	}
	data, count, err := BuildAccountBackup(backup)
	if err != nil {
		return nil, "", 0, err
	}
	return data, backup.GetFileName(time.Now()), count, nil
}
