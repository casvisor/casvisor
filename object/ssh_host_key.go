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
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"

	"github.com/casvisor/casvisor/util"
	"golang.org/x/crypto/ssh"
)

// SshHostKey is the SSH gateway's own host key, kept in the database so that
// it survives container rebuilds and is shared by every replica.
type SshHostKey struct {
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	PrivateKey  string `xorm:"mediumtext" json:"privateKey"`
}

const sshHostKeyName = "ed25519"

func GetSshHostKeySigner() (ssh.Signer, error) {
	hostKey := SshHostKey{Name: sshHostKeyName}
	existed, err := adapter.engine.Get(&hostKey)
	if err != nil {
		return nil, err
	}

	if !existed {
		_, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		block, err := ssh.MarshalPrivateKey(privateKey, "casvisor")
		if err != nil {
			return nil, err
		}

		hostKey.CreatedTime = util.GetCurrentTime()
		hostKey.PrivateKey, err = encryptCredential(string(pem.EncodeToMemory(block)))
		if err != nil {
			return nil, err
		}
		_, err = adapter.engine.Insert(&hostKey)
		if err != nil {
			return nil, err
		}
	}

	privateKey, err := decryptCredential(hostKey.PrivateKey)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey([]byte(privateKey))
}
