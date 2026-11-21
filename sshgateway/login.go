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

package sshgateway

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/object"
	"golang.org/x/crypto/ssh"
)

// handshakeConns lets an authentication step that waits for a person, such as a reviewer or a
// verification code, give the handshake more than its usual 30 seconds.
var handshakeConns sync.Map

func extendHandshake(meta ssh.ConnMetadata, d time.Duration) {
	if conn, ok := handshakeConns.Load(meta.RemoteAddr().String()); ok {
		_ = conn.(net.Conn).SetDeadline(time.Now().Add(d))
	}
}

// checkLoginRules applies the login rules once the public key or certificate is known. A rule that rejects the
// sign-in, holds it for review or asks for a verification code goes on with keyboard-interactive,
// which ssh clients show, after the client has proven that it has the private key.
func checkLoginRules(meta ssh.ConnMetadata, user *casdoorsdk.User, perms *ssh.Permissions) (*ssh.Permissions, error) {
	clientIp := getHost(meta.RemoteAddr())
	decision, err := object.CheckLoginRules(user, clientIp, object.LoginMethodSsh, time.Now())
	if err != nil {
		return nil, err
	}

	switch decision.Action {
	case object.LoginActionReject:
		return nil, &ssh.PartialSuccessError{Next: ssh.ServerAuthCallbacks{
			KeyboardInteractiveCallback: func(meta ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
				addLoginAudit(meta, user, "login-reject", fmt.Sprintf("SSH sign-in rejected by the login rule: %s", decision.RuleName()))
				_, _ = challenge("", fmt.Sprintf("casvisor: the sign-in is not allowed by the login rule: %s", decision.RuleName()), nil, nil)
				return nil, fmt.Errorf("rejected by the login rule: %s", decision.RuleName())
			},
		}}
	case object.LoginActionReview:
		return nil, &ssh.PartialSuccessError{Next: ssh.ServerAuthCallbacks{
			KeyboardInteractiveCallback: func(meta ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
				return waitLoginReview(meta, challenge, user, decision, perms)
			},
		}}
	case object.LoginActionMfa:
		if !object.IsLocalAuth() {
			return perms, nil
		}
		return nil, &ssh.PartialSuccessError{Next: ssh.ServerAuthCallbacks{
			KeyboardInteractiveCallback: func(meta ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
				return checkVerificationCode(meta, challenge, user, decision, perms)
			},
		}}
	}
	return perms, nil
}

func waitLoginReview(meta ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge, user *casdoorsdk.User, decision *object.LoginDecision, perms *ssh.Permissions) (*ssh.Permissions, error) {
	extendHandshake(meta, object.LoginReviewTimeout+time.Minute)
	review, err := object.AddLoginReview(user, decision, getHost(meta.RemoteAddr()), string(meta.ClientVersion()), object.LoginMethodSsh)
	if err != nil {
		return nil, err
	}
	addLoginAudit(meta, user, "login-review", fmt.Sprintf("SSH sign-in held for review %s by the login rule: %s", review.Name, decision.RuleName()))

	_, err = challenge("", fmt.Sprintf("casvisor: the login rule %s holds this sign-in until an administrator approves it (request %s, waiting up to %d minutes)...",
		decision.RuleName(), review.Name[:8], int(object.LoginReviewTimeout.Minutes())), nil, nil)
	if err != nil {
		_ = object.CancelLoginReview(review.GetId())
		return nil, err
	}

	status, err := object.WaitLoginReview(review.GetId(), nil)
	if err != nil {
		return nil, err
	}
	if status != object.LoginReviewApproved {
		_, _ = challenge("", fmt.Sprintf("casvisor: the sign-in was %s", strings.ToLower(status)), nil, nil)
		return nil, fmt.Errorf("the login review is %s", strings.ToLower(status))
	}

	used, err := object.ConsumeLoginReview(review.GetId())
	if err != nil || !used {
		return nil, fmt.Errorf("the approval has already been used")
	}
	return perms, nil
}

func checkVerificationCode(meta ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge, user *casdoorsdk.User, decision *object.LoginDecision, perms *ssh.Permissions) (*ssh.Permissions, error) {
	clientIp := getHost(meta.RemoteAddr())
	enabled, _, err := object.CheckLocalUserMfaCode(user.Owner, user.Name, "", clientIp)
	if err != nil {
		return nil, err
	}
	if !enabled {
		addLoginAudit(meta, user, "login-mfa-setup", fmt.Sprintf("SSH sign-in needs two-step verification by the login rule: %s", decision.RuleName()))
		_, _ = challenge("", "casvisor: turn on two-step verification in your account on the Casvisor website first", nil, nil)
		return nil, fmt.Errorf("two-step verification is not turned on")
	}

	extendHandshake(meta, 3*time.Minute)
	for attempt := 0; attempt < 3; attempt++ {
		answers, err := challenge("", "", []string{"Verification code: "}, []bool{true})
		if err != nil {
			return nil, err
		}
		if len(answers) != 1 {
			return nil, fmt.Errorf("no verification code")
		}

		_, ok, err := object.CheckLocalUserMfaCode(user.Owner, user.Name, strings.TrimSpace(answers[0]), clientIp)
		if err != nil {
			return nil, err
		}
		if ok {
			return perms, nil
		}
		_, _ = challenge("", "casvisor: the verification code is wrong or has been used", nil, nil)
	}
	return nil, fmt.Errorf("wrong verification code")
}

func addLoginAudit(meta ssh.ConnMetadata, user *casdoorsdk.User, action string, detail string) {
	object.AddAuditRecord(&object.AuditEvent{
		Organization: user.Owner,
		User:         user.Name,
		ClientIp:     getHost(meta.RemoteAddr()),
		UserAgent:    string(meta.ClientVersion()),
		Method:       "Login",
		Action:       action,
		Detail:       detail,
	})
}
