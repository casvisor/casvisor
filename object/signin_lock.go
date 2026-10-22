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
	"strings"
	"sync"
	"time"
)

type signinFailure struct {
	count       int
	lockedUntil time.Time
}

var (
	signinFailures   = map[string]*signinFailure{}
	signinFailureMux sync.Mutex
)

func getSigninLock(userId string, clientIp string) time.Time {
	signinFailureMux.Lock()
	defer signinFailureMux.Unlock()

	failure, ok := signinFailures[userId+"|"+clientIp]
	if !ok || time.Now().After(failure.lockedUntil) {
		return time.Time{}
	}
	return failure.lockedUntil
}

func addSigninFailure(userId string, clientIp string) {
	signinFailureMux.Lock()
	defer signinFailureMux.Unlock()

	key := userId + "|" + clientIp
	failure, ok := signinFailures[key]
	if !ok || (!failure.lockedUntil.IsZero() && time.Now().After(failure.lockedUntil)) {
		failure = &signinFailure{}
		signinFailures[key] = failure
	}

	failure.count++
	if failure.count >= maxSigninFailures {
		failure.lockedUntil = time.Now().Add(signinLockTime)
	}
}

func clearSigninFailures(userId string) {
	signinFailureMux.Lock()
	defer signinFailureMux.Unlock()

	for key := range signinFailures {
		if strings.HasPrefix(key, userId+"|") {
			delete(signinFailures, key)
		}
	}
}
