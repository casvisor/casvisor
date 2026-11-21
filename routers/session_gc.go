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
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/beego/beego/context"
	"github.com/beego/beego/logs"
	"github.com/beego/beego/session"
)

// RejectEncodedBody refuses compressed request bodies before beego copies them,
// since beego inflates a gzip body without any limit on its size.
func RejectEncodedBody(ctx *context.Context) {
	if ctx.Request.Header.Get("Content-Encoding") != "" {
		ctx.Output.SetStatus(http.StatusUnsupportedMediaType)
		_ = ctx.Output.Body([]byte("compressed request bodies are not supported"))
	}
}

// StartSessionFileCleaner removes, every hour, the session files that beego creates for
// each request without a cookie and that never got anything stored in them.
func StartSessionFileCleaner(savePath string) {
	empty, err := session.EncodeGob(map[interface{}]interface{}{})
	if err != nil {
		logs.Warning("failed to encode an empty session: %s", err.Error())
		return
	}

	go func() {
		for {
			cleanEmptySessionFiles(savePath, empty)
			time.Sleep(time.Hour)
		}
	}()
}

func cleanEmptySessionFiles(savePath string, empty []byte) {
	before := time.Now().Add(-time.Hour)
	files, err := filepath.Glob(filepath.Join(savePath, "?", "?", "*"))
	if err != nil {
		return
	}

	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil || info.IsDir() || info.ModTime().After(before) || info.Size() > int64(len(empty)) {
			continue
		}

		content, err := os.ReadFile(file)
		if err != nil || (len(content) != 0 && !bytes.Equal(content, empty)) {
			continue
		}
		_ = os.Remove(file)
	}
}
