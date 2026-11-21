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

package sshgateway

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
	"golang.org/x/crypto/ssh"
)

const (
	certUserExtension = "casvisor-cert-user"
	certIdExtension   = "casvisor-cert-id"
)

// getTrustedUserCaKeys reads sshGatewayTrustedUserCaKeys, like sshd's TrustedUserCAKeys: the CA
// public keys themselves, one per line or separated by ";", or the path of a file that has them.
func getTrustedUserCaKeys() ([]ssh.PublicKey, error) {
	value := strings.TrimSpace(conf.GetConfigString("sshGatewayTrustedUserCaKeys"))
	if value == "" {
		return nil, nil
	}

	text := value
	if !strings.Contains(value, " ") {
		data, err := os.ReadFile(value)
		if err != nil {
			return nil, fmt.Errorf("failed to read sshGatewayTrustedUserCaKeys: %s", err.Error())
		}
		text = string(data)
	}

	keys := []ssh.PublicKey{}
	for _, line := range strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == ';' }) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			return nil, fmt.Errorf("invalid key in sshGatewayTrustedUserCaKeys: %s", err.Error())
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// getCertUserId maps a certificate principal to a Casvisor user: "<organization>/<user>",
// or "<user>" in the default organization.
func getCertUserId(principal string) string {
	switch strings.Count(principal, "/") {
	case 0:
		return util.GetIdFromOwnerAndName(object.GetLocalOrganization(), principal)
	case 1:
		return principal
	default:
		return ""
	}
}

// checkCertificate signs in a user whose OpenSSH certificate is signed by a trusted CA, such as
// one issued by Vault, step-ca or "ssh-keygen -s", with no key registered in Casvisor. The user is
// the certificate's principal; when it has several, the SSH username picks one: "alice@web1".
func checkCertificate(meta ssh.ConnMetadata, cert *ssh.Certificate) (*ssh.Permissions, error) {
	caKeys, err := getTrustedUserCaKeys()
	if err != nil {
		return nil, err
	}
	if len(caKeys) == 0 {
		return nil, fmt.Errorf("certificates are not accepted, set sshGatewayTrustedUserCaKeys to trust a CA")
	}

	if cert.CertType != ssh.UserCert {
		return nil, fmt.Errorf("not a user certificate")
	}
	if len(cert.ValidPrincipals) == 0 {
		return nil, fmt.Errorf("a certificate without principals is not accepted")
	}

	userName, _, _ := parseUsername(meta.User())
	principal := ""
	for _, p := range cert.ValidPrincipals {
		if getCertUserId(p) == "" {
			continue
		}
		if userName == "" || p == userName || getCertUserId(p) == getCertUserId(userName) {
			if principal != "" && userName == "" {
				return nil, fmt.Errorf("the certificate has several principals, pick one with \"ssh -p <port> <principal>@<asset>@<host>\"")
			}
			principal = p
		}
	}
	if principal == "" {
		return nil, fmt.Errorf("the certificate is not valid for the user: %s", userName)
	}

	checker := &ssh.CertChecker{
		IsUserAuthority: func(auth ssh.PublicKey) bool {
			for _, key := range caKeys {
				if bytes.Equal(key.Marshal(), auth.Marshal()) {
					return true
				}
			}
			return false
		},
	}
	if !checker.IsUserAuthority(cert.SignatureKey) {
		return nil, fmt.Errorf("the certificate is signed by an unknown CA")
	}
	err = checker.CheckCert(principal, cert)
	if err != nil {
		return nil, err
	}

	owner, name := util.GetOwnerAndNameFromId(getCertUserId(principal))
	user, err := object.GetCasdoorUser(owner, name)
	if err != nil {
		return nil, err
	}

	perms := &ssh.Permissions{
		// source-address is enforced by x/crypto/ssh from the critical options
		CriticalOptions: cert.CriticalOptions,
		Extensions: map[string]string{
			certUserExtension: util.GetIdFromOwnerAndName(user.Owner, user.Name),
			certIdExtension:   fmt.Sprintf("%s (serial %d, CA %s)", cert.KeyId, cert.Serial, ssh.FingerprintSHA256(cert.SignatureKey)),
		},
	}
	return checkLoginRules(meta, user, perms)
}
