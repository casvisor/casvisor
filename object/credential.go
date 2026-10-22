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
	"fmt"
	"strings"

	"github.com/casvisor/casvisor/conf"
	"xorm.io/core"
)

const encryptedPrefix = "enc:v1:"

func getCredentialCipher() (cipher.AEAD, error) {
	key := conf.GetConfigString("credentialKey")
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
		return "", fmt.Errorf("the credential is encrypted but credentialKey is not configured")
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
		return "", fmt.Errorf("failed to decrypt the credential, credentialKey may be wrong: %s", err.Error())
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

// EncryptAssetCredentials encrypts the plaintext credentials of assets and gateways left in the database,
// e.g. the ones saved before credentialKey was configured.
func EncryptAssetCredentials() error {
	if conf.GetConfigString("credentialKey") == "" {
		return nil
	}

	assets := []*Asset{}
	err := adapter.engine.Find(&assets)
	if err != nil {
		return err
	}

	for _, asset := range assets {
		if (asset.Password == "" || strings.HasPrefix(asset.Password, encryptedPrefix)) &&
			(asset.PrivateKey == "" || strings.HasPrefix(asset.PrivateKey, encryptedPrefix)) {
			continue
		}

		err = asset.encryptCredentials()
		if err != nil {
			return err
		}
		_, err = adapter.engine.ID(core.PK{asset.Owner, asset.Name}).Cols("password", "private_key").Update(asset)
		if err != nil {
			return err
		}
	}

	gateways := []*NetworkGateway{}
	err = adapter.engine.Find(&gateways)
	if err != nil {
		return err
	}

	for _, gateway := range gateways {
		if (gateway.Password == "" || strings.HasPrefix(gateway.Password, encryptedPrefix)) &&
			(gateway.PrivateKey == "" || strings.HasPrefix(gateway.PrivateKey, encryptedPrefix)) {
			continue
		}

		err = gateway.encryptCredentials()
		if err != nil {
			return err
		}
		_, err = adapter.engine.ID(core.PK{gateway.Owner, gateway.Name}).Cols("password", "private_key").Update(gateway)
		if err != nil {
			return err
		}
	}
	return nil
}
