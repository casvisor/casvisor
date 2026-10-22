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

package object

import (
	"fmt"
	"strings"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/conf"
)

// IsUserAllowedToSignin checks the user against allowedUsers, a comma-separated list of "owner/name",
// for deployments whose Casdoor application is shared with other users.
func IsUserAllowedToSignin(userId string) bool {
	allowedUsers := conf.GetConfigString("allowedUsers")
	if allowedUsers == "" {
		return true
	}

	for _, allowedUser := range strings.Split(allowedUsers, ",") {
		if strings.TrimSpace(allowedUser) == userId {
			return true
		}
	}
	return false
}

// GetCasdoorUser reads a user from Casdoor with the application's credentials,
// for callers like the SSH gateway that have no sign-in token to take the user from.
func GetCasdoorUser(owner string, name string) (*casdoorsdk.User, error) {
	client := casdoorsdk.NewClient(
		conf.GetConfigString("casdoorEndpoint"),
		conf.GetConfigString("clientId"),
		conf.GetConfigString("clientSecret"),
		"",
		owner,
		conf.GetConfigString("casdoorApplication"),
	)

	user, err := client.GetUser(name)
	if err != nil {
		return nil, err
	}
	if user == nil || user.Name == "" {
		return nil, fmt.Errorf("the user: %s/%s does not exist", owner, name)
	}
	if user.IsForbidden || user.IsDeleted {
		return nil, fmt.Errorf("the user: %s/%s is forbidden to sign in", owner, name)
	}
	return user, nil
}
