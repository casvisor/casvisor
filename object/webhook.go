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
	"net/url"
	"slices"
	"strings"
	"unicode"

	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

const (
	WebhookTypeJson     = "JSON"
	WebhookTypeDingTalk = "DingTalk"
	WebhookTypeFeishu   = "Feishu"
	WebhookTypeWeCom    = "WeCom"
	WebhookTypeSlack    = "Slack"
	WebhookTypeEmail    = "Email"
)

var webhookTypes = []string{WebhookTypeJson, WebhookTypeDingTalk, WebhookTypeFeishu, WebhookTypeWeCom, WebhookTypeSlack, WebhookTypeEmail}

const (
	WebhookEventCommandBlocked        = "command.blocked"
	WebhookEventCommandAlerted        = "command.alerted"
	WebhookEventAccessRequestCreated  = "access_request.created"
	WebhookEventAccessRequestReviewed = "access_request.reviewed"
	WebhookEventLoginReviewCreated    = "login_review.created"
	WebhookEventLoginReviewReviewed   = "login_review.reviewed"
	WebhookEventCommandReviewCreated  = "command_review.created"
	WebhookEventCommandReviewReviewed = "command_review.reviewed"
	WebhookEventJobSucceeded          = "job.succeeded"
	WebhookEventJobFailed             = "job.failed"
	WebhookEventJobScheduleMissed     = "job_schedule.missed"
	WebhookEventAccountRiskFound      = "account_risk.found"
	WebhookEventAccountBackupFailed   = "account_backup.failed"
	WebhookEventReportFailed          = "report.failed"
	WebhookEventTest                  = "test"
)

var webhookEvents = []string{
	WebhookEventCommandBlocked, WebhookEventCommandAlerted,
	WebhookEventAccessRequestCreated, WebhookEventAccessRequestReviewed,
	WebhookEventLoginReviewCreated, WebhookEventLoginReviewReviewed,
	WebhookEventCommandReviewCreated, WebhookEventCommandReviewReviewed,
	WebhookEventJobSucceeded, WebhookEventJobFailed, WebhookEventJobScheduleMissed,
	WebhookEventAccountRiskFound, WebhookEventAccountBackupFailed, WebhookEventReportFailed,
}

// Webhook posts the events of its organization that it subscribes to, to a URL: as JSON, or as a
// text message to a DingTalk, Feishu, WeCom or Slack bot, or emails them with an SMTP server.
type Webhook struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	UpdatedTime string `xorm:"varchar(100)" json:"updatedTime"`
	DisplayName string `xorm:"varchar(100)" json:"displayName"`
	Description string `xorm:"varchar(500)" json:"description"`

	Type   string `xorm:"varchar(20)" json:"type"`
	Url    string `xorm:"varchar(1000)" json:"url"`
	Secret string `xorm:"varchar(500)" json:"secret"`

	// the SMTP server of the Email type, whose password is the secret
	SmtpHost     string   `xorm:"varchar(200)" json:"smtpHost"`
	SmtpPort     int      `json:"smtpPort"`
	SmtpUsername string   `xorm:"varchar(200)" json:"smtpUsername"`
	SmtpFrom     string   `xorm:"varchar(200)" json:"smtpFrom"`
	Recipients   []string `xorm:"varchar(2000)" json:"recipients"`

	Events    []string `xorm:"varchar(1000)" json:"events"`
	IsEnabled bool     `json:"isEnabled"`
}

func (webhook *Webhook) GetId() string {
	return fmt.Sprintf("%s/%s", webhook.Owner, webhook.Name)
}

func GetWebhookCount(owner, field, value string) (int64, error) {
	session := GetSession(owner, -1, -1, field, value, "", "")
	return session.Count(&Webhook{})
}

func GetWebhooks(owner string) ([]*Webhook, error) {
	webhooks := []*Webhook{}
	err := adapter.engine.Desc("created_time").Find(&webhooks, &Webhook{Owner: owner})
	if err != nil {
		return webhooks, err
	}
	return webhooks, nil
}

func GetPaginationWebhooks(owner string, offset, limit int, field, value, sortField, sortOrder string) ([]*Webhook, error) {
	webhooks := []*Webhook{}
	session := GetSession(owner, offset, limit, field, value, sortField, sortOrder)
	err := session.Find(&webhooks)
	if err != nil {
		return webhooks, err
	}
	return webhooks, nil
}

