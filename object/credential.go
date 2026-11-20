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
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/conf"
	"xorm.io/xorm"
)

const encryptedPrefix = "enc:v1:"

// keyFileMode is what getCredentialCipher does when the key file is missing.
type keyFileMode int

const (
	keyFileRequired keyFileMode = iota
	keyFileCreate
	// keyFileCreateIfUnused creates it only while the database holds no encrypted credential,
	// so that a lost key is never replaced by a new one.
	keyFileCreateIfUnused
)

// credentialColumnLimits are the bytes the credential columns hold: text for passwords and
// TOTP secrets, mediumtext for private keys.
var credentialColumnLimits = map[string]int{
	"password":    65535,
	"totp_secret": 65535,
	"private_key": 16777215,
}

var errCredentialKeyNotConfigured = errors.New("credentialKey and credentialKeyFile are not configured, set either of them to a key on persistent storage")

// credentialKeyMu serializes reading, creating and pinning the key.
var credentialKeyMu sync.Mutex

// pinnedCredentialCipher is the cipher this process has used, so that replacing the key file
// while it runs can't mix two keys in the database.
var pinnedCredentialCipher *credentialCipher

type credentialCipher struct {
	gcm    cipher.AEAD
	source string
}

// getCredentialKeyFilePath returns where the generated key is kept: credentialKeyFile, or the
// directory of a SQLite database file. It returns "" for other databases, where a file in the
// container would be lost when the container is recreated.
func getCredentialKeyFilePath() string {
	if path := strings.TrimSpace(conf.GetConfigString("credentialKeyFile")); path != "" {
		return path
	}

	driverName, dataSourceName := conf.GetConfigString("driverName"), conf.GetConfigDataSourceName()
	if adapter != nil && adapter.driverName != "" {
		driverName, dataSourceName = adapter.driverName, adapter.dataSourceName
	}
	if IsSqlite(driverName) {
		if path := getSqliteDatabaseFile(dataSourceName); path != "" {
			return filepath.Join(filepath.Dir(path), "credential.key")
		}
	}
	return ""
}

// getSqliteDatabaseFile returns the file of a SQLite data source, or "" for an in-memory database.
func getSqliteDatabaseFile(dataSourceName string) string {
	dsn, isUri := strings.CutPrefix(dataSourceName, "file:")
	if isUri {
		dsn, _, _ = strings.Cut(dsn, "#")
	}
	path, query, _ := strings.Cut(dsn, "?")
	if isUri {
		params, _ := url.ParseQuery(query)
		if slices.Contains(params["mode"], "memory") || slices.Contains(params["vfs"], "memdb") {
			return ""
		}
		switch {
		case strings.HasPrefix(path, "///"):
			path = path[2:]
		case strings.HasPrefix(path, "//localhost/"):
			path = strings.TrimPrefix(path, "//localhost")
		case path == "//" || path == "//localhost":
			return ""
		}
		if decoded, err := url.PathUnescape(path); err == nil {
			path = decoded
		}
		if strings.HasPrefix(path, "/") && filepath.VolumeName(path[1:]) != "" {
			path = path[1:]
		}
	}
	if path == ":memory:" {
		return ""
	}
	return path
}

func readCredentialKeyFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read credential key file %s: %w", path, err)
	}
	key := strings.TrimSpace(string(data))
	if key == "" {
		return "", fmt.Errorf("credential key file %s is empty", path)
	}
	return key, nil
}

// createCredentialKeyFile writes a new random key to path, or returns the key another process
// wrote there first.
func createCredentialKeyFile(path string) (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	key := hex.EncodeToString(random)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	err := publishCredentialKeyFile(path, key)
	if err != nil && !errors.Is(err, os.ErrExist) {
		return "", fmt.Errorf("failed to create credential key file %s: %w", path, err)
	}
	created := err == nil
	if err := syncDir(dir); err != nil {
		return "", fmt.Errorf("failed to sync the directory of credential key file %s: %w", path, err)
	}
	if !created {
		return readCredentialKeyFile(path)
	}

	logs.Info("generated credential key file: %s", path)
	return key, nil
}

