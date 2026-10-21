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
	"strings"
)

// GetClientIp only trusts X-Forwarded-For when the request comes from a local reverse proxy,
// and then takes the last hop, which is the address the proxy saw.
func GetClientIp(req *http.Request) string {
	remoteIp := req.RemoteAddr
	if host, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		remoteIp = host
	}

	ip := net.ParseIP(remoteIp)
	if ip == nil {
		return ""
	}

	if ip.IsLoopback() || ip.IsPrivate() {
		forwarded := strings.Split(req.Header.Get("X-Forwarded-For"), ",")
		last := strings.TrimSpace(forwarded[len(forwarded)-1])
		if net.ParseIP(last) != nil {
			return last
		}
	}

	return ip.String()
}
