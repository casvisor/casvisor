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

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

const (
	CommandReviewPending  = "Pending"
	CommandReviewApproved = "Approved"
	CommandReviewUsed     = "Used"
	CommandReviewRejected = "Rejected"
	CommandReviewCanceled = "Canceled"
	CommandReviewExpired  = "Expired"
)

// CommandReviewTimeout is how long a command waits for a reviewer, and how long an approval
// stays usable.
const CommandReviewTimeout = 10 * time.Minute

// CommandReview is a command that a Review rule holds until an administrator approves it.
type CommandReview struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	UpdatedTime string `xorm:"varchar(100)" json:"updatedTime"`

	Session  string `xorm:"varchar(200) index" json:"session"`
	Asset    string `xorm:"varchar(200) index" json:"asset"`
	User     string `xorm:"varchar(200) index" json:"user"`
	Operator string `xorm:"varchar(200)" json:"operator"`
	ClientIp string `xorm:"varchar(100)" json:"clientIp"`
	Protocol string `xorm:"varchar(20)" json:"protocol"`
	Text     string `xorm:"mediumtext" json:"text"`
	Rule     string `xorm:"varchar(200)" json:"rule"`

	Status       string `xorm:"varchar(20) index" json:"status"`
	ExpireTime   string `xorm:"varchar(100)" json:"expireTime"`
	Reviewer     string `xorm:"varchar(200)" json:"reviewer"`
	ReviewedTime string `xorm:"varchar(100)" json:"reviewedTime"`
	Comment      string `xorm:"varchar(500)" json:"comment"`
}

func (review *CommandReview) GetId() string {
	return fmt.Sprintf("%s/%s", review.Owner, review.Name)
}

func (review *CommandReview) isExpired(now time.Time) bool {
	expireTime, err := time.Parse(time.RFC3339, review.ExpireTime)
	return err != nil || now.After(expireTime)
}

// reviewWakers wakes the commands waiting in this process as soon as they are reviewed.
var reviewWakers sync.Map

func wakeCommandReview(id string) {
	if value, ok := reviewWakers.LoadAndDelete(id); ok {
		close(value.(chan struct{}))
	}
}

// AddCommandReview holds the command for review under the rule.
func AddCommandReview(command *Command, rule *CommandRule) (*CommandReview, error) {
	now := time.Now()
	review := &CommandReview{
		Owner:       command.Owner,
		Name:        strings.ReplaceAll(util.GenerateId(), "-", ""),
		CreatedTime: now.Format(time.RFC3339),
		UpdatedTime: now.Format(time.RFC3339),
		Session:     command.Session,
		Asset:       command.Asset,
		User:        command.User,
		Operator:    command.Operator,
		ClientIp:    command.ClientIp,
		Protocol:    command.Protocol,
		Text:        command.Text,
		Rule:        rule.GetId(),
		Status:      CommandReviewPending,
		ExpireTime:  now.Add(CommandReviewTimeout).Format(time.RFC3339),
	}
	_, err := adapter.engine.Insert(review)
	if err != nil {
		return nil, err
	}
	notifyCommandReview(review, true)
	return review, nil
}

func getCommandReview(owner string, name string) (*CommandReview, error) {
	if owner == "" || name == "" {
		return nil, nil
	}

	review := CommandReview{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&review)
	if err != nil {
		return nil, err
	}
	if !existed {
		return nil, nil
	}

	if (review.Status == CommandReviewPending || review.Status == CommandReviewApproved) && review.isExpired(time.Now()) {
		review.Status = CommandReviewExpired
		_, err = adapter.engine.ID(core.PK{owner, name}).Where("(status = ? or status = ?)", CommandReviewPending, CommandReviewApproved).
			Cols("status").Update(&CommandReview{Status: CommandReviewExpired})
		if err != nil {
			return nil, err
		}
	}
	return &review, nil
}

func GetCommandReview(id string) (*CommandReview, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	return getCommandReview(owner, name)
}

func expireCommandReviews(owner string) {
	query := adapter.engine.Where("(status = ? or status = ?) and expire_time < ?", CommandReviewPending, CommandReviewApproved, time.Now().Format(time.RFC3339))
	if owner != "" {
		query = query.And("owner = ?", owner)
	}
	_, _ = query.Cols("status").Update(&CommandReview{Status: CommandReviewExpired})
}

func GetCommandReviewCount(owner, status, field, value string) (int64, error) {
	expireCommandReviews(owner)
	session := GetSession(owner, -1, -1, field, value, "", "")
	return session.Count(&CommandReview{Status: status})
}