// publishCredentialKeyFile links a complete temporary file to path, so that no reader sees a
// partly written key. Where hard links aren't supported, it creates path exclusively instead.
func publishCredentialKeyFile(path string, key string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".credential.key.*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	err = writeCredentialKeyFile(tmp, key)
	if err != nil {
		return err
	}

	err = os.Link(tmp.Name(), path)
	if err == nil || errors.Is(err, os.ErrExist) {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	err = writeCredentialKeyFile(file, key)
	if err != nil {
		_ = os.Remove(path)
	}
	return err
}

func writeCredentialKeyFile(file *os.File, key string) error {
	_, err := file.WriteString(key + "\n")
	if err == nil {
		err = file.Sync()
	}
	return errors.Join(err, file.Close())
}

// syncDir makes a new file in dir survive a crash. Windows can't sync a directory.
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}

	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, errors.ErrUnsupported) {
		// some file systems can't sync a directory either
		err = nil
	}
	return errors.Join(err, d.Close())
}

// credentialEquals compares column with a value byte for byte, since the text collations of
// MySQL ignore case and trailing spaces.
func credentialEquals(engine *xorm.Engine, column string) string {
	if strings.EqualFold(engine.DriverName(), "mysql") {
		return column + " = CAST(? AS BINARY)"
	}
	return column + " = ?"
}

