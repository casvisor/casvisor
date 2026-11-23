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
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/util"
)

const webhookTimeout = 10 * time.Second

// WebhookField is a line of the text message, Name: Value, and Key: Value in the data of the JSON.
type WebhookField struct {
	Key   string
	Name  string
	Value string
}

// WebhookEvent is what a webhook posts: as it is with the JSON type, as a text message to chat bots.
type WebhookEvent struct {
	Type         string
	Title        string
	Time         string
	Organization string
	User         string
	Fields       []WebhookField
}

func (event *WebhookEvent) getJson() map[string]interface{} {
	data := map[string]string{}
	for _, field := range event.Fields {
		data[field.Key] = field.Value
	}
	return map[string]interface{}{
		"type":         event.Type,
		"title":        event.Title,
		"time":         event.Time,
		"organization": event.Organization,
		"user":         event.User,
		"data":         data,
	}
}

// getText starts with Casvisor, so that a DingTalk bot whose security setting is the keyword Casvisor takes it.
func (event *WebhookEvent) getText() string {
	lines := []string{fmt.Sprintf("[Casvisor] %s", event.Title)}
	if event.User != "" {
		lines = append(lines, fmt.Sprintf("User: %s/%s", event.Organization, event.User))
	}
	for _, field := range event.Fields {
		if field.Value != "" {
			// a value typed by a user, such as the reason of an access request, stays on its
			// own line, so that it can't add lines that look like Casvisor's
			lines = append(lines, fmt.Sprintf("%s: %s", field.Name, strings.Join(strings.Fields(field.Value), " ")))
		}
	}
	lines = append(lines, fmt.Sprintf("Time: %s", event.Time))
	return strings.Join(lines, "\n")
}

func getHmac(key string, message string) []byte {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(message))
	return mac.Sum(nil)
}

// post sends the event in the format of the webhook's type, signed when the webhook has a secret.
func (webhook *Webhook) post(event *WebhookEvent) error {
	target := webhook.Url
	header := http.Header{}
	var body interface{}
	text := event.getText()

	switch webhook.Type {
	case WebhookTypeDingTalk:
		body = map[string]interface{}{"msgtype": "text", "text": map[string]string{"content": text}}
		if webhook.Secret != "" {
			timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
			sign := base64.StdEncoding.EncodeToString(getHmac(webhook.Secret, timestamp+"\n"+webhook.Secret))
			target = addQuery(target, url.Values{"timestamp": {timestamp}, "sign": {sign}})
		}
	case WebhookTypeFeishu:
		message := map[string]interface{}{"msg_type": "text", "content": map[string]string{"text": text}}
		if webhook.Secret != "" {
			timestamp := strconv.FormatInt(time.Now().Unix(), 10)
			message["timestamp"] = timestamp
			message["sign"] = base64.StdEncoding.EncodeToString(getHmac(timestamp+"\n"+webhook.Secret, ""))
		}
		body = message
	case WebhookTypeWeCom:
		body = map[string]interface{}{"msgtype": "text", "text": map[string]string{"content": text}}
	case WebhookTypeSlack:
		// Slack reads <...> as mentions and links
		body = map[string]string{"text": strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)}
	default:
		body = event.getJson()
	}

	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if webhook.Type == WebhookTypeJson {
		header.Set("X-Casvisor-Event", event.Type)
		if webhook.Secret != "" {
			header.Set("X-Casvisor-Signature-256", "sha256="+hex.EncodeToString(getHmac(webhook.Secret, string(data))))
		}
	}

	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header = header
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout:   webhookTimeout,
		Transport: webhookTransport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to post to the webhook: %s", strings.ReplaceAll(err.Error(), webhook.Url, webhook.getHost()))
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// the body is not shown, so that a webhook can't be used to read internal pages
		return fmt.Errorf("the webhook returned HTTP %d", resp.StatusCode)
	}
	return getBotError(respBody)
}

// webhookTransport refuses the link-local addresses, where cloud servers keep their
// metadata and credentials; webhooks on the internal network still work.
var webhookTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout: webhookTimeout,
		Control: refuseMetadataAddress,
	}).DialContext,
	TLSHandshakeTimeout: webhookTimeout,
}

