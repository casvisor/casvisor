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
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/casvisor/casvisor/util"
)

func getI18nFilePath(language string) string {
	return fmt.Sprintf("../web/src/locales/%s/data.json", language)
}

func readI18nFile(language string) *I18nData {
	s := util.ReadStringFromPath(getI18nFilePath(language))

	data := &I18nData{}
	err := util.JsonToStruct(s, data)
	if err != nil {
		panic(err)
	}
	return data
}

func writeI18nFile(language string, data *I18nData) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	err := encoder.Encode(data)
	if err != nil {
		panic(err)
	}

	util.WriteStringToPath(buf.String(), getI18nFilePath(language))
}

func addMissingData(data *I18nData, words *I18nData) {
	for namespace, pairs := range *words {
		if _, ok := (*data)[namespace]; !ok {
			(*data)[namespace] = map[string]string{}
		}

		for key, value := range pairs {
			if _, ok := (*data)[namespace][key]; !ok {
				(*data)[namespace][key] = value
			}
		}
	}
}
