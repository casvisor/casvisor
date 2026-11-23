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
	"sync"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

const (
	CloudAddressPublic  = "Public"
	CloudAddressPrivate = "Private"

	CloudSyncSucceeded = "Succeeded"
	CloudSyncFailed    = "Failed"

	AssetImportMissing = "missing"
	AssetImportDelete  = "delete"

	minCloudSyncInterval = 5
)

// CloudAccount is an account of a cloud provider whose servers Casvisor keeps as assets.
type CloudAccount struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	UpdatedTime string `xorm:"varchar(100)" json:"updatedTime"`
	DisplayName string `xorm:"varchar(100)" json:"displayName"`
	Description string `xorm:"varchar(500)" json:"description"`

	Provider string `xorm:"varchar(50)" json:"provider"`
	// AccessKey is the AccessKey ID, SecretId or client ID, and AccessSecret its secret, or for
	// GCP the JSON key of a service account
	AccessKey      string   `xorm:"varchar(200)" json:"accessKey"`
	AccessSecret   string   `xorm:"mediumtext" json:"accessSecret"`
	TenantId       string   `xorm:"varchar(100)" json:"tenantId"`
	SubscriptionId string   `xorm:"varchar(100)" json:"subscriptionId"`
	ProjectId      string   `xorm:"varchar(100)" json:"projectId"`
	Regions        []string `xorm:"mediumtext" json:"regions"`

	// what new assets get: the address they are reached at, the username by OS, where in the
	// asset tree they go (a node per region under it), their tag and network gateway
	AddressType     string `xorm:"varchar(20)" json:"addressType"`
	LinuxUsername   string `xorm:"varchar(100)" json:"linuxUsername"`
	WindowsUsername string `xorm:"varchar(100)" json:"windowsUsername"`
	NodePath        string `xorm:"varchar(500)" json:"nodePath"`
	Tag             string `xorm:"varchar(100)" json:"tag"`
	NetworkGateway  string `xorm:"varchar(100)" json:"networkGateway"`

	// SyncInterval is in minutes, 0 to sync only by hand
	SyncInterval  int  `json:"syncInterval"`
	DeleteMissing bool `json:"deleteMissing"`
	IsEnabled     bool `json:"isEnabled"`

	LastSyncTime    string `xorm:"varchar(100)" json:"lastSyncTime"`
	LastSyncStatus  string `xorm:"varchar(20)" json:"lastSyncStatus"`
	LastSyncMessage string `xorm:"varchar(1000)" json:"lastSyncMessage"`
}

type CloudSyncSummary struct {
	Instances    int `json:"instances"`
	Created      int `json:"created"`
	Updated      int `json:"updated"`
	Skipped      int `json:"skipped"`
	Failed       int `json:"failed"`
	Missing      int `json:"missing"`
	Deleted      int `json:"deleted"`
	CreatedNodes int `json:"createdNodes"`
}

type CloudSyncResult struct {
	Results []*AssetImportResult `json:"results"`
	Summary *CloudSyncSummary    `json:"summary"`
}

func (account *CloudAccount) GetId() string {
	return util.GetIdFromOwnerAndName(account.Owner, account.Name)
}

func GetCloudAccountCount(owner, field, value string) (int64, error) {
	session := GetSession(owner, -1, -1, field, value, "", "")
	return session.Count(&CloudAccount{})
}

func GetCloudAccounts(owner string) ([]*CloudAccount, error) {
	accounts := []*CloudAccount{}
	err := adapter.engine.Desc("created_time").Find(&accounts, &CloudAccount{Owner: owner})
	return accounts, err
}

func GetPaginationCloudAccounts(owner string, offset, limit int, field, value, sortField, sortOrder string) ([]*CloudAccount, error) {
	accounts := []*CloudAccount{}
	session := GetSession(owner, offset, limit, field, value, sortField, sortOrder)
	err := session.Find(&accounts)
	return accounts, err
}

