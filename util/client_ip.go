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
	"fmt"
	"net"
	"net/http"
	"strings"
)

// GetClientIp only trusts X-Forwarded-For when the request comes from a local reverse proxy,
// and then takes the last hop, which is the address the proxy saw.
var trustedProxies []*net.IPNet

// InitTrustedProxies sets the proxies whose X-Forwarded-For is believed, as a
// comma-separated list of IPs or CIDRs, besides this machine itself.
func InitTrustedProxies(list string) {
	trustedProxies = nil
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !strings.Contains(item, "/") {
			if ip := net.ParseIP(item); ip != nil && ip.To4() != nil {
				item += "/32"
			} else {
				item += "/128"
			}
		}
		_, network, err := net.ParseCIDR(item)
		if err != nil {
			panic(fmt.Errorf("invalid trustedProxies entry: %s", item))
		}
		trustedProxies = append(trustedProxies, network)
	}
}

func isTrustedProxy(ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}
	for _, network := range trustedProxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// GetClientIp returns the address of the client, read from X-Forwarded-For only
// as far back as the proxies in front of Casvisor are trusted.
func GetClientIp(req *http.Request) string {
	remoteIp := req.RemoteAddr
	if host, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		remoteIp = host
	}

	ip := net.ParseIP(remoteIp)
	if ip == nil {
		return ""
	}

	if isTrustedProxy(ip) {
		forwarded := strings.Split(req.Header.Get("X-Forwarded-For"), ",")
		for i := len(forwarded) - 1; i >= 0; i-- {
			hop := net.ParseIP(strings.TrimSpace(forwarded[i]))
			if hop == nil {
				break
			}
			ip = hop
			if !isTrustedProxy(hop) {
				break
			}
		}
	}

	return ip.String()
}
