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
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/beego/beego/context"
	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/object"
)

// MetricsFilter serves /metrics to Prometheus when metricsToken is set, to requests
// that send it as "Authorization: Bearer <token>".
func MetricsFilter(ctx *context.Context) {
	if ctx.Request.URL.Path != "/metrics" {
		return
	}

	token := conf.GetConfigString("metricsToken")
	if token == "" {
		http.NotFound(ctx.ResponseWriter, ctx.Request)
		return
	}

	given, ok := strings.CutPrefix(ctx.Request.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(given), []byte(token)) != 1 {
		ctx.ResponseWriter.Header().Set("WWW-Authenticate", `Bearer realm="casvisor"`)
		http.Error(ctx.ResponseWriter, "unauthorized", http.StatusUnauthorized)
		return
	}

	text, err := object.GetMetricsText()
	if err != nil {
		http.Error(ctx.ResponseWriter, err.Error(), http.StatusInternalServerError)
		return
	}
	ctx.ResponseWriter.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = ctx.ResponseWriter.Write([]byte(text))
}
