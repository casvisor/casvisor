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

package util

import (
	"net/http"
	"testing"
)

func TestGetClientIp(t *testing.T) {
	if got := GetClientIp(nil); got != "" {
		t.Errorf("GetClientIp(nil) = %q, want empty string", got)
	}
	if got := GetIPFromRequest(nil); got != "" {
		t.Errorf("GetIPFromRequest(nil) = %q, want empty string", got)
	}

	const proxies = " 10.0.0.0/8, , 192.0.2.10 , invalid "
	client := []string{"198.51.100.20"}
	tests := []struct {
		name      string
		proxies   string
		remote    string
		forwarded []string
		want      string
	}{
		{"IPv4 loopback", "", "127.0.0.1:1234", client, "198.51.100.20"},
		{"IPv6 loopback", "", "[::1]:1234", client, "198.51.100.20"},
		{"bare IPv6 loopback", "", "::1", client, "198.51.100.20"},
		{"zoned IPv6 peer", "", "[fe80::1%eth0]:1234", client, "fe80::1"},
		{"mapped IPv4 loopback", "  ", "[::ffff:127.0.0.1]", client, "198.51.100.20"},
		{"untrusted 10/8", "", "10.1.2.3:1234", client, "10.1.2.3"},
		{"untrusted 172.16/12", "", "172.16.0.1:1234", client, "172.16.0.1"},
		{"untrusted 192.168/16", "", "192.168.0.1:1234", client, "192.168.0.1"},
		{"untrusted public peer", "", "8.8.8.8:1234", client, "8.8.8.8"},
		{"untrusted mapped IPv4 peer", "", "[::ffff:192.168.0.1]:1234", client, "192.168.0.1"},
		{"invalid peer", "", "localhost:1234", client, ""},
		{"missing header", "", "127.0.0.1", nil, "127.0.0.1"},
		{"empty header", "", "127.0.0.1", []string{""}, "127.0.0.1"},
		{"peel CIDR and single IP", proxies, "10.1.2.3:1234", []string{"198.51.100.20, 10.0.0.2, 192.0.2.10"}, "198.51.100.20"},
		{"multiple headers and spoofed leftmost", proxies, "10.1.2.3:1234", []string{"203.0.113.1, 198.51.100.20", "10.0.0.2, 192.0.2.10"}, "198.51.100.20"},
		{"malformed hop stops peeling", proxies, "10.1.2.3:1234", []string{"198.51.100.20, invalid, 192.0.2.10"}, "192.0.2.10"},
		{"zoned IPv6 hop stops peeling", proxies, "10.1.2.3:1234", []string{"198.51.100.20, fe80::1%eth0, 192.0.2.10"}, "192.0.2.10"},
		{"zoned IPv6 loopback hop stops peeling", "", "[::1]:1234", []string{"198.51.100.20, ::1%zone"}, "::1"},
		{"empty hop stops peeling", proxies, "10.1.2.3:1234", []string{"198.51.100.20, , 192.0.2.10"}, "192.0.2.10"},
		{"reject hop with port", proxies, "10.1.2.3:1234", []string{"198.51.100.20, 192.0.2.10:8080"}, "10.1.2.3"},
		{"all trusted", proxies, "10.1.2.3:1234", []string{"10.0.0.2, 192.0.2.10"}, "10.0.0.2"},
		{"cache follows config change", "10.2.0.0/16", "10.1.2.3:1234", client, "10.1.2.3"},
		{"explicit config excludes loopback", proxies, "127.0.0.1:1234", client, "127.0.0.1"},
		{"invalid config fails closed", "not-a-cidr, 10.0.0.0/44", "127.0.0.1:1234", client, "127.0.0.1"},
		{"empty entries fail closed", " , ", "127.0.0.1:1234", client, "127.0.0.1"},
		{"IPv6 CIDR", "2001:db8:1::/64", "[2001:db8:1::3]:1234", []string{"[2001:db8:2::1], [2001:db8:1::2]"}, "2001:db8:2::1"},
		{"mapped CIDR", "::ffff:192.0.2.0/120", "192.0.2.10", []string{"198.51.100.20, ::ffff:192.0.2.11"}, "198.51.100.20"},
		{"mapped single IP", "::ffff:192.0.2.10", "192.0.2.10", []string{"[::ffff:198.51.100.20]"}, "198.51.100.20"},
		{"cache restores default", "", "127.0.0.1:1234", client, "198.51.100.20"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("trustedProxies", tt.proxies)
			r := &http.Request{RemoteAddr: tt.remote, Header: make(http.Header)}
			for _, forwarded := range tt.forwarded {
				r.Header.Add("X-Forwarded-For", forwarded)
			}
			if got := GetClientIp(r); got != tt.want {
				t.Errorf("GetClientIp = %q, want %q", got, tt.want)
			}
			wantInfo := tt.want
			if wantInfo != "" {
				wantInfo += ": "
			}
			if got := GetIPFromRequest(r); got != wantInfo {
				t.Errorf("GetIPFromRequest = %q, want %q", got, wantInfo)
			}
		})
	}
}
