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
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"

	"github.com/casvisor/casvisor/conf"
)

// A nil set trusts loopback only; a configured but empty set trusts no proxies.
type trustedProxySet []netip.Prefix

var trustedProxyCache struct {
	sync.RWMutex
	raw string
	set trustedProxySet
}

func getTrustedProxySet() trustedProxySet {
	raw := conf.GetConfigString("trustedProxies")
	trustedProxyCache.RLock()
	if raw == trustedProxyCache.raw {
		set := trustedProxyCache.set
		trustedProxyCache.RUnlock()
		return set
	}
	trustedProxyCache.RUnlock()

	trustedProxyCache.Lock()
	defer trustedProxyCache.Unlock()
	if raw != trustedProxyCache.raw {
		trustedProxyCache.set = parseTrustedProxySet(raw)
		trustedProxyCache.raw = raw
	}
	return trustedProxyCache.set
}

func parseTrustedProxySet(raw string) trustedProxySet {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	set := make(trustedProxySet, 0)
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			// Only prefixes wholly in mapped IPv4 space can be unmapped.
			if prefix.Addr().Is4In6() && prefix.Bits() >= 96 {
				prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
			}
			set = append(set, prefix.Masked())
		} else if addr, err := netip.ParseAddr(entry); err == nil && addr.Zone() == "" {
			addr = addr.Unmap()
			set = append(set, netip.PrefixFrom(addr, addr.BitLen()))
		}
	}
	return set
}

func (set trustedProxySet) contains(addr netip.Addr) bool {
	if set == nil {
		return addr.IsLoopback()
	}
	for _, prefix := range set {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func parseClientAddr(value string) netip.Addr {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		value = value[1 : len(value)-1]
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}
	}
	return addr
}

// GetClientIp follows X-Forwarded-For only as far as the trusted proxies go.
func GetClientIp(req *http.Request) string {
	if req == nil {
		return ""
	}

	peer := req.RemoteAddr
	if host, _, err := net.SplitHostPort(peer); err == nil {
		peer = host
	}
	current := parseClientAddr(peer)
	if !current.IsValid() {
		return ""
	}
	current = current.WithZone("").Unmap()

	forwarded := req.Header.Values("X-Forwarded-For")
	if len(forwarded) == 0 {
		return current.String()
	}
	trusted := getTrustedProxySet()
	if !trusted.contains(current) {
		return current.String()
	}

	for i := len(forwarded) - 1; i >= 0; i-- {
		hops := strings.Split(forwarded[i], ",")
		for j := len(hops) - 1; j >= 0; j-- {
			hop := parseClientAddr(hops[j])
			if !hop.IsValid() || hop.Zone() != "" {
				return current.String()
			}
			hop = hop.Unmap()
			if !trusted.contains(hop) {
				return hop.String()
			}
			current = hop
		}
	}
	return current.String()
}
