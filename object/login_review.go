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
	"time"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

const (
	LoginReviewPending  = "Pending"
	LoginReviewApproved = "Approved"
	LoginReviewRejected = "Rejected"
	LoginReviewCanceled = "Canceled"
	LoginReviewExpired  = "Expired"
	LoginReviewUsed     = "Used"
)

// LoginReviewTimeout is how long a sign-in waits for a reviewer, and how long an approval stays usable.
const LoginReviewTimeout = 10 * time.Minute

// LoginReview is a sign-in that a login rule holds until an administrator approves it.
type LoginReview struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	UpdatedTime string `xorm:"varchar(100)" json:"updatedTime"`

	User         string `xorm:"varchar(100) index" json:"user"`
	ClientIp     string `xorm:"varchar(100)" json:"clientIp"`
	ClientIpDesc string `xorm:"varchar(200)" json:"clientIpDesc"`
	UserAgent    string `xorm:"varchar(500)" json:"userAgent"`
	Method       string `xorm:"varchar(20)" json:"method"`
	Rule         string `xorm:"varchar(100)" json:"rule"`
	Status       string `xorm:"varchar(20) index" json:"status"`
	ExpireTime   string `xorm:"varchar(100)" json:"expireTime"`
	Reviewer     string `xorm:"varchar(100)" json:"reviewer"`
	ReviewedTime string `xorm:"varchar(100)" json:"reviewedTime"`
	Comment      string `xorm:"varchar(500)" json:"comment"`
}

func (review *LoginReview) GetId() string {
	return fmt.Sprintf("%s/%s", review.Owner, review.Name)
}

func (review *LoginReview) isExpired(now time.Time) bool {
	expireTime, err := time.Parse(time.RFC3339, review.ExpireTime)
	return err != nil || now.After(expireTime)
}

func AddLoginReview(user *casdoorsdk.User, decision *LoginDecision, clientIp string, userAgent string, method string) (*LoginReview, error) {
	now := time.Now()
	if len(userAgent) > 500 {
		userAgent = userAgent[:500]
	}
	review := &LoginReview{
		Owner:        user.Owner,
		Name:         strings.ReplaceAll(util.GenerateId(), "-", ""),
		CreatedTime:  now.Format(time.RFC3339),
		UpdatedTime:  now.Format(time.RFC3339),
		User:         user.Name,
		ClientIp:     clientIp,
		ClientIpDesc: util.GetDescFromIP(clientIp),
		UserAgent:    userAgent,
		Method:       method,
		Rule:         decision.RuleName(),
		Status:       LoginReviewPending,
		ExpireTime:   now.Add(LoginReviewTimeout).Format(time.RFC3339),
	}
	_, err := adapter.engine.Insert(review)
	if err != nil {
		return nil, err
	}
	notifyLoginReview("created", review)
	return review, nil
}

func getLoginReview(owner string, name string) (*LoginReview, error) {
	if owner == "" || name == "" {
		return nil, nil
	}

	review := LoginReview{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&review)
	if err != nil {
		return nil, err
	}
	if !existed {
		return nil, nil
	}

	if (review.Status == LoginReviewPending || review.Status == LoginReviewApproved) && review.isExpired(time.Now()) {
		review.Status = LoginReviewExpired
		_, err = adapter.engine.ID(core.PK{owner, name}).Where("(status = ? or status = ?)", LoginReviewPending, LoginReviewApproved).Cols("status").Update(&LoginReview{Status: LoginReviewExpired})
		if err != nil {
			return nil, err
		}
	}
	return &review, nil
}

func GetLoginReview(id string) (*LoginReview, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	return getLoginReview(owner, name)
}

func GetLoginReviewCount(owner, status, field, value string) (int64, error) {
	expireLoginReviews(owner)
	session := GetSession(owner, -1, -1, field, value, "", "")
	return session.Count(&LoginReview{Status: status})
}

func GetPaginationLoginReviews(owner string, status string, offset, limit int, field, value, sortField, sortOrder string) ([]*LoginReview, error) {
	expireLoginReviews(owner)
	reviews := []*LoginReview{}
	session := GetSession(owner, offset, limit, field, value, sortField, sortOrder)
	err := session.Find(&reviews, &LoginReview{Status: status})
	if err != nil {
		return reviews, err
	}
	return reviews, nil
}