func GetPaginationCommandReviews(owner string, status string, offset, limit int, field, value, sortField, sortOrder string) ([]*CommandReview, error) {
	expireCommandReviews(owner)
	reviews := []*CommandReview{}
	session := GetSession(owner, offset, limit, field, value, sortField, sortOrder)
	err := session.Find(&reviews, &CommandReview{Status: status})
	if err != nil {
		return reviews, err
	}
	return reviews, nil
}

// ReviewCommand approves or rejects a held command. Neither the user of the session nor the
// administrator who typed the command may review it.
func ReviewCommand(id string, reviewer *casdoorsdk.User, approve bool, comment string) (*CommandReview, error) {
	review, err := GetCommandReview(id)
	if err != nil {
		return nil, err
	}
	if review == nil {
		return nil, fmt.Errorf("the command review: %s does not exist", id)
	}
	if review.Status != CommandReviewPending {
		return nil, fmt.Errorf("the command review: %s is %s, only a pending one can be reviewed", id, strings.ToLower(review.Status))
	}

	reviewerId := util.GetIdFromOwnerAndName(reviewer.Owner, reviewer.Name)
	if reviewerId == review.User || reviewerId == review.Operator {
		return nil, fmt.Errorf("you can not review your own command")
	}

	rule, err := GetCommandRule(review.Rule)
	if err != nil {
		return nil, err
	}
	allowed := IsAdminUser(reviewer) && (reviewer.Owner == "built-in" || reviewer.Owner == review.Owner)
	if rule != nil {
		allowed = rule.CanReviewCommand(reviewer)
	}
	if !allowed {
		return nil, fmt.Errorf("you are not a reviewer of the command rule: %s", review.Rule)
	}

	if runes := []rune(comment); len(runes) > 500 {
		comment = string(runes[:500])
	}
	now := time.Now()
	status := CommandReviewRejected
	if approve {
		status = CommandReviewApproved
	}
	update := &CommandReview{
		Status:       status,
		Reviewer:     reviewerId,
		ReviewedTime: now.Format(time.RFC3339),
		UpdatedTime:  now.Format(time.RFC3339),
		Comment:      comment,
		ExpireTime:   now.Add(CommandReviewTimeout).Format(time.RFC3339),
	}
	affected, err := adapter.engine.ID(core.PK{review.Owner, review.Name}).Where("status = ?", CommandReviewPending).
		Cols("status", "reviewer", "reviewed_time", "updated_time", "comment", "expire_time").Update(update)
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, fmt.Errorf("the command review: %s has just been changed by someone else", id)
	}

	review.Status, review.Reviewer, review.ReviewedTime, review.Comment, review.ExpireTime = update.Status, update.Reviewer, update.ReviewedTime, update.Comment, update.ExpireTime
	wakeCommandReview(review.GetId())
	notifyCommandReview(review, false)
	return review, nil
}

// ConsumeCommandReview turns an approved review into a used one, once, and tells whether it did.
func ConsumeCommandReview(id string) (bool, error) {
	review, err := GetCommandReview(id)
	if err != nil || review == nil || review.Status != CommandReviewApproved {
		return false, err
	}

	affected, err := adapter.engine.ID(core.PK{review.Owner, review.Name}).Where("status = ?", CommandReviewApproved).
		Cols("status", "updated_time").Update(&CommandReview{Status: CommandReviewUsed, UpdatedTime: util.GetCurrentTime()})
	if err != nil {
		return false, err
	}
	return affected != 0, nil
}

// CancelCommandReview withdraws a held command that is still waiting.
func CancelCommandReview(id string) error {
	owner, name := util.GetOwnerAndNameFromId(id)
	affected, err := adapter.engine.ID(core.PK{owner, name}).Where("status = ?", CommandReviewPending).
		Cols("status", "updated_time").Update(&CommandReview{Status: CommandReviewCanceled, UpdatedTime: util.GetCurrentTime()})
	if err == nil && affected != 0 {
		wakeCommandReview(id)
	}
	return err
}

