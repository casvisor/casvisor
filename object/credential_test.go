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
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh"
	"xorm.io/core"
	"xorm.io/xorm"
)

const (
	credentialTestOwner            = "credential-test"
	credentialTestPassword         = "test-local-password"
	credentialTestClientIP         = "192.0.2.10"
	credentialTestTextPlaintextMax = 49118
)

func checkCredentialTest(t *testing.T, ok bool, format string, args ...any) {
	t.Helper()
	if !ok {
		t.Fatalf(format, args...)
	}
}

func checkCredentialTestError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func checkCredentialTestEqual[T comparable](t *testing.T, want, got T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func checkCredentialTestErrorContains(t *testing.T, err error, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if err == nil || !strings.Contains(err.Error(), fragment) {
			t.Fatalf("got error %v, want it to include %q", err, fragment)
		}
	}
}

// resetCredentialCipher forgets the pinned cipher, as a restart does.
func resetCredentialCipher() {
	credentialKeyMu.Lock()
	defer credentialKeyMu.Unlock()
	pinnedCredentialCipher = nil
}

func setupCredentialTestCipher(t *testing.T) {
	t.Helper()
	resetCredentialCipher()
	t.Cleanup(resetCredentialCipher)
}

func setupCredentialTestDB(t *testing.T, legacyDDL ...string) string {
	t.Helper()
	setupCredentialTestCipher(t)
	path := filepath.Join(t.TempDir(), "keys", "credential.key")
	t.Setenv("credentialKey", "")
	t.Setenv("credentialKeyFile", path)

	engine, err := xorm.NewEngine("sqlite", ":memory:")
	checkCredentialTestError(t, err)
	engine.SetMaxOpenConns(1)
	engine.SetMaxIdleConns(1)
	previous := adapter
	a := &Adapter{driverName: "sqlite", dataSourceName: ":memory:", engine: engine}
	adapter = a
	t.Cleanup(func() {
		adapter = previous
		if err := engine.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	for _, statement := range legacyDDL {
		_, err := engine.Exec(statement)
		checkCredentialTestError(t, err)
	}
	a.createTable()
	return path
}

func readCredentialTestRow[T any](t *testing.T, id any) *T {
	t.Helper()
	row := new(T)
	found, err := adapter.engine.ID(id).Get(row)
	checkCredentialTestError(t, err)
	checkCredentialTest(t, found, "%T %v not found", row, id)
	return row
}

func requireStoredCredential(t *testing.T, stored, want string) {
	t.Helper()
	if want == "" {
		checkCredentialTestEqual(t, "", stored)
		return
	}
	checkCredentialTest(t, strings.HasPrefix(stored, encryptedPrefix), "credential was not encrypted")
	plaintext, err := decryptCredential(stored)
	checkCredentialTestError(t, err)
	checkCredentialTest(t, plaintext == want, "decrypted credential did not match the input")
}

func TestCredentialKeyFilePath(t *testing.T) {
	configuredDB := filepath.Join("configured", "casvisor.db")
	configuredKey := filepath.Join("configured", "credential.key")
	fileDB := filepath.Join("db", "casvisor.db")
	fileKey := filepath.Join("db", "credential.key")
	override := filepath.Join(t.TempDir(), "operator.key")
	uriDir := t.TempDir()
	uriPath := filepath.ToSlash(filepath.Join(uriDir, "casvisor.db"))
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	fileURI := (&url.URL{Scheme: "file", Host: "localhost", Path: uriPath}).String()
	t.Setenv("driverName", "sqlite")
	t.Setenv("dataSourceName", configuredDB)
	t.Setenv("RUNNING_IN_DOCKER", "")

	tests := []struct {
		name     string
		active   *Adapter
		override string
		want     string
	}{
		{"override", &Adapter{driverName: "sqlite", dataSourceName: fileDB}, " \t" + override + " \n", override},
		{"file-backed SQLite", &Adapter{driverName: "sqlite", dataSourceName: fileDB}, "", fileKey},
		{"non-URI memory mode", &Adapter{driverName: "sqlite", dataSourceName: fileDB + "?mode=memory"}, "", fileKey},
		{"file URI", &Adapter{driverName: "sqlite3", dataSourceName: "file:db/casvisor.db?mode=rwc&cache=shared"}, "", fileKey},
		{"absolute local URI", &Adapter{driverName: "sqlite", dataSourceName: fileURI}, "", filepath.Join(uriDir, "credential.key")},
		{"escaped file URI", &Adapter{driverName: "sqlite", dataSourceName: "file:db%20dir/casvisor.db"}, "", filepath.Join("db dir", "credential.key")},
		{"configured SQLite", nil, "", configuredKey},
		{"adapter without driver", &Adapter{dataSourceName: fileDB}, "", configuredKey},
		{"empty DSN", &Adapter{driverName: "sqlite"}, "", ""},
		{"in-memory SQLite", &Adapter{driverName: "sqlite", dataSourceName: ":memory:"}, "", ""},
		{"in-memory URI", &Adapter{driverName: "sqlite", dataSourceName: "file::memory:?cache=shared"}, "", ""},
		{"named shared memory", &Adapter{driverName: "sqlite", dataSourceName: "file:db/shared?cache=shared&mode=memory#ignored"}, "", ""},
		{"memdb VFS URI", &Adapter{driverName: "sqlite", dataSourceName: "file:/memdb1?vfs=memdb"}, "", ""},
		{"MySQL", &Adapter{driverName: "mysql", dataSourceName: "user@tcp(localhost:3306)/casvisor"}, " \t", ""},
		{"MySQL with override", &Adapter{driverName: "mysql"}, override, override},
		{"PostgreSQL", &Adapter{driverName: "postgres"}, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := adapter
			adapter = tt.active
			t.Cleanup(func() { adapter = previous })
			t.Setenv("credentialKeyFile", tt.override)
			checkCredentialTestEqual(t, tt.want, getCredentialKeyFilePath())
		})
	}
}

func TestCredentialKeyFilePersistence(t *testing.T) {
	setupCredentialTestCipher(t)
	dir := filepath.Join(t.TempDir(), "db")
	database := filepath.Join(dir, "casvisor.db")
	path := filepath.Join(dir, "credential.key")
	t.Setenv("credentialKey", "")
	t.Setenv("credentialKeyFile", "")
	previous := adapter
	t.Cleanup(func() { adapter = previous })
	adapter = &Adapter{driverName: "sqlite", dataSourceName: database}

	sealed, err := encryptCredential("first credential")
	checkCredentialTestError(t, err)
	saved, err := os.ReadFile(path)
	checkCredentialTestError(t, err)
	key := strings.TrimSpace(string(saved))
	decoded, err := hex.DecodeString(key)
	if err != nil || len(decoded) != 32 || string(saved) != key+"\n" {
		t.Fatal("expected a newline-terminated, 32-byte hex key")
	}
	stamp := time.Unix(1600000000, 0)
	checkCredentialTestError(t, os.Chtimes(path, stamp, stamp))
	before, err := os.Stat(path)
	checkCredentialTestError(t, err)
	if runtime.GOOS != "windows" {
		checkCredentialTestEqual(t, os.FileMode(0o600), before.Mode().Perm())
	}

	adapter = &Adapter{driverName: "sqlite", dataSourceName: database}
	resetCredentialCipher()
	requireStoredCredential(t, sealed, "first credential")
	second, err := encryptCredential("second credential")
	checkCredentialTestError(t, err)
	requireStoredCredential(t, second, "second credential")
	existingKey, err := createCredentialKeyFile(path)
	checkCredentialTestError(t, err)
	checkCredentialTest(t, existingKey == key, "creation did not reuse the existing key")
	after, err := os.Stat(path)
	checkCredentialTestError(t, err)
	latest, err := os.ReadFile(path)
	checkCredentialTestError(t, err)
	checkCredentialTest(t, os.SameFile(before, after) && before.ModTime().Equal(after.ModTime()) && bytes.Equal(saved, latest), "key file was rewritten")
	files, err := os.ReadDir(dir)
	checkCredentialTestError(t, err)
	checkCredentialTestEqual(t, 1, len(files))

	override := filepath.Join(dir, "override.key")
	t.Setenv("credentialKeyFile", override)
	resetCredentialCipher()
	plain, err := decryptCredential(sealed)
	checkCredentialTestErrorContains(t, err, "credential key file "+override, "does not exist")
	checkCredentialTestEqual(t, "", plain)
	_, err = os.Stat(override)
	checkCredentialTest(t, errors.Is(err, os.ErrNotExist), "expected missing key file, got %v", err)
	checkCredentialTestError(t, os.WriteFile(override, []byte(" \t"+key+"\n"), 0o600))
	requireStoredCredential(t, sealed, "first credential")

	t.Setenv("credentialKey", key)
	t.Setenv("credentialKeyFile", filepath.Join(dir, "missing.key"))
	resetCredentialCipher()
	requireStoredCredential(t, sealed, "first credential")
}

func TestExplicitCredentialKeyCompatibility(t *testing.T) {
	setupCredentialTestCipher(t)
	const key = " legacy key, including whitespace \n"
	path := filepath.Join(t.TempDir(), "credential.key")
	t.Setenv("credentialKey", key)
	t.Setenv("credentialKeyFile", path)

	digest := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(digest[:])
	checkCredentialTestError(t, err)
	legacyCipher, err := cipher.NewGCM(block)
	checkCredentialTestError(t, err)
	nonce := make([]byte, legacyCipher.NonceSize())
	legacy := "enc:v1:" + base64.StdEncoding.EncodeToString(legacyCipher.Seal(nonce, nonce, []byte("legacy credential"), nil))
	requireStoredCredential(t, legacy, "legacy credential")

	value, err := encryptCredential("new credential")
	checkCredentialTestError(t, err)
	checkCredentialTest(t, strings.HasPrefix(value, "enc:v1:"), "credential was not encrypted")
	sealed, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "enc:v1:"))
	checkCredentialTestError(t, err)
	nonceSize := legacyCipher.NonceSize()
	checkCredentialTest(t, len(sealed) >= nonceSize+legacyCipher.Overhead(), "ciphertext is too short")
	plain, err := legacyCipher.Open(nil, sealed[:nonceSize], sealed[nonceSize:], nil)
	checkCredentialTestError(t, err)
	checkCredentialTestEqual(t, "new credential", string(plain))
	_, err = os.Stat(path)
	checkCredentialTest(t, errors.Is(err, os.ErrNotExist), "expected missing key file, got %v", err)

	checkCredentialTestError(t, os.WriteFile(path, []byte(" \n"), 0o600))
	requireStoredCredential(t, legacy, "legacy credential")
	_, err = encryptCredential("credential with an unused key file")
	checkCredentialTestError(t, err)
	t.Setenv("credentialKey", "different explicit key")
	requireStoredCredential(t, value, "new credential")
}

