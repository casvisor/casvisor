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
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/util"
)

// casdoorUserCheckInterval is how often the Casdoor user of a connected session is read
// again, so that a user deleted, forbidden or moved out of a group in Casdoor is cut off.
const casdoorUserCheckInterval = 5 * time.Minute

const PermissionRevokedCode = 802

// LiveSession is who holds a connected session, kept so that the session can be
// checked again when permissions or assets change, or a permission expires.
type LiveSession struct {
	User     *casdoorsdk.User
	AssetId  string
	Account  string
	ClientIp string
	// OnActions passes the actions held now to a session that enforces them itself, like the SSH gateway
	OnActions func(actions []string)
	// Operations are the file and clipboard operations the session was started with; a session
	// without OnActions is ended when one of them is revoked, to start again with what is left
	Operations []string

	userCheckedTime time.Time
}

var liveSessions sync.Map

func AddLiveSession(sessionId string, liveSession *LiveSession) {
	liveSessions.Store(sessionId, liveSession)
}

func removeLiveSession(sessionId string) {
	liveSessions.Delete(sessionId)
}

var checkMutex sync.Mutex

// CheckLiveSessions disconnects the sessions whose user may no longer connect to the asset.
func CheckLiveSessions() {
	checkMutex.Lock()
	defer checkMutex.Unlock()

	liveSessions.Range(func(key, value interface{}) bool {
		sessionId := key.(string)
		liveSession := value.(*LiveSession)

		message, err := liveSession.check()
		if err != nil {
			logs.Error(fmt.Sprintf("CheckLiveSessions(): failed to check the session: %s: %s", sessionId, err.Error()))
			return true
		}
		if message == "" {
			return true
		}

		err = CloseSession(sessionId, PermissionRevokedCode, message)
		if err != nil {
			logs.Error(fmt.Sprintf("CheckLiveSessions(): failed to close the session: %s: %s", sessionId, err.Error()))
		}
		removeLiveSession(sessionId)
		return true
	})
}

// check returns why the session has to end, or "" when it may go on.
func (liveSession *LiveSession) check() (string, error) {
	if IsLocalAuth() {
		user, err := getLocalUser(liveSession.User.Owner, liveSession.User.Name)
		if err != nil {
			return "", err
		}
		if user == nil || user.IsForbidden {
			return fmt.Sprintf("The user: %s has been deleted or forbidden", liveSession.User.Name), nil
		}
		liveSession.User = user.ToCasdoorUser()
	} else if time.Since(liveSession.userCheckedTime) >= casdoorUserCheckInterval {
		userId := util.GetIdFromOwnerAndName(liveSession.User.Owner, liveSession.User.Name)
		user, err := GetCasdoorUser(liveSession.User.Owner, liveSession.User.Name)
		if err != nil {
			if errors.Is(err, ErrUserNotAllowed) {
				return fmt.Sprintf("The user: %s has been deleted or forbidden", liveSession.User.Name), nil
			}
			// Casdoor could not be reached: keep the session and try again at the next check
			logs.Warning("failed to read the user: %s from Casdoor: %s", userId, err.Error())
		} else if !IsUserAllowedToSignin(userId) {
			return fmt.Sprintf("The user: %s is no longer allowed to sign in", liveSession.User.Name), nil
		} else {
			liveSession.User = user
			liveSession.userCheckedTime = time.Now()
		}
	}

	asset, err := GetAsset(liveSession.AssetId)
	if err != nil {
		return "", err
	}
	if asset == nil {
		return fmt.Sprintf("The asset: %s has been deleted", liveSession.AssetId), nil
	}

	actions, err := GetAssetAccountActions(liveSession.User, asset, &liveSession.Account, liveSession.ClientIp)
	if err != nil {
		return "", err
	}
	if !slices.Contains(actions, ActionConnect) {
		if liveSession.Account != "" {
			return fmt.Sprintf("The permission to connect to the asset: %s as the account: %s has been revoked, or the account has been removed", asset.Name, liveSession.Account), nil
		}
		return fmt.Sprintf("The permission to connect to the asset: %s has been revoked", asset.Name), nil
	}

	if liveSession.OnActions != nil {
		liveSession.OnActions(actions)
	} else {
		operations := GetSessionOperations(actions)
		for _, operation := range liveSession.Operations {
			if !slices.Contains(operations, operation) {
				return fmt.Sprintf("The permission to %s on the asset: %s has been revoked, connect again", operation, asset.Name), nil
			}
		}
	}
	return "", nil
}

// StartLiveSessionChecker checks the connected sessions every half a minute, for
// permissions that run out at their end time.
func StartLiveSessionChecker() {
	go func() {
		for range time.Tick(30 * time.Second) {
			CheckLiveSessions()
		}
	}()
}
