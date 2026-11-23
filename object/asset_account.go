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
	"strings"

	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

const (
	AccountRotateSucceeded = "Succeeded"
	AccountRotateFailed    = "Failed"
	AccountRotateRunning   = "Running"
)

// AssetAccount is a system account of an asset besides the asset's own account, which users
// connect as without ever seeing its password.
type AssetAccount struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	UpdatedTime string `xorm:"varchar(100)" json:"updatedTime"`

	Asset       string `xorm:"varchar(100) index" json:"asset"`
	Username    string `xorm:"varchar(100)" json:"username"`
	DisplayName string `xorm:"varchar(100)" json:"displayName"`
	Description string `xorm:"varchar(500)" json:"description"`
	Password    string `xorm:"text" json:"password"`
	PrivateKey  string `xorm:"mediumtext" json:"privateKey"`
	IsEnabled   bool   `json:"isEnabled"`

	AutoRotate         bool   `json:"autoRotate"`
	RotateIntervalDays int    `json:"rotateIntervalDays"`
	LastRotatedTime    string `xorm:"varchar(100)" json:"lastRotatedTime"`
	RotateStatus       string `xorm:"varchar(20)" json:"rotateStatus"`
	RotateMessage      string `xorm:"varchar(1000)" json:"rotateMessage"`

	// Template is the account template the account was created from, if any
	Template string `xorm:"varchar(100) index" json:"template"`
}

// AccountInfo is what a user sees of an account they may connect as.
type AccountInfo struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	IsDefault   bool   `json:"isDefault"`
}

func (account *AssetAccount) GetId() string {
	return util.GetIdFromOwnerAndName(account.Owner, account.Name)
}

func GetAssetAccounts(owner string, asset string) ([]*AssetAccount, error) {
	accounts := []*AssetAccount{}
	err := adapter.engine.Asc("username").Find(&accounts, &AssetAccount{Owner: owner, Asset: asset})
	if err != nil {
		return accounts, err
	}

	return accounts, nil
}

func getAllAssetAccounts(owner string) ([]*AssetAccount, error) {
	accounts := []*AssetAccount{}
	err := adapter.engine.Find(&accounts, &AssetAccount{Owner: owner})
	return accounts, err
}

func getAssetAccount(owner string, name string) (*AssetAccount, error) {
	if owner == "" || name == "" {
		return nil, nil
	}

	account := AssetAccount{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&account)
	if err != nil {
		return nil, err
	}
	if !existed {
		return nil, nil
	}

	err = account.decryptCredentials()
	if err != nil {
		return nil, err
	}
	return &account, nil
}

func GetAssetAccount(id string) (*AssetAccount, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	return getAssetAccount(owner, name)
}

func getAssetAccountByUsername(owner string, asset string, username string) (*AssetAccount, error) {
	account := AssetAccount{}
	existed, err := adapter.engine.Where("owner = ? and asset = ? and username = ?", owner, asset, username).Get(&account)
	if err != nil {
		return nil, err
	}
	if !existed {
		return nil, nil
	}

	err = account.decryptCredentials()
	if err != nil {
		return nil, err
	}
	return &account, nil
}

func GetMaskedAssetAccounts(accounts []*AssetAccount, errs ...error) ([]*AssetAccount, error) {
	if len(errs) > 0 && errs[0] != nil {
		return nil, errs[0]
	}

	for _, account := range accounts {
		account.mask()
	}
	return accounts, nil
}

func GetMaskedAssetAccount(account *AssetAccount, errs ...error) (*AssetAccount, error) {
	if len(errs) > 0 && errs[0] != nil {
		return nil, errs[0]
	}

	if account != nil {
		account.mask()
	}
	return account, nil
}

func (account *AssetAccount) mask() {
	if account.Password != "" {
		account.Password = "***"
	}
	if account.PrivateKey != "" {
		account.PrivateKey = "***"
	}
}

func (account *AssetAccount) encryptCredentials() error {
	var err error
	account.Password, err = encryptCredential(account.Password)
	if err != nil {
		return err
	}
	account.PrivateKey, err = encryptCredential(account.PrivateKey)
	return err
}

func (account *AssetAccount) decryptCredentials() error {
	var err error
	account.Password, err = decryptCredential(account.Password)
	if err != nil {
		return err
	}
	account.PrivateKey, err = decryptCredential(account.PrivateKey)
	return err
}

