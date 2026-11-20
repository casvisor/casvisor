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
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/conf"
)

const encryptedPrefix = "enc:v1:"

// credentialTables are the columns that hold credentials, with the primary key of their table.
var credentialTables = []struct {
	bean    interface{}
	keys    []string
	columns []string
}{
	{&Asset{}, []string{"owner", "name"}, []string{"password", "private_key"}},
	{&AssetAccount{}, []string{"owner", "name"}, []string{"password", "private_key"}},
	{&NetworkGateway{}, []string{"owner", "name"}, []string{"password", "private_key"}},
	{&LocalUser{}, []string{"owner", "name"}, []string{"totp_secret"}},
	{&SshHostKey{}, []string{"name"}, []string{"private_key"}},
}

// credentialKeyFromFile is the key read or generated at startup when credentialKey is not set.
var credentialKeyFromFile string

func getCredentialKey() string {
	if key := conf.GetConfigString("credentialKey"); key != "" {
		return key
	}
	return credentialKeyFromFile
}

func getCredentialCipher() (cipher.AEAD, error) {
	key := getCredentialKey()
	if key == "" {
		return nil, nil
	}

	sum := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func encryptCredential(plaintext string) (string, error) {
	if plaintext == "" || strings.HasPrefix(plaintext, encryptedPrefix) {
		return plaintext, nil
	}

	gcm, err := getCredentialCipher()
	if err != nil {
		return "", err
	}
	if gcm == nil {
		return plaintext, nil
	}

	nonce := make([]byte, gcm.NonceSize())
	_, err = rand.Read(nonce)
	if err != nil {
		return "", err
	}

	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encryptedPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

func decryptCredential(value string) (string, error) {
	if !strings.HasPrefix(value, encryptedPrefix) {
		return value, nil
	}

	gcm, err := getCredentialCipher()
	if err != nil {
		return "", err
	}
	if gcm == nil {
		return "", fmt.Errorf("the credential is encrypted but neither credentialKey nor the credential key file is available")
	}

	sealed, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, encryptedPrefix))
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize() {
		return "", fmt.Errorf("the encrypted credential is too short")
	}

	plaintext, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt the credential, the credential key may be wrong: %s", err.Error())
	}
	return string(plaintext), nil
}

func (asset *Asset) encryptCredentials() error {
	var err error
	asset.Password, err = encryptCredential(asset.Password)
	if err != nil {
		return err
	}
	asset.PrivateKey, err = encryptCredential(asset.PrivateKey)
	return err
}

func (asset *Asset) decryptCredentials() error {
	var err error
	asset.Password, err = decryptCredential(asset.Password)
	if err != nil {
		return err
	}
	asset.PrivateKey, err = decryptCredential(asset.PrivateKey)
	return err
}

// getCredentialKeyFile returns where the key is kept when credentialKey is not set: credentialKeyFile,
// or else next to a SQLite database, so that it lives in the same volume and is backed up with it.
func getCredentialKeyFile() string {
	if path := conf.GetConfigString("credentialKeyFile"); path != "" {
		return path
	}
	if adapter != nil && IsSqlite(adapter.driverName) {
		path := strings.TrimPrefix(strings.SplitN(adapter.dataSourceName, "?", 2)[0], "file:")
		if path != "" && path != ":memory:" {
			return filepath.Join(filepath.Dir(path), "credential.key")
		}
	}
	return ""
}

func hasEncryptedCredentials() (bool, error) {
	for _, table := range credentialTables {
		for _, column := range table.columns {
			exist, err := adapter.engine.Where(column+" like ?", encryptedPrefix+"%").Exist(table.bean)
			if err != nil || exist {
				return exist, err
			}
		}
	}
	return false, nil
}

// loadCredentialKeyFile reads the key file, or creates it with a random key while nothing in the
// database is encrypted yet. A lost key file is an error, never replaced by a new key.
func loadCredentialKeyFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		key := strings.TrimSpace(string(data))
		if key == "" {
			return "", fmt.Errorf("the credential key file: %s is empty", path)
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	encrypted, err := hasEncryptedCredentials()
	if err != nil {
		return "", err
	}
	if encrypted {
		return "", fmt.Errorf("the credentials in the database are encrypted but the credential key file: %s does not exist, restore it or set credentialKey", path)
	}

	random := make([]byte, 32)
	_, err = rand.Read(random)
	if err != nil {
		return "", err
	}
	key := hex.EncodeToString(random)

	err = os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		// another instance has just created it
		return loadCredentialKeyFile(path)
	}
	if err != nil {
		return "", err
	}
	_, err = file.WriteString(key + "\n")
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}

	logs.Info(fmt.Sprintf("generated the credential key file: %s, back it up together with the database", path))
	return key, nil
}

// EncryptAssetCredentials loads the credential key and encrypts the credentials left in the database
// as plaintext, e.g. the ones saved before the key was there. Without credentialKey, the key is kept
// in a file next to a SQLite database, or in credentialKeyFile.
func EncryptAssetCredentials() error {
	if conf.GetConfigString("credentialKey") == "" {
		path := getCredentialKeyFile()
		if path == "" {
			logs.Warning("credentials are stored unencrypted, set credentialKey or credentialKeyFile to encrypt them")
			return nil
		}
		key, err := loadCredentialKeyFile(path)
		if err != nil {
			return err
		}
		credentialKeyFromFile = key
	}

	for _, table := range credentialTables {
		for _, column := range table.columns {
			rows, err := adapter.engine.Table(table.bean).Cols(append(table.keys, column)...).
				Where(column+" != '' and "+column+" not like ?", encryptedPrefix+"%").QueryString()
			if err != nil {
				return err
			}

			for _, row := range rows {
				encrypted, err := encryptCredential(row[column])
				if err != nil {
					return err
				}

				// only while the row still holds what was read, so that a credential another instance
				// has just changed is not overwritten
				session := adapter.engine.Table(table.bean).Where(column+" = ?", row[column])
				for _, key := range table.keys {
					session = session.And(key+" = ?", row[key])
				}
				_, err = session.Update(map[string]interface{}{column: encrypted})
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}