func getCloudAccount(owner string, name string) (*CloudAccount, error) {
	if owner == "" || name == "" {
		return nil, nil
	}

	account := CloudAccount{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&account)
	if err != nil {
		return nil, err
	}
	if !existed {
		return nil, nil
	}

	account.AccessSecret, err = decryptCredential(account.AccessSecret)
	if err != nil {
		return nil, err
	}
	return &account, nil
}

func GetCloudAccount(id string) (*CloudAccount, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	return getCloudAccount(owner, name)
}

// GetMaskedCloudAccounts hides the secrets of cloud accounts, which are only written.
func GetMaskedCloudAccounts(accounts []*CloudAccount) []*CloudAccount {
	for _, account := range accounts {
		if account != nil && account.AccessSecret != "" {
			account.AccessSecret = "***"
		}
	}
	return accounts
}

func (account *CloudAccount) check() error {
	if !slices.Contains(cloudProviders, account.Provider) {
		return fmt.Errorf("invalid cloud provider: %s", account.Provider)
	}
	if account.Provider != CloudProviderGcp && account.AccessKey == "" {
		return fmt.Errorf("the access key can not be empty")
	}
	if account.AccessSecret == "" {
		return fmt.Errorf("the secret can not be empty")
	}
	if account.Provider == CloudProviderAzure && (account.TenantId == "" || account.SubscriptionId == "") {
		return fmt.Errorf("an Azure account needs the tenant ID and the subscription ID")
	}
	if account.Provider == CloudProviderGcp {
		_, err := getGcpKey(account)
		if err != nil {
			return err
		}
	}

	if account.AddressType == "" {
		account.AddressType = CloudAddressPublic
	}
	if account.AddressType != CloudAddressPublic && account.AddressType != CloudAddressPrivate {
		return fmt.Errorf("invalid address type: %s", account.AddressType)
	}
	if account.SyncInterval < 0 || (account.SyncInterval > 0 && account.SyncInterval < minCloudSyncInterval) {
		return fmt.Errorf("the sync interval should be 0, to sync by hand, or at least %d minutes", minCloudSyncInterval)
	}
	if account.NetworkGateway != "" {
		gateway, err := getNetworkGateway(account.Owner, account.NetworkGateway)
		if err != nil {
			return err
		}
		if gateway == nil {
			return fmt.Errorf("the network gateway: %s does not exist", account.NetworkGateway)
		}
	}

	regions := []string{}
	for _, region := range account.Regions {
		region = strings.TrimSpace(region)
		if region != "" && !slices.Contains(regions, region) {
			regions = append(regions, region)
		}
	}
	account.Regions = regions
	return nil
}

// fillSavedSecret keeps the saved secret when the form sends it masked.
func (account *CloudAccount) fillSavedSecret(saved *CloudAccount) {
	if account.AccessSecret == "***" {
		account.AccessSecret = ""
		if saved != nil {
			account.AccessSecret = saved.AccessSecret
		}
	}
}

func (account *CloudAccount) save(update func(*CloudAccount) (int64, error)) (bool, error) {
	err := account.check()
	if err != nil {
		return false, err
	}

	stored := *account
	stored.AccessSecret, err = encryptCredential(account.AccessSecret)
	if err != nil {
		return false, err
	}
	affected, err := update(&stored)
	if err != nil {
		return false, err
	}
	return affected != 0, nil
}

func UpdateCloudAccount(id string, account *CloudAccount) (bool, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	old, err := getCloudAccount(owner, name)
	if err != nil {
		return false, err
	} else if old == nil {
		return false, nil
	}

	account.fillSavedSecret(old)
	account.UpdatedTime = util.GetCurrentTime()
	account.LastSyncTime, account.LastSyncStatus, account.LastSyncMessage = old.LastSyncTime, old.LastSyncStatus, old.LastSyncMessage
	affected, err := account.save(func(stored *CloudAccount) (int64, error) {
		return adapter.engine.ID(core.PK{owner, name}).AllCols().Update(stored)
	})
	if err != nil || !affected {
		return affected, err
	}

	if account.Name != name || account.Owner != owner {
		_, err = adapter.engine.Where("owner = ? and cloud_account = ?", owner, name).Cols("cloud_account").Update(&Asset{CloudAccount: account.Name})
	}
	return true, err
}