func TestCredentialKeyFileReadError(t *testing.T) {
	setupCredentialTestCipher(t)
	path := t.TempDir()
	t.Setenv("credentialKey", "")
	t.Setenv("credentialKeyFile", path)

	value, err := encryptCredential("new credential")
	checkCredentialTestErrorContains(t, err, "failed to read credential key file", path)
	checkCredentialTestEqual(t, "", value)
	value, err = decryptCredential(encryptedPrefix + "invalid")
	checkCredentialTestErrorContains(t, err, "failed to read credential key file", path)
	checkCredentialTestEqual(t, "", value)
}

func TestAddAssetEncryptsPrefixedPlaintext(t *testing.T) {
	setupCredentialTestDB(t)
	const literal = "enc:v1:!"
	asset := &Asset{Owner: credentialTestOwner, Name: "prefixed-password", Password: literal}
	ok, err := AddAsset(asset)
	checkCredentialTestError(t, err)
	checkCredentialTest(t, ok, "write did not affect any rows")

	id := core.PK{asset.Owner, asset.Name}
	stored := readCredentialTestRow[Asset](t, id).Password
	checkCredentialTest(t, stored != literal, "prefixed plaintext was stored unencrypted")
	requireStoredCredential(t, stored, literal)
	plain, err := GetAsset(asset.GetId())
	checkCredentialTestError(t, err)
	checkCredentialTest(t, plain != nil, "asset not found")
	checkCredentialTestEqual(t, literal, plain.Password)

	retained, err := encryptCredential(stored)
	checkCredentialTestError(t, err)
	checkCredentialTestEqual(t, stored, retained)
	checkCredentialTestError(t, EncryptAssetCredentials())
	checkCredentialTestEqual(t, stored, readCredentialTestRow[Asset](t, id).Password)
}