func (account *AssetAccount) check() error {
	account.Username = strings.TrimSpace(account.Username)
	if account.Username == "" {
		return fmt.Errorf("the username of the account cannot be empty")
	}
	if strings.ContainsAny(account.Username, ":@/\\ \t\r\n'\"") {
		return fmt.Errorf("the username: %s contains characters that are not allowed", account.Username)
	}

	asset, err := getAsset(account.Owner, account.Asset)
	if err != nil {
		return err
	}
	if asset == nil {
		return fmt.Errorf("the asset: %s does not exist in the organization: %s", account.Asset, account.Owner)
	}
	if asset.Username == account.Username {
		return fmt.Errorf("the username: %s is the asset's own account", account.Username)
	}

	existing, err := getAssetAccountByUsername(account.Owner, account.Asset, account.Username)
	if err != nil {
		return err
	}
	if existing != nil && existing.Name != account.Name {
		return fmt.Errorf("the asset: %s already has an account: %s", account.Asset, account.Username)
	}

	if account.Password == "" && account.PrivateKey == "" {
		return fmt.Errorf("the account: %s needs a password or a private key", account.Username)
	}
	if account.RotateIntervalDays < 0 {
		account.RotateIntervalDays = 0
	}
	if account.AutoRotate && account.RotateIntervalDays == 0 {
		account.RotateIntervalDays = 30
	}
	if account.AutoRotate {
		platform, err := getAssetPlatform(asset)
		if err != nil {
			return err
		}
		if !platform.canChange() || !platform.reaches(asset) {
			return fmt.Errorf("Casvisor can't change the passwords of the asset: %s, a %s asset over %s, so they can't be rotated", asset.Name, platform.getDisplayName(), asset.Protocol)
		}
	}
	return nil
}

func AddAssetAccount(account *AssetAccount) (bool, error) {
	if account.Name == "" {
		account.Name = util.GenerateId()
	}
	if account.CreatedTime == "" {
		account.CreatedTime = util.GetCurrentTime()
	}
	account.UpdatedTime = util.GetCurrentTime()

	err := account.check()
	if err != nil {
		return false, err
	}

	err = account.encryptCredentials()
	if err != nil {
		return false, err
	}

	affected, err := adapter.engine.Insert(account)
	if err != nil {
		return false, err
	}

	return affected != 0, nil
}

func UpdateAssetAccount(id string, account *AssetAccount) (bool, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	old, err := getAssetAccount(owner, name)
	if err != nil {
		return false, err
	}
	if old == nil {
		return false, nil
	}

	if account.Password == "***" {
		account.Password = old.Password
	}
	if account.PrivateKey == "***" {
		account.PrivateKey = old.PrivateKey
	}
	account.Owner = owner
	account.Name = name
	account.Asset = old.Asset
	account.LastRotatedTime = old.LastRotatedTime
	account.RotateStatus = old.RotateStatus
	account.RotateMessage = old.RotateMessage
	account.UpdatedTime = util.GetCurrentTime()

	err = account.check()
	if err != nil {
		return false, err
	}

	err = account.encryptCredentials()
	if err != nil {
		return false, err
	}

	affected, err := adapter.engine.ID(core.PK{owner, name}).AllCols().Update(account)
	if err != nil {
		return false, err
	}

	return affected != 0, nil
}

func DeleteAssetAccount(account *AssetAccount) (bool, error) {
	affected, err := adapter.engine.ID(core.PK{account.Owner, account.Name}).Delete(&AssetAccount{})
	if err != nil {
		return false, err
	}

	return affected != 0, nil
}

func deleteAssetAccounts(owner string, asset string) error {
	_, err := adapter.engine.Delete(&AssetAccount{Owner: owner, Asset: asset})
	return err
}

func renameAssetAccounts(owner string, oldName string, newName string) error {
	_, err := adapter.engine.Where("owner = ? and asset = ?", owner, oldName).Cols("asset").Update(&AssetAccount{Asset: newName})
	return err
}

func updateAssetAccountRotation(account *AssetAccount, password string, status string, message string) error {
	cols := []string{"rotate_status", "rotate_message", "updated_time"}
	update := &AssetAccount{RotateStatus: status, RotateMessage: message, UpdatedTime: util.GetCurrentTime()}
	if password != "" {
		encrypted, err := encryptCredential(password)
		if err != nil {
			return err
		}
		update.Password = encrypted
		update.LastRotatedTime = util.GetCurrentTime()
		cols = append(cols, "password", "last_rotated_time")
	}
	_, err := adapter.engine.ID(core.PK{account.Owner, account.Name}).Cols(cols...).Update(update)
	return err
}

// WithAccount returns a copy of the asset that connects as the account, which is the
// asset's own account when username is "" or its username.
func (asset *Asset) WithAccount(username string) (*Asset, error) {
	if username == "" || username == asset.Username {
		return asset, nil
	}

	account, err := getAssetAccountByUsername(asset.Owner, asset.Name, username)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, fmt.Errorf("the asset: %s has no account: %s", asset.Name, username)
	}
	if !account.IsEnabled {
		return nil, fmt.Errorf("the account: %s of the asset: %s is disabled", username, asset.Name)
	}

	res := *asset
	res.Username = account.Username
	res.Password = account.Password
	res.PrivateKey = account.PrivateKey
	return &res, nil
}

// getAccountKeys lists the accounts of the asset as permissions match them: "default:<username>" for
// the asset's own account and "account:<username>" for the others.
func getAccountKeys(asset *Asset, accounts []*AssetAccount) []string {
	res := []string{"default:" + asset.Username}
	for _, account := range accounts {
		if account.Asset == asset.Name && account.IsEnabled {
			res = append(res, "account:"+account.Username)
		}
	}
	return res
}

func getAccountKey(asset *Asset, username string) string {
	if username == "" || username == asset.Username {
		return "default:" + asset.Username
	}
	return "account:" + username
}