// getBotError reads the error that DingTalk, Feishu and WeCom bots return with HTTP 200.
func getBotError(body []byte) error {
	var result struct {
		ErrCode *int   `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		Code    *int   `json:"code"`
		Msg     string `json:"msg"`
	}
	if json.Unmarshal(body, &result) != nil {
		return nil
	}
	if result.ErrCode != nil && *result.ErrCode != 0 {
		return fmt.Errorf("the bot returned %d: %s", *result.ErrCode, result.ErrMsg)
	}
	if result.Code != nil && *result.Code != 0 {
		return fmt.Errorf("the bot returned %d: %s", *result.Code, result.Msg)
	}
	return nil
}

func addQuery(rawUrl string, query url.Values) string {
	u, err := url.Parse(rawUrl)
	if err != nil {
		return rawUrl
	}
	values := u.Query()
	for key, value := range query {
		values[key] = value
	}
	u.RawQuery = values.Encode()
	return u.String()
}

// getHost is what logs show of the URL, whose path and query often hold a token.
func (webhook *Webhook) getHost() string {
	u, err := url.Parse(webhook.Url)
	if err != nil {
		return "the webhook"
	}
	return u.Scheme + "://" + u.Host
}

var (
	webhookQueue     chan *WebhookEvent
	webhookQueueOnce sync.Once
)

// sendWebhooks hands the event to the webhooks of its organization that subscribe to it, in the
// background, so that a slow or failing webhook holds up nothing; when too many events wait, it is dropped.
func sendWebhooks(event *WebhookEvent) {
	webhookQueueOnce.Do(func() {
		webhookQueue = make(chan *WebhookEvent, 1000)
		go func() {
			for event := range webhookQueue {
				deliverWebhooks(event)
			}
		}()
	})

	if event.Time == "" {
		event.Time = util.GetCurrentTime()
	}
	select {
	case webhookQueue <- event:
	default:
		logs.Warning("webhook: dropped the event %s of %s, too many events are waiting", event.Type, event.Organization)
	}
}

func deliverWebhooks(event *WebhookEvent) {
	webhooks, err := GetWebhooks(event.Organization)
	if err != nil {
		logs.Warning("webhook: %s", err.Error())
		return
	}

	for _, webhook := range webhooks {
		if !webhook.IsEnabled || !slices.Contains(webhook.Events, event.Type) {
			continue
		}
		webhook.Secret, err = decryptCredential(webhook.Secret)
		if err == nil {
			err = webhook.post(event)
		}
		if err != nil {
			logs.Warning("webhook: %s to %s: %s", event.Type, webhook.GetId(), err.Error())
		}
	}
}

func notifyCommand(command *Command) {
	eventType, title := WebhookEventCommandBlocked, "Command blocked"
	if command.Action == CommandActionAlert {
		eventType, title = WebhookEventCommandAlerted, "Command alerted"
	} else if command.Action != CommandActionBlock {
		return
	}

	// the user of a command is an id, the user of the other events a name
	user := command.User
	if strings.Contains(user, "/") {
		_, user = util.GetOwnerAndNameFromIdNoCheck(user)
	}
	sendWebhooks(&WebhookEvent{
		Type:         eventType,
		Title:        title,
		Time:         command.CreatedTime,
		Organization: command.Owner,
		User:         user,
		Fields: []WebhookField{
			{"asset", "Asset", command.Asset},
			{"command", "Command", command.Text},
			{"rule", "Rule", command.Rule},
			{"clientIp", "Client IP", command.ClientIp},
			{"session", "Session", command.Session},
		},
	})
}

func notifyAccessRequest(request *AccessRequest, created bool) {
	event := &WebhookEvent{
		Type:         WebhookEventAccessRequestReviewed,
		Title:        "Access request " + strings.ToLower(request.Status),
		Time:         request.ReviewedTime,
		Organization: request.Owner,
		User:         request.User,
		Fields: []WebhookField{
			{"request", "Request", request.Name},
			{"status", "Status", request.Status},
			{"assets", "Assets", strings.Join(request.Assets, ", ")},
			{"accounts", "Accounts", strings.Join(request.Accounts, ", ")},
			{"actions", "Actions", strings.Join(request.Actions, ", ")},
			{"startTime", "Start time", request.StartTime},
			{"endTime", "End time", request.EndTime},
			{"reason", "Reason", request.Reason},
			{"reviewer", "Reviewer", request.Reviewer},
			{"comment", "Comment", request.ReviewComment},
		},
	}
	if created {
		event.Type, event.Title, event.Time = WebhookEventAccessRequestCreated, "Access request waiting for review", request.CreatedTime
	}
	sendWebhooks(event)
}

func notifyLoginReview(review *LoginReview, created bool) {
	event := &WebhookEvent{
		Type:         WebhookEventLoginReviewReviewed,
		Title:        "Sign-in " + strings.ToLower(review.Status),
		Time:         review.ReviewedTime,
		Organization: review.Owner,
		User:         review.User,
		Fields: []WebhookField{
			{"review", "Review", review.Name},
			{"status", "Status", review.Status},
			{"clientIp", "Client IP", strings.TrimSpace(review.ClientIp + " " + review.ClientIpDesc)},
			{"method", "Method", review.Method},
			{"rule", "Rule", review.Rule},
			{"reviewer", "Reviewer", review.Reviewer},
			{"comment", "Comment", review.Comment},
		},
	}
	if created {
		event.Type, event.Title, event.Time = WebhookEventLoginReviewCreated, "Sign-in waiting for review", review.CreatedTime
		event.Fields = append(event.Fields, WebhookField{"expireTime", "Expire time", review.ExpireTime})
	}
	sendWebhooks(event)
}

func notifyCommandReview(review *CommandReview, created bool) {
	// the user of a command is an id, the user of the other events a name
	user := review.User
	if strings.Contains(user, "/") {
		_, user = util.GetOwnerAndNameFromIdNoCheck(user)
	}
	event := &WebhookEvent{
		Type:         WebhookEventCommandReviewReviewed,
		Title:        "Command " + strings.ToLower(review.Status),
		Time:         review.ReviewedTime,
		Organization: review.Owner,
		User:         user,
		Fields: []WebhookField{
			{"review", "Review", review.Name},
			{"status", "Status", review.Status},
			{"asset", "Asset", review.Asset},
			{"command", "Command", review.Text},
			{"operator", "Operator", review.Operator},
			{"rule", "Rule", review.Rule},
			{"clientIp", "Client IP", review.ClientIp},
			{"session", "Session", review.Session},
			{"reviewer", "Reviewer", review.Reviewer},
			{"comment", "Comment", review.Comment},
		},
	}
	if created {
		event.Type, event.Title, event.Time = WebhookEventCommandReviewCreated, "Command waiting for review", review.CreatedTime
		event.Fields = append(event.Fields, WebhookField{"expireTime", "Expire time", review.ExpireTime})
	}
	sendWebhooks(event)
}