func TestAssetCredentialRoundTrip(t *testing.T) {
	setupCredentialTestDB(t)
	asset := &Asset{
		Owner:      credentialTestOwner,
		Name:       "target",
		Protocol:   "SSH",
		Username:   "root",
		Password:   "asset-password",
		PrivateKey: "asset-private-key",
	}
	ok, err := AddAsset(asset)
	checkCredentialTestError(t, err)
	checkCredentialTest(t, ok, "write did not affect any rows")
	assetPK := core.PK{asset.Owner, asset.Name}
	rawAsset := readCredentialTestRow[Asset](t, assetPK)
	requireStoredCredential(t, rawAsset.Password, "asset-password")
	requireStoredCredential(t, rawAsset.PrivateKey, "asset-private-key")
	maskedAsset, err := GetMaskedAsset(GetAsset(asset.GetId()))
	checkCredentialTestError(t, err)
	checkCredentialTest(t, maskedAsset != nil, "masked asset not found")
	checkCredentialTestEqual(t, "***", maskedAsset.Password)
	checkCredentialTestEqual(t, "***", maskedAsset.PrivateKey)
	assets, err := GetMaskedAssets(GetAssets(asset.Owner))
	checkCredentialTestError(t, err)
	checkCredentialTestEqual(t, 1, len(assets))
	checkCredentialTestEqual(t, "***", assets[0].Password)
	checkCredentialTestEqual(t, "***", assets[0].PrivateKey)

	maskedAsset.PrivateKey = "new-asset-private-key"
	ok, err = UpdateAsset(asset.GetId(), maskedAsset)
	checkCredentialTestError(t, err)
	checkCredentialTest(t, ok, "write did not affect any rows")
	rawAsset = readCredentialTestRow[Asset](t, assetPK)
	requireStoredCredential(t, rawAsset.Password, "asset-password")
	requireStoredCredential(t, rawAsset.PrivateKey, "new-asset-private-key")
	plainAsset, err := GetAsset(asset.GetId())
	checkCredentialTestError(t, err)
	checkCredentialTest(t, plainAsset != nil, "asset not found")
	checkCredentialTestEqual(t, "asset-password", plainAsset.Password)
	checkCredentialTestEqual(t, "new-asset-private-key", plainAsset.PrivateKey)

	account := &AssetAccount{
		Owner:      asset.Owner,
		Name:       "account",
		Asset:      asset.Name,
		Username:   "operator",
		Password:   "account-password",
		PrivateKey: "account-private-key",
		IsEnabled:  true,
	}
	ok, err = AddAssetAccount(account)
	checkCredentialTestError(t, err)
	checkCredentialTest(t, ok, "write did not affect any rows")
	accountPK := core.PK{account.Owner, account.Name}
	rawAccount := readCredentialTestRow[AssetAccount](t, accountPK)
	requireStoredCredential(t, rawAccount.Password, "account-password")
	requireStoredCredential(t, rawAccount.PrivateKey, "account-private-key")
	maskedAccount, err := GetMaskedAssetAccount(GetAssetAccount(account.GetId()))
	checkCredentialTestError(t, err)
	checkCredentialTest(t, maskedAccount != nil, "masked asset account not found")
	checkCredentialTestEqual(t, "***", maskedAccount.Password)
	checkCredentialTestEqual(t, "***", maskedAccount.PrivateKey)
	accounts, err := GetMaskedAssetAccounts(GetAssetAccounts(asset.Owner, asset.Name))
	checkCredentialTestError(t, err)
	checkCredentialTestEqual(t, 1, len(accounts))
	checkCredentialTestEqual(t, "***", accounts[0].Password)
	checkCredentialTestEqual(t, "***", accounts[0].PrivateKey)

	maskedAccount.Password = "new-account-password"
	ok, err = UpdateAssetAccount(account.GetId(), maskedAccount)
	checkCredentialTestError(t, err)
	checkCredentialTest(t, ok, "write did not affect any rows")
	rawAccount = readCredentialTestRow[AssetAccount](t, accountPK)
	requireStoredCredential(t, rawAccount.Password, "new-account-password")
	requireStoredCredential(t, rawAccount.PrivateKey, "account-private-key")
	checkCredentialTestError(t, updateAssetAccountRotation(account, "rotated-password", AccountRotateSucceeded, ""))
	rotated := readCredentialTestRow[AssetAccount](t, accountPK)
	requireStoredCredential(t, rotated.Password, "rotated-password")
	checkCredentialTestEqual(t, rawAccount.PrivateKey, rotated.PrivateKey)
	checkCredentialTestEqual(t, AccountRotateSucceeded, rotated.RotateStatus)
	checkCredentialTest(t, rotated.LastRotatedTime != "", "rotation timestamp was not saved")
	plainAccount, err := GetAssetAccount(account.GetId())
	checkCredentialTestError(t, err)
	checkCredentialTest(t, plainAccount != nil, "asset account not found")
	checkCredentialTestEqual(t, "rotated-password", plainAccount.Password)
	checkCredentialTestEqual(t, "account-private-key", plainAccount.PrivateKey)
	withAccount, err := plainAsset.WithAccount(account.Username)
	checkCredentialTestError(t, err)
	checkCredentialTest(t, withAccount != nil, "asset with account not returned")
	checkCredentialTestEqual(t, account.Username, withAccount.Username)
	checkCredentialTestEqual(t, plainAccount.Password, withAccount.Password)
	checkCredentialTestEqual(t, plainAccount.PrivateKey, withAccount.PrivateKey)
	checkCredentialTestEqual(t, "root", plainAsset.Username)

	gateway := &NetworkGateway{
		Owner:      asset.Owner,
		Name:       "gateway",
		Host:       "gateway.example.test",
		Port:       22,
		Username:   "jump",
		Password:   "gateway-password",
		PrivateKey: "gateway-private-key",
	}
	ok, err = AddNetworkGateway(gateway)
	checkCredentialTestError(t, err)
	checkCredentialTest(t, ok, "write did not affect any rows")
	gatewayPK := core.PK{gateway.Owner, gateway.Name}
	rawGateway := readCredentialTestRow[NetworkGateway](t, gatewayPK)
	requireStoredCredential(t, rawGateway.Password, "gateway-password")
	requireStoredCredential(t, rawGateway.PrivateKey, "gateway-private-key")
	maskedGateway, err := GetMaskedNetworkGateway(GetNetworkGateway(gateway.GetId()))
	checkCredentialTestError(t, err)
	checkCredentialTest(t, maskedGateway != nil, "masked gateway not found")
	checkCredentialTestEqual(t, "***", maskedGateway.Password)
	checkCredentialTestEqual(t, "***", maskedGateway.PrivateKey)
	gateways, err := GetMaskedNetworkGateways(GetNetworkGateways(gateway.Owner))
	checkCredentialTestError(t, err)
	checkCredentialTestEqual(t, 1, len(gateways))
	checkCredentialTestEqual(t, "***", gateways[0].Password)
	checkCredentialTestEqual(t, "***", gateways[0].PrivateKey)

	maskedGateway.PrivateKey = "new-gateway-private-key"
	ok, err = UpdateNetworkGateway(gateway.GetId(), maskedGateway)
	checkCredentialTestError(t, err)
	checkCredentialTest(t, ok, "write did not affect any rows")
	rawGateway = readCredentialTestRow[NetworkGateway](t, gatewayPK)
	requireStoredCredential(t, rawGateway.Password, "gateway-password")
	requireStoredCredential(t, rawGateway.PrivateKey, "new-gateway-private-key")
	plainGateway, err := GetNetworkGateway(gateway.GetId())
	checkCredentialTestError(t, err)
	checkCredentialTest(t, plainGateway != nil, "gateway not found")
	checkCredentialTestEqual(t, "gateway-password", plainGateway.Password)
	checkCredentialTestEqual(t, "new-gateway-private-key", plainGateway.PrivateKey)
}

func uniqueCredentialTestTotpKey(t *testing.T) ([]byte, int64) {
	t.Helper()
	key := make([]byte, 20)
	for {
		_, err := rand.Read(key)
		checkCredentialTestError(t, err)
		now := time.Now().Unix() / totpPeriod
		// Avoid code collisions in the acceptance windows on either side of a step boundary.
		codes := map[string]struct{}{}
		for step := now - 1; step <= now+2; step++ {
			codes[getTotpCode(key, step)] = struct{}{}
		}
		if len(codes) == 4 {
			return key, now
		}
	}
}

func TestLocalUserMfaCredentialRoundTrip(t *testing.T) {
	setupCredentialTestDB(t)
	user := &LocalUser{Owner: credentialTestOwner, Name: "mfa-user", Password: credentialTestPassword}
	t.Cleanup(func() { clearSigninFailures(user.GetId()) })
	ok, err := AddLocalUser(user)
	checkCredentialTestError(t, err)
	checkCredentialTest(t, ok, "write did not affect any rows")

	key, now := uniqueCredentialTestTotpKey(t)
	secret := totpEncoding.EncodeToString(key)
	checkCredentialTestError(t, EnableLocalUserMfa(user.Owner, user.Name, secret, getTotpCode(key, now)))
	stored, err := GetLocalUser(user.GetId())
	checkCredentialTestError(t, err)
	checkCredentialTest(t, stored != nil, "local user not found")
	checkCredentialTest(t, stored.MfaEnabled, "MFA was not enabled")
	checkCredentialTestEqual(t, now, stored.TotpLastStep)
	requireStoredCredential(t, stored.TotpSecret, secret)

	signedIn, err := SigninLocalUser(user.Owner, user.Name, credentialTestPassword, "xxxxxx", credentialTestClientIP)
	checkCredentialTestErrorContains(t, err, "verification code is wrong")
	checkCredentialTest(t, signedIn == nil, "sign-in returned a user on failure")
	code := getTotpCode(key, now+1)
	signedIn, err = SigninLocalUser(user.Owner, user.Name, credentialTestPassword, code, credentialTestClientIP)
	checkCredentialTestError(t, err)
	checkCredentialTest(t, signedIn != nil, "sign-in did not return a user")
	checkCredentialTestEqual(t, now+1, signedIn.TotpLastStep)
	signedIn, err = SigninLocalUser(user.Owner, user.Name, credentialTestPassword, code, credentialTestClientIP)
	checkCredentialTestErrorContains(t, err, "verification code is wrong or has been used")
	checkCredentialTest(t, signedIn == nil, "sign-in returned a user on failure")

	encoded, err := json.Marshal(stored)
	checkCredentialTestError(t, err)
	checkCredentialTest(t, !strings.Contains(strings.ToLower(string(encoded)), "totpsecret"), "TOTP secret field leaked in JSON")
	checkCredentialTest(t, !strings.Contains(string(encoded), secret), "plaintext TOTP secret leaked in JSON")
	checkCredentialTest(t, !strings.Contains(string(encoded), stored.TotpSecret), "encrypted TOTP secret leaked in JSON")

	preconfigured := &LocalUser{
		Owner:      credentialTestOwner,
		Name:       "preconfigured-mfa",
		Password:   credentialTestPassword,
		MfaEnabled: true,
		TotpSecret: secret,
	}
	ok, err = AddLocalUser(preconfigured)
	checkCredentialTestError(t, err)
	checkCredentialTest(t, ok, "write did not affect any rows")
	rawUser := readCredentialTestRow[LocalUser](t, core.PK{preconfigured.Owner, preconfigured.Name})
	requireStoredCredential(t, rawUser.TotpSecret, secret)
}

