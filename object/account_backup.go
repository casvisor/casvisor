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
	"encoding/csv"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

const (
	BackupRunSucceeded = "Succeeded"
	BackupRunFailed    = "Failed"

	minBackupPasswordLength = 12
)

// AccountBackupColumns are the columns of accounts.csv in a backup.
var AccountBackupColumns = []string{"asset", "host", "protocol", "username", "password", "privateKey", "displayName", "description", "isEnabled", "autoRotate", "rotateIntervalDays", "template", "lastRotatedTime"}

// AccountBackup makes zip files of the passwords and private keys Casvisor keeps, encrypted with
// AES-256 and its password, to download or to email on a cron schedule.
type AccountBackup struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	UpdatedTime string `xorm:"varchar(100)" json:"updatedTime"`
	DisplayName string `xorm:"varchar(100)" json:"displayName"`
	Description string `xorm:"varchar(500)" json:"description"`

	// IncludeAssets adds assets.csv: the assets with their own accounts, as the asset import reads them
	IncludeAssets bool `json:"includeAssets"`
	// Password is what the zip file is encrypted with
	Password string `xorm:"varchar(500)" json:"password"`
	// Webhooks are the webhooks of the Email type that the backup is emailed with
	Webhooks []string `xorm:"varchar(1000)" json:"webhooks"`

	Cron      string `xorm:"varchar(100)" json:"cron"`
	TimeZone  string `xorm:"varchar(100)" json:"timeZone"`
	IsEnabled bool   `json:"isEnabled"`

	NextRunTime      string `xorm:"varchar(100)" json:"nextRunTime"`
	LastRunTime      string `xorm:"varchar(100)" json:"lastRunTime"`
	LastRunStatus    string `xorm:"varchar(20)" json:"lastRunStatus"`
	LastRunMessage   string `xorm:"varchar(1000)" json:"lastRunMessage"`
	LastAccountCount int    `json:"lastAccountCount"`
}

func (backup *AccountBackup) GetId() string {
	return util.GetIdFromOwnerAndName(backup.Owner, backup.Name)
}

func GetAccountBackupCount(owner, field, value string) (int64, error) {
	session := GetSession(owner, -1, -1, field, value, "", "")
	return session.Count(&AccountBackup{})
}

func GetPaginationAccountBackups(owner string, offset, limit int, field, value, sortField, sortOrder string) ([]*AccountBackup, error) {
	backups := []*AccountBackup{}
	session := GetSession(owner, offset, limit, field, value, sortField, sortOrder)
	err := session.Find(&backups)
	return backups, err
}

func getAccountBackup(owner string, name string) (*AccountBackup, error) {
	if owner == "" || name == "" {
		return nil, nil
	}
	backup := AccountBackup{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&backup)
	if err != nil || !existed {
		return nil, err
	}
	backup.Password, err = decryptCredential(backup.Password)
	if err != nil {
		return nil, err
	}
	return &backup, nil
}

func GetAccountBackup(id string) (*AccountBackup, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	return getAccountBackup(owner, name)
}

// GetMaskedAccountBackups hides the passwords of the backups, which are only written.
func GetMaskedAccountBackups(backups []*AccountBackup) []*AccountBackup {
	for _, backup := range backups {
		if backup != nil && backup.Password != "" {
			backup.Password = "***"
		}
	}
	return backups
}

func (backup *AccountBackup) getSchedule() *JobSchedule {
	return &JobSchedule{Cron: backup.Cron, TimeZone: backup.TimeZone, IsEnabled: backup.IsEnabled}
}

func (backup *AccountBackup) check() error {
	if len([]rune(backup.Password)) < minBackupPasswordLength {
		return fmt.Errorf("the password of the backup should have at least %d characters", minBackupPasswordLength)
	}

	webhooks := []string{}
	for _, name := range backup.Webhooks {
		if name == "" || slices.Contains(webhooks, name) {
			continue
		}
		webhook, err := getWebhook(backup.Owner, name)
		if err != nil {
			return err
		}
		if webhook == nil {
			return fmt.Errorf("the webhook: %s does not exist", name)
		}
		if webhook.Type != WebhookTypeEmail {
			return fmt.Errorf("the webhook: %s is not of the Email type, a backup can only be emailed", name)
		}
		webhooks = append(webhooks, name)
	}
	backup.Webhooks = webhooks

	backup.Cron = strings.TrimSpace(backup.Cron)
	backup.NextRunTime = ""
	if backup.Cron == "" {
		backup.IsEnabled = false
		return nil
	}
	if len(backup.Webhooks) == 0 {
		return fmt.Errorf("a backup on a schedule needs a webhook of the Email type to send it with")
	}
	next, err := backup.getSchedule().getNextRunTime(time.Now())
	if err != nil {
		return err
	}
	if backup.IsEnabled {
		backup.NextRunTime = next
	}
	return nil
}