// hasEncryptedCredentials tells whether the database holds any encrypted credential.
func hasEncryptedCredentials(engine *xorm.Engine) (bool, error) {
	startsEncrypted := func(column string) string {
		return credentialEquals(engine, fmt.Sprintf("SUBSTR(%s, 1, %d)", column, len(encryptedPrefix)))
	}
	keyCond := startsEncrypted("private_key")
	passwordCond := startsEncrypted("password") + " OR " + keyCond
	for _, table := range []struct {
		bean any
		cond string
		args []any
	}{
		{&Asset{}, passwordCond, []any{encryptedPrefix, encryptedPrefix}},
		{&AssetAccount{}, passwordCond, []any{encryptedPrefix, encryptedPrefix}},
		{&NetworkGateway{}, passwordCond, []any{encryptedPrefix, encryptedPrefix}},
		{&LocalUser{}, startsEncrypted("totp_secret"), []any{encryptedPrefix}},
		{&SshHostKey{}, keyCond, []any{encryptedPrefix}},
	} {
		found, err := engine.Where(table.cond, table.args...).Exist(table.bean)
		if err != nil {
			return false, fmt.Errorf("check %T for encrypted credentials: %w", table.bean, err)
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

func loadCredentialKeyFile(path string, mode keyFileMode) (string, error) {
	key, err := readCredentialKeyFile(path)
	if !errors.Is(err, os.ErrNotExist) {
		return key, err
	}

	create := mode == keyFileCreate
	if mode == keyFileCreateIfUnused {
		create = true
		if adapter != nil && adapter.engine != nil {
			found, checkErr := hasEncryptedCredentials(adapter.engine)
			if checkErr != nil {
				return "", checkErr
			}
			create = !found
		}
	}
	if !create {
		return "", fmt.Errorf("credentialKey is not configured and credential key file %s does not exist: %w", path, err)
	}
	return createCredentialKeyFile(path)
}

// getCredentialCipher returns the cipher of credentialKey, or else of the key file. The caller
// holds credentialKeyMu.
func getCredentialCipher(mode keyFileMode) (*credentialCipher, error) {
	if pinnedCredentialCipher != nil {
		return pinnedCredentialCipher, nil
	}

	key := conf.GetConfigString("credentialKey")
	source := "credentialKey"
	if key == "" {
		path := getCredentialKeyFilePath()
		if path == "" {
			return nil, errCredentialKeyNotConfigured
		}
		var err error
		key, err = loadCredentialKeyFile(path, mode)
		if err != nil {
			return nil, err
		}
		source = "credential key file " + path
	}

	sum := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &credentialCipher{gcm: gcm, source: source}, nil
}

func checkCredentialLength(column string, value string) error {
	limit := credentialColumnLimits[column]
	if limit > 0 && len(value) > limit {
		return fmt.Errorf("the %s is too long, it takes %d bytes to store and the column holds %d", column, len(value), limit)
	}
	return nil
}

// encryptCredential encrypts a credential. A value already encrypted with the current key is
// kept as it is, anything else is encrypted, even if it looks encrypted.
func encryptCredential(plaintext string) (string, error) {
	return encryptCredentialForColumn(plaintext, "")
}

// encryptCredentialForColumn is encryptCredential for a value stored in column, whose size it
// also checks.
func encryptCredentialForColumn(plaintext string, column string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	credentialKeyMu.Lock()
	defer credentialKeyMu.Unlock()
	c, err := getCredentialCipher(keyFileCreateIfUnused)
	if errors.Is(err, errCredentialKeyNotConfigured) {
		return getPlaintextCredential(plaintext, column)
	}
	if err != nil {
		return "", err
	}

	value := plaintext
	if !c.isCiphertext(value) {
		value, err = c.seal(value)
		if err != nil {
			return "", err
		}
	}
	err = checkCredentialLength(column, value)
	if err != nil {
		return "", err
	}
	pinnedCredentialCipher = c
	return value, nil
}

// getPlaintextCredential keeps a credential unencrypted when no key is configured, which is
// only allowed while the database holds no encrypted credential.
func getPlaintextCredential(plaintext string, column string) (string, error) {
	if adapter != nil && adapter.engine != nil {
		found, err := hasEncryptedCredentials(adapter.engine)
		if err != nil {
			return "", err
		}
		if found {
			return "", errCredentialKeyNotConfigured
		}
	}
	if strings.HasPrefix(plaintext, encryptedPrefix) {
		return "", fmt.Errorf("cannot store a credential with the %s prefix: %w", encryptedPrefix, errCredentialKeyNotConfigured)
	}
	err := checkCredentialLength(column, plaintext)
	if err != nil {
		return "", err
	}
	return plaintext, nil
}

func decryptCredential(value string) (string, error) {
	if !strings.HasPrefix(value, encryptedPrefix) {
		return value, nil
	}

	credentialKeyMu.Lock()
	defer credentialKeyMu.Unlock()
	c, err := getCredentialCipher(keyFileRequired)
	if err != nil {
		return "", err
	}
	plaintext, err := c.open(value)
	if err != nil {
		return "", err
	}
	pinnedCredentialCipher = c
	return plaintext, nil
}

func (c *credentialCipher) seal(plaintext string) (string, error) {
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	sealed := c.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encryptedPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

func (c *credentialCipher) open(value string) (string, error) {
	sealed, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, encryptedPrefix))
	if err != nil {
		return "", fmt.Errorf("invalid encrypted credential (using %s): %w", c.source, err)
	}
	nonceSize := c.gcm.NonceSize()
	if len(sealed) < nonceSize+c.gcm.Overhead() {
		return "", fmt.Errorf("the encrypted credential is too short (using %s)", c.source)
	}

	plaintext, err := c.gcm.Open(nil, sealed[:nonceSize], sealed[nonceSize:], nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt the credential, %s may be wrong: %w", c.source, err)
	}
	return string(plaintext), nil
}

// isCiphertext tells whether value was encrypted with this cipher.
func (c *credentialCipher) isCiphertext(value string) bool {
	if !strings.HasPrefix(value, encryptedPrefix) {
		return false
	}
	_, err := c.open(value)
	return err == nil
}

func (asset *Asset) encryptCredentials() error {
	var err error
	asset.Password, err = encryptCredentialForColumn(asset.Password, "password")
	if err != nil {
		return err
	}
	asset.PrivateKey, err = encryptCredentialForColumn(asset.PrivateKey, "private_key")
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