func TestSshHostKeyCredentialRoundTrip(t *testing.T) {
	setupCredentialTestDB(t)
	signer, err := GetSshHostKeySigner()
	checkCredentialTestError(t, err)
	raw := readCredentialTestRow[SshHostKey](t, sshHostKeyName)
	checkCredentialTest(t, strings.HasPrefix(raw.PrivateKey, encryptedPrefix), "host key was not encrypted")
	loaded, err := GetSshHostKeySigner()
	checkCredentialTestError(t, err)
	checkCredentialTestEqual(t, ssh.FingerprintSHA256(signer.PublicKey()), ssh.FingerprintSHA256(loaded.PublicKey()))
	checkCredentialTestEqual(t, raw.PrivateKey, readCredentialTestRow[SshHostKey](t, sshHostKeyName).PrivateKey)
}

type credentialTestRows struct {
	asset   Asset
	account AssetAccount
	gateway NetworkGateway
	user    LocalUser
	hostKey SshHostKey
}

func newLegacyCredentialTestRows(t *testing.T) credentialTestRows {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	checkCredentialTestError(t, err)
	block, err := ssh.MarshalPrivateKey(privateKey, "test host key")
	checkCredentialTestError(t, err)
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(credentialTestPassword), bcrypt.MinCost)
	checkCredentialTestError(t, err)

	return credentialTestRows{
		asset: Asset{
			Owner:      credentialTestOwner,
			Name:       "legacy-asset",
			Password:   "legacy asset password",
			PrivateKey: "legacy asset private key",
		},
		account: AssetAccount{
			Owner:      credentialTestOwner,
			Name:       "legacy-account",
			Asset:      "legacy-asset",
			Username:   "operator",
			Password:   "legacy account password",
			PrivateKey: "legacy account private key",
		},
		gateway: NetworkGateway{
			Owner:      credentialTestOwner,
			Name:       "legacy-gateway",
			Password:   "legacy gateway password",
			PrivateKey: "legacy gateway private key",
		},
		user: LocalUser{
			Owner:        credentialTestOwner,
			Name:         "legacy-user",
			PasswordHash: string(passwordHash),
			MfaEnabled:   true,
			TotpSecret:   totpEncoding.EncodeToString([]byte("twenty-byte-totp-key")),
		},
		hostKey: SshHostKey{Name: sshHostKeyName, PrivateKey: string(pem.EncodeToMemory(block))},
	}
}

func (rows *credentialTestRows) insert(t *testing.T) {
	t.Helper()
	for _, bean := range []any{&rows.asset, &rows.account, &rows.gateway, &rows.user, &rows.hostKey} {
		affected, err := adapter.engine.Insert(bean)
		checkCredentialTest(t, err == nil && affected == 1, "insert %T affected %d rows: %v", bean, affected, err)
	}
}

func (rows credentialTestRows) read(t *testing.T) credentialTestRows {
	t.Helper()
	return credentialTestRows{
		asset:   *readCredentialTestRow[Asset](t, core.PK{rows.asset.Owner, rows.asset.Name}),
		account: *readCredentialTestRow[AssetAccount](t, core.PK{rows.account.Owner, rows.account.Name}),
		gateway: *readCredentialTestRow[NetworkGateway](t, core.PK{rows.gateway.Owner, rows.gateway.Name}),
		user:    *readCredentialTestRow[LocalUser](t, core.PK{rows.user.Owner, rows.user.Name}),
		hostKey: *readCredentialTestRow[SshHostKey](t, rows.hostKey.Name),
	}
}

func (rows credentialTestRows) values() [8]string {
	return [8]string{
		rows.asset.Password,
		rows.asset.PrivateKey,
		rows.account.Password,
		rows.account.PrivateKey,
		rows.gateway.Password,
		rows.gateway.PrivateKey,
		rows.user.TotpSecret,
		rows.hostKey.PrivateKey,
	}
}

func (rows credentialTestRows) checkPublicReads(t *testing.T) {
	t.Helper()
	asset, err := GetAsset(rows.asset.GetId())
	checkCredentialTestError(t, err)
	checkCredentialTest(t, asset != nil, "expected non-nil asset")
	checkCredentialTestEqual(t, rows.asset.Password, asset.Password)
	checkCredentialTestEqual(t, rows.asset.PrivateKey, asset.PrivateKey)
	account, err := GetAssetAccount(rows.account.GetId())
	checkCredentialTestError(t, err)
	checkCredentialTest(t, account != nil, "expected non-nil account")
	checkCredentialTestEqual(t, rows.account.Password, account.Password)
	checkCredentialTestEqual(t, rows.account.PrivateKey, account.PrivateKey)
	gateway, err := GetNetworkGateway(rows.gateway.GetId())
	checkCredentialTestError(t, err)
	checkCredentialTest(t, gateway != nil, "expected non-nil gateway")
	checkCredentialTestEqual(t, rows.gateway.Password, gateway.Password)
	checkCredentialTestEqual(t, rows.gateway.PrivateKey, gateway.PrivateKey)

	t.Cleanup(func() { clearSigninFailures(rows.user.GetId()) })
	key, err := totpEncoding.DecodeString(rows.user.TotpSecret)
	checkCredentialTestError(t, err)
	code := getTotpCode(key, time.Now().Unix()/totpPeriod)
	user, err := SigninLocalUser(rows.user.Owner, rows.user.Name, credentialTestPassword, code, credentialTestClientIP)
	checkCredentialTestError(t, err)
	checkCredentialTest(t, user != nil, "expected non-nil user")
	checkCredentialTestEqual(t, rows.user.GetId(), user.GetId())

	originalSigner, err := ssh.ParsePrivateKey([]byte(rows.hostKey.PrivateKey))
	checkCredentialTestError(t, err)
	migratedSigner, err := GetSshHostKeySigner()
	checkCredentialTestError(t, err)
	checkCredentialTestEqual(t, ssh.FingerprintSHA256(originalSigner.PublicKey()), ssh.FingerprintSHA256(migratedSigner.PublicKey()))
}

