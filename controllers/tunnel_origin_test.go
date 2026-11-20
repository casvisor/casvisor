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

package controllers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNativeUpgraderCheckOrigin(t *testing.T) {
	if nativeUpgrader.CheckOrigin(nil) {
		t.Fatal("nil request was allowed")
	}

	const host, ui = "api.example.com", "https://ui.example.net"
	tests := []struct {
		name           string
		host           string
		tls            bool
		allowedOrigins string
		origins        []string
		want           bool
	}{
		{"agent without Origin", host, false, "", nil, true},
		{"empty Origin", host, false, "", []string{""}, false},
		{"null Origin", host, false, "", []string{"null"}, false},
		{"duplicate Origin", host, false, "", []string{"http://api.example.com", "http://api.example.com"}, false},
		{"same HTTP origin", host, false, "", []string{"http://API.example.com"}, true},
		{"same HTTPS origin", host, true, "", []string{"https://api.example.com"}, true},
		{"HTTP default port", "api.example.com:80", false, "", []string{"http://api.example.com"}, true},
		{"reject port zero", "api.example.com:0", false, "", []string{"http://api.example.com:0"}, false},
		{"reject leading-zero port", "api.example.com:080", false, "", []string{"http://api.example.com:080"}, false},
		{"HTTPS default port through proxy", host, false, "", []string{"https://api.example.com:443"}, true},
		{"HTTPS nondefault port through proxy", host, false, "", []string{"https://api.example.com:8443"}, false},
		{"same IPv6 origin", "[::1]:19000", false, "", []string{"http://[::1]:19000"}, true},
		{"uncompressed IPv6 origin", "[::1]:19000", false, "", []string{"http://[0:0:0:0:0:0:0:1]:19000"}, true},
		{"IPv6 default port", "[::1]", true, "", []string{"https://[::1]:443"}, true},
		{"TLS rejects HTTP origin", host, true, "", []string{"http://api.example.com"}, false},
		{"cross-origin by default", host, false, "", []string{ui}, false},
		{"different port by default", "localhost:19000", false, "", []string{"http://localhost:18001"}, false},
		{"explicit dev frontend", "localhost:19000", false, "http://localhost:18001", []string{"http://localhost:18001"}, true},
		{"normalized allowlist", host, true, " *, , https://UI.example.net:443 ,", []string{ui}, true},
		{"allowlist different scheme", host, false, ui, []string{"http://ui.example.net"}, false},
		{"allowlist different port", host, false, ui, []string{ui + ":8443"}, false},
		{"prefix spoof", host, false, ui, []string{"https://notui.example.net"}, false},
		{"suffix spoof", host, false, ui, []string{ui + ".evil.example"}, false},
		{"dot spoof", host, false, ui, []string{"https://uiXexample.net"}, false},
		{"trailing dot is not normalized", host, false, ui, []string{ui + "."}, false},
		{"wildcard", host, false, "*", []string{ui}, false},
		{"wildcard subdomain", host, false, "https://*.example.net", []string{ui}, false},
		{"malformed allowlist entry", host, false, ui + "/", []string{ui}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("allowedOrigins", tt.allowedOrigins)
			scheme := "http"
			if tt.tls {
				scheme = "https"
			}
			r := httptest.NewRequest(http.MethodGet, scheme+"://"+tt.host+"/api/tunnel", nil)
			for _, origin := range tt.origins {
				r.Header.Add("Origin", origin)
			}
			if got := nativeUpgrader.CheckOrigin(r); got != tt.want {
				t.Fatalf("CheckOrigin(%q) = %v, want %v", tt.origins, got, tt.want)
			}
		})
	}
}

func TestNativeUpgraderRejectsMalformedOrigins(t *testing.T) {
	t.Setenv("allowedOrigins", "https://example.com")
	for _, origin := range []string{
		"ftp://example.com",
		"https://user@example.com",
		"https://example.com/",
		"https://example.com?",
		"https://example.com#",
		"https://example.com:55abc",
		"https://example.com:",
		"https://[not-an-ip]",
		"https://example.com, https://example.com",
	} {
		r := httptest.NewRequest(http.MethodGet, "https://example.com/api/tunnel", nil)
		r.Header.Set("Origin", origin)
		if nativeUpgrader.CheckOrigin(r) {
			t.Errorf("malformed Origin %q was allowed", origin)
		}
	}
}
