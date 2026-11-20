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
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"xorm.io/xorm"
)

func listenAssetTCP(t *testing.T) *net.TCPListener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = ln.Close()
	})
	return ln.(*net.TCPListener)
}

func TestAssetTCP(t *testing.T) {
	for _, protocol := range []string{"RDP", "vNc", "Telnet"} {
		t.Run(protocol, func(t *testing.T) {
			ln := listenAssetTCP(t)
			asset := &Asset{Protocol: protocol, Host: " 127.0.0.1 ", Port: ln.Addr().(*net.TCPAddr).Port}
			res, err := TestAsset(asset)
			if err != nil {
				t.Fatal(err)
			}
			if res == nil || res.LatencyMs < 0 {
				t.Fatalf("unexpected result: %+v", res)
			}

			deadline := time.Now().Add(dialTimeout)
			if err := ln.SetDeadline(deadline); err != nil {
				t.Fatal(err)
			}
			conn, err := ln.Accept()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if err := conn.SetReadDeadline(deadline); err != nil {
				t.Fatal(err)
			}
			var buf [1]byte
			if n, err := conn.Read(buf[:]); n != 0 || err != io.EOF {
				t.Fatalf("read after probe = (%d, %v), want (0, EOF)", n, err)
			}
		})
	}
}

func TestAssetUnreachable(t *testing.T) {
	ln := listenAssetTCP(t)
	asset := &Asset{Protocol: "RDP", Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port}
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := TestAsset(asset); err == nil {
		t.Fatal("expected a connection error for a closed port")
	}
}

func TestAssetValidation(t *testing.T) {
	for _, tt := range []struct {
		asset *Asset
		want  string
	}{
		{nil, "asset is required"},
		{&Asset{Protocol: ProtocolDesktop}, "needs its Casvisor Desktop device"},
		{&Asset{Host: " \t", Port: 22}, "host is required"},
		{&Asset{Host: "127.0.0.1"}, "invalid asset port"},
		{&Asset{Host: "127.0.0.1", Port: 70000}, "invalid asset port"},
		{&Asset{Host: "127.0.0.1", Port: 22}, "unsupported protocol"},
		{&Asset{Host: "127.0.0.1", Port: 22, Protocol: "HTTP"}, "unsupported protocol: HTTP"},
	} {
		if _, err := TestAsset(tt.asset); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("TestAsset(%+v) error = %v, want %q", tt.asset, err, tt.want)
		}
	}
}

func newAssetTestDB(t *testing.T) *xorm.Engine {
	t.Helper()
	engine, err := xorm.NewEngine("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	engine.SetMaxOpenConns(1)
	previous := adapter
	t.Cleanup(func() {
		adapter = previous
		_ = engine.Close()
	})
	adapter = &Adapter{driverName: "sqlite", dataSourceName: ":memory:", engine: engine}
	if err := engine.Sync2(new(Asset), new(NetworkGateway)); err != nil {
		t.Fatal(err)
	}
	return engine
}

func TestAssetMaskedSSH(t *testing.T) {
	engine := newAssetTestDB(t)
	ln := listenAssetTCP(t)
	request := Asset{Owner: "org", Name: "test", Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, Protocol: "ssh", Username: "test", Password: "***"}
	if _, err := TestAsset(&request); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("masked lookup before insert: got %v, want missing asset", err)
	}
	stored := request
	stored.Password = "stored-password"
	if _, err := engine.Insert(&stored); err != nil {
		t.Fatal(err)
	}

	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if c.User() != stored.Username || string(password) != stored.Password {
				return nil, fmt.Errorf("invalid credentials")
			}
			return nil, nil
		},
	}
	config.AddHostKey(signer)
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			t.Errorf("accept SSH connection: %v", err)
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(dialTimeout))
		server, _, _, err := ssh.NewServerConn(conn, config)
		if err != nil {
			t.Errorf("SSH handshake: %v", err)
			return
		}
		defer server.Close()
		_ = server.Wait()
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})

	res, err := TestAsset(&request)
	if err != nil {
		t.Fatal(err)
	}
	want := ssh.FingerprintSHA256(signer.PublicKey())
	if res == nil || res.LatencyMs < 0 || !strings.Contains(res.Message, "SSH-2.0-") || res.Fingerprint != want {
		t.Fatalf("unexpected SSH result: %+v, want fingerprint %s", res, want)
	}
	if request.Password != "***" || request.Fingerprint != "" {
		t.Fatal("probe modified the request")
	}
	saved, err := getAsset(request.Owner, request.Name)
	if err != nil || saved == nil || saved.Fingerprint != "" {
		t.Fatalf("probe unexpectedly modified stored fingerprint: %+v, err=%v", saved, err)
	}
}

func TestAssetKubernetes(t *testing.T) {
	engine := newAssetTestDB(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cluster/version" || r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unexpected probe", http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"gitVersion":"v1.30.0"}`)
	}))
	defer server.Close()

	asset := Asset{Owner: "org", Name: "cluster", Protocol: ProtocolKubernetes, Host: server.URL + "/cluster/", Password: "test-token"}
	if _, err := engine.Insert(&asset); err != nil {
		t.Fatal(err)
	}
	asset.Password = "***"
	want := getCertificateFingerprint(server.Certificate().RawSubjectPublicKeyInfo)
	for _, pin := range []string{"", want} {
		asset.Fingerprint = pin
		res, err := TestAsset(&asset)
		if err != nil {
			t.Fatal(err)
		}
		if res.Fingerprint != want || res.Message != "v1.30.0" || res.LatencyMs < 0 {
			t.Fatalf("unexpected Kubernetes result: %+v", res)
		}
		if asset.Fingerprint != pin || asset.Port != 0 || asset.Password != "***" {
			t.Fatal("probe modified the request")
		}
	}
	asset.Fingerprint = strings.Repeat("0", 64)
	if _, err := TestAsset(&asset); err == nil || !strings.Contains(err.Error(), "has changed") {
		t.Fatalf("mismatched pin: got %v, want key change error", err)
	}
	saved, err := getAsset(asset.Owner, asset.Name)
	if err != nil || saved == nil || saved.Fingerprint != "" {
		t.Fatalf("probe unexpectedly modified stored fingerprint: %+v, err=%v", saved, err)
	}
}

func TestAssetDesktop(t *testing.T) {
	engine := newAssetTestDB(t)
	for _, tt := range []struct {
		name        string
		gatewayType string
		enabled     bool
		want        string
	}{
		{"missing", "", false, "does not exist"},
		{"disabled", NetworkGatewayTypeDesktop, false, "disabled"},
		{"ssh", NetworkGatewayTypeSsh, true, "not a Casvisor Desktop device"},
		{"agent", NetworkGatewayTypeAgent, true, "not a Casvisor Desktop device"},
		{"offline", NetworkGatewayTypeDesktop, true, "has never connected"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			asset := &Asset{Owner: "org", Protocol: ProtocolDesktop, NetworkGateway: tt.name}
			if tt.gatewayType != "" {
				gateway := &NetworkGateway{Owner: asset.Owner, Name: tt.name, Type: tt.gatewayType, IsEnabled: tt.enabled, AgentKey: "test-key"}
				if _, err := engine.Insert(gateway); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := TestAsset(asset); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("TestAsset error = %v, want %q", err, tt.want)
			}
		})
	}
}
