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
	"fmt"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
)

func (c *ApiController) checkOwner(owner string) bool {
	user := c.GetSessionUser()
	if user != nil && (user.Owner == "built-in" || user.Owner == owner) {
		return true
	}

	c.ResponseError(fmt.Sprintf("you cannot manage the resources of the organization: %s", owner))
	return false
}

func getUserId(user *casdoorsdk.User) string {
	return fmt.Sprintf("%s/%s", user.Owner, user.Name)
}

func (c *ApiController) isAdmin() bool {
	return object.IsAdminUser(c.GetSessionUser())
}

func (c *ApiController) getAuthzClientIp() string {
	return util.GetClientIp(c.Ctx.Request)
}
