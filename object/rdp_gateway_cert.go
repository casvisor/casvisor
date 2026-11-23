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
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"sync"
	"time"

	"github.com/casvisor/casvisor/util"
)

// the certificate that mstsc sees from the Remote Desktop Gateway when Casvisor signs in to the asset for it,
// kept with the SSH host key so that it stays the same across restarts and replicas, and mstsc can remember it
const rdpGatewayCertName = "rdp-gateway"

var rdpGatewayCert struct {
	mu   sync.Mutex
	cert *tls.Certificate
}

// GetRdpGatewayCertificate returns the certificate and its SHA-256 fingerprint, which is what mstsc keeps as
// the CertHash of a remote computer it was told to trust.
func GetRdpGatewayCertificate() (*tls.Certificate, string, error) {
	rdpGatewayCert.mu.Lock()
	defer rdpGatewayCert.mu.Unlock()

	if rdpGatewayCert.cert == nil {
		cert, err := loadRdpGatewayCertificate()
		if err != nil {
			return nil, "", err
		}
		rdpGatewayCert.cert = cert
	}
	sum := sha256.Sum256(rdpGatewayCert.cert.Certificate[0])
	return rdpGatewayCert.cert, hex.EncodeToString(sum[:]), nil
}

func loadRdpGatewayCertificate() (*tls.Certificate, error) {
	record := SshHostKey{Name: rdpGatewayCertName}
	existed, err := adapter.engine.Get(&record)
	if err != nil {
		return nil, err
	}

	if !existed {
		data, err := newRdpGatewayCertificate()
		if err != nil {
			return nil, err
		}
		record.CreatedTime = util.GetCurrentTime()
		record.PrivateKey, err = encryptCredential(data)
		if err != nil {
			return nil, err
		}
		_, err = adapter.engine.Insert(&record)
		if err != nil {
			// another replica made it first
			_, err = adapter.engine.Get(&record)
			if err != nil {
				return nil, err
			}
		}
	}

	data, err := decryptCredential(record.PrivateKey)
	if err != nil {
		return nil, err
	}
	cert, err := tls.X509KeyPair([]byte(data), []byte(data))
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// newRdpGatewayCertificate makes a self-signed certificate: mstsc reaches it inside the gateway tunnel, which
// HTTPS and the one-time token already authenticate.
func newRdpGatewayCertificate() (string, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", err
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "Casvisor"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(20, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return "", err
	}
	res := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	res = append(res, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})...)
	return string(res), nil
}
