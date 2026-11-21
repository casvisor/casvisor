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
	"encoding/hex"
	"strings"
	"sync"
	"testing"
	"time"

	"xorm.io/xorm"
)

func resetSessionSharesForTest(t *testing.T) {
	t.Helper()
	prevNow := sessionShareNow
	sessionSharesMu.Lock()
	for _, s := range sessionShares {
		revokeLocked(s)
	}
	sessionSharesMu.Unlock()
	t.Cleanup(func() {
		sessionShareNow = prevNow
		sessionSharesMu.Lock()
		for _, s := range sessionShares {
			revokeLocked(s)
		}
		sessionSharesMu.Unlock()
	})
}

func TestSessionShareLifecycleAndTTL(t *testing.T) {
	resetSessionSharesForTest(t)

	base := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	now := base
	sessionShareNow = func() time.Time { return now }

	if _, err := CreateSessionShare(nil, 0); err == nil {
		t.Fatal("expected error for nil session")
	}
	if _, err := CreateSessionShare(&Session{Owner: "org1", Name: "s1", Status: Disconnected}, 0); err == nil {
		t.Fatal("expected error for disconnected session")
	}

	sess := &Session{Owner: "org1", Name: "s1", Asset: "org1/vm1", Creator: "org1/alice", Status: Connected}
	ttlCases := []struct {
		in   time.Duration
		want time.Duration
	}{
		{0, time.Hour},
		{-time.Minute, 5 * time.Minute},
		{time.Minute, 5 * time.Minute},
		{15 * time.Minute, 15 * time.Minute},
		{48 * time.Hour, 24 * time.Hour},
	}
	for _, tc := range ttlCases {
		share, err := CreateSessionShare(sess, tc.in)
		if err != nil {
			t.Fatalf("CreateSessionShare(%v): %v", tc.in, err)
		}
		if got := share.ExpiresAt.Sub(base); got != tc.want {
			t.Fatalf("TTL(%v) = %v, want %v", tc.in, got, tc.want)
		}
		if raw, err := hex.DecodeString(share.Token); err != nil || len(raw) != 32 {
			t.Fatalf("unexpected token %q (err=%v)", share.Token, err)
		}
		if len(share.Code) != 6 || share.Code != strings.ToUpper(share.Code) {
			t.Fatalf("unexpected code %q", share.Code)
		}
	}

	share, err := CreateSessionShare(sess, 15*time.Minute)
	if err != nil {
		t.Fatalf("CreateSessionShare: %v", err)
	}

	got, ctx, err := VerifySessionShare(share.Token, " "+strings.ToLower(share.Code)+" ", "org1")
	if err != nil {
		t.Fatalf("VerifySessionShare valid: %v", err)
	}
	if got.Code != "" || got.SessionId != "org1/s1" || got.Owner != "org1" || got.Creator != "org1/alice" {
		t.Fatalf("unexpected verified share: %+v", got)
	}
	if _, _, err := VerifySessionShare(share.Token, share.Code, "built-in"); err != nil {
		t.Fatalf("VerifySessionShare built-in: %v", err)
	}
	if _, _, err := VerifySessionShare(share.Token, share.Code, "org2"); err == nil {
		t.Fatal("expected error for cross-org viewer")
	}
	select {
	case <-ctx.Done():
		t.Fatal("share context already cancelled")
	default:
	}

	if _, _, err := VerifySessionShare(share.Token, "WRONG1", "org1"); err == nil {
		t.Fatal("expected error for wrong code")
	}
	if _, _, err := VerifySessionShare("missing", share.Code, "org1"); err == nil {
		t.Fatal("expected error for unknown token")
	}

	if RevokeSessionShare(share.Token, func(owner, creator string) bool { return false }) {
		t.Fatal("RevokeSessionShare succeeded when allow returned false")
	}
	if _, _, err := VerifySessionShare(share.Token, share.Code, "org1"); err != nil {
		t.Fatalf("share should remain valid after denied revoke: %v", err)
	}
	if !RevokeSessionShare(share.Token, func(owner, creator string) bool {
		return owner == "org1" && creator == "org1/alice"
	}) {
		t.Fatal("expected RevokeSessionShare to succeed")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("expected share context to close on revoke")
	}
	if _, _, err := VerifySessionShare(share.Token, share.Code, "org1"); err == nil {
		t.Fatal("expected revoked share to fail verification")
	}

	expiring, err := CreateSessionShare(sess, 10*time.Minute)
	if err != nil {
		t.Fatalf("CreateSessionShare: %v", err)
	}
	now = base.Add(10 * time.Minute)
	if _, _, err := VerifySessionShare(expiring.Token, expiring.Code, "org1"); err == nil {
		t.Fatal("expected expired share to fail verification")
	}
}

