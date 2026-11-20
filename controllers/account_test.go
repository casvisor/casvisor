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

package controllers

import (
	"maps"
	"os"
	"testing"

	"github.com/casvisor/casvisor/object"
)

func TestAccountExtra(t *testing.T) {
	for _, tt := range []struct {
		name          string
		watermark     string
		user          *object.LocalUser
		setupRequired bool
		want          map[string]bool
	}{
		{"default", "", nil, false, map[string]bool{"enableWatermark": false}},
		{"enabled", "true", nil, false, map[string]bool{"enableWatermark": true}},
		{"disabled", "false", nil, false, map[string]bool{"enableWatermark": false}},
		{"local MFA enabled", "false", &object.LocalUser{MfaEnabled: true}, true, map[string]bool{"enableWatermark": false, "mfaEnabled": true, "mfaSetupRequired": false}},
		{"local MFA disabled", "true", &object.LocalUser{MfaEnabled: false}, false, map[string]bool{"enableWatermark": true, "mfaEnabled": false, "mfaSetupRequired": false}},
		{"local MFA setup required", "true", &object.LocalUser{}, true, map[string]bool{"enableWatermark": true, "mfaEnabled": false, "mfaSetupRequired": true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("enableWatermark", tt.watermark)
			if tt.watermark == "" {
				if err := os.Unsetenv("enableWatermark"); err != nil {
					t.Fatal(err)
				}
			}
			if got := accountExtra(tt.user, tt.setupRequired); !maps.Equal(got, tt.want) {
				t.Errorf("accountExtra() = %v, want %v", got, tt.want)
			}
		})
	}
}
