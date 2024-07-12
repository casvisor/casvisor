// Copyright 2023 The casbin Authors. All Rights Reserved.
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

package util

import (
	"encoding/base64"
	"fmt"
	"regexp"

	"github.com/beego/beego"
	vault "github.com/hashicorp/vault/api"
)

func InitVaultClient() (*vault.Client, error) {
	config := vault.DefaultConfig()
	if beego.AppConfig.String("vaultEndpoint") == "" {
		return nil, fmt.Errorf("vault address is empty")
	}
	config.Address = beego.AppConfig.String("vaultEndpoint")
	client, err := vault.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create vault client: %v", err)
	}

	client.SetToken(beego.AppConfig.String("vaultToken"))

	return client, nil
}

func EncryPassword(client *vault.Client, plaintext string) (string, error) {
	// base64 编码
	plaintextBase64 := base64.StdEncoding.EncodeToString([]byte(plaintext))
	data := map[string]interface{}{
		"plaintext": plaintextBase64,
	}

	secret, err := client.Logical().Write("transit/encrypt/my-key", data)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt data: %v", err)
	}

	ciphertext := secret.Data["ciphertext"].(string)
	return ciphertext, nil
}

func DecryPassword(client *vault.Client, ciphertext string) (string, error) {
	re := regexp.MustCompile(`^vault.*`)
	if !re.MatchString(ciphertext) {
		return ciphertext, nil
	}
	data := map[string]interface{}{
		"ciphertext": ciphertext,
	}

	secret, err := client.Logical().Write("transit/decrypt/my-key", data)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt data: %v", err)
	}

	plaintextBase64 := secret.Data["plaintext"].(string)

	// base64 解码
	plaintext, err := base64.StdEncoding.DecodeString(plaintextBase64)
	if err != nil {
		return "", fmt.Errorf("failed to decode plaintext: %v", err)
	}

	return string(plaintext), nil
}
