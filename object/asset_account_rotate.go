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
	"github.com/jackc/pgx/v5"
)

const accountPasswordChars = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!#%+-=_."

var (
	rotatingAccounts          sync.Map
	setAccountPasswordFunc    = setAccountPassword
	verifyAccountPasswordFunc = verifyAccountPassword
	sqlBackslashQuoteReplacer = strings.NewReplacer(`\`, `\\`, `'`, `''`)
)

type databaseRotator interface {
	queryStrings(query string, args ...interface{}) ([][]string, error)
	exec(statement string) error
}

func canRotateAccountProtocol(protocol string) bool {
	return strings.EqualFold(protocol, "SSH") || IsDatabaseProtocol(protocol)
}

func checkRotatableAsset(asset *Asset) error {
	if !canRotateAccountProtocol(asset.Protocol) {
		return fmt.Errorf("the passwords of accounts can only be changed on SSH, MySQL and PostgreSQL assets, the asset: %s uses %s", asset.Name, asset.Protocol)
	}
	return nil
}

func validateDatabaseValue(name string, value string, allowEmpty bool) error {
	if !allowEmpty && value == "" {
		return fmt.Errorf("the %s cannot be empty", name)
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("the %s cannot contain null bytes or line breaks", name)
	}
	return nil
}

func quoteMysqlString(value string, noBackslashEscapes bool) string {
	if noBackslashEscapes {
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	}
	return "'" + sqlBackslashQuoteReplacer.Replace(value) + "'"
}

func quotePostgresqlString(value string) string {
	if strings.Contains(value, `\`) {
		return "E'" + sqlBackslashQuoteReplacer.Replace(value) + "'"
	}
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func buildMysqlAccountPasswordSql(username string, host string, password string, create bool, noBackslashEscapes bool) (string, error) {
	if err := validateDatabaseValue("username", username, false); err != nil {
		return "", err
	}
	if err := validateDatabaseValue("host", host, true); err != nil {
		return "", err
	}
	if err := validateDatabaseValue("password", password, false); err != nil {
		return "", err
	}
	verb := "ALTER USER"
	if create {
		verb = "CREATE USER"
	}
	return fmt.Sprintf("%s %s@%s IDENTIFIED BY %s", verb, quoteMysqlString(username, noBackslashEscapes), quoteMysqlString(host, noBackslashEscapes), quoteMysqlString(password, noBackslashEscapes)), nil
}

func buildPostgresqlAccountPasswordSql(username string, password string, create bool) (string, error) {
	if err := validateDatabaseValue("username", username, false); err != nil {
		return "", err
	}
	if err := validateDatabaseValue("password", password, false); err != nil {
		return "", err
	}
	ident := pgx.Identifier{username}.Sanitize()
	literal := quotePostgresqlString(password)
	if create {
		return fmt.Sprintf("CREATE ROLE %s WITH LOGIN PASSWORD %s", ident, literal), nil
	}
	return fmt.Sprintf("ALTER ROLE %s WITH PASSWORD %s", ident, literal), nil
}

func sanitizeRotateError(err error, password string) error {
	if err == nil || password == "" || !strings.Contains(err.Error(), password) {
		return err
	}
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), password, "***"))
}

func setMysqlAccountPassword(db databaseRotator, username string, password string, create bool) error {
	rows, err := db.queryStrings("SELECT user, host FROM mysql.user WHERE user = ? ORDER BY host", username)
	if err != nil {
		return err
	}
	noBackslashEscapes := false
	if modeRows, modeErr := db.queryStrings("SELECT @@sql_mode"); modeErr == nil && len(modeRows) > 0 && len(modeRows[0]) > 0 {
		noBackslashEscapes = strings.Contains(strings.ToUpper(modeRows[0][0]), "NO_BACKSLASH_ESCAPES")
	}
	if len(rows) == 0 {
		if !create {
			return fmt.Errorf("the MySQL account: %s does not exist", username)
		}
		rows = [][]string{{username, "%"}}
	} else {
		create = false
	}
	for _, row := range rows {
		if len(row) < 2 {
			return fmt.Errorf("invalid MySQL account entry for %s", username)
		}
		stmt, err := buildMysqlAccountPasswordSql(row[0], row[1], password, create, noBackslashEscapes)
		if err != nil {
			return err
		}
		if err = db.exec(stmt); err != nil {
			return sanitizeRotateError(err, password)
		}
	}
	return nil
}

func setPostgresqlAccountPassword(db databaseRotator, username string, password string, create bool) error {
	rows, err := db.queryStrings("SELECT 1 FROM pg_roles WHERE rolname = $1", username)
	if err != nil {
		return err
	}
	exists := len(rows) > 0
	if !exists && !create {
		return fmt.Errorf("the PostgreSQL role: %s does not exist", username)
	}
	stmt, err := buildPostgresqlAccountPasswordSql(username, password, !exists)
	if err != nil {
		return err
	}
	return sanitizeRotateError(db.exec(stmt), password)
}

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
// account, which must be root, able to sudo without a password, a Windows administrator,
// or a database administrator.
func setAccountPassword(asset *Asset, username string, password string, create bool) error {
	if err := checkRotatableAsset(asset); err != nil {
		return err
	}
	if err := validateDatabaseValue("password", password, false); err != nil {
		return err
	}
	if IsDatabaseProtocol(asset.Protocol) {
		if err := validateDatabaseValue("username", username, false); err != nil {
			return err
		}
		client, err := DialDatabase(asset)
		if err != nil {
			return err
		}
		defer client.Close()
		if asset.Protocol == ProtocolMysql {
			return setMysqlAccountPassword(client, username, password, create)
		}
		return setPostgresqlAccountPassword(client, username, password, create)
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
	if err := checkRotatableAsset(asset); err != nil {
		return err
	}
	target := *asset
	target.Username = username
	target.Password = password
	target.PrivateKey = ""
	if IsDatabaseProtocol(asset.Protocol) {
		client, err := DialDatabase(&target)
		if err != nil {
			return err
		}
		client.Close()
		return nil
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
	if err = checkRotatableAsset(asset); err != nil {
		return nil, err
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

	err = setAccountPasswordFunc(asset, account.Username, password, isPush)
	if err != nil {
		err = sanitizeRotateError(err, password)
		_ = updateAssetAccountRotation(account, "", AccountRotateFailed, truncate(err.Error(), 1000))
		return nil, err
	}

	status := AccountRotateSucceeded
	message := ""
	err = verifyAccountPasswordFunc(asset, account.Username, password)
	if err != nil {
		err = sanitizeRotateError(err, password)
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

	if status == AccountRotateFailed {
		return nil, fmt.Errorf("%s", message)
	}
	return GetMaskedAssetAccount(GetAssetAccount(id))
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