func AddCloudAccount(account *CloudAccount) (bool, error) {
	account.fillSavedSecret(nil)
	account.LastSyncTime, account.LastSyncStatus, account.LastSyncMessage = "", "", ""
	return account.save(func(stored *CloudAccount) (int64, error) {
		return adapter.engine.Insert(stored)
	})
}

// DeleteCloudAccount deletes the account and keeps its assets, which are no longer synced.
func DeleteCloudAccount(account *CloudAccount) (bool, error) {
	affected, err := adapter.engine.ID(core.PK{account.Owner, account.Name}).Delete(&CloudAccount{})
	if err != nil {
		return false, err
	}

	_, err = adapter.engine.Where("owner = ? and cloud_account = ?", account.Owner, account.Name).Cols("cloud_account", "cloud_instance").Update(&Asset{})
	if err != nil {
		return false, err
	}
	return affected != 0, nil
}

// GetCloudRegions lists the regions of the account as it is in the form, the secret of the saved
// account id being used when the form has it masked.
func GetCloudRegions(id string, account *CloudAccount) ([]string, error) {
	var saved *CloudAccount
	if id != "" {
		var err error
		saved, err = GetCloudAccount(id)
		if err != nil {
			return nil, err
		}
		if saved != nil && saved.Owner != account.Owner {
			saved = nil
		}
	}
	target := *account
	target.fillSavedSecret(saved)
	err := target.check()
	if err != nil {
		return nil, err
	}
	return listCloudRegions(&target)
}

func (account *CloudAccount) getHost(instance *CloudInstance) string {
	first, second := instance.PublicIps, instance.PrivateIps
	if account.AddressType == CloudAddressPrivate {
		first, second = second, first
	}
	for _, ips := range [][]string{first, second} {
		for _, ip := range ips {
			if ip != "" {
				return ip
			}
		}
	}
	return ""
}

func (account *CloudAccount) getDescription(instance *CloudInstance) string {
	parts := []string{account.Provider}
	if instance.Type != "" {
		parts = append(parts, instance.Type)
	}
	return fmt.Sprintf("%s in %s, %s", strings.Join(parts, " "), instance.Region, instance.Id)
}

func getCloudAssetBaseName(instance *CloudInstance) string {
	name := instance.Name
	if name == "" {
		name = instance.Id
	}
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune("/@:", r) || r <= ' ' {
			return '-'
		}
		return r
	}, strings.TrimSpace(name))
	runes := []rune(name)
	for len(string(runes)) > 80 {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}

// getCloudAssetName names a new asset after its server, adding the end of the server's ID when
// the name is taken; the names of the assets this sync deletes are free.
func getCloudAssetName(owner string, instance *CloudInstance, used map[string]bool, freed map[string]bool) (string, error) {
	base := getCloudAssetBaseName(instance)
	id := getCloudAssetBaseName(&CloudInstance{Id: instance.Id})
	if len(id) > 8 {
		id = id[len(id)-8:]
	}
	for _, name := range []string{base, base + "-" + id} {
		if used[name] {
			continue
		}
		asset, err := getAsset(owner, name)
		if err != nil {
			return "", err
		}
		if asset == nil || freed[name] {
			return name, nil
		}
	}
	return "", fmt.Errorf("the names %s and %s-%s are both taken", base, base, id)
}

var cloudSyncing sync.Map