func expireLoginReviews(owner string) {
	query := adapter.engine.Where("(status = ? or status = ?) and expire_time < ?", LoginReviewPending, LoginReviewApproved, time.Now().Format(time.RFC3339))
	if owner != "" {
		query = query.And("owner = ?", owner)
	}
	_, _ = query.Cols("status").Update(&LoginReview{Status: LoginReviewExpired})
}

// ReviewLogin approves or rejects a held sign-in. The approved user still has to finish signing in
// within LoginReviewTimeout.
func ReviewLogin(id string, reviewer *casdoorsdk.User, approve bool, comment string) (*LoginReview, error) {
	review, err := GetLoginReview(id)
	if err != nil {
		return nil, err
	}
	if review == nil {
		return nil, fmt.Errorf("the login review: %s does not exist", id)
	}
	if review.Status != LoginReviewPending {
		return nil, fmt.Errorf("the login review: %s is %s, only a pending one can be reviewed", id, strings.ToLower(review.Status))
	}
	if reviewer.Owner == review.Owner && reviewer.Name == review.User {
		return nil, fmt.Errorf("you can not review your own sign-in")
	}

	rule, err := getLoginRule(review.Owner, review.Rule)
	if err != nil {
		return nil, err
	}
	allowed := IsAdminUser(reviewer) && (reviewer.Owner == "built-in" || reviewer.Owner == review.Owner)
	if rule != nil {
		allowed = rule.CanReviewLogin(reviewer)
	}
	if !allowed {
		return nil, fmt.Errorf("you are not a reviewer of the login rule: %s", review.Rule)
	}

	now := time.Now()
	status := LoginReviewRejected
	if approve {
		status = LoginReviewApproved
	}
	update := &LoginReview{
		Status:       status,
		Reviewer:     util.GetIdFromOwnerAndName(reviewer.Owner, reviewer.Name),
		ReviewedTime: now.Format(time.RFC3339),
		UpdatedTime:  now.Format(time.RFC3339),
		Comment:      comment,
		ExpireTime:   now.Add(LoginReviewTimeout).Format(time.RFC3339),
	}
	affected, err := adapter.engine.ID(core.PK{review.Owner, review.Name}).Where("status = ?", LoginReviewPending).
		Cols("status", "reviewer", "reviewed_time", "updated_time", "comment", "expire_time").Update(update)
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, fmt.Errorf("the login review: %s has just been changed by someone else", id)
	}

	review.Status, review.Reviewer, review.ReviewedTime, review.Comment = update.Status, update.Reviewer, update.ReviewedTime, update.Comment
	notifyLoginReview(strings.ToLower(status), review)
	return review, nil
}

// ConsumeLoginReview turns an approved review into a used one, once, and tells whether it did.
func ConsumeLoginReview(id string) (bool, error) {
	review, err := GetLoginReview(id)
	if err != nil || review == nil || review.Status != LoginReviewApproved {
		return false, err
	}

	affected, err := adapter.engine.ID(core.PK{review.Owner, review.Name}).Where("status = ?", LoginReviewApproved).
		Cols("status", "updated_time").Update(&LoginReview{Status: LoginReviewUsed, UpdatedTime: util.GetCurrentTime()})
	if err != nil {
		return false, err
	}
	return affected != 0, nil
}

func CancelLoginReview(id string) error {
	owner, name := util.GetOwnerAndNameFromId(id)
	_, err := adapter.engine.ID(core.PK{owner, name}).Where("status = ?", LoginReviewPending).
		Cols("status", "updated_time").Update(&LoginReview{Status: LoginReviewCanceled, UpdatedTime: util.GetCurrentTime()})
	return err
}

// WaitLoginReview waits until the review is no longer pending, or until it times out, and returns its status.
func WaitLoginReview(id string, done <-chan struct{}) (string, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		review, err := GetLoginReview(id)
		if err != nil {
			return "", err
		}
		if review == nil {
			return LoginReviewCanceled, nil
		}
		if review.Status != LoginReviewPending {
			return review.Status, nil
		}

		select {
		case <-done:
			_ = CancelLoginReview(id)
			return LoginReviewCanceled, nil
		case <-ticker.C:
		}
	}
}
