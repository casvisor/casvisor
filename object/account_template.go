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
	"fmt"
	"slices"
	"strings"

	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

// AccountTemplate is a system account to give many assets at once: applying it creates the
// account on each asset it covers, or brings the accounts it created before in line with it.
type AccountTemplate struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	UpdatedTime string `xorm:"varchar(100)" json:"updatedTime"`
	DisplayName string `xorm:"varchar(100)" json:"displayName"`
	Description string `xorm:"varchar(500)" json:"description"`

	Username   string `xorm:"varchar(100)" json:"username"`
	Password   string `xorm:"text" json:"password"`
	PrivateKey string `xorm:"mediumtext" json:"privateKey"`
	// RandomPassword gives each asset a password of its own, set on the asset when applied
	RandomPassword     bool `json:"randomPassword"`
	AutoRotate         bool `json:"autoRotate"`
	RotateIntervalDays int  `json:"rotateIntervalDays"`
	// Push sets the account on the asset when applied, creating it there if needed
	Push bool `json:"push"`

	Assets    []string `xorm:"mediumtext" json:"assets"`
	AssetTags []string `xorm:"mediumtext" json:"assetTags"`
}

type TemplateApplyResult struct {
	Results []*AssetImportResult `json:"results"`
	Summary *AssetImportSummary  `json:"summary"`
}

func (template *AccountTemplate) GetId() string {
	return util.GetIdFromOwnerAndName(template.Owner, template.Name)
}

func GetAccountTemplateCount(owner, field, value string) (int64, error) {
	session := GetSession(owner, -1, -1, field, value, "", "")
	return session.Count(&AccountTemplate{})
}

func GetPaginationAccountTemplates(owner string, offset, limit int, field, value, sortField, sortOrder string) ([]*AccountTemplate, error) {
	templates := []*AccountTemplate{}
	session := GetSession(owner, offset, limit, field, value, sortField, sortOrder)
	err := session.Find(&templates)
	return templates, err
}

func getAccountTemplate(owner string, name string) (*AccountTemplate, error) {
	if owner == "" || name == "" {
		return nil, nil
	}
	template := AccountTemplate{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&template)
	if err != nil || !existed {
		return nil, err
	}
	template.Password, err = decryptCredential(template.Password)
	if err != nil {
		return nil, err
	}
	template.PrivateKey, err = decryptCredential(template.PrivateKey)
	if err != nil {
		return nil, err
	}
	return &template, nil
}

func GetAccountTemplate(id string) (*AccountTemplate, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	return getAccountTemplate(owner, name)
}

// GetMaskedAccountTemplates hides the secrets of templates, which are only written.
func GetMaskedAccountTemplates(templates []*AccountTemplate) []*AccountTemplate {
	for _, template := range templates {
		if template == nil {
			continue
		}
		if template.Password != "" {
			template.Password = "***"
		}
		if template.PrivateKey != "" {
			template.PrivateKey = "***"
		}
	}
	return templates
}

func (template *AccountTemplate) check() error {
	template.Username = strings.TrimSpace(template.Username)
	if template.Username == "" {
		return fmt.Errorf("the username cannot be empty")
	}
	if strings.ContainsAny(template.Username, ":@/\\ \t\r\n'\"") {
		return fmt.Errorf("the username: %s contains characters that are not allowed", template.Username)
	}
	if template.RandomPassword {
		template.Password = ""
		if !template.Push {
			return fmt.Errorf("a random password for each asset has to be pushed to the assets, turn on Push")
		}
	} else if template.Password == "" && template.PrivateKey == "" {
		return fmt.Errorf("the template needs a password or a private key, or a random password for each asset")
	}
	if template.Push && template.Password == "" && !template.RandomPassword {
		return fmt.Errorf("only a password can be pushed to the assets, not a private key")
	}
	if template.RotateIntervalDays < 0 {
		template.RotateIntervalDays = 0
	}
	if template.AutoRotate && template.RotateIntervalDays == 0 {
		template.RotateIntervalDays = 30
	}
	return nil
}