func (backup *AccountBackup) save(update func(*AccountBackup) (int64, error)) (bool, error) {
	err := backup.check()
	if err != nil {
		return false, err
	}
	stored := *backup
	stored.Password, err = encryptCredential(backup.Password)
	if err != nil {
		return false, err
	}
	affected, err := update(&stored)
	return affected != 0, err
}

func UpdateAccountBackup(id string, backup *AccountBackup) (bool, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	old, err := getAccountBackup(owner, name)
	if err != nil {
		return false, err
	} else if old == nil {
		return false, nil
	}

	if backup.Password == "***" {
		backup.Password = old.Password
	}
	backup.UpdatedTime = util.GetCurrentTime()
	backup.LastRunTime, backup.LastRunStatus, backup.LastRunMessage, backup.LastAccountCount = old.LastRunTime, old.LastRunStatus, old.LastRunMessage, old.LastAccountCount
	return backup.save(func(stored *AccountBackup) (int64, error) {
		return adapter.engine.ID(core.PK{owner, name}).AllCols().Update(stored)
	})
}

func AddAccountBackup(backup *AccountBackup) (bool, error) {
	if backup.Password == "***" {
		backup.Password = ""
	}
	backup.LastRunTime, backup.LastRunStatus, backup.LastRunMessage, backup.LastAccountCount = "", "", "", 0
	return backup.save(func(stored *AccountBackup) (int64, error) {
		return adapter.engine.Insert(stored)
	})
}

func DeleteAccountBackup(backup *AccountBackup) (bool, error) {
	affected, err := adapter.engine.ID(core.PK{backup.Owner, backup.Name}).Delete(&AccountBackup{})
	return affected != 0, err
}

