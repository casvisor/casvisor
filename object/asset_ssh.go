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
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"
)

func DialSsh(asset *Asset) (*ssh.Client, error) {
	if asset.Username == "" {
		return nil, fmt.Errorf("the username of the asset: %s is empty", asset.GetId())
	}

	password := asset.Password
	auth := []ssh.AuthMethod{}
	if asset.PrivateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(asset.PrivateKey))
		if _, ok := err.(*ssh.PassphraseMissingError); ok {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(asset.PrivateKey), []byte(password))
		}
		if err != nil {
			return nil, fmt.Errorf("failed to parse the private key of the asset: %s: %s", asset.GetId(), err.Error())
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

	config := &ssh.ClientConfig{
		User:            asset.Username,
		Auth:            auth,
		HostKeyCallback: getHostKeyCallback(asset),
		Timeout:         10 * time.Second,
	}

	address := net.JoinHostPort(asset.Host, strconv.Itoa(asset.Port))
	return ssh.Dial("tcp", address, config)
}

// getHostKeyCallback pins the host key on the first connection and rejects a different key afterwards.
func getHostKeyCallback(asset *Asset) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		fingerprint := ssh.FingerprintSHA256(key)
		if asset.Fingerprint == "" {
			asset.Fingerprint = fingerprint
			return UpdateAssetFingerprint(asset.GetId(), fingerprint)
		}
		if asset.Fingerprint != fingerprint {
			return fmt.Errorf("the host key of the asset: %s has changed from %s to %s, clear the fingerprint of the asset if the change is expected", asset.GetId(), asset.Fingerprint, fingerprint)
		}
		return nil
	}
}