func TestEncryptAssetCredentials(t *testing.T) {
	tests := []struct {
		name  string
		mixed bool
	}{
		{"legacy plaintext", false},
		{"partially encrypted", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := setupCredentialTestDB(t)
			legacy := newLegacyCredentialTestRows(t)
			seed := legacy
			if tt.mixed {
				for _, field := range []*string{&seed.asset.Password, &seed.account.PrivateKey, &seed.gateway.Password, &seed.user.TotpSecret, &seed.hostKey.PrivateKey} {
					sealed, err := encryptCredential(*field)
					checkCredentialTestError(t, err)
					*field = sealed
				}
			} else {
				_, err := os.Stat(path)
				checkCredentialTest(t, errors.Is(err, os.ErrNotExist), "expected missing key file, got %v", err)
			}
			seed.insert(t)
			resetCredentialCipher()
			if tt.mixed {
				// Encrypting the private key must not include the existing ciphertext in the update.
				_, err := adapter.engine.Exec(`CREATE TRIGGER reject_asset_password_update
					BEFORE UPDATE OF password ON asset
					BEGIN
						SELECT RAISE(ABORT, 'unchanged asset password was updated');
					END`)
				checkCredentialTestError(t, err)
			}
			empty := credentialTestRows{
				asset:   Asset{Owner: "empty", Name: legacy.asset.Name},
				account: AssetAccount{Owner: "empty", Name: legacy.account.Name},
				gateway: NetworkGateway{Owner: "empty", Name: legacy.gateway.Name},
				user:    LocalUser{Owner: "empty", Name: legacy.user.Name},
				hostKey: SshHostKey{Name: "empty"},
			}
			empty.insert(t)

			checkCredentialTestError(t, EncryptAssetCredentials())
			_, err := os.Stat(path)
			checkCredentialTestError(t, err)
			migrated := seed.read(t).values()
			before := seed.values()
			original := legacy.values()
			for i, value := range migrated {
				requireStoredCredential(t, value, original[i])
				if strings.HasPrefix(before[i], encryptedPrefix) {
					checkCredentialTest(t, before[i] == value, "existing ciphertext %d changed", i)
				}
			}
			checkCredentialTestEqual(t, empty.values(), empty.read(t).values())
			legacy.checkPublicReads(t)

			checkCredentialTestError(t, EncryptAssetCredentials())
			checkCredentialTestEqual(t, migrated, seed.read(t).values())
			checkCredentialTestEqual(t, empty.values(), empty.read(t).values())
		})
	}
}

func TestUnconfiguredCredentialEncryption(t *testing.T) {
	for _, driverName := range []string{"mysql", "sqlite"} {
		t.Run(driverName, func(t *testing.T) {
			path := setupCredentialTestDB(t)
			if driverName == "mysql" {
				// Use SQLite for SQL execution and the MySQL adapter settings for key selection.
				adapter.driverName = driverName
				adapter.dataSourceName = "user@tcp(localhost:3306)/casvisor"
			}
			t.Setenv("credentialKeyFile", "")
			dir := t.TempDir()
			cwd, err := os.Getwd()
			checkCredentialTestError(t, err)
			checkCredentialTestError(t, os.Chdir(dir))
			t.Cleanup(func() { checkCredentialTestError(t, os.Chdir(cwd)) })
			checkNoKeyFile := func() {
				t.Helper()
				files, err := os.ReadDir(dir)
				checkCredentialTestError(t, err)
				checkCredentialTestEqual(t, 0, len(files))
				_, err = os.Stat(path)
				checkCredentialTest(t, errors.Is(err, os.ErrNotExist), "expected missing key file, got %v", err)
			}

			rows := newLegacyCredentialTestRows(t)
			rows.gateway.PrivateKey = "ENC:V1:plaintext"
			rows.insert(t)
			checkCredentialTestError(t, EncryptAssetCredentials())
			checkCredentialTestEqual(t, rows.values(), rows.read(t).values())
			checkNoKeyFile()

			pending := &Asset{Owner: credentialTestOwner, Name: "pending", Password: "new plaintext"}
			ok, err := AddAsset(pending)
			checkCredentialTestError(t, err)
			checkCredentialTest(t, ok, "plaintext write did not affect any rows")
			checkCredentialTestEqual(t, "new plaintext", readCredentialTestRow[Asset](t, core.PK{pending.Owner, pending.Name}).Password)
			rejected := &Asset{Owner: credentialTestOwner, Name: "rejected", Password: encryptedPrefix + "literal"}
			ok, err = AddAsset(rejected)
			checkCredentialTestErrorContains(t, err, "prefix", "credentialKey and credentialKeyFile are not configured")
			checkCredentialTest(t, !ok, "unauthenticated ciphertext was written")
			checkNoKeyFile()

			t.Setenv("credentialKey", "test explicit key")
			resetCredentialCipher()
			checkCredentialTestError(t, EncryptAssetCredentials())
			before := rows.read(t).values()
			t.Setenv("credentialKey", "")
			resetCredentialCipher()

			checkFailure := func(err error) {
				t.Helper()
				checkCredentialTestErrorContains(t, err, "credentialKey and credentialKeyFile are not configured", "persistent storage")
			}
			checkFailure(EncryptAssetCredentials())
			asset, err := GetAsset(rows.asset.GetId())
			checkFailure(err)
			checkCredentialTest(t, asset == nil, "asset was returned without the credential key")
			rejected.Password = "new credential"
			ok, err = AddAsset(rejected)
			checkFailure(err)
			checkCredentialTest(t, !ok, "write succeeded without the credential key")
			t.Cleanup(func() { clearSigninFailures(rows.user.GetId()) })
			user, err := SigninLocalUser(rows.user.Owner, rows.user.Name, credentialTestPassword, "123456", credentialTestClientIP)
			checkFailure(err)
			checkCredentialTest(t, user == nil, "sign-in succeeded without the credential key")
			checkCredentialTestEqual(t, before, rows.read(t).values())
			checkNoKeyFile()
		})
	}
}

func TestMySQLCredentialKeyConfiguration(t *testing.T) {
	for _, configKey := range []string{"credentialKey", "credentialKeyFile"} {
		t.Run(configKey, func(t *testing.T) {
			path := setupCredentialTestDB(t)
			adapter.driverName = "mysql"
			adapter.dataSourceName = "user@tcp(localhost:3306)/casvisor"
			if configKey == "credentialKey" {
				t.Setenv("credentialKey", "test explicit key")
				t.Setenv("credentialKeyFile", "")
			}
			rows := newLegacyCredentialTestRows(t)
			rows.insert(t)
			checkCredentialTestError(t, EncryptAssetCredentials())
			original := rows.values()
			for i, value := range rows.read(t).values() {
				requireStoredCredential(t, value, original[i])
			}
			rows.checkPublicReads(t)

			asset := &Asset{Owner: credentialTestOwner, Name: "new-asset", Password: "new credential"}
			ok, err := AddAsset(asset)
			checkCredentialTestError(t, err)
			checkCredentialTest(t, ok, "write did not affect any rows")
			requireStoredCredential(t, readCredentialTestRow[Asset](t, core.PK{asset.Owner, asset.Name}).Password, "new credential")
			_, err = os.Stat(path)
			if configKey == "credentialKeyFile" {
				checkCredentialTestError(t, err)
			} else {
				checkCredentialTest(t, errors.Is(err, os.ErrNotExist), "expected missing key file, got %v", err)
			}
		})
	}
}

func TestEncryptAssetCredentialsAcceptsTextLengths(t *testing.T) {
	legacyDDL := []string{
		`CREATE TABLE asset (owner VARCHAR(100) NOT NULL, name VARCHAR(100) NOT NULL,
			password VARCHAR(1000), private_key MEDIUMTEXT, PRIMARY KEY (owner, name))`,
		`CREATE TABLE asset_account (owner VARCHAR(100) NOT NULL, name VARCHAR(100) NOT NULL,
			password VARCHAR(1000), private_key MEDIUMTEXT, PRIMARY KEY (owner, name))`,
		`CREATE TABLE network_gateway (owner VARCHAR(100) NOT NULL, name VARCHAR(100) NOT NULL,
			password VARCHAR(1000), private_key MEDIUMTEXT, PRIMARY KEY (owner, name))`,
		`CREATE TABLE local_user (owner VARCHAR(100) NOT NULL, name VARCHAR(100) NOT NULL,
			totp_secret VARCHAR(200), PRIMARY KEY (owner, name))`,
	}
	tests := []struct {
		name          string
		passwordBytes int
		secretBytes   int
		ddl           []string
	}{
		{"legacy columns", 717, 117, legacyDDL},
		{"exact TEXT limit", credentialTestTextPlaintextMax, credentialTestTextPlaintextMax, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupCredentialTestDB(t, tt.ddl...)
			rows := newLegacyCredentialTestRows(t)
			password := strings.Repeat("p", tt.passwordBytes)
			rows.asset.Password = password
			rows.account.Password = password
			rows.gateway.Password = password
			rows.user.TotpSecret = strings.Repeat("A", tt.secretBytes)
			rows.insert(t)

			checkCredentialTestError(t, EncryptAssetCredentials())
			original := rows.values()
			for i, value := range rows.read(t).values() {
				requireStoredCredential(t, value, original[i])
				if len(original[i]) == credentialTestTextPlaintextMax {
					checkCredentialTestEqual(t, 65535, len(value))
				}
			}
		})
	}
}

