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

package routers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beego/beego/context"
	"github.com/casvisor/casvisor/object"
)

func TestConnectQueryExcludedFromAudit(t *testing.T) {
	const path = "/api/get-asset-ssh"
	for _, query := range []string{
		"sessionId=org/session&ticket=one-time&password=legacy",
		"sessionId=org/session&%74icket=one-time&password=&password=legacy",
		"sessionId=org/session&ticket=one-time&password=legacy%ZZ",
	} {
		r := httptest.NewRequest(http.MethodGet, path+"?"+query, nil)
		ctx := context.NewContext()
		ctx.Reset(httptest.NewRecorder(), r)
		redactConnectQuery(ctx)
		if r.URL.RawQuery != query || r.URL.Query().Get("ticket") != "one-time" {
			t.Fatal("redacting the log URI changed the query available to the handler")
		}
		if r.RequestURI != path+"?sessionId=org%2Fsession" {
			t.Fatal("access log URI did not retain only the session ID")
		}
		record, err := object.NewRecord(ctx)
		if err != nil || record.RequestUri != path+"?sessionId=org/session" {
			t.Fatal("audit record URI did not retain only the session ID")
		}
	}
}