// SyncCloudAccount brings the assets of the account in line with its servers: new servers become
// assets, the display name, address, OS and description of the synced assets follow their
// servers, and the assets whose servers are gone are reported, or deleted when the account says
// so. With dryRun nothing is saved.
func SyncCloudAccount(id string, dryRun bool) (*CloudSyncResult, error) {
	if _, running := cloudSyncing.LoadOrStore(id, true); running {
		return nil, fmt.Errorf("the cloud account: %s is being synced", id)
	}
	defer cloudSyncing.Delete(id)

	account, err := GetCloudAccount(id)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, fmt.Errorf("the cloud account: %s does not exist", id)
	}

	res, err := syncCloudAccount(account, dryRun)
	if !dryRun {
		status, message := CloudSyncSucceeded, ""
		if err != nil {
			status, message = CloudSyncFailed, err.Error()
		} else {
			s := res.Summary
			message = fmt.Sprintf("%d servers: %d created, %d updated, %d unchanged, %d failed, %d missing, %d deleted", s.Instances, s.Created, s.Updated, s.Skipped, s.Failed, s.Missing, s.Deleted)
			if s.Failed > 0 {
				status = CloudSyncFailed
			}
		}
		if runes := []rune(message); len(runes) > 1000 {
			message = string(runes[:1000])
		}
		_, updateErr := adapter.engine.ID(core.PK{account.Owner, account.Name}).Cols("last_sync_time", "last_sync_status", "last_sync_message").
			Update(&CloudAccount{LastSyncTime: util.GetCurrentTime(), LastSyncStatus: status, LastSyncMessage: message})
		if updateErr != nil {
			logs.Error(fmt.Sprintf("SyncCloudAccount(): %s", updateErr.Error()))
		}
	}
	return res, err
}

func syncCloudAccount(account *CloudAccount, dryRun bool) (*CloudSyncResult, error) {
	instances, err := listCloudInstances(account)
	if err != nil {
		return nil, err
	}

	owner := account.Owner
	synced := []*Asset{}
	err = adapter.engine.Where("owner = ? and cloud_account = ?", owner, account.Name).Find(&synced)
	if err != nil {
		return nil, err
	}
	byInstance := map[string]*Asset{}
	for _, asset := range synced {
		err = asset.decryptCredentials()
		if err != nil {
			return nil, err
		}
		byInstance[asset.CloudInstance] = asset
	}

	nodes, err := GetAssetNodes(owner)
	if err != nil {
		return nil, err
	}
	oldNodes := []string{}
	for _, node := range nodes {
		oldNodes = append(oldNodes, node.Name)
	}

	res := &CloudSyncResult{Results: []*AssetImportResult{}, Summary: &CloudSyncSummary{Instances: len(instances)}}
	summary := res.Summary
	seen := map[string]bool{}
	for _, instance := range instances {
		seen[instance.Id] = true
	}

	// the assets whose servers are gone go first, so that a server back under a new ID can
	// take the name of its old asset
	missing := []*AssetImportResult{}
	freedNames := map[string]bool{}
	for _, asset := range synced {
		if seen[asset.CloudInstance] {
			continue
		}
		result := &AssetImportResult{Name: asset.Name, Action: AssetImportMissing, Message: fmt.Sprintf("the server: %s is no longer in the cloud account", asset.CloudInstance)}
		missing = append(missing, result)

		// a list that comes back empty is never taken as every server being gone
		if !account.DeleteMissing || len(instances) == 0 {
			summary.Missing++
			continue
		}
		result.Action = AssetImportDelete
		if !dryRun {
			_, err = DeleteAsset(asset)
			if err != nil {
				result.Action = AssetImportError
				result.Message = err.Error()
				summary.Failed++
				continue
			}
		}
		freedNames[asset.Name] = true
		summary.Deleted++
	}

	usedNames := map[string]bool{}
	for i, instance := range instances {
		result := &AssetImportResult{Row: i + 1, Name: instance.Id}
		res.Results = append(res.Results, result)
		fail := func(err error) {
			result.Action = AssetImportError
			result.Message = err.Error()
			summary.Failed++
		}

		host := account.getHost(instance)
		if host == "" {
			fail(fmt.Errorf("the server: %s (%s) has no IP address", instance.Name, instance.Id))
			continue
		}
		displayName := instance.Name
		if displayName == "" {
			displayName = instance.Id
		}

		old := byInstance[instance.Id]
		if old != nil {
			result.Name = old.Name
			asset := *old
			asset.DisplayName = displayName
			asset.Host = host
			asset.Os = instance.Os
			asset.Description = account.getDescription(instance)
			err = asset.normalizeAddress()
			if err != nil {
				fail(err)
				continue
			}

			result.Changes = getAssetChanges(old, &asset)
			if len(result.Changes) == 0 {
				result.Action = AssetImportSkip
				result.Message = "nothing to change"
				summary.Skipped++
				continue
			}
			err = asset.checkKeptSecrets(old, true, true)
			if err != nil {
				fail(err)
				continue
			}
			if asset.Password != "" {
				asset.Password = "***"
			}
			if asset.PrivateKey != "" {
				asset.PrivateKey = "***"
			}
			result.Action = AssetImportUpdate
			if !dryRun {
				asset.UpdatedTime = util.GetCurrentTime()
				_, err = UpdateAsset(asset.GetId(), &asset)
				if err != nil {
					fail(err)
					continue
				}
			}
			summary.Updated++
			continue
		}

		name, err := getCloudAssetName(owner, instance, usedNames, freedNames)
		if err != nil {
			fail(err)
			continue
		}
		usedNames[name] = true
		result.Name = name

		asset := &Asset{
			Owner:          owner,
			Name:           name,
			CreatedTime:    util.GetCurrentTime(),
			DisplayName:    displayName,
			Description:    account.getDescription(instance),
			Protocol:       "SSH",
			Host:           host,
			Username:       account.LinuxUsername,
			Os:             instance.Os,
			Tag:            account.Tag,
			NetworkGateway: account.NetworkGateway,
			CloudAccount:   account.Name,
			CloudInstance:  instance.Id,
		}
		if instance.Os == "Windows" {
			asset.Protocol = "RDP"
			asset.Username = account.WindowsUsername
		}
		asset.Port = assetDefaultPorts[asset.Protocol]
		result.Changes = []string{fmt.Sprintf("%s %s:%d", asset.Protocol, asset.Host, asset.Port)}

		if account.NodePath != "" {
			path := strings.TrimRight(account.NodePath, "/") + "/" + instance.Region
			node, created, err := resolveAssetNodePath(owner, &nodes, path, !dryRun)
			if err != nil {
				fail(err)
				continue
			}
			if created {
				result.Changes = append(result.Changes, "new node: "+path)
			}
			asset.Nodes = []string{node}
		}

		result.Action = AssetImportCreate
		if !dryRun {
			asset.UpdatedTime = asset.CreatedTime
			_, err = AddAsset(asset)
		} else if asset.NetworkGateway != "" {
			err = asset.checkNetworkGateway()
		}
		if err != nil {
			fail(err)
			continue
		}
		summary.Created++
	}

	for i, result := range missing {
		result.Row = len(instances) + i + 1
		res.Results = append(res.Results, result)
	}

	for _, node := range nodes {
		if strings.HasPrefix(node.Name, "?") || !slices.Contains(oldNodes, node.Name) {
			summary.CreatedNodes++
		}
	}
	return res, nil
}