func TestEncryptAssetCredentialsRollsBackOversizedValues(t *testing.T) {
	tests := []struct {
		column string
		field  func(*credentialTestRows) *string
	}{
		{"password", func(rows *credentialTestRows) *string { return &rows.gateway.Password }},
		{"totp_secret", func(rows *credentialTestRows) *string { return &rows.user.TotpSecret }},
	}
	for _, tt := range tests {
		t.Run(tt.column, func(t *testing.T) {
			setupCredentialTestDB(t)
			rows := newLegacyCredentialTestRows(t)
			*tt.field(&rows) = strings.Repeat("x", credentialTestTextPlaintextMax+1)
			rows.insert(t)

			// The oversized value is in a later table, after the migration updates earlier rows.
			err := EncryptAssetCredentials()
			checkCredentialTestErrorContains(t, err, "the "+tt.column+" is too long", "65539 bytes")
			checkCredentialTestEqual(t, rows.values(), rows.read(t).values())
			credentialKeyMu.Lock()
			pinned := pinnedCredentialCipher
			credentialKeyMu.Unlock()
			checkCredentialTest(t, pinned == nil, "rolled-back migration pinned a cipher")
		})
	}
}

func TestCredentialTextLengthOnWrites(t *testing.T) {
	tests := []struct {
		name          string
		passwordBytes int
		secretBytes   int
		oversized     bool
	}{
		{"legacy lengths", 717, 117, false},
		{"exact TEXT limit", credentialTestTextPlaintextMax, credentialTestTextPlaintextMax, false},
		{"over TEXT limit", credentialTestTextPlaintextMax + 1, credentialTestTextPlaintextMax + 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupCredentialTestDB(t)
			rows := newLegacyCredentialTestRows(t)
			rows.insert(t)
			password := strings.Repeat("p", tt.passwordBytes)
			secret := strings.Repeat("A", tt.secretBytes)
			if tt.secretBytes == credentialTestTextPlaintextMax {
				// A trimmed space keeps the stored input at the limit with valid unpadded base32.
				secret = secret[:len(secret)-1] + " "
			}
			checkWrite := func(name, column string, ok bool, err error) {
				t.Helper()
				if tt.oversized {
					checkCredentialTestErrorContains(t, err, "the "+column+" is too long", "65539 bytes")
					checkCredentialTest(t, !ok, "%s unexpectedly succeeded", name)
					return
				}
				checkCredentialTestError(t, err)
				checkCredentialTest(t, ok, "%s did not affect any rows", name)
			}
			checkStored := func(stored, input string) {
				t.Helper()
				requireStoredCredential(t, stored, input)
				if len(input) == credentialTestTextPlaintextMax {
					checkCredentialTestEqual(t, 65535, len(stored))
				}
			}

			asset := &Asset{Owner: credentialTestOwner, Name: "new-asset", Password: password}
			ok, err := AddAsset(asset)
			checkWrite("AddAsset", "password", ok, err)
			if !tt.oversized {
				checkStored(readCredentialTestRow[Asset](t, core.PK{asset.Owner, asset.Name}).Password, password)
			}
			updatedAsset := rows.asset
			updatedAsset.Password = password
			ok, err = UpdateAsset(rows.asset.GetId(), &updatedAsset)
			checkWrite("UpdateAsset", "password", ok, err)
			if !tt.oversized {
				checkStored(readCredentialTestRow[Asset](t, core.PK{updatedAsset.Owner, updatedAsset.Name}).Password, password)
			}

			account := &AssetAccount{
				Owner:    credentialTestOwner,
				Name:     "new-account",
				Asset:    rows.asset.Name,
				Username: "alternate",
				Password: password,
			}
			ok, err = AddAssetAccount(account)
			checkWrite("AddAssetAccount", "password", ok, err)
			if !tt.oversized {
				checkStored(readCredentialTestRow[AssetAccount](t, core.PK{account.Owner, account.Name}).Password, password)
			}
			updatedAccount := rows.account
			updatedAccount.Password = password
			ok, err = UpdateAssetAccount(rows.account.GetId(), &updatedAccount)
			checkWrite("UpdateAssetAccount", "password", ok, err)
			accountPK := core.PK{rows.account.Owner, rows.account.Name}
			if !tt.oversized {
				checkStored(readCredentialTestRow[AssetAccount](t, accountPK).Password, password)
			}
			err = updateAssetAccountRotation(&rows.account, password, AccountRotateSucceeded, "")
			checkWrite("updateAssetAccountRotation", "password", err == nil, err)
			if !tt.oversized {
				rotated := readCredentialTestRow[AssetAccount](t, accountPK)
				checkStored(rotated.Password, password)
				checkCredentialTestEqual(t, AccountRotateSucceeded, rotated.RotateStatus)
			}

			gateway := &NetworkGateway{
				Owner:    credentialTestOwner,
				Name:     "new-gateway",
				Host:     "gateway.example.test",
				Port:     22,
				Username: "jump",
				Password: password,
			}
			ok, err = AddNetworkGateway(gateway)
			checkWrite("AddNetworkGateway", "password", ok, err)
			if !tt.oversized {
				checkStored(readCredentialTestRow[NetworkGateway](t, core.PK{gateway.Owner, gateway.Name}).Password, password)
			}
			gateway.Name = rows.gateway.Name
			gateway.Password = password
			ok, err = UpdateNetworkGateway(rows.gateway.GetId(), gateway)
			checkWrite("UpdateNetworkGateway", "password", ok, err)
			if !tt.oversized {
				checkStored(readCredentialTestRow[NetworkGateway](t, core.PK{gateway.Owner, gateway.Name}).Password, password)
			}

			user := &LocalUser{Owner: credentialTestOwner, Name: "new-user", Password: credentialTestPassword, TotpSecret: secret}
			ok, err = AddLocalUser(user)
			checkWrite("AddLocalUser", "totp_secret", ok, err)
			if !tt.oversized {
				checkStored(readCredentialTestRow[LocalUser](t, core.PK{user.Owner, user.Name}).TotpSecret, secret)
			}
			key, err := totpEncoding.DecodeString(strings.TrimSpace(secret))
			checkCredentialTestError(t, err)
			code := getTotpCode(key, time.Now().Unix()/totpPeriod)
			err = EnableLocalUserMfa(rows.user.Owner, rows.user.Name, secret, code)
			checkWrite("EnableLocalUserMfa", "totp_secret", err == nil, err)
			if tt.oversized {
				checkCredentialTestEqual(t, rows.values(), rows.read(t).values())
			} else {
				checkStored(readCredentialTestRow[LocalUser](t, core.PK{rows.user.Owner, rows.user.Name}).TotpSecret, secret)
			}
		})
	}
}

func TestPrivateKeyCredentialLimit(t *testing.T) {
	limit := credentialColumnLimits["private_key"]
	checkCredentialTestEqual(t, 16777215, limit)
	value := strings.Repeat("x", limit)
	checkCredentialTestError(t, checkCredentialLength("private_key", value))
	err := checkCredentialLength("private_key", value+"x")
	checkCredentialTestErrorContains(t, err, "the private_key is too long", "16777216 bytes", "holds 16777215")
}

