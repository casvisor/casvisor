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

package util

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed 17monipdb.dat
var ipDbData embed.FS

func InitIpDb() {
	data, err := fs.ReadFile(ipDbData, "17monipdb.dat")
	if err != nil {
		panic(err)
	}

	InitWithData(data)
}

func GetDescFromIP(ip string) string {
	info, err := Find(ip)
	if err != nil {
		return ""
	}

	res := info.Country + ", " + info.Region + ", " + info.City
	if info.Isp != Null {
		res += ", " + info.Isp
	}

	return res
}

func GetIPInfo(clientIP string) string {
	if clientIP == "" {
		return ""
	}

	ips := strings.Split(clientIP, ",")
	res := ""
	for i := range ips {
		ip := strings.TrimSpace(ips[i])
		// desc := GetDescFromIP(ip)
		ipstr := fmt.Sprintf("%s: %s", ip, "")
		if i != len(ips)-1 {
			res += ipstr + " -> "
		} else {
			res += ipstr
		}
	}

	return res
}

func GetIPFromRequest(req *http.Request) string {
	return GetIPInfo(GetClientIp(req))
}
