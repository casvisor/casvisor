// Copyright 2024 The casbin Authors. All Rights Reserved.
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
	"net/url"
	"os"
)

var hostname = ""

func init() {
	name, err := os.Hostname()
	if err != nil {
		panic(err)
	}

	hostname = name
}

func GetHostname() string {
	return hostname
}

func QueryUnescape(s string) string {
	res, err := url.QueryUnescape(s)
	if err != nil {
		return ""
	}

	return res
}
