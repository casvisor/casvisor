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

package recording

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/conf"
)

// StartRetention deletes the recordings older than recordingRetentionDays once
// an hour; 0 or empty keeps them forever.
func StartRetention() {
	go func() {
		for {
			deleteExpired()
			time.Sleep(time.Hour)
		}
	}()
}

func deleteExpired() {
	days := 0
	_, _ = fmt.Sscanf(conf.GetConfigString("recordingRetentionDays"), "%d", &days)
	if days <= 0 {
		return
	}

	deadline := time.Now().AddDate(0, 0, -days)
	root := getRoot()
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".cvr") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().After(deadline) {
			return nil
		}
		err = os.Remove(path)
		if err != nil {
			logs.Error(fmt.Sprintf("recording.deleteExpired(): %s", err.Error()))
		}
		return nil
	})

	// remove the day directories left empty
	entries, _ := filepath.Glob(filepath.Join(root, "*", "*"))
	for _, dir := range entries {
		_ = os.Remove(dir)
	}
}
