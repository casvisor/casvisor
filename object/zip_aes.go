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
	"archive/zip"
	"bytes"
	"compress/flate"
	"crypto/aes"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"
)

// The zip files of account backups are encrypted with AES-256 as WinZip does (AE-2), so that 7-Zip,
// WinRAR, Bandizip and the unzip of macOS open them with the password, without Casvisor.
const (
	zipMethodAes        = 99
	zipAesExtraId       = 0x9901
	zipAesSaltSize      = 16
	zipAesKeySize       = 32
	zipAesMacSize       = 10
	zipAesIterations    = 1000
	maxZipEntrySize     = 64 << 20
	maxZipArchiveSize   = 64 << 20
	errZipWrongPassword = "the password of the zip file is wrong"
)

// ZipEntry is a file in a zip file.
type ZipEntry struct {
	Name string
	Data []byte
}

func getZipAesKeys(password string, salt []byte) (encKey []byte, macKey []byte, verifier []byte, err error) {
	key, err := pbkdf2.Key(sha1.New, password, salt, zipAesIterations, 2*zipAesKeySize+2)
	if err != nil {
		return nil, nil, nil, err
	}
	return key[:zipAesKeySize], key[zipAesKeySize : 2*zipAesKeySize], key[2*zipAesKeySize:], nil
}

// zipAesCtr is the CTR mode of WinZip, whose counter is little-endian and starts at 1.
func zipAesCtr(key []byte, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	res := make([]byte, len(data))
	counter := make([]byte, aes.BlockSize)
	stream := make([]byte, aes.BlockSize)
	var n uint64
	for i := 0; i < len(data); i += aes.BlockSize {
		n++
		binary.LittleEndian.PutUint64(counter, n)
		block.Encrypt(stream, counter)
		end := min(i+aes.BlockSize, len(data))
		for j := i; j < end; j++ {
			res[j] = data[j] ^ stream[j-i]
		}
	}
	return res, nil
}

func getDosTime(t time.Time) (uint16, uint16) {
	date := uint16((t.Year()-1980)<<9 | int(t.Month())<<5 | t.Day())
	clock := uint16(t.Hour()<<11 | t.Minute()<<5 | t.Second()/2)
	return date, clock
}