func getWebhook(owner string, name string) (*Webhook, error) {
	if owner == "" || name == "" {
		return nil, nil
	}

	webhook := Webhook{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&webhook)
	if err != nil {
		return nil, err
	}
	if !existed {
		return nil, nil
	}

	webhook.Secret, err = decryptCredential(webhook.Secret)
	if err != nil {
		return nil, err
	}
	return &webhook, nil
}

func GetWebhook(id string) (*Webhook, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	return getWebhook(owner, name)
}

// GetMaskedWebhooks hides the secrets of webhooks, which are only written.
func GetMaskedWebhooks(webhooks []*Webhook) []*Webhook {
	for _, webhook := range webhooks {
		if webhook != nil && webhook.Secret != "" {
			webhook.Secret = "***"
		}
	}
	return webhooks
}

func (webhook *Webhook) check() error {
	if !slices.Contains(webhookTypes, webhook.Type) {
		return fmt.Errorf("invalid webhook type: %s", webhook.Type)
	}

	if webhook.Type == WebhookTypeEmail {
		webhook.Url = ""
		webhook.SmtpHost = strings.TrimSpace(webhook.SmtpHost)
		webhook.SmtpUsername = strings.TrimSpace(webhook.SmtpUsername)
		webhook.SmtpFrom = strings.TrimSpace(webhook.SmtpFrom)
		recipients := []string{}
		for _, recipient := range webhook.Recipients {
			for _, address := range strings.FieldsFunc(recipient, func(r rune) bool { return r == ',' || r == ';' || unicode.IsSpace(r) }) {
				if !slices.Contains(recipients, address) {
					recipients = append(recipients, address)
				}
			}
		}
		webhook.Recipients = recipients
		err := webhook.checkMail()
		if err != nil {
			return err
		}
	} else {
		webhook.SmtpHost, webhook.SmtpPort, webhook.SmtpUsername, webhook.SmtpFrom, webhook.Recipients = "", 0, "", "", []string{}
		u, err := url.Parse(webhook.Url)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("invalid webhook URL: %s, it should start with http:// or https://", webhook.Url)
		}
	}

	for _, event := range webhook.Events {
		if !slices.Contains(webhookEvents, event) {
			return fmt.Errorf("invalid webhook event: %s", event)
		}
	}
	return nil
}

// fillSavedSecret keeps the saved secret when the form sends it masked.
func (webhook *Webhook) fillSavedSecret(saved *Webhook) {
	if webhook.Secret == "***" {
		webhook.Secret = ""
		if saved != nil {
			webhook.Secret = saved.Secret
		}
	}
}

func (webhook *Webhook) save(update func(*Webhook) (int64, error)) (bool, error) {
	err := webhook.check()
	if err != nil {
		return false, err
	}

	stored := *webhook
	stored.Secret, err = encryptCredential(webhook.Secret)
	if err != nil {
		return false, err
	}
	affected, err := update(&stored)
	if err != nil {
		return false, err
	}
	return affected != 0, nil
}

func UpdateWebhook(id string, webhook *Webhook) (bool, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	old, err := getWebhook(owner, name)
	if err != nil {
		return false, err
	} else if old == nil {
		return false, nil
	}

	webhook.fillSavedSecret(old)
	webhook.UpdatedTime = util.GetCurrentTime()
	return webhook.save(func(stored *Webhook) (int64, error) {
		return adapter.engine.ID(core.PK{owner, name}).AllCols().Update(stored)
	})
}

func AddWebhook(webhook *Webhook) (bool, error) {
	webhook.fillSavedSecret(nil)
	return webhook.save(func(stored *Webhook) (int64, error) {
		return adapter.engine.Insert(stored)
	})
}

func DeleteWebhook(webhook *Webhook) (bool, error) {
	affected, err := adapter.engine.ID(core.PK{webhook.Owner, webhook.Name}).Delete(&Webhook{})
	if err != nil {
		return false, err
	}
	return affected != 0, nil
}

// TestWebhook sends a test event with the webhook as it is in the form, the secret of the saved
// webhook id being used when the form has it masked.
func TestWebhook(id string, webhook *Webhook) error {
	var saved *Webhook
	if id != "" {
		owner, name := util.GetOwnerAndNameFromIdNoCheck(id)
		var err error
		saved, err = getWebhook(owner, name)
		if err != nil {
			return err
		}
	}

	target := *webhook
	target.fillSavedSecret(saved)
	err := target.check()
	if err != nil {
		return err
	}

	event := &WebhookEvent{
		Type:         WebhookEventTest,
		Title:        "Test message",
		Time:         util.GetCurrentTime(),
		Organization: target.Owner,
		Fields:       []WebhookField{{"webhook", "Webhook", target.GetId()}},
	}
	return target.post(event)
}
