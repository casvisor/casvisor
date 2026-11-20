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
	"errors"
	"fmt"
	"strings"

	"github.com/beego/beego/logs"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
	"xorm.io/core"
	"xorm.io/xorm"
)

const maxCredentialMigrationAttempts = 5

var errCredentialMigrationConflict = errors.New("credential migration conflict")

type credentialMigrationRow struct {
	id      any
	bean    any
	columns []string
	values  []*string
}

func loadCredentialMigrationRows(session *xorm.Session) ([]credentialMigrationRow, error) {
	var assets []*Asset
	if err := session.Cols("owner", "name", "password", "private_key").Find(&assets); err != nil {
		return nil, err
	}
	var accounts []*AssetAccount
	if err := session.Cols("owner", "name", "password", "private_key").Find(&accounts); err != nil {
		return nil, err
	}
	var gateways []*NetworkGateway
	if err := session.Cols("owner", "name", "password", "private_key").Find(&gateways); err != nil {
		return nil, err
	}
	var users []*LocalUser
	if err := session.Cols("owner", "name", "totp_secret").Find(&users); err != nil {
		return nil, err
	}
	var hostKeys []*SshHostKey
	if err := session.Cols("name", "private_key").Find(&hostKeys); err != nil {
		return nil, err
	}

	rows := []credentialMigrationRow{}
	passwordColumns := []string{"password", "private_key"}
	for _, asset := range assets {
		rows = append(rows, credentialMigrationRow{core.PK{asset.Owner, asset.Name}, asset, passwordColumns, []*string{&asset.Password, &asset.PrivateKey}})
	}
	for _, account := range accounts {
		rows = append(rows, credentialMigrationRow{core.PK{account.Owner, account.Name}, account, passwordColumns, []*string{&account.Password, &account.PrivateKey}})
	}
	for _, gateway := range gateways {
		rows = append(rows, credentialMigrationRow{core.PK{gateway.Owner, gateway.Name}, gateway, passwordColumns, []*string{&gateway.Password, &gateway.PrivateKey}})
	}
	for _, user := range users {
		rows = append(rows, credentialMigrationRow{core.PK{user.Owner, user.Name}, user, []string{"totp_secret"}, []*string{&user.TotpSecret}})
	}
	for _, hostKey := range hostKeys {
		rows = append(rows, credentialMigrationRow{hostKey.Name, hostKey, []string{"private_key"}, []*string{&hostKey.PrivateKey}})
	}
	return rows, nil
}

// EncryptAssetCredentials encrypts the plaintext credentials left in the database, e.g. the ones
// saved before encryption was on. The adapter has already widened the credential columns to text
// when it synced the tables. Every encrypted credential is checked against the key before anything
// is written, and a credential is only updated while it still holds the value read, so that a
// password rotated by another instance meanwhile is not overwritten.
func EncryptAssetCredentials() error {
	return encryptAssetCredentials(nil)
}

// encryptAssetCredentials calls onConflict, if set, after each attempt rolled back by a conflict.
func encryptAssetCredentials(onConflict func(error)) error {
	if adapter == nil || adapter.engine == nil {
		return fmt.Errorf("the database adapter is not initialized")
	}

	credentialKeyMu.Lock()
	defer credentialKeyMu.Unlock()

	var err error
	for range maxCredentialMigrationAttempts {
		err = migrateCredentials(adapter.engine)
		if !isCredentialMigrationConflict(err) {
			return err
		}
		if onConflict != nil {
			onConflict(err)
		}
	}
	return fmt.Errorf("the credential migration failed after %d attempts: %w", maxCredentialMigrationAttempts, err)
}

// isCredentialMigrationConflict tells whether another writer got in the way of the migration,
// including SQLITE_BUSY_SNAPSHOT when another connection committed after the rows were read.
func isCredentialMigrationConflict(err error) bool {
	if errors.Is(err, errCredentialMigrationConflict) {
		return true
	}
	var sqliteErr *sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code()&0xff == sqlite3.SQLITE_BUSY
}

func migrateCredentials(engine *xorm.Engine) error {
	session := engine.NewSession()
	defer session.Close()
	err := session.Begin()
	if err != nil {
		return err
	}
	defer session.Rollback()

	rows, err := loadCredentialMigrationRows(session)
	if err != nil {
		return err
	}
	c, err := getMigrationCipher(rows)
	if err != nil || c == nil {
		return err
	}

	for _, row := range rows {
		cols := []string{}
		conds := []string{}
		args := []any{}
		for i, value := range row.values {
			if *value == "" || strings.HasPrefix(*value, encryptedPrefix) {
				continue
			}
			encrypted, err := c.seal(*value)
			if err != nil {
				return err
			}
			err = checkCredentialLength(row.columns[i], encrypted)
			if err != nil {
				return fmt.Errorf("%T %v: %w", row.bean, row.id, err)
			}
			cols = append(cols, row.columns[i])
			conds = append(conds, credentialEquals(engine, row.columns[i]))
			args = append(args, *value)
			*value = encrypted
		}
		if len(cols) == 0 {
			continue
		}

		affected, err := session.ID(row.id).Where(strings.Join(conds, " AND "), args...).Cols(cols...).Update(row.bean)
		if err != nil {
			return err
		}
		if affected != 1 {
			return fmt.Errorf("%w: update %T %v affected %d rows", errCredentialMigrationConflict, row.bean, row.id, affected)
		}
	}

	err = session.Commit()
	if err != nil {
		return err
	}
	pinnedCredentialCipher = c
	return nil
}

// getMigrationCipher checks every encrypted credential against the key. It returns nil when no
// key is configured and nothing is encrypted yet, which leaves the database as it is.
func getMigrationCipher(rows []credentialMigrationRow) (*credentialCipher, error) {
	var c *credentialCipher
	for _, row := range rows {
		for i, value := range row.values {
			if !strings.HasPrefix(*value, encryptedPrefix) {
				continue
			}
			if c == nil {
				var err error
				c, err = getCredentialCipher(keyFileRequired)
				if err != nil {
					return nil, err
				}
			}
			_, err := c.open(*value)
			if err != nil {
				return nil, fmt.Errorf("the %s of %T %v can't be decrypted: %w", row.columns[i], row.bean, row.id, err)
			}
		}
	}
	if c != nil {
		return c, nil
	}

	// nothing is encrypted yet, as the transaction has just read
	c, err := getCredentialCipher(keyFileCreate)
	if errors.Is(err, errCredentialKeyNotConfigured) {
		logs.Warning("credentials are stored unencrypted, set credentialKey or credentialKeyFile to a key on persistent storage to encrypt them")
		return nil, nil
	}
	return c, err
}
