package util

import (
	"encoding/base64"
	"fmt"
	"github.com/beego/beego"
	vault "github.com/hashicorp/vault/api"
	"regexp"
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