func (template *AccountTemplate) save(update func(*AccountTemplate) (int64, error)) (bool, error) {
	err := template.check()
	if err != nil {
		return false, err
	}
	stored := *template
	stored.Password, err = encryptCredential(template.Password)
	if err != nil {
		return false, err
	}
	stored.PrivateKey, err = encryptCredential(template.PrivateKey)
	if err != nil {
		return false, err
	}
	affected, err := update(&stored)
	return affected != 0, err
}

func UpdateAccountTemplate(id string, template *AccountTemplate) (bool, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	old, err := getAccountTemplate(owner, name)
	if err != nil {
		return false, err
	} else if old == nil {
		return false, nil
	}

	if template.Password == "***" {
		template.Password = old.Password
	}
	if template.PrivateKey == "***" {
		template.PrivateKey = old.PrivateKey
	}
	template.UpdatedTime = util.GetCurrentTime()
	affected, err := template.save(func(stored *AccountTemplate) (int64, error) {
		return adapter.engine.ID(core.PK{owner, name}).AllCols().Update(stored)
	})
	if err != nil || !affected {
		return affected, err
	}
	if template.Name != name {
		_, err = adapter.engine.Where("owner = ? and template = ?", owner, name).Cols("template").Update(&AssetAccount{Template: template.Name})
	}
	return true, err
}

func AddAccountTemplate(template *AccountTemplate) (bool, error) {
	if template.Password == "***" {
		template.Password = ""
	}
	if template.PrivateKey == "***" {
		template.PrivateKey = ""
	}
	return template.save(func(stored *AccountTemplate) (int64, error) {
		return adapter.engine.Insert(stored)
	})
}

// DeleteAccountTemplate deletes the template and keeps the accounts it created.
func DeleteAccountTemplate(template *AccountTemplate) (bool, error) {
	affected, err := adapter.engine.ID(core.PK{template.Owner, template.Name}).Delete(&AccountTemplate{})
	if err != nil {
		return false, err
	}
	_, err = adapter.engine.Where("owner = ? and template = ?", template.Owner, template.Name).Cols("template").Update(&AssetAccount{})
	return affected != 0, err
}

// getTemplateAssets returns the assets the template covers: the ones named, and the ones with the tags.
func (template *AccountTemplate) getTemplateAssets() ([]*Asset, error) {
	assets, err := GetAssets(template.Owner)
	if err != nil {
		return nil, err
	}
	res := []*Asset{}
	for _, asset := range assets {
		if slices.Contains(template.Assets, "*") || slices.Contains(template.Assets, asset.Name) || (asset.Tag != "" && slices.Contains(template.AssetTags, asset.Tag)) {
			res = append(res, asset)
		}
	}
	return res, nil
}

