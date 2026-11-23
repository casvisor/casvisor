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
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"math/big"
	"slices"
	"time"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

const (
	ShareModeView    = "View"
	ShareModeControl = "Control"

	maxShareMinutes      = 8 * 60
	maxShareCodeFailures = 5
)

// SessionShare lets other users of the organization join a connected session, to watch it or
// to type in it too, with a link and a code that its user hands them.
type SessionShare struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`

	Session string `xorm:"varchar(200) index" json:"session"`
	Asset   string `xorm:"varchar(200)" json:"asset"`
	Creator string `xorm:"varchar(200)" json:"creator"`
	// Users are the names of the users who may join, any user of the organization when empty
	Users          []string `xorm:"mediumtext" json:"users"`
	Mode           string   `xorm:"varchar(20)" json:"mode"`
	Code           string   `xorm:"varchar(20)" json:"code"`
	ExpireTime     string   `xorm:"varchar(100)" json:"expireTime"`
	IsRevoked      bool     `json:"isRevoked"`
	FailedAttempts int      `json:"failedAttempts"`
	JoinCount      int      `json:"joinCount"`
}

func (share *SessionShare) GetId() string {
	return util.GetIdFromOwnerAndName(share.Owner, share.Name)
}

func (share *SessionShare) isActive(now time.Time) bool {
	expireTime, err := time.Parse(time.RFC3339, share.ExpireTime)
	return !share.IsRevoked && err == nil && now.Before(expireTime)
}

// CanManage tells whether the user may see the code of the share and revoke it: the user of
// the session, or an administrator of the organization.
func (share *SessionShare) CanManage(user *casdoorsdk.User) bool {
	if user == nil {
		return false
	}
	if util.GetIdFromOwnerAndName(user.Owner, user.Name) == share.Creator {
		return true
	}
	return IsAdminUser(user) && (user.Owner == "built-in" || user.Owner == share.Owner)
}

// CanJoin tells whether the user is one the share is for.
func (share *SessionShare) CanJoin(user *casdoorsdk.User) bool {
	if user == nil || user.Owner != share.Owner {
		return false
	}
	if util.GetIdFromOwnerAndName(user.Owner, user.Name) == share.Creator {
		return false
	}
	return len(share.Users) == 0 || slices.Contains(share.Users, user.Name)
}

func getShareCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func getShareName() (string, error) {
	b := make([]byte, 12)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// AddSessionShare shares a connected session of the user, for the given minutes.
func AddSessionShare(session *Session, creator *casdoorsdk.User, users []string, mode string, minutes int) (*SessionShare, error) {
	if session == nil || session.Status != Connected {
		return nil, fmt.Errorf("only a connected session can be shared")
	}
	if mode != ShareModeView && mode != ShareModeControl {
		return nil, fmt.Errorf("invalid share mode: %s", mode)
	}
	if minutes <= 0 || minutes > maxShareMinutes {
		return nil, fmt.Errorf("a share lasts from 1 to %d minutes", maxShareMinutes)
	}

	cleaned := []string{}
	for _, user := range users {
		if user != "" && user != creator.Name && !slices.Contains(cleaned, user) {
			cleaned = append(cleaned, user)
		}
	}

	name, err := getShareName()
	if err != nil {
		return nil, err
	}
	code, err := getShareCode()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	share := &SessionShare{
		Owner:       session.Owner,
		Name:        name,
		CreatedTime: now.Format(time.RFC3339),
		Session:     session.GetId(),
		Asset:       session.Asset,
		Creator:     util.GetIdFromOwnerAndName(creator.Owner, creator.Name),
		Users:       cleaned,
		Mode:        mode,
		Code:        code,
		ExpireTime:  now.Add(time.Duration(minutes) * time.Minute).Format(time.RFC3339),
	}
	_, err = adapter.engine.Insert(share)
	if err != nil {
		return nil, err
	}
	return share, nil
}

func GetSessionShare(id string) (*SessionShare, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	if owner == "" || name == "" {
		return nil, nil
	}

	share := SessionShare{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&share)
	if err != nil || !existed {
		return nil, err
	}
	return &share, nil
}

// GetSessionShares returns the shares of a session that are still open.
func GetSessionShares(sessionId string) ([]*SessionShare, error) {
	shares := []*SessionShare{}
	err := adapter.engine.Where("session = ? and is_revoked = ? and expire_time > ?", sessionId, false, time.Now().Format(time.RFC3339)).
		Desc("created_time").Find(&shares)
	return shares, err
}

func RevokeSessionShare(id string) error {
	owner, name := util.GetOwnerAndNameFromId(id)
	_, err := adapter.engine.ID(core.PK{owner, name}).Cols("is_revoked").Update(&SessionShare{IsRevoked: true})
	return err
}

// IsSessionShareActive tells whether the share still lets the users who joined through it stay.
func IsSessionShareActive(id string) bool {
	share, err := GetSessionShare(id)
	if err != nil || share == nil || !share.isActive(time.Now()) {
		return false
	}
	session, err := GetConnSession(share.Session)
	return err == nil && session != nil && session.Status == Connected
}

// CheckSessionShare lets the user join through the share with the code. Too many wrong codes
// revoke the share.
func CheckSessionShare(id string, user *casdoorsdk.User, code string) (*SessionShare, error) {
	share, err := GetSessionShare(id)
	if err != nil {
		return nil, err
	}
	if share == nil || !share.CanJoin(user) {
		return nil, fmt.Errorf("the share does not exist, or it is not for you")
	}
	if !share.isActive(time.Now()) {
		return nil, fmt.Errorf("the share has expired or been revoked")
	}

	if subtle.ConstantTimeCompare([]byte(code), []byte(share.Code)) != 1 {
		share.FailedAttempts++
		cols := []string{"failed_attempts"}
		if share.FailedAttempts >= maxShareCodeFailures {
			share.IsRevoked = true
			cols = append(cols, "is_revoked")
		}
		_, err = adapter.engine.ID(core.PK{share.Owner, share.Name}).Cols(cols...).Update(share)
		if err != nil {
			return nil, err
		}
		if share.IsRevoked {
			return nil, fmt.Errorf("the code is wrong, and the share is revoked after %d wrong codes", maxShareCodeFailures)
		}
		return nil, fmt.Errorf("the code is wrong")
	}

	session, err := GetConnSession(share.Session)
	if err != nil {
		return nil, err
	}
	if session == nil || session.Status != Connected {
		return nil, fmt.Errorf("the shared session has ended")
	}

	_, err = adapter.engine.ID(core.PK{share.Owner, share.Name}).Incr("join_count").Update(&SessionShare{})
	if err != nil {
		return nil, err
	}
	return share, nil
}