// WriteAesZip makes a zip file of the entries, each compressed and then encrypted with the password.
func WriteAesZip(entries []*ZipEntry, password string, modified time.Time) ([]byte, error) {
	buf := &bytes.Buffer{}
	writer := zip.NewWriter(buf)
	date, clock := getDosTime(modified)

	for _, entry := range entries {
		compressed := &bytes.Buffer{}
		deflater, err := flate.NewWriter(compressed, flate.BestCompression)
		if err != nil {
			return nil, err
		}
		_, err = deflater.Write(entry.Data)
		if err == nil {
			err = deflater.Close()
		}
		if err != nil {
			return nil, err
		}

		salt := make([]byte, zipAesSaltSize)
		_, err = rand.Read(salt)
		if err != nil {
			return nil, err
		}
		encKey, macKey, verifier, err := getZipAesKeys(password, salt)
		if err != nil {
			return nil, err
		}
		encrypted, err := zipAesCtr(encKey, compressed.Bytes())
		if err != nil {
			return nil, err
		}
		mac := hmac.New(sha1.New, macKey)
		mac.Write(encrypted)

		body := append(append(append(salt, verifier...), encrypted...), mac.Sum(nil)[:zipAesMacSize]...)
		// AE-2, "AE", AES-256, then deflate as the real method
		extra := []byte{0x01, 0x99, 7, 0, 2, 0, 'A', 'E', 3, 8, 0}
		header := &zip.FileHeader{
			Name:               entry.Name,
			Method:             zipMethodAes,
			Flags:              0x1 | 0x800,
			CreatorVersion:     51,
			ReaderVersion:      51,
			ModifiedDate:       date,
			ModifiedTime:       clock,
			CompressedSize64:   uint64(len(body)),
			UncompressedSize64: uint64(len(entry.Data)),
			Extra:              extra,
		}
		w, err := writer.CreateRaw(header)
		if err != nil {
			return nil, err
		}
		_, err = w.Write(body)
		if err != nil {
			return nil, err
		}
	}

	err := writer.Close()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// getZipAesMethod reads the real compression method of an entry encrypted with AES, and its key size.
func getZipAesMethod(extra []byte) (uint16, int, error) {
	for len(extra) >= 4 {
		id := binary.LittleEndian.Uint16(extra)
		size := int(binary.LittleEndian.Uint16(extra[2:]))
		if len(extra) < 4+size {
			break
		}
		if id == zipAesExtraId && size >= 7 {
			field := extra[4 : 4+size]
			keySizes := map[byte]int{1: 16, 2: 24, 3: 32}
			keySize, ok := keySizes[field[4]]
			if !ok {
				return 0, 0, fmt.Errorf("unknown AES strength: %d", field[4])
			}
			return binary.LittleEndian.Uint16(field[5:]), keySize, nil
		}
		extra = extra[4+size:]
	}
	return 0, 0, errors.New("the zip file is encrypted in a way that is not AES")
}

func readZipAesEntry(file *zip.File, password string) ([]byte, error) {
	method, keySize, err := getZipAesMethod(file.Extra)
	if err != nil {
		return nil, err
	}
	raw, err := file.OpenRaw()
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(raw, maxZipArchiveSize+1))
	if err != nil {
		return nil, err
	}
	saltSize := keySize / 2
	if len(body) < saltSize+2+zipAesMacSize {
		return nil, fmt.Errorf("the file: %s in the zip file is cut off", file.Name)
	}
	salt := body[:saltSize]
	verifier := body[saltSize : saltSize+2]
	encrypted := body[saltSize+2 : len(body)-zipAesMacSize]
	authCode := body[len(body)-zipAesMacSize:]

	key, err := pbkdf2.Key(sha1.New, password, salt, zipAesIterations, 2*keySize+2)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare(key[2*keySize:], verifier) != 1 {
		return nil, errors.New(errZipWrongPassword)
	}
	mac := hmac.New(sha1.New, key[keySize:2*keySize])
	mac.Write(encrypted)
	if !hmac.Equal(mac.Sum(nil)[:zipAesMacSize], authCode) {
		return nil, fmt.Errorf("the file: %s in the zip file was changed or is damaged", file.Name)
	}
	compressed, err := zipAesCtr(key[:keySize], encrypted)
	if err != nil {
		return nil, err
	}

	switch method {
	case zip.Store:
		return compressed, nil
	case zip.Deflate:
		data, err := io.ReadAll(io.LimitReader(flate.NewReader(bytes.NewReader(compressed)), maxZipEntrySize+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxZipEntrySize {
			return nil, fmt.Errorf("the file: %s in the zip file is too large", file.Name)
		}
		return data, nil
	default:
		return nil, fmt.Errorf("the file: %s in the zip file is compressed with the method %d, which is not supported", file.Name, method)
	}
}

// ReadAesZip reads the files of a zip file, decrypting the ones that are encrypted with AES.
func ReadAesZip(data []byte, password string) (map[string][]byte, error) {
	if len(data) > maxZipArchiveSize {
		return nil, fmt.Errorf("the zip file is larger than %d MB", maxZipArchiveSize>>20)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("the file is not a zip file: %s", err.Error())
	}

	res := map[string][]byte{}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		var content []byte
		if file.Method == zipMethodAes {
			content, err = readZipAesEntry(file, password)
		} else {
			if file.Flags&0x1 != 0 {
				return nil, errors.New("the zip file is encrypted with the old ZipCrypto, which is not supported: encrypt it with AES-256")
			}
			var rc io.ReadCloser
			rc, err = file.Open()
			if err == nil {
				content, err = io.ReadAll(io.LimitReader(rc, maxZipEntrySize+1))
				_ = rc.Close()
				if err == nil && len(content) > maxZipEntrySize {
					err = fmt.Errorf("the file: %s in the zip file is too large", file.Name)
				}
			}
		}
		if err != nil {
			return nil, err
		}
		res[file.Name] = content
	}
	return res, nil
}
