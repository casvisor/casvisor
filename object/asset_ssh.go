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
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

func DialSsh(asset *Asset) (*ssh.Client, error) {
	if asset.Username == "" {
		return nil, fmt.Errorf("the username of the asset: %s is empty", asset.GetId())
	}

	auth, err := getSshAuthMethods(asset.GetId(), asset.Password, asset.PrivateKey)
	if err != nil {
		return nil, err
	}

	config := &ssh.ClientConfig{
		User: asset.Username,
		Auth: auth,
		HostKeyCallback: getHostKeyCallback("asset", asset.GetId(), &asset.Fingerprint, func(fingerprint string) error {
			return UpdateAssetFingerprint(asset.GetId(), fingerprint)
		}),
		Timeout: dialTimeout,
	}

	conn, err := DialAsset(asset)
	if err != nil {
		return nil, err
	}
	return newSshClient(conn, asset.getAddress(), config)
}

// newSshClient runs the SSH handshake on conn, which is closed when the handshake fails.
func newSshClient(conn net.Conn, address string, config *ssh.ClientConfig) (*ssh.Client, error) {
	_ = conn.SetDeadline(time.Now().Add(config.Timeout))
	c, chans, reqs, err := ssh.NewClientConn(conn, address, config)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})

	return ssh.NewClient(c, chans, reqs), nil
}

func getSshAuthMethods(id string, password string, privateKey string) ([]ssh.AuthMethod, error) {
	auth := []ssh.AuthMethod{}
	if privateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(privateKey))
		if _, ok := err.(*ssh.PassphraseMissingError); ok {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(privateKey), []byte(password))
		}
		if err != nil {
			return nil, fmt.Errorf("failed to parse the private key of: %s: %s", id, err.Error())
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if password != "" {
		auth = append(auth,
			ssh.Password(password),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = password
				}
				return answers, nil
			}),
		)
	}
	return auth, nil
}

// getHostKeyCallback pins the host key on the first connection and rejects a different key afterwards.
func getHostKeyCallback(kind string, id string, pinned *string, update func(fingerprint string) error) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		fingerprint := ssh.FingerprintSHA256(key)
		if *pinned == "" {
			*pinned = fingerprint
			return update(fingerprint)
		}
		if *pinned != fingerprint {
			return fmt.Errorf("the host key of the %s: %s has changed from %s to %s, clear the fingerprint of the %s if the change is expected", kind, id, *pinned, fingerprint, kind)
		}
		return nil
	}
}