func TestEncryptAssetCredentialsRetriesRotation(t *testing.T) {
	const rotatedPassword = "rotated-during-upgrade"
	tests := []struct {
		name          string
		wantConflicts int
		persistent    bool
	}{
		{"retry succeeds", 1, false},
		{"retry exhausted", maxCredentialMigrationAttempts, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupCredentialTestDB(t)
			rows := newLegacyCredentialTestRows(t)
			rows.insert(t)
			// the trigger plays another instance rotating the account's password mid-migration
			_, err := adapter.engine.Exec(`CREATE TRIGGER rotate_during_migration
				AFTER UPDATE OF password ON asset
				BEGIN
					UPDATE asset_account SET password = 'rotated-during-upgrade'
					WHERE owner = NEW.owner AND asset = NEW.name;
				END`)
			checkCredentialTestError(t, err)

			conflicts := 0
			err = encryptAssetCredentials(func(err error) {
				conflicts++
				checkCredentialTest(t, conflicts <= tt.wantConflicts, "unexpected additional conflict: %v", err)
				checkCredentialTestErrorContains(t, err, "update *object.AssetAccount", "affected 0 rows")
				checkCredentialTestEqual(t, rows.values(), rows.read(t).values())
				// This callback runs under credentialKeyMu.
				checkCredentialTest(t, pinnedCredentialCipher == nil, "conflicting attempt pinned a cipher")
				if !tt.persistent {
					_, err := adapter.engine.Exec("DROP TRIGGER rotate_during_migration")
					checkCredentialTestError(t, err)
					// Persist the simulated rotator's write outside the rolled-back transaction.
					id := core.PK{rows.account.Owner, rows.account.Name}
					affected, err := adapter.engine.ID(id).Cols("password").Update(&AssetAccount{Password: rotatedPassword})
					checkCredentialTestError(t, err)
					checkCredentialTestEqual(t, int64(1), affected)
				}
			})
			checkCredentialTestEqual(t, tt.wantConflicts, conflicts)
			if tt.persistent {
				checkCredentialTest(t, errors.Is(err, errCredentialMigrationConflict), "expected a conflict error, got %v", err)
				checkCredentialTestErrorContains(t, err, "after 5 attempts", "credential migration conflict")
				checkCredentialTestEqual(t, rows.values(), rows.read(t).values())
				return
			}
			checkCredentialTestError(t, err)
			rows.account.Password = rotatedPassword
			original := rows.values()
			for i, value := range rows.read(t).values() {
				requireStoredCredential(t, value, original[i])
			}
			account, err := GetAssetAccount(rows.account.GetId())
			checkCredentialTestError(t, err)
			checkCredentialTest(t, account != nil, "expected non-nil account")
			checkCredentialTestEqual(t, rotatedPassword, account.Password)
		})
	}
}

func TestEncryptAssetCredentialsInitializesKey(t *testing.T) {
	path := setupCredentialTestDB(t)
	_, err := os.Stat(path)
	checkCredentialTest(t, errors.Is(err, os.ErrNotExist), "expected missing key file, got %v", err)
	checkCredentialTestError(t, EncryptAssetCredentials())
	_, err = os.Stat(path)
	checkCredentialTestError(t, err)
}

func TestEncryptAssetCredentialsRequiresAdapter(t *testing.T) {
	setupCredentialTestCipher(t)
	t.Setenv("credentialKey", "")
	t.Setenv("credentialKeyFile", filepath.Join(t.TempDir(), "credential.key"))
	previous := adapter
	t.Cleanup(func() { adapter = previous })
	adapter = nil
	checkCredentialTestErrorContains(t, EncryptAssetCredentials(), "database adapter is not initialized")
	adapter = &Adapter{}
	checkCredentialTestErrorContains(t, EncryptAssetCredentials(), "database adapter is not initialized")
}

func TestEncryptAssetCredentialsRequiresKeyForHostKey(t *testing.T) {
	path := setupCredentialTestDB(t)
	rows := newLegacyCredentialTestRows(t)
	sealed, err := encryptCredential(rows.hostKey.PrivateKey)
	checkCredentialTestError(t, err)
	rows.hostKey.PrivateKey = sealed
	rows.insert(t)
	checkCredentialTestError(t, os.Remove(path))
	resetCredentialCipher()

	err = EncryptAssetCredentials()
	checkCredentialTestErrorContains(t, err, "credentialKey is not configured", path, "does not exist")
	checkCredentialTestEqual(t, rows.values(), rows.read(t).values())
	_, err = os.Stat(path)
	checkCredentialTest(t, errors.Is(err, os.ErrNotExist), "expected missing key file, got %v", err)
}

func TestAddAssetRequiresCredentialKey(t *testing.T) {
	tests := []struct {
		name  string
		field func(*credentialTestRows) *string
	}{
		{"asset password", func(rows *credentialTestRows) *string { return &rows.asset.Password }},
		{"asset private key", func(rows *credentialTestRows) *string { return &rows.asset.PrivateKey }},
		{"asset account password", func(rows *credentialTestRows) *string { return &rows.account.Password }},
		{"asset account private key", func(rows *credentialTestRows) *string { return &rows.account.PrivateKey }},
		{"gateway password", func(rows *credentialTestRows) *string { return &rows.gateway.Password }},
		{"gateway private key", func(rows *credentialTestRows) *string { return &rows.gateway.PrivateKey }},
		{"local user TOTP", func(rows *credentialTestRows) *string { return &rows.user.TotpSecret }},
		{"SSH host key", func(rows *credentialTestRows) *string { return &rows.hostKey.PrivateKey }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := setupCredentialTestDB(t)
			rows := newLegacyCredentialTestRows(t)
			field := tt.field(&rows)
			sealed, err := encryptCredential(*field)
			checkCredentialTestError(t, err)
			*field = sealed
			rows.insert(t)
			checkCredentialTestError(t, os.Remove(path))
			resetCredentialCipher()

			pending := &Asset{Owner: credentialTestOwner, Name: "pending", Password: "new credential"}
			ok, err := AddAsset(pending)
			checkCredentialTestErrorContains(t, err, "credentialKey is not configured", path, "does not exist")
			checkCredentialTest(t, !ok, "write succeeded without the credential key")
			checkCredentialTest(t, errors.Is(err, os.ErrNotExist), "expected missing key file error, got %v", err)
			_, err = os.Stat(path)
			checkCredentialTest(t, errors.Is(err, os.ErrNotExist), "expected missing key file, got %v", err)

			found, err := adapter.engine.ID(core.PK{pending.Owner, pending.Name}).Exist(&Asset{})
			checkCredentialTestError(t, err)
			checkCredentialTest(t, !found, "asset was inserted despite the missing key")
			checkCredentialTestEqual(t, rows.values(), rows.read(t).values())
		})
	}
}

func TestAddAssetFailsWhenCredentialCheckFails(t *testing.T) {
	for _, keyConfig := range []string{"missing key file", "unconfigured"} {
		t.Run(keyConfig, func(t *testing.T) {
			path := setupCredentialTestDB(t)
			if keyConfig == "unconfigured" {
				t.Setenv("credentialKeyFile", "")
			}
			checkCredentialTestError(t, adapter.engine.DropTables(&SshHostKey{}))
			ok, err := AddAsset(&Asset{Owner: credentialTestOwner, Name: "pending", Password: "new credential"})
			checkCredentialTestErrorContains(t, err, "check *object.SshHostKey for encrypted credentials")
			checkCredentialTest(t, !ok, "write succeeded without checking for encrypted credentials")
			_, err = os.Stat(path)
			checkCredentialTest(t, errors.Is(err, os.ErrNotExist), "expected missing key file, got %v", err)
		})
	}
}