func writeCsv(header []string, rows [][]string) ([]byte, error) {
	buf := &bytes.Buffer{}
	// with a BOM, so that Excel reads it as UTF-8
	buf.WriteString("\xEF\xBB\xBF")
	writer := csv.NewWriter(buf)
	err := writer.Write(header)
	if err != nil {
		return nil, err
	}
	err = writer.WriteAll(rows)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// backupCell keeps a spreadsheet from running a cell as a formula, as the asset export does;
// passwords and private keys are left as they are.
func backupCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func unquoteBackupCell(s string) string {
	if strings.HasPrefix(s, "'") && len(s) > 1 && strings.ContainsRune("=+-@\t\r", rune(s[1])) {
		return s[1:]
	}
	return s
}

const accountBackupReadme = `Casvisor account backup

accounts.csv: the system accounts of the assets, with their passwords and private keys.
assets.csv: the assets with the passwords and private keys of their own accounts, when the backup
includes them. It is in the format of the asset import of Casvisor.

Restore it on the Account backups page of Casvisor: the assets first, then the accounts.
In the private keys, \n stands for a new line.
`

// BuildAccountBackup makes the zip file of the backup, and returns it with the count of the accounts in it.
func BuildAccountBackup(backup *AccountBackup) ([]byte, int, error) {
	assets, err := GetAssets(backup.Owner)
	if err != nil {
		return nil, 0, err
	}
	assetMap := map[string]*Asset{}
	for _, asset := range assets {
		err = asset.decryptCredentials()
		if err != nil {
			return nil, 0, err
		}
		assetMap[asset.Name] = asset
	}
	accounts := []*AssetAccount{}
	err = adapter.engine.Asc("asset", "username").Find(&accounts, &AssetAccount{Owner: backup.Owner})
	if err != nil {
		return nil, 0, err
	}

	rows := [][]string{}
	for _, account := range accounts {
		err = account.decryptCredentials()
		if err != nil {
			return nil, 0, err
		}
		host, protocol := "", ""
		if asset := assetMap[account.Asset]; asset != nil {
			host, protocol = asset.Host+":"+strconv.Itoa(asset.Port), asset.Protocol
		}
		rows = append(rows, []string{
			backupCell(account.Asset), backupCell(host), protocol, backupCell(account.Username),
			account.Password, strings.ReplaceAll(account.PrivateKey, "\n", `\n`),
			backupCell(account.DisplayName), backupCell(account.Description),
			strconv.FormatBool(account.IsEnabled), strconv.FormatBool(account.AutoRotate), strconv.Itoa(account.RotateIntervalDays),
			backupCell(account.Template), account.LastRotatedTime,
		})
	}
	accountCsv, err := writeCsv(AccountBackupColumns, rows)
	if err != nil {
		return nil, 0, err
	}
	entries := []*ZipEntry{{Name: "README.txt", Data: []byte(strings.ReplaceAll(accountBackupReadme, "\n", "\r\n"))}, {Name: "accounts.csv", Data: accountCsv}}
	count := len(accounts)

	if backup.IncludeAssets {
		nodeMap, err := GetAssetNodeMap(backup.Owner)
		if err != nil {
			return nil, 0, err
		}
		assetRows := [][]string{}
		for i := len(assets) - 1; i >= 0; i-- {
			asset := assets[i]
			row := GetAssetCsvRow(asset, nodeMap)
			for j := range row {
				row[j] = backupCell(row[j])
			}
			row[7], row[8], row[15] = asset.Password, strings.ReplaceAll(asset.PrivateKey, "\n", `\n`), asset.ElevationPassword
			assetRows = append(assetRows, row)
			if asset.Password != "" || asset.PrivateKey != "" {
				count++
			}
		}
		assetCsv, err := writeCsv(AssetImportColumns, assetRows)
		if err != nil {
			return nil, 0, err
		}
		entries = append(entries, &ZipEntry{Name: "assets.csv", Data: assetCsv})
	}

	data, err := WriteAesZip(entries, backup.Password, time.Now())
	return data, count, err
}

func (backup *AccountBackup) GetFileName(t time.Time) string {
	return fmt.Sprintf("casvisor-accounts-%s-%s.zip", backup.Owner, t.Format("20060102-1504"))
}

var accountBackupLock sync.Mutex

// RunAccountBackup makes the backup now and emails it with its webhooks.
func RunAccountBackup(id string) error {
	accountBackupLock.Lock()
	defer accountBackupLock.Unlock()

	backup, err := GetAccountBackup(id)
	if err != nil {
		return err
	}
	if backup == nil {
		return fmt.Errorf("the account backup: %s does not exist", id)
	}
	return backup.run(time.Now())
}

func (backup *AccountBackup) run(now time.Time) (err error) {
	count := 0
	defer func() {
		update := &AccountBackup{LastRunTime: now.Format(time.RFC3339), LastRunStatus: BackupRunSucceeded, LastAccountCount: count}
		update.LastRunMessage = fmt.Sprintf("sent to %s", strings.Join(backup.Webhooks, ", "))
		if err != nil {
			update.LastRunStatus, update.LastRunMessage = BackupRunFailed, truncate(err.Error(), 1000)
			notifyAccountBackupFailed(backup, err.Error(), update.LastRunTime)
		}
		if backup.IsEnabled && backup.Cron != "" {
			update.NextRunTime, _ = backup.getSchedule().getNextRunTime(now)
		}
		_, updateErr := adapter.engine.ID(core.PK{backup.Owner, backup.Name}).Cols("last_run_time", "last_run_status", "last_run_message", "last_account_count", "next_run_time").Update(update)
		if updateErr != nil {
			logs.Error(fmt.Sprintf("AccountBackup.run(): %s", updateErr.Error()))
		}
	}()

	if len(backup.Webhooks) == 0 {
		return fmt.Errorf("the backup has no webhook of the Email type to send it with")
	}
	data, count, err := BuildAccountBackup(backup)
	if err != nil {
		return err
	}

	fileName := backup.GetFileName(now)
	body := fmt.Sprintf("The account backup %s of the organization %s, made at %s, is attached: %s, with %d accounts.\n\n"+
		"It is a zip file encrypted with AES-256. Open it with the password of the backup, with 7-Zip, WinRAR, Bandizip or the Archive Utility of macOS, or restore it on the Account backups page of Casvisor.\n",
		backup.Name, backup.Owner, now.Format(time.RFC3339), fileName, count)
	attachment := &MailAttachment{Name: fileName, ContentType: "application/zip", Data: data}

	failed := []string{}
	for _, name := range backup.Webhooks {
		webhook, getErr := getWebhook(backup.Owner, name)
		if getErr == nil && webhook == nil {
			getErr = fmt.Errorf("it does not exist")
		} else if getErr == nil && webhook.Type != WebhookTypeEmail {
			getErr = fmt.Errorf("it is not of the Email type")
		}
		if getErr == nil {
			getErr = webhook.sendMail(fmt.Sprintf("[Casvisor] Account backup of %s", backup.Owner), body, []*MailAttachment{attachment})
		}
		if getErr != nil {
			failed = append(failed, fmt.Sprintf("%s: %s", name, getErr.Error()))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("failed to send the backup with %s", strings.Join(failed, "; "))
	}
	return nil
}

// runDueAccountBackups runs the backups whose cron expression matches this minute.
func runDueAccountBackups(now time.Time) {
	backups := []*AccountBackup{}
	err := adapter.engine.Where("is_enabled = ?", true).Find(&backups)
	if err != nil {
		logs.Error(fmt.Sprintf("runDueAccountBackups(): %s", err.Error()))
		return
	}

	minute := now.Truncate(time.Minute)
	for _, backup := range backups {
		schedule := backup.getSchedule()
		cron, err := parseCron(backup.Cron)
		if err != nil {
			continue
		}
		location, err := schedule.getLocation()
		if err != nil || !cron.matches(minute.In(location)) {
			continue
		}
		if last, err := time.Parse(time.RFC3339, backup.LastRunTime); err == nil && !last.Before(minute) {
			continue
		}

		backup.Password, err = decryptCredential(backup.Password)
		if err == nil {
			accountBackupLock.Lock()
			err = backup.run(minute)
			accountBackupLock.Unlock()
		}
		if err != nil {
			logs.Warning(fmt.Sprintf("runDueAccountBackups(): the account backup: %s failed: %s", backup.GetId(), err.Error()))
		}
	}
}
