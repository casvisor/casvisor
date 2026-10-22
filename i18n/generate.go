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

package i18n

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/casvisor/casvisor/util"
)

type I18nData map[string]map[string]string

var (
	reI18n         *regexp.Regexp
	reLabelKey     *regexp.Regexp
	reModeTitleKey *regexp.Regexp
)

func init() {
	reI18n = regexp.MustCompile(`i18next\.t\(\s*(?:"([^"\n]+)"|` + "`([^`$\\n]+)`" + `)\s*[,)]`)
	reLabelKey = regexp.MustCompile(`labelKey="([^"\n]+)"`)
	reModeTitleKey = regexp.MustCompile(`getModeTitleKey\("([^"\n]+)"`)
}

func getAllI18nStrings(fileContent string) []string {
	res := []string{}

	for _, match := range reI18n.FindAllStringSubmatch(fileContent, -1) {
		res = append(res, match[1]+match[2])
	}
	for _, match := range reLabelKey.FindAllStringSubmatch(fileContent, -1) {
		res = append(res, match[1])
	}
	for _, match := range reModeTitleKey.FindAllStringSubmatch(fileContent, -1) {
		key := match[1]
		res = append(res, key, strings.Replace(key, ":Edit ", ":New ", 1), strings.Replace(key, ":Edit ", ":View ", 1))
	}
	return res
}

func getAllSourceFilePaths() []string {
	path := "../web/src"

	res := []string{}
	err := filepath.Walk(path,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if info.IsDir() {
				if info.Name() == "locales" || info.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}

			ext := filepath.Ext(info.Name())
			if ext != ".ts" && ext != ".tsx" {
				return nil
			}

			res = append(res, path)
			return nil
		})
	if err != nil {
		panic(err)
	}

	return res
}

func parseToData() *I18nData {
	data := I18nData{}
	for _, path := range getAllSourceFilePaths() {
		fileContent := util.ReadStringFromPath(path)
		for _, word := range getAllI18nStrings(fileContent) {
			tokens := strings.SplitN(word, ":", 2)
			if len(tokens) != 2 || tokens[0] == "" || tokens[1] == "" {
				continue
			}

			namespace := tokens[0]
			key := tokens[1]
			if _, ok := data[namespace]; !ok {
				data[namespace] = map[string]string{}
			}
			data[namespace][key] = key
		}
	}

	return &data
}