func TestCredentialKeyFailures(t *testing.T) {
	tests := []struct {
		name        string
		explicitKey string
		fileContent string
		missing     bool
		wantError   string
	}{
		{name: "wrong explicit key", explicitKey: "wrong explicit key", wantError: "failed to decrypt"},
		{name: "missing key file", missing: true, wantError: "does not exist"},
		{name: "wrong key file", fileContent: "wrong file key", wantError: "failed to decrypt"},
		{name: "empty key file", wantError: "is empty"},
		{name: "whitespace key file", fileContent: " \t\n", wantError: "is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := setupCredentialTestDB(t)
			rows := newLegacyCredentialTestRows(t)
			rows.insert(t)
			checkCredentialTestError(t, EncryptAssetCredentials())
			before := rows.read(t).values()
			pending := &Asset{Owner: credentialTestOwner, Name: "pending", Password: "plaintext to migrate"}
			_, err := adapter.engine.Insert(pending)
			checkCredentialTestError(t, err)

			keySource := "credential key file " + path
			switch {
			case tt.explicitKey != "":
				t.Setenv("credentialKey", tt.explicitKey)
				keySource = "credentialKey"
			case tt.missing:
				checkCredentialTestError(t, os.Remove(path))
			default:
				checkCredentialTestError(t, os.WriteFile(path, []byte(tt.fileContent), 0o600))
			}
			resetCredentialCipher()
			checkFailure := func(err error) {
				t.Helper()
				checkCredentialTestErrorContains(t, err, keySource, tt.wantError)
				if tt.missing {
					checkCredentialTestErrorContains(t, err, "credentialKey is not configured")
					checkCredentialTest(t, errors.Is(err, os.ErrNotExist), "expected missing key file, got %v", err)
				}
			}

			asset, err := GetAsset(rows.asset.GetId())
			checkFailure(err)
			checkCredentialTest(t, asset == nil, "expected no asset on failure")
			account, err := GetAssetAccount(rows.account.GetId())
			checkFailure(err)
			checkCredentialTest(t, account == nil, "expected no account on failure")
			gateway, err := GetNetworkGateway(rows.gateway.GetId())
			checkFailure(err)
			checkCredentialTest(t, gateway == nil, "expected no gateway on failure")
			key, err := totpEncoding.DecodeString(rows.user.TotpSecret)
			checkCredentialTestError(t, err)
			code := getTotpCode(key, time.Now().Unix()/totpPeriod)
			user, err := SigninLocalUser(rows.user.Owner, rows.user.Name, credentialTestPassword, code, credentialTestClientIP)
			checkFailure(err)
			checkCredentialTest(t, user == nil, "expected no user on failure")
			signer, err := GetSshHostKeySigner()
			checkFailure(err)
			checkCredentialTest(t, signer == nil, "expected no signer on failure")

			checkFailure(EncryptAssetCredentials())
			checkCredentialTestEqual(t, before, rows.read(t).values())
			persisted := readCredentialTestRow[Asset](t, core.PK{pending.Owner, pending.Name})
			checkCredentialTestEqual(t, pending.Password, persisted.Password)
			if tt.missing {
				_, err = os.Stat(path)
				checkCredentialTest(t, errors.Is(err, os.ErrNotExist), "expected missing key file, got %v", err)
			}
			if tt.wantError == "is empty" {
				value, err := encryptCredential("new credential")
				checkFailure(err)
				checkCredentialTestEqual(t, "", value)
				data, err := os.ReadFile(path)
				checkCredentialTestError(t, err)
				checkCredentialTestEqual(t, tt.fileContent, string(data))
			}
		})
	}
}

func TestCredentialCipherRemainsPinnedAfterKeyFileReplacement(t *testing.T) {
	path := setupCredentialTestDB(t)
	old := Asset{Owner: credentialTestOwner, Name: "old-asset", Password: "old credential"}
	_, err := adapter.engine.Insert(&old)
	checkCredentialTestError(t, err)
	checkCredentialTestError(t, EncryptAssetCredentials())
	key, err := os.ReadFile(path)
	checkCredentialTestError(t, err)

	checkCredentialTestError(t, os.WriteFile(path, []byte("different credential key\n"), 0o600))
	newAsset := Asset{Owner: credentialTestOwner, Name: "new-asset", Password: "new credential"}
	ok, err := AddAsset(&newAsset)
	checkCredentialTestError(t, err)
	checkCredentialTest(t, ok, "write did not affect any rows")
	assets := []Asset{old, {Owner: newAsset.Owner, Name: newAsset.Name, Password: "new credential"}}
	checkReads := func() {
		t.Helper()
		for _, want := range assets {
			asset, err := GetAsset(want.GetId())
			checkCredentialTestError(t, err)
			checkCredentialTest(t, asset != nil, "expected non-nil asset")
			checkCredentialTestEqual(t, want.Password, asset.Password)
		}
	}
	checkReads()

	resetCredentialCipher()
	err = EncryptAssetCredentials()
	checkCredentialTestErrorContains(t, err, "failed to decrypt", path)
	for _, want := range assets {
		asset, err := GetAsset(want.GetId())
		checkCredentialTestErrorContains(t, err, "failed to decrypt", path)
		checkCredentialTest(t, asset == nil, "expected no asset on failure")
	}

	// Neither failed authentication attempt should have pinned the replacement key.
	checkCredentialTestError(t, os.WriteFile(path, key, 0o600))
	checkCredentialTestError(t, EncryptAssetCredentials())
	checkReads()
}

func TestCorruptCredentialCiphertext(t *testing.T) {
	path := setupCredentialTestDB(t)
	rows := newLegacyCredentialTestRows(t)
	valid, err := encryptCredential(rows.asset.Password)
	checkCredentialTestError(t, err)
	rows.asset.Password = valid
	rows.insert(t)
	_, err = adapter.engine.Exec(`CREATE TRIGGER reject_unverified_credential_write
		BEFORE UPDATE OF private_key ON asset
		BEGIN
			SELECT RAISE(ABORT, 'credential written before ciphertext verification');
		END`)
	checkCredentialTestError(t, err)

	sealed, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(valid, encryptedPrefix))
	checkCredentialTestError(t, err)
	const nonceSize = 12
	checkCredentialTest(t, len(sealed) > nonceSize, "ciphertext is too short")
	truncated := encryptedPrefix + base64.StdEncoding.EncodeToString(sealed[:nonceSize])
	sealed[len(sealed)-1] ^= 1
	corrupted := encryptedPrefix + base64.StdEncoding.EncodeToString(sealed)
	tests := []struct {
		name      string
		value     string
		wantError string
	}{
		{"malformed base64", encryptedPrefix + "!", "invalid encrypted credential"},
		{"missing authentication tag", truncated, "too short"},
		{"corrupted tag", corrupted, "failed to decrypt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupCredentialTestCipher(t)
			// the host key table is checked last, so nothing before it may have been written
			_, err := adapter.engine.ID(rows.hostKey.Name).Cols("private_key").Update(&SshHostKey{PrivateKey: tt.value})
			checkCredentialTestError(t, err)
			before := rows.read(t).values()
			plain, err := decryptCredential(tt.value)
			checkCredentialTestErrorContains(t, err, tt.wantError, "credential key file "+path)
			checkCredentialTestEqual(t, "", plain)
			err = EncryptAssetCredentials()
			checkCredentialTestErrorContains(t, err, tt.wantError, "SshHostKey")
			checkCredentialTestEqual(t, before, rows.read(t).values())
		})
	}
}

func TestConcurrentCredentialKeyGeneration(t *testing.T) {
	setupCredentialTestCipher(t)
	path := filepath.Join(t.TempDir(), "keys", "credential.key")
	t.Setenv("credentialKey", "")
	t.Setenv("credentialKeyFile", path)

	var results [32]struct {
		value string
		err   error
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i].value, results[i].err = encryptCredential("credential " + strconv.Itoa(i))
		}()
	}
	close(start)
	wg.Wait()
	resetCredentialCipher()
	for i, result := range results {
		checkCredentialTestError(t, result.err)
		requireStoredCredential(t, result.value, "credential "+strconv.Itoa(i))
	}
	files, err := os.ReadDir(filepath.Dir(path))
	checkCredentialTestError(t, err)
	checkCredentialTestEqual(t, 1, len(files))
	checkCredentialTestEqual(t, filepath.Base(path), files[0].Name())
}