func isCloudSyncDue(account *CloudAccount, now time.Time) bool {
	if !account.IsEnabled || account.SyncInterval <= 0 {
		return false
	}
	if account.LastSyncTime == "" {
		return true
	}
	last, err := time.Parse(time.RFC3339, account.LastSyncTime)
	return err != nil || now.Sub(last) >= time.Duration(account.SyncInterval)*time.Minute
}

func syncDueCloudAccounts() {
	accounts := []*CloudAccount{}
	err := adapter.engine.Where("is_enabled = ? and sync_interval > 0", true).Find(&accounts)
	if err != nil {
		logs.Error(fmt.Sprintf("syncDueCloudAccounts(): %s", err.Error()))
		return
	}

	now := time.Now()
	for _, account := range accounts {
		if !isCloudSyncDue(account, now) {
			continue
		}
		_, err = SyncCloudAccount(account.GetId(), false)
		if err != nil {
			logs.Warning(fmt.Sprintf("syncDueCloudAccounts(): failed to sync the cloud account: %s: %s", account.GetId(), err.Error()))
		}
	}
}

func StartCloudSyncer() {
	go func() {
		time.Sleep(time.Minute)
		syncDueCloudAccounts()
		for range time.Tick(time.Minute) {
			syncDueCloudAccounts()
		}
	}()
}