// ApplyAccountTemplate creates the template's account on each asset it covers, and brings the
// accounts it created before in line with it: their password or private key, unless each asset
// has a random one, and their rotation. An asset that already has an account of that username
// not from the template, or whose own account has it, is left alone. With Push, the password is
// set on the asset too. With dryRun nothing is saved or pushed.
func ApplyAccountTemplate(id string, dryRun bool) (*TemplateApplyResult, error) {
	template, err := GetAccountTemplate(id)
	if err != nil {
		return nil, err
	}
	if template == nil {
		return nil, fmt.Errorf("the account template: %s does not exist", id)
	}
	err = template.check()
	if err != nil {
		return nil, err
	}

	assets, err := template.getTemplateAssets()
	if err != nil {
		return nil, err
	}

	res := &TemplateApplyResult{Results: []*AssetImportResult{}, Summary: &AssetImportSummary{}}
	summary := res.Summary
	for i, asset := range assets {
		result := &AssetImportResult{Row: i + 1, Name: asset.Name}
		res.Results = append(res.Results, result)
		fail := func(err error) {
			result.Action = AssetImportError
			result.Message = err.Error()
			summary.Failed++
		}

		if asset.Username == template.Username {
			result.Action = AssetImportSkip
			result.Message = fmt.Sprintf("%s is the asset's own account", template.Username)
			summary.Skipped++
			continue
		}
		platform, err := getAssetPlatform(asset)
		if err != nil {
			fail(err)
			continue
		}
		canRotate := platform.canChange() && platform.reaches(asset)
		if template.Push && (!platform.canPush() || !platform.reaches(asset)) {
			result.Action = AssetImportSkip
			result.Message = fmt.Sprintf("accounts can't be pushed to this asset of the platform: %s over %s", platform.getDisplayName(), asset.Protocol)
			summary.Skipped++
			continue
		}

		old, err := getAssetAccountByUsername(asset.Owner, asset.Name, template.Username)
		if err != nil {
			fail(err)
			continue
		}
		if old != nil && old.Template != template.Name {
			result.Action = AssetImportSkip
			result.Message = fmt.Sprintf("the asset already has an account: %s that is not from this template", template.Username)
			summary.Skipped++
			continue
		}

		account := &AssetAccount{
			Owner:              asset.Owner,
			Asset:              asset.Name,
			Username:           template.Username,
			DisplayName:        template.DisplayName,
			Password:           template.Password,
			PrivateKey:         template.PrivateKey,
			IsEnabled:          true,
			AutoRotate:         template.AutoRotate && canRotate,
			RotateIntervalDays: template.RotateIntervalDays,
			Template:           template.Name,
		}
		if old != nil {
			*account = *old
			account.DisplayName = template.DisplayName
			account.AutoRotate = template.AutoRotate && canRotate
			account.RotateIntervalDays = template.RotateIntervalDays
			if !template.RandomPassword {
				account.Password, account.PrivateKey = template.Password, template.PrivateKey
			}
		}

		push := template.Push && (old == nil || !template.RandomPassword && old.Password != template.Password)
		if template.RandomPassword && old == nil {
			// the asset's own password is set by the push below
			account.Password, err = platform.generatePassword()
			if err != nil {
				fail(err)
				continue
			}
		}

		if old != nil {
			result.Changes = getTemplateAccountChanges(old, account)
			if len(result.Changes) == 0 {
				result.Action = AssetImportSkip
				result.Message = "nothing to change"
				summary.Skipped++
				continue
			}
			result.Action = AssetImportUpdate
		} else {
			result.Action = AssetImportCreate
			result.Changes = []string{"account: " + template.Username}
		}
		if push {
			result.Changes = append(result.Changes, "set the password on the asset")
		}

		if dryRun {
			if result.Action == AssetImportCreate {
				summary.Created++
			} else {
				summary.Updated++
			}
			continue
		}

		if old != nil {
			_, err = UpdateAssetAccount(old.GetId(), account)
		} else {
			_, err = AddAssetAccount(account)
		}
		if err != nil {
			fail(err)
			continue
		}
		if push {
			_, err = RotateAssetAccount(account.GetId(), true)
			if err != nil {
				fail(fmt.Errorf("the account is saved, but setting its password on the asset failed: %s", err.Error()))
				continue
			}
		}
		if result.Action == AssetImportCreate {
			summary.Created++
		} else {
			summary.Updated++
		}
	}
	return res, nil
}

func getTemplateAccountChanges(old *AssetAccount, account *AssetAccount) []string {
	res := []string{}
	if old.DisplayName != account.DisplayName {
		res = append(res, fmt.Sprintf("display name: %s -> %s", old.DisplayName, account.DisplayName))
	}
	if old.Password != account.Password {
		res = append(res, "password")
	}
	if old.PrivateKey != account.PrivateKey {
		res = append(res, "private key")
	}
	if old.AutoRotate != account.AutoRotate || old.RotateIntervalDays != account.RotateIntervalDays {
		res = append(res, fmt.Sprintf("rotation: %v every %d days", account.AutoRotate, account.RotateIntervalDays))
	}
	return res
}
