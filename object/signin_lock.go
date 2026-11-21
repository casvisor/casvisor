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
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type signinFailure struct {
	count       int
	lockedUntil time.Time
	lastTime    time.Time
}

// maxSigninFailureEntries bounds the failures kept in memory; older ones are dropped.
const maxSigninFailureEntries = 100000

// getSigninKey ignores the case of the user name, as the database does when it looks it up.
func getSigninKey(userId string, clientIp string) string {
	return strings.ToLower(userId) + "|" + clientIp
}

var (
	signinFailures   = map[string]*signinFailure{}
	signinFailureMux sync.Mutex
)

func getSigninLock(userId string, clientIp string) time.Time {
	signinFailureMux.Lock()
	defer signinFailureMux.Unlock()

	failure, ok := signinFailures[getSigninKey(userId, clientIp)]
	if !ok || time.Now().After(failure.lockedUntil) {
		return time.Time{}
	}
	return failure.lockedUntil
}

func addSigninFailure(userId string, clientIp string) {
	signinFailureMux.Lock()
	defer signinFailureMux.Unlock()

	key := getSigninKey(userId, clientIp)
	failure, ok := signinFailures[key]
	if !ok || (!failure.lockedUntil.IsZero() && time.Now().After(failure.lockedUntil)) {
		if len(signinFailures) >= maxSigninFailureEntries {
			pruneSigninFailures()
		}
		failure = &signinFailure{}
		signinFailures[key] = failure
	}

	failure.count++
	failure.lastTime = time.Now()
	if failure.count >= maxSigninFailures {
		failure.lockedUntil = time.Now().Add(signinLockTime)
	}
}

func clearSigninFailures(userId string) {
	signinFailureMux.Lock()
	defer signinFailureMux.Unlock()

	prefix := strings.ToLower(userId) + "|"
	for key := range signinFailures {
		if strings.HasPrefix(key, prefix) {
			delete(signinFailures, key)
		}
	}
}

// pruneSigninFailures drops the failures that no longer lock anyone; the caller holds the lock.
func pruneSigninFailures() {
	now := time.Now()
	for key, failure := range signinFailures {
		if now.After(failure.lockedUntil) && now.Sub(failure.lastTime) > signinLockTime {
			delete(signinFailures, key)
		}
	}
	if len(signinFailures) >= maxSigninFailureEntries {
		for key, failure := range signinFailures {
			if now.After(failure.lockedUntil) {
				delete(signinFailures, key)
			}
		}
	}
}

// mfaFailureKey counts the wrong codes of a user from every client IP together: only
// someone who knows the password gets to enter a code, so this cannot lock out a user from
// elsewhere, while it keeps the six digits from being guessed from many IPs.
const mfaFailureKey = "mfa"

func getMfaLock(userId string) time.Time {
	return getSigninLock(userId, mfaFailureKey)
}

func addMfaFailure(userId string, clientIp string) {
	addSigninFailure(userId, clientIp)
	addSigninFailure(userId, mfaFailureKey)
}

// checkPasswordWithLock checks the password of a signed-in user for a change to their
// account, counting failures as a sign-in does.
func checkPasswordWithLock(user *LocalUser, password string, clientIp string) error {
	userId := user.GetId()
	if lockedUntil := getSigninLock(userId, clientIp); !lockedUntil.IsZero() {
		return fmt.Errorf("too many failed attempts, try again after %s", lockedUntil.Format("15:04:05"))
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		addSigninFailure(userId, clientIp)
		return fmt.Errorf("the password is wrong")
	}
	return nil
}
