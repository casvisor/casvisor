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
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/casvisor/casvisor/util"
	"github.com/skip2/go-qrcode"
	"xorm.io/core"
)

const totpPeriod = 30

var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// MfaSetup is a new TOTP secret for the user to add to an authenticator app.
type MfaSetup struct {
	Secret string `json:"secret"`
	Url    string `json:"url"`
	QrCode string `json:"qrCode"`
}

func GenerateMfaSetup(user *LocalUser) (*MfaSetup, error) {
	key := make([]byte, 20)
	_, err := rand.Read(key)
	if err != nil {
		return nil, err
	}
	secret := totpEncoding.EncodeToString(key)

	label := url.PathEscape(fmt.Sprintf("Casvisor:%s", user.Name))
	otpUrl := fmt.Sprintf("otpauth://totp/%s?secret=%s&issuer=Casvisor&algorithm=SHA1&digits=6&period=%d", label, secret, totpPeriod)
	png, err := qrcode.Encode(otpUrl, qrcode.Medium, 256)
	if err != nil {
		return nil, err
	}

	return &MfaSetup{Secret: secret, Url: otpUrl, QrCode: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)}, nil
}

func getTotpCode(key []byte, step int64) string {
	msg := make([]byte, 8)
	binary.BigEndian.PutUint64(msg, uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1000000)
}

// checkTotpCode returns the time step the code belongs to, allowing one step of clock drift either
// way, and rejects a step at or before lastStep so that a code works only once.
func checkTotpCode(secret string, code string, lastStep int64) (int64, bool) {
	key, err := totpEncoding.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil || len(key) == 0 {
		return 0, false
	}
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != 6 {
		return 0, false
	}

	now := time.Now().Unix() / totpPeriod
	for _, step := range []int64{now - 1, now, now + 1} {
		if step <= lastStep {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(getTotpCode(key, step)), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// EnableLocalUserMfa turns on two-factor sign-in once the user shows a code from the new secret.
func EnableLocalUserMfa(owner string, name string, secret string, code string) error {
	user, err := getLocalUser(owner, name)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("the user: %s/%s does not exist", owner, name)
	}
	if secret == "" {
		return fmt.Errorf("set up the authenticator app first")
	}

	step, ok := checkTotpCode(secret, code, 0)
	if !ok {
		return fmt.Errorf("the code is wrong or has expired")
	}

	encrypted, err := encryptCredential(secret)
	if err != nil {
		return err
	}
	_, err = adapter.engine.ID(core.PK{owner, name}).Cols("mfa_enabled", "totp_secret", "totp_last_step").
		Update(&LocalUser{MfaEnabled: true, TotpSecret: encrypted, TotpLastStep: step})
	return err
}

// DisableLocalUserMfa turns off two-factor sign-in after checking the user's password.
func DisableLocalUserMfa(owner string, name string, password string, clientIp string) error {
	user, err := getLocalUser(owner, name)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("the user: %s/%s does not exist", owner, name)
	}
	err = checkPasswordWithLock(user, password, clientIp)
	if err != nil {
		return err
	}
	return clearLocalUserMfa(owner, name)
}

func clearLocalUserMfa(owner string, name string) error {
	_, err := adapter.engine.ID(core.PK{owner, name}).Cols("mfa_enabled", "totp_secret", "totp_last_step").
		Update(&LocalUser{MfaEnabled: false, TotpSecret: "", TotpLastStep: 0})
	return err
}

// checkLocalUserMfa checks the second factor of a user whose password is right.
func checkLocalUserMfa(user *LocalUser, code string) (bool, error) {
	secret, err := decryptCredential(user.TotpSecret)
	if err != nil {
		return false, err
	}

	step, ok := checkTotpCode(secret, code, user.TotpLastStep)
	if !ok {
		return false, nil
	}

	// the step only moves forward, so of two sign-ins with the same code at once only one gets through
	affected, err := adapter.engine.ID(core.PK{user.Owner, user.Name}).And("totp_last_step is null or totp_last_step < ?", step).
		Cols("totp_last_step").Update(&LocalUser{TotpLastStep: step})
	if err != nil || affected == 0 {
		return false, err
	}
	user.TotpLastStep = step
	return true, nil
}

// CheckLocalUserMfaCode checks a verification code of a user who signed in another way, such as with a
// public key. An empty code only tells whether the user has two-step verification on.
func CheckLocalUserMfaCode(owner string, name string, code string, clientIp string) (enabled bool, ok bool, err error) {
	user, err := getLocalUser(owner, name)
	if err != nil || user == nil || !user.MfaEnabled {
		return false, false, err
	}
	if code == "" {
		return true, false, nil
	}

	userId := util.GetIdFromOwnerAndName(owner, name)
	if lockedUntil := getSigninLock(userId, clientIp); !lockedUntil.IsZero() {
		return true, false, fmt.Errorf("too many failed attempts, try again after %s", lockedUntil.Format("15:04:05"))
	}
	ok, err = checkLocalUserMfa(user, code)
	if err != nil {
		return true, false, err
	}
	if !ok {
		addSigninFailure(userId, clientIp)
	}
	return true, ok, nil
}

func ResetLocalUserMfa(owner string, name string) error {
	return clearLocalUserMfa(owner, name)
}
