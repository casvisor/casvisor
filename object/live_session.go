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
	"slices"
	"sync"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
)

const permissionRevokedCode = 802

// LiveSession is who holds a connected session, kept so that the session can be
// checked again when permissions or assets change, or a permission expires.
type LiveSession struct {
	User     *casdoorsdk.User
	AssetId  string
	ClientIp string
	// OnActions passes the actions held now to a session that enforces them itself, like the SSH gateway
	OnActions func(actions []string)
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

		err = CloseSession(sessionId, permissionRevokedCode, message)
		if err != nil {
			logs.Error(fmt.Sprintf("CheckLiveSessions(): failed to close the session: %s: %s", sessionId, err.Error()))
		}
		removeLiveSession(sessionId)
		return true
	})
}

// check returns why the session has to end, or "" when it may go on.
func (liveSession *LiveSession) check() (string, error) {
	asset, err := GetAsset(liveSession.AssetId)
	if err != nil {
		return "", err
	}
	if asset == nil {
		return fmt.Sprintf("The asset: %s has been deleted", liveSession.AssetId), nil
	}

	actions, err := GetAssetActions(liveSession.User, asset, liveSession.ClientIp)
	if err != nil {
		return "", err
	}
	if !slices.Contains(actions, ActionConnect) {
		return fmt.Sprintf("The permission to connect to the asset: %s has been revoked", asset.Name), nil
	}

	if liveSession.OnActions != nil {
		liveSession.OnActions(actions)
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
