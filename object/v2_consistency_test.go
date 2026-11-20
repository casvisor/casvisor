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
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"golang.org/x/crypto/bcrypt"
	"xorm.io/xorm"
)

func setupTestAdapter(t *testing.T) *xorm.Engine {
	t.Helper()
	engine, err := xorm.NewEngine("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	engine.SetMaxOpenConns(1)
	previous := adapter
	t.Cleanup(func() {
		adapter = previous
		if err := engine.Close(); err != nil {
			t.Error(err)
		}
	})
	testAdapter := &Adapter{engine: engine}
	testAdapter.createTable()
	adapter = testAdapter
	return engine
}

func insertConsistencyTestRow(t *testing.T, row any) {
	t.Helper()
	affected, err := adapter.engine.Insert(row)
	if err != nil || affected != 1 {
		t.Fatalf("insert %T: affected=%d, err=%v", row, affected, err)
	}
}

func addTestAccessRequest(t *testing.T) *AccessRequest {
	t.Helper()
	asset := &Asset{Owner: "test", Name: "host", Protocol: "SSH"}
	insertConsistencyTestRow(t, asset)
	request := &AccessRequest{
		Owner:   asset.Owner,
		User:    "requester",
		Assets:  []string{asset.Name},
		Actions: []string{ActionConnect, ActionCopy},
		EndTime: time.Now().Add(time.Hour).Format(time.RFC3339),
		Reason:  "maintenance",
	}
	if added, err := AddAccessRequest(request); err != nil || !added {
		t.Fatalf("add access request: added=%t, err=%v", added, err)
	}
	return request
}

func loadTestAccessRequest(t *testing.T, request *AccessRequest) *AccessRequest {
	t.Helper()
	current, err := getAccessRequest(request.Owner, request.Name)
	if err != nil || current == nil {
		t.Fatalf("load access request %s: request=%v, err=%v", request.GetId(), current, err)
	}
	return current
}

func requireTestPermissionCount(t *testing.T, owner string, count int) []*Permission {
	t.Helper()
	permissions, err := GetPermissions(owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(permissions) != count {
		t.Fatalf("got %d permissions, want %d", len(permissions), count)
	}
	return permissions
}

func runConcurrentAttempts(n int, attempt func(int) error) []error {
	start := make(chan struct{})
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func() {
			defer wg.Done()
			<-start
			errs[i] = attempt(i)
		}()
	}
	close(start)
	wg.Wait()
	return errs
}

func requireSingleSuccessfulAttempt(t *testing.T, errs []error) int {
	t.Helper()
	winner := -1
	for i, err := range errs {
		if err != nil {
			continue
		}
		if winner != -1 {
			t.Fatalf("attempts %d and %d both succeeded: %v", winner, i, errs)
		}
		winner = i
	}
	if winner == -1 {
		t.Fatalf("no attempt succeeded: %v", errs)
	}
	return winner
}

func TestApproveAccessRequestConcurrency(t *testing.T) {
	setupTestAdapter(t)
	request := addTestAccessRequest(t)
	actions := []string{ActionConnect}
	errs := runConcurrentAttempts(12, func(_ int) error {
		candidate := *request
		_, err := ApproveAccessRequest(&candidate, "reviewer", "approved", actions, "")
		return err
	})
	requireSingleSuccessfulAttempt(t, errs)
	for _, err := range errs {
		if err != nil && err.Error() != "only pending requests can be approved, the request is Approved" {
			t.Errorf("unexpected approval error: %v", err)
		}
	}

	current := loadTestAccessRequest(t, request)
	wantPermission := "request_" + request.Name
	if current.Status != AccessRequestApproved || current.Permission != wantPermission {
		t.Fatalf("request status=%q, permission=%q; want Approved and %q", current.Status, current.Permission, wantPermission)
	}
	permissions := requireTestPermissionCount(t, request.Owner, 1)
	if permissions[0].Name != wantPermission || !slices.Equal(permissions[0].Actions, actions) || !slices.Equal(current.Actions, actions) {
		t.Fatalf("request actions=%v, permission=%+v; want %q with actions %v", current.Actions, permissions[0], wantPermission, actions)
	}
}

func TestApproveAccessRequestRaceWithRejectAndCancel(t *testing.T) {
	setupTestAdapter(t)
	request := addTestAccessRequest(t)
	attempts := []struct {
		status string
		action string
		apply  func(*AccessRequest) error
	}{
		{
			status: AccessRequestApproved,
			action: "approved",
			apply: func(candidate *AccessRequest) error {
				_, err := ApproveAccessRequest(candidate, "reviewer", "approved", nil, "")
				return err
			},
		},
		{
			status: AccessRequestRejected,
			action: "rejected",
			apply: func(candidate *AccessRequest) error {
				return RejectAccessRequest(candidate, "reviewer", "rejected")
			},
		},
		{status: AccessRequestCancelled, action: "cancelled", apply: CancelAccessRequest},
	}
	errs := runConcurrentAttempts(len(attempts), func(i int) error {
		candidate := *request
		return attempts[i].apply(&candidate)
	})
	winner := requireSingleSuccessfulAttempt(t, errs)
	current := loadTestAccessRequest(t, request)
	if current.Status != attempts[winner].status {
		t.Fatalf("request status=%q, want %q", current.Status, attempts[winner].status)
	}
	for i, err := range errs {
		want := fmt.Sprintf("only pending requests can be %s, the request is %s", attempts[i].action, current.Status)
		if err != nil && err.Error() != want {
			t.Errorf("%s error=%v, want %q", attempts[i].action, err, want)
		}
	}

	wantPermissions := 0
	if current.Status == AccessRequestApproved {
		wantPermissions = 1
	}
	requireTestPermissionCount(t, request.Owner, wantPermissions)
}

func TestApproveAccessRequestRollbackOnPermissionConflict(t *testing.T) {
	setupTestAdapter(t)
	request := addTestAccessRequest(t)
	insertConsistencyTestRow(t, &Permission{Owner: request.Owner, Name: "request_" + request.Name})
	before := *request
	before.Actions = slices.Clone(request.Actions)
	stored := loadTestAccessRequest(t, request)
	narrowedEnd := time.Now().Add(30 * time.Minute).Format(time.RFC3339)
	permission, err := ApproveAccessRequest(request, "reviewer", "approved", []string{ActionConnect}, narrowedEnd)
	if permission != nil || err == nil {
		t.Fatalf("approve with conflicting permission: permission=%v, err=%v", permission, err)
	}
	if !reflect.DeepEqual(*request, before) {
		t.Errorf("failed approval modified the caller: got %+v, want %+v", *request, before)
	}

	current := loadTestAccessRequest(t, request)
	if !reflect.DeepEqual(current, stored) {
		t.Errorf("failed approval changed the stored request: got %+v, want %+v", current, stored)
	}
	requireTestPermissionCount(t, request.Owner, 1)
}

func TestRevokeAccessRequestConcurrencyAndRollback(t *testing.T) {
	approve := func(t *testing.T) *AccessRequest {
		t.Helper()
		request := addTestAccessRequest(t)
		if _, err := ApproveAccessRequest(request, "reviewer", "original approval", nil, ""); err != nil {
			t.Fatal(err)
		}
		return request
	}

	t.Run("concurrent", func(t *testing.T) {
		setupTestAdapter(t)
		request := approve(t)
		errs := runConcurrentAttempts(12, func(_ int) error {
			candidate := *request
			candidate.Permission = "stale_permission"
			candidate.ReviewComment = "stale comment"
			return RevokeAccessRequest(&candidate, "revoker", "")
		})
		requireSingleSuccessfulAttempt(t, errs)
		for _, err := range errs {
			if err != nil && err.Error() != "only approved requests can be revoked, the request is Revoked" {
				t.Errorf("unexpected revocation error: %v", err)
			}
		}
		current := loadTestAccessRequest(t, request)
		if current.Status != AccessRequestRevoked || current.ReviewComment != request.ReviewComment {
			t.Errorf("request status=%q, comment=%q; want Revoked and %q", current.Status, current.ReviewComment, request.ReviewComment)
		}
		requireTestPermissionCount(t, request.Owner, 0)
	})

	t.Run("rollback", func(t *testing.T) {
		engine := setupTestAdapter(t)
		request := approve(t)
		before := *request
		stored := loadTestAccessRequest(t, request)
		requireTestPermissionCount(t, request.Owner, 1)
		if _, err := engine.Exec("CREATE TRIGGER fail_permission_delete BEFORE DELETE ON permission BEGIN SELECT RAISE(ABORT, 'permission deletion blocked'); END"); err != nil {
			t.Fatal(err)
		}
		if err := RevokeAccessRequest(request, "revoker", "revoked"); err == nil {
			t.Fatal("revocation succeeded despite a failing permission delete")
		}
		if !reflect.DeepEqual(*request, before) {
			t.Errorf("failed revocation modified the caller: got %+v, want %+v", *request, before)
		}
		current := loadTestAccessRequest(t, request)
		if current.Status != AccessRequestApproved || !reflect.DeepEqual(current, stored) {
			t.Errorf("failed revocation changed the stored request: got %+v, want %+v", current, stored)
		}
		requireTestPermissionCount(t, request.Owner, 1)
	})

	t.Run("empty stored permission ignores stale caller", func(t *testing.T) {
		setupTestAdapter(t)
		unrelated := &Permission{Owner: "test", Name: "unrelated-grant"}
		insertConsistencyTestRow(t, unrelated)
		request := &AccessRequest{
			Owner:  "test",
			Name:   "approved-no-perm",
			Status: AccessRequestApproved,
		}
		insertConsistencyTestRow(t, request)

		stale := *request
		stale.Permission = unrelated.Name
		if err := RevokeAccessRequest(&stale, "revoker", "done"); err != nil {
			t.Fatalf("revoke request with empty stored permission: %v", err)
		}
		if stale.Permission != "" || stale.Status != AccessRequestRevoked {
			t.Fatalf("revoked request=%+v, want empty permission and Revoked status", stale)
		}
		requireTestPermissionCount(t, "test", 1)
	})
}

func TestSigninLocalUserTotpReplayAtomic(t *testing.T) {
	engine := setupTestAdapter(t)
	t.Setenv("credentialKey", "test-credential-key")

	const fixedStep int64 = 60000000
	previousNow := totpNow
	totpNow = func() time.Time { return time.Unix(fixedStep*totpPeriod+10, 0) }
	t.Cleanup(func() { totpNow = previousNow })

	const password = "test-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}

	key := []byte("test-totp-key")
	secret, err := encryptCredential(totpEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	user := &LocalUser{
		Owner:        "test",
		Name:         "mfa-user",
		PasswordHash: string(hash),
		MfaEnabled:   true,
		TotpSecret:   secret,
	}
	insertConsistencyTestRow(t, user)
	t.Cleanup(func() { clearSigninFailures(user.GetId()) })

	signin := func(code, ip string) error {
		signedIn, err := SigninLocalUser(user.Owner, user.Name, password, code, ip)
		if err == nil && signedIn == nil {
			return fmt.Errorf("sign-in returned a nil user without an error")
		}
		return err
	}
	checkStep := func(want int64) {
		t.Helper()
		current, err := getLocalUser(user.Owner, user.Name)
		if err != nil || current == nil {
			t.Fatalf("load local user: user=%v, err=%v", current, err)
		}
		if current.TotpLastStep != want {
			t.Fatalf("stored TOTP step=%d, want %d", current.TotpLastStep, want)
		}
	}
	const replayError = "the verification code is wrong or has been used"
	code := getTotpCode(key, fixedStep)
	errs := runConcurrentAttempts(12, func(i int) error {
		return signin(code, fmt.Sprintf("192.0.2.%d", i+1))
	})
	requireSingleSuccessfulAttempt(t, errs)
	for _, err := range errs {
		if err != nil && err.Error() != replayError {
			t.Errorf("unexpected concurrent sign-in error: %v", err)
		}
	}
	checkStep(fixedStep)

	// A verifier that read the user before the winner must also reject the code.
	stale := *user
	ok, err := checkLocalUserMfa(&stale, code)
	if err != nil || ok || stale.TotpLastStep != user.TotpLastStep {
		t.Fatalf("stale TOTP check: ok=%t, step=%d, err=%v", ok, stale.TotpLastStep, err)
	}

	const clientIP = "192.0.2.100"
	if err := signin(code, clientIP); err == nil || err.Error() != replayError {
		t.Fatalf("sign-in replay: %v, want %q", err, replayError)
	}
	nextCode := getTotpCode(key, fixedStep+1)
	if err := signin(nextCode, clientIP); err != nil {
		t.Fatalf("sign-in with next step: %v", err)
	}
	checkStep(fixedStep + 1)

	if _, err := engine.Exec("UPDATE local_user SET totp_last_step = NULL WHERE owner = ? AND name = ?", user.Owner, user.Name); err != nil {
		t.Fatal(err)
	}
	if err := signin(nextCode, clientIP); err != nil {
		t.Fatalf("sign-in with a legacy NULL step: %v", err)
	}
	checkStep(fixedStep + 1)
	if err := signin(nextCode, clientIP); err == nil || err.Error() != replayError {
		t.Fatalf("replay after a legacy NULL step: %v, want %q", err, replayError)
	}
}

func TestMatchCommandRuleInvalidPatternFailClosed(t *testing.T) {
	setupTestAdapter(t)
	tests := []struct {
		name    string
		pattern string
		enabled bool
		wantErr string
	}{
		{name: "invalid", pattern: "[unclosed", enabled: true, wantErr: "invalid pattern in command rule"},
		{name: "empty", enabled: true, wantErr: "empty pattern in command rule"},
		{name: "disabled", pattern: "[unclosed"},
	}
	matchers := []struct {
		name  string
		match func(*casdoorsdk.User, *Asset, string) (*CommandRule, error)
	}{
		{name: "MatchCommandRule", match: MatchCommandRule},
		{name: "MatchSqlRule", match: MatchSqlRule},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			asset := &Asset{Owner: "test-" + test.name, Name: "db", Protocol: ProtocolMysql}
			user := &casdoorsdk.User{Owner: asset.Owner, Name: "user"}
			rule := &CommandRule{
				Owner:     asset.Owner,
				Name:      "malformed",
				Pattern:   test.pattern,
				Action:    CommandActionBlock,
				IsEnabled: test.enabled,
			}
			insertConsistencyTestRow(t, rule)
			for _, matcher := range matchers {
				matched, err := matcher.match(user, asset, "SELECT 1")
				if test.wantErr == "" {
					if matched != nil || err != nil {
						t.Errorf("%s did not ignore a disabled rule: rule=%v, err=%v", matcher.name, matched, err)
					}
					continue
				}
				want := test.wantErr + " " + rule.GetId()
				if matched != nil || err == nil || !strings.Contains(err.Error(), want) {
					t.Errorf("%s: rule=%v, err=%v; want nil and an error with %q", matcher.name, matched, err, want)
				}
			}
		})
	}

	t.Run("valid block still matches alongside malformed rule", func(t *testing.T) {
		asset := &Asset{Owner: "test-mixed", Name: "db", Protocol: ProtocolMysql}
		user := &casdoorsdk.User{Owner: asset.Owner, Name: "user"}
		insertConsistencyTestRow(t, &CommandRule{
			Owner:       asset.Owner,
			Name:        "malformed-newer",
			CreatedTime: "2026-01-02T00:00:00Z",
			Pattern:     "[unclosed",
			Action:      CommandActionBlock,
			IsEnabled:   true,
		})
		validBlock := &CommandRule{
			Owner:       asset.Owner,
			Name:        "block-drop",
			CreatedTime: "2026-01-01T00:00:00Z",
			Pattern:     "^DROP ",
			Action:      CommandActionBlock,
			IsEnabled:   true,
		}
		insertConsistencyTestRow(t, validBlock)

		matched, err := MatchCommandRule(user, asset, "DROP TABLE users")
		if err != nil || matched == nil || matched.Name != validBlock.Name {
			t.Fatalf("matching block alongside malformed rule: rule=%v, err=%v; want %q", matched, err, validBlock.Name)
		}
		matched, err = MatchCommandRule(user, asset, "SELECT 1")
		if matched != nil || err == nil || !strings.Contains(err.Error(), "invalid pattern in command rule "+asset.Owner+"/malformed-newer") {
			t.Fatalf("non-matching command with malformed rule: rule=%v, err=%v; want pattern error", matched, err)
		}
	})

	t.Run("missing user or asset fails closed", func(t *testing.T) {
		asset := &Asset{Owner: "test-missing", Name: "db", Protocol: ProtocolMysql}
		user := &casdoorsdk.User{Owner: asset.Owner, Name: "user"}
		for _, matcher := range matchers {
			if matched, err := matcher.match(nil, asset, "SELECT 1"); matched != nil || err == nil {
				t.Errorf("%s with nil user: rule=%v, err=%v; want nil rule and error", matcher.name, matched, err)
			}
			if matched, err := matcher.match(user, nil, "SELECT 1"); matched != nil || err == nil {
				t.Errorf("%s with nil asset: rule=%v, err=%v; want nil rule and error", matcher.name, matched, err)
			}
		}
	})
}
