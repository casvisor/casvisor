package dbgate

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
)

const (
	defaultEncryptionKey = "mQAUaXhavRGJDxDTXSCg7Ej0xMmGCrx6OKA07DIMBiDcYYkvkaXjTAzPUEHEHEf9"
	keyFileName          = ".key"
)

var (
	encryptionKey string
	encryptor     *Encryptor
)

type keyData struct {
	EncryptionKey string `json:"encryptionKey"`
}

func loadEncryptionKey() (string, error) {
	if encryptionKey != "" {
		return encryptionKey, nil
	}

	encryptor, _ := NewEncryptor(defaultEncryptionKey)
	keyFile := filepath.Join(dataDir(), keyFileName)

	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		newKey := make([]byte, 32)
		_, err := rand.Read(newKey)
		if err != nil {
			return "", err
		}

		result := keyData{EncryptionKey: string(newKey)}
		encryptedKey, err := encryptor.encrypt(result)
		if err != nil {
			return "", err
		}

		err = os.WriteFile(keyFile, []byte(encryptedKey), 0o644)
		if err != nil {
			return "", err
		}
	}

	encryptedData, err := os.ReadFile(keyFile)
	if err != nil {
		return "", err
	}

	decryptedData, err := encryptor.decrypt(string(encryptedData))
	if err != nil {
		return "", err
	}
	dataMap, ok := decryptedData.(map[string]interface{})
	if !ok {
		return "", errors.New("failed to parse decrypted data")
	}

	var data keyData
	data.EncryptionKey, ok = dataMap["encryptionKey"].(string)
	if !ok {
		return "", errors.New("failed to parse encryptionKey")
	}

	encryptionKey = data.EncryptionKey

	return encryptionKey, nil
}

func getEncryptor() (*Encryptor, error) {
	if encryptor != nil {
		return encryptor, nil
	}

	key, err := loadEncryptionKey()
	if err != nil {
		return nil, err
	}

	encryptor, _ = NewEncryptor(key)
	if err != nil {
		return nil, err
	}

	return encryptor, nil
}