// WaitCommandReview waits until the review is no longer pending, the waiter gives up through
// cancel, or the review times out. An approval is used up here, so the review comes back as
// Used when the command may run.
func WaitCommandReview(id string, cancel <-chan struct{}) (*CommandReview, error) {
	wake := make(chan struct{})
	reviewWakers.Store(id, wake)
	defer reviewWakers.CompareAndDelete(id, wake)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		review, err := GetCommandReview(id)
		if err != nil {
			return nil, err
		}
		if review == nil {
			return nil, fmt.Errorf("the command review: %s does not exist", id)
		}

		switch review.Status {
		case CommandReviewPending:
		case CommandReviewApproved:
			used, err := ConsumeCommandReview(id)
			if err != nil {
				return nil, err
			}
			if used {
				review.Status = CommandReviewUsed
				return review, nil
			}
			continue
		default:
			return review, nil
		}

		select {
		case <-cancel:
			err = CancelCommandReview(id)
			if err != nil {
				return nil, err
			}
			// a reviewer may have got there first
			return GetCommandReview(id)
		case <-wake:
		case <-ticker.C:
		}
	}
}

// GetReviewOutcome tells why a held command did not run, or "" when it was approved.
func (review *CommandReview) GetReviewOutcome() string {
	switch review.Status {
	case CommandReviewUsed:
		return ""
	case CommandReviewRejected:
		_, reviewer := util.GetOwnerAndNameFromIdNoCheck(review.Reviewer)
		if review.Comment != "" {
			return fmt.Sprintf("the command is rejected by %s: %s", reviewer, review.Comment)
		}
		return fmt.Sprintf("the command is rejected by %s", reviewer)
	case CommandReviewCanceled:
		return "the command is canceled while it waits for review"
	case CommandReviewExpired:
		return "the command is not reviewed in time"
	}
	return fmt.Sprintf("the review of the command is %s", strings.ToLower(review.Status))
}

// HoldCommand holds a command that a Review rule matched until it is reviewed, canceled or
// times out, and fills in the command for the record: its rule, its review and, when it may
// not run, why. onHeld is told the review once it is waiting.
func HoldCommand(command *Command, rule *CommandRule, cancel <-chan struct{}, onHeld func(review *CommandReview)) (bool, error) {
	command.Action = CommandActionReview
	command.Rule = rule.GetId()

	review, err := AddCommandReview(command, rule)
	if err != nil {
		command.Error = "the command is blocked because it can't be held for review"
		return false, err
	}
	command.Review = review.Name
	if onHeld != nil {
		onHeld(review)
	}

	review, err = WaitCommandReview(review.GetId(), cancel)
	if err != nil {
		command.Error = "the command is blocked because its review can't be read"
		return false, err
	}

	command.Error = review.GetReviewOutcome()
	if review.Reviewer != "" {
		command.Reviewer = review.Reviewer
	}
	return command.Error == "", nil
}

// GetHeldMessage is what the user sees while the command waits.
func GetHeldMessage(rule *CommandRule, review *CommandReview) string {
	return fmt.Sprintf("the command needs the approval of an administrator by the rule: %s, it waits for review (request %s) for up to %d minutes", rule.GetDisplayName(), review.Name, int(CommandReviewTimeout.Minutes()))
}

// FindCommandReview returns the review of the same text in the session that a request sent
// again goes by, for a request that can't wait for its review: one still waiting, one approved
// and not yet used, or one rejected in the last CommandReviewTimeout, so that a rejected
// request isn't asked again right away. nil means the request is to be held anew.
func FindCommandReview(owner string, session string, text string) (*CommandReview, error) {
	now := time.Now()
	since := now.Add(-2 * CommandReviewTimeout).Format(time.RFC3339)
	reviews := []*CommandReview{}
	err := adapter.engine.Where("owner = ? and session = ? and created_time > ? and (status = ? or status = ? or status = ?)", owner, session, since, CommandReviewPending, CommandReviewApproved, CommandReviewRejected).
		Desc("created_time").Limit(50).Find(&reviews)
	if err != nil {
		return nil, err
	}

	for _, review := range reviews {
		if review.Text != text {
			continue
		}
		if review.Status == CommandReviewRejected {
			reviewedTime, err := time.Parse(time.RFC3339, review.ReviewedTime)
			if err == nil && now.Sub(reviewedTime) < CommandReviewTimeout {
				return review, nil
			}
			continue
		}
		if !review.isExpired(now) {
			return review, nil
		}
	}
	return nil, nil
}

// CancelSessionCommandReviews withdraws the commands of an ended session that still wait.
func CancelSessionCommandReviews(session string) error {
	reviews := []*CommandReview{}
	err := adapter.engine.Where("session = ? and status = ?", session, CommandReviewPending).Find(&reviews)
	if err != nil {
		return err
	}
	for _, review := range reviews {
		err = CancelCommandReview(review.GetId())
		if err != nil {
			return err
		}
	}
	return nil
}
