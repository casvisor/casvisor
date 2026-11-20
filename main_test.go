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

package main

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/beego/beego/context"
)

func TestNewCorsFilter(t *testing.T) {
	newCorsFilter(nil)(nil)
	newCorsFilter(nil)(context.NewContext())

	const ui = "https://ui.example.net"
	allowUI := []string{ui}
	tests := []struct {
		name            string
		method          string
		preflightMethod string
		allowedOrigins  []string
		origins         []string
		wantOrigin      string
		wantStatus      int
	}{
		{"no Origin", http.MethodOptions, "", nil, nil, "", 0},
		{"same origin", http.MethodGet, "", nil, []string{" http://EXAMPLE.com:19000 "}, "http://EXAMPLE.com:19000", 0},
		{"allowlist GET", http.MethodGet, "", allowUI, []string{ui}, ui, 0},
		{"allowlist OPTIONS", http.MethodOptions, "", allowUI, []string{ui}, ui, 0},
		{"allowlist preflight", http.MethodOptions, http.MethodGet, allowUI, []string{ui}, ui, http.StatusNoContent},
		{"disallowed GET", http.MethodGet, "", nil, []string{ui}, "", 0},
		{"disallowed preflight", http.MethodOptions, http.MethodGet, nil, []string{ui}, "", http.StatusForbidden},
		{"duplicate Origin", http.MethodOptions, http.MethodGet, allowUI, []string{ui, ui}, "", http.StatusForbidden},
		{"wildcard GET", http.MethodGet, "", []string{"*"}, []string{ui}, "", 0},
		{"wildcard preflight", http.MethodOptions, http.MethodGet, []string{"*"}, []string{ui}, "", http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, "http://example.com:19000/api/test", nil)
			for _, origin := range tt.origins {
				r.Header.Add("Origin", origin)
			}
			if tt.preflightMethod != "" {
				r.Header.Set("Access-Control-Request-Method", tt.preflightMethod)
			}
			w := httptest.NewRecorder()
			w.Header().Add("Vary", "Accept-Encoding")
			ctx := context.NewContext()
			ctx.Reset(w, r)
			newCorsFilter(tt.allowedOrigins)(ctx)
			if ctx.ResponseWriter.Started != (tt.wantStatus != 0) || (tt.wantStatus != 0 && w.Code != tt.wantStatus) {
				t.Fatalf("response started=%v, status=%d, want status %d (0 means no response)", ctx.ResponseWriter.Started, w.Code, tt.wantStatus)
			}

			res := w.Result()
			defer res.Body.Close()
			wantVary := []string{"Accept-Encoding", "Origin"}
			if tt.wantStatus != 0 {
				wantVary = append(wantVary, "Access-Control-Request-Method", "Access-Control-Request-Headers")
			}
			if got := res.Header.Values("Vary"); !slices.Equal(got, wantVary) {
				t.Errorf("Vary = %q, want %q", got, wantVary)
			}
			wantHeaders := map[string]string{
				"Access-Control-Allow-Origin":      tt.wantOrigin,
				"Access-Control-Allow-Credentials": "",
				"Access-Control-Expose-Headers":    "",
				"Access-Control-Allow-Methods":     "",
				"Access-Control-Allow-Headers":     "",
			}
			if tt.wantOrigin != "" {
				wantHeaders["Access-Control-Allow-Credentials"] = "true"
				wantHeaders["Access-Control-Expose-Headers"] = "Content-Length"
			}
			if tt.wantStatus == http.StatusNoContent {
				wantHeaders["Access-Control-Allow-Methods"] = "GET, POST, DELETE, PUT, PATCH, OPTIONS"
				wantHeaders["Access-Control-Allow-Headers"] = "Origin, X-Requested-With, Content-Type, Accept, Authorization"
			}
			for name, want := range wantHeaders {
				if got := res.Header.Get(name); got != want || (want == "" && len(res.Header.Values(name)) != 0) {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
		})
	}
}
