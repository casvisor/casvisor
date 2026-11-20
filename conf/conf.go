// Copyright 2023 The casbin Authors. All Rights Reserved.
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

package conf

import (
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/beego/beego"
	"github.com/beego/beego/config"
)

// localConfigPath holds a server's own settings and secrets (dataSourceName, clientSecret, credentialKey, ...), which
// override conf/app.conf. It is not in git, so pulling a new version never conflicts with it.
const localConfigPath = "conf/app.local.conf"

func init() {
	if _, err := os.Stat(localConfigPath); err != nil {
		return
	}

	local, err := config.NewConfig("ini", localConfigPath)
	if err != nil {
		panic(err)
	}
	items, err := local.GetSection("default")
	if err != nil {
		panic(err)
	}
	for key, value := range items {
		err = beego.AppConfig.Set(key, value)
		if err != nil {
			panic(err)
		}
	}

	if value, ok := items["httpport"]; ok {
		beego.BConfig.Listen.HTTPPort, err = strconv.Atoi(value)
		if err != nil {
			panic(err)
		}
	}
}

func GetConfigString(key string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}

	res := beego.AppConfig.String(key)
	if res == "" {
		if key == "staticBaseUrl" {
			res = "https://cdn.casbin.org"
		} else if key == "logConfig" {
			res = "{\"filename\": \"logs/casdoor.log\", \"maxdays\":99999, \"perm\":\"0770\"}"
		}
	}

	return res
}

func GetConfigBool(key string) bool {
	value := GetConfigString(key)
	if value == "true" {
		return true
	} else {
		return false
	}
}

func GetAllowedOrigins() []string {
	return splitConfigList(GetConfigString("allowedOrigins"))
}

func GetTrustedProxies() []string {
	return splitConfigList(GetConfigString("trustedProxies"))
}

func splitConfigList(value string) []string {
	var entries []string
	for _, entry := range strings.Split(value, ",") {
		if entry = strings.TrimSpace(entry); entry != "" {
			entries = append(entries, entry)
		}
	}
	return entries
}

// IsOriginAllowed checks a browser origin against the request host and the configured allowlist.
func IsOriginAllowed(r *http.Request, allowedOrigins []string) bool {
	if r == nil {
		return false
	}
	origins := r.Header.Values("Origin")
	if len(origins) != 1 {
		return false
	}
	incoming, ok := parseOrigin(origins[0])
	if !ok {
		return false
	}

	// HTTPS can also reach a plain HTTP listener through a TLS-terminating proxy.
	if r.TLS == nil || incoming.scheme == "https" {
		if same, ok := parseOrigin(incoming.scheme + "://" + r.Host); ok && incoming == same {
			return true
		}
	}
	for _, entry := range allowedOrigins {
		if allowed, ok := parseOrigin(entry); ok && incoming == allowed {
			return true
		}
	}
	return false
}

type origin struct {
	scheme string
	host   string
	port   string
}

func parseOrigin(value string) (origin, bool) {
	value = strings.TrimSpace(value)
	// Reject empty query/fragment delimiters, combined origins and wildcards.
	if strings.ContainsAny(value, "?#,*") {
		return origin{}, false
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" {
		return origin{}, false
	}

	host, port := u.Hostname(), u.Port()
	if host == "" || strings.HasSuffix(u.Host, ":") {
		return origin{}, false
	}
	if strings.HasPrefix(u.Host, "[") {
		ip, err := netip.ParseAddr(host)
		if err != nil || !ip.Is6() || ip.Zone() != "" {
			return origin{}, false
		}
		host = ip.String()
	} else if strings.ContainsAny(host, ":[]<>\"%") {
		return origin{}, false
	}

	if port != "" {
		if p, err := strconv.ParseUint(port, 10, 16); err != nil || p == 0 || port[0] == '0' {
			return origin{}, false
		}
	} else if u.Scheme == "https" {
		port = "443"
	} else {
		port = "80"
	}
	return origin{scheme: u.Scheme, host: strings.ToLower(host), port: port}, true
}

func GetConfigDataSourceName() string {
	dataSourceName := GetConfigString("dataSourceName")

	runningInDocker := os.Getenv("RUNNING_IN_DOCKER")
	if runningInDocker == "true" {
		// https://stackoverflow.com/questions/48546124/what-is-linux-equivalent-of-host-docker-internal
		if runtime.GOOS == "linux" {
			dataSourceName = strings.ReplaceAll(dataSourceName, "localhost", "172.17.0.1")
		} else {
			dataSourceName = strings.ReplaceAll(dataSourceName, "localhost", "host.docker.internal")
		}
	}

	return dataSourceName
}

func GetLanguage(language string) string {
	if language == "" || language == "*" {
		return "en"
	}

	if len(language) != 2 || language == "nu" {
		return "en"
	} else {
		return language
	}
}
