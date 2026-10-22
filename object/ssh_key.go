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
	"fmt"
	"strings"

	"github.com/casvisor/casvisor/util"
	"golang.org/x/crypto/ssh"
	"xorm.io/core"
)

// SshKey is a public key a user signs in to the SSH gateway with.
type SshKey struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	DisplayName string `xorm:"varchar(100)" json:"displayName"`

	User         string `xorm:"varchar(100) index" json:"user"`
	PublicKey    string `xorm:"mediumtext" json:"publicKey"`
	Fingerprint  string `xorm:"varchar(100) unique" json:"fingerprint"`
	LastUsedTime string `xorm:"varchar(100)" json:"lastUsedTime"`
}

func (sshKey *SshKey) GetId() string {
	return util.GetIdFromOwnerAndName(sshKey.Owner, sshKey.Name)
}

func (sshKey *SshKey) GetUserId() string {
	return util.GetIdFromOwnerAndName(sshKey.Owner, sshKey.User)
}

func GetUserSshKeys(owner string, user string) ([]*SshKey, error) {
	sshKeys := []*SshKey{}
	err := adapter.engine.Desc("created_time").Find(&sshKeys, &SshKey{Owner: owner, User: user})
	if err != nil {
		return sshKeys, err
	}

	return sshKeys, nil
}

func getSshKey(owner string, name string) (*SshKey, error) {
	if owner == "" || name == "" {
		return nil, nil
	}

	sshKey := SshKey{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&sshKey)
	if err != nil {
		return nil, err
	}

	if existed {
		return &sshKey, nil
	}
	return nil, nil
}

func GetSshKey(id string) (*SshKey, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	return getSshKey(owner, name)
}

func GetSshKeyByFingerprint(fingerprint string) (*SshKey, error) {
	sshKey := SshKey{Fingerprint: fingerprint}
	existed, err := adapter.engine.Get(&sshKey)
	if err != nil {
		return nil, err
	}

	if existed {
		return &sshKey, nil
	}
	return nil, nil
}

func AddSshKey(sshKey *SshKey) (bool, error) {
	publicKey, comment, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(sshKey.PublicKey)))
	if err != nil {
		return false, fmt.Errorf("invalid SSH public key: %s", err.Error())
	}

	fingerprint := ssh.FingerprintSHA256(publicKey)
	existing, err := GetSshKeyByFingerprint(fingerprint)
	if err != nil {
		return false, err
	}
	if existing != nil {
		return false, fmt.Errorf("the SSH public key: %s has already been added", fingerprint)
	}

	sshKey.Name = util.GenerateId()
	sshKey.CreatedTime = util.GetCurrentTime()
	sshKey.PublicKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(publicKey)))
	sshKey.Fingerprint = fingerprint
	sshKey.LastUsedTime = ""
	if sshKey.DisplayName == "" {
		sshKey.DisplayName = comment
	}

	affected, err := adapter.engine.Insert(sshKey)
	if err != nil {
		return false, err
	}

	return affected != 0, nil
}

func DeleteSshKey(sshKey *SshKey) (bool, error) {
	affected, err := adapter.engine.ID(core.PK{sshKey.Owner, sshKey.Name}).Delete(&SshKey{})
	if err != nil {
		return false, err
	}

	return affected != 0, nil
}

func UpdateSshKeyLastUsedTime(sshKey *SshKey) error {
	sshKey.LastUsedTime = util.GetCurrentTime()
	_, err := adapter.engine.ID(core.PK{sshKey.Owner, sshKey.Name}).Cols("last_used_time").Update(sshKey)
	return err
}