func TestSessionShareBruteForceAndCloseCleanup(t *testing.T) {
	resetSessionSharesForTest(t)

	sess := &Session{Owner: "org1", Name: "s2", Creator: "org1/bob", Status: Connected}
	share, err := CreateSessionShare(sess, time.Hour)
	if err != nil {
		t.Fatalf("CreateSessionShare: %v", err)
	}
	for i := 0; i < 15; i++ {
		if _, _, err := VerifySessionShare(share.Token, "BADCOD", "org2"); err == nil {
			t.Fatal("expected cross-org attempt to fail")
		}
	}
	if _, _, err := VerifySessionShare(share.Token, share.Code, "org1"); err != nil {
		t.Fatalf("cross-org attempts must not lock out valid org1 viewer: %v", err)
	}
	for i := 0; i < 10; i++ {
		if _, _, err := VerifySessionShare(share.Token, "BADCOD", "org1"); err == nil {
			t.Fatal("expected wrong code to fail")
		}
	}
	if _, _, err := VerifySessionShare(share.Token, share.Code, "org1"); err == nil {
		t.Fatal("expected share to be revoked after repeated wrong codes")
	}

	engine, err := xorm.NewEngine("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	engine.SetMaxOpenConns(1)
	prevAdapter := adapter
	t.Cleanup(func() {
		adapter = prevAdapter
		_ = engine.Close()
	})
	if err := engine.Sync2(new(Session)); err != nil {
		t.Fatalf("sync Session: %v", err)
	}
	adapter = &Adapter{driverName: "sqlite", dataSourceName: ":memory:", engine: engine}
	if _, err := AddSession(sess); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	liveShare, err := CreateSessionShare(sess, time.Hour)
	if err != nil {
		t.Fatalf("CreateSessionShare: %v", err)
	}
	_, ctx, err := VerifySessionShare(liveShare.Token, liveShare.Code, "org1")
	if err != nil {
		t.Fatalf("VerifySessionShare: %v", err)
	}
	if err := CloseSession(sess.GetId(), 0, "done"); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("expected CloseSession to cancel share context")
	}
	if _, _, err := VerifySessionShare(liveShare.Token, liveShare.Code, "org1"); err == nil {
		t.Fatal("expected closed session share to be invalidated")
	}
}

func TestSessionShareConcurrency(t *testing.T) {
	resetSessionSharesForTest(t)

	sess := &Session{Owner: "org1", Name: "concurrent", Creator: "org1/alice", Status: Connected}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s, err := CreateSessionShare(sess, 15*time.Minute)
			if err != nil {
				t.Errorf("CreateSessionShare: %v", err)
				return
			}
			_, _, _ = VerifySessionShare(s.Token, s.Code, "org1")
			_, _, _ = VerifySessionShare(s.Token, "BAD123", "org1")
			if idx%2 == 0 {
				RevokeSessionShare(s.Token, nil)
			}
		}(i)
	}
	wg.Wait()
	CloseSessionShareBySession(sess.GetId())
}
