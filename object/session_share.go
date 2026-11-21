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
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/casvisor/casvisor/util"
)

const shareAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

type SessionShare struct {
	Token       string             `json:"token"`
	Code        string             `json:"code,omitempty"`
	SessionId   string             `json:"sessionId"`
	Asset       string             `json:"asset,omitempty"`
	Owner       string             `json:"owner"`
	Creator     string             `json:"creator"`
	CreatedTime string             `json:"createdTime"`
	ExpiresAt   time.Time          `json:"expiresAt"`
	Revoked     bool               `json:"revoked,omitempty"`
	failures    int                `json:"-"`
	ctx         context.Context    `json:"-"`
	cancel      context.CancelFunc `json:"-"`
}

var (
	sessionShares   = map[string]*SessionShare{}
	sessionSharesMu sync.RWMutex
	sessionShareNow = time.Now
)

func revokeLocked(s *SessionShare) {
	s.Revoked = true
	s.cancel()
	delete(sessionShares, s.Token)
}

func activeShareLocked(token string) *SessionShare {
	s := sessionShares[token]
	if s != nil && (s.Revoked || !sessionShareNow().UTC().Before(s.ExpiresAt)) {
		revokeLocked(s)
		return nil
	}
	return s
}

func CreateSessionShare(session *Session, ttl time.Duration) (*SessionShare, error) {
	if session == nil || session.Owner == "" || session.Name == "" || session.Status != Connected {
		return nil, errors.New("the session is not connected")
	}
	if ttl == 0 {
		ttl = time.Hour
	}
	ttl = min(max(ttl, 5*time.Minute), 24*time.Hour)
	var raw [38]byte
	_, _ = rand.Read(raw[:])
	for i := 32; i < 38; i++ {
		raw[i] = shareAlphabet[raw[i]&31]
	}
	exp := sessionShareNow().UTC().Add(ttl).Truncate(time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), ttl)
	s := &SessionShare{Token: hex.EncodeToString(raw[:32]), Code: string(raw[32:]), SessionId: session.GetId(), Asset: session.Asset, Owner: session.Owner, Creator: session.Creator, CreatedTime: util.GetCurrentTime(), ExpiresAt: exp, ctx: ctx, cancel: cancel}
	sessionSharesMu.Lock()
	defer sessionSharesMu.Unlock()
	for k := range sessionShares {
		activeShareLocked(k)
	}
	out := *s
	sessionShares[s.Token] = s
	return &out, nil
}

func VerifySessionShare(token, code, viewerOwner string) (*SessionShare, context.Context, error) {
	norm := strings.ToUpper(strings.TrimSpace(code))
	sessionSharesMu.Lock()
	defer sessionSharesMu.Unlock()
	s := activeShareLocked(token)
	if s != nil && viewerOwner != "built-in" && viewerOwner != s.Owner {
		return nil, nil, errors.New("you cannot access a shared session of another organization")
	}
	if s == nil || len(norm) != 6 || subtle.ConstantTimeCompare([]byte(norm), []byte(s.Code)) != 1 {
		if s != nil {
			if s.failures++; s.failures >= 10 {
				revokeLocked(s)
			}
		}
		return nil, nil, errors.New("invalid or expired session share")
	}
	out := *s
	out.Code = ""
	return &out, s.ctx, nil
}

func RevokeSessionShare(token string, allow func(owner, creator string) bool) bool {
	sessionSharesMu.Lock()
	defer sessionSharesMu.Unlock()
	s := activeShareLocked(token)
	if s == nil || (allow != nil && !allow(s.Owner, s.Creator)) {
		return false
	}
	revokeLocked(s)
	return true
}

func CloseSessionShareBySession(sessionId string) {
	sessionSharesMu.Lock()
	defer sessionSharesMu.Unlock()
	for _, s := range sessionShares {
		if s.SessionId == sessionId {
			revokeLocked(s)
		}
	}
}
