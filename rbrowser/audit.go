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

package rbrowser

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/webproxy"
)

const (
	maxCommandText  = 4000
	maxCommandError = 1000
	reviewRefresh   = 5
)

type pausedRequest struct {
	RequestId string `json:"requestId"`
	Request   struct {
		Url    string `json:"url"`
		Method string `json:"method"`
	} `json:"request"`
}

func truncate(s string, n int) string {
	if runes := []rune(s); len(runes) > n {
		return string(runes[:n])
	}
	return s
}

// newCommand returns a command of the session, made now: microseconds keep the requests in order.
func (b *Browser) newCommand(text string) *object.Command {
	return &object.Command{
		Owner:       b.asset.Owner,
		CreatedTime: time.Now().Format("2006-01-02T15:04:05.000000Z07:00"),
		Session:     b.id,
		Asset:       b.asset.GetId(),
		User:        b.user.Owner + "/" + b.user.Name,
		ClientIp:    b.clientIp,
		Text:        truncate(text, maxCommandText),
		Protocol:    object.ProtocolBrowser,
	}
}

func (b *Browser) saveCommand(command *object.Command) {
	command.Error = truncate(command.Error, maxCommandError)
	err := object.AddCommand(command)
	if err != nil {
		logs.Error(fmt.Sprintf("rbrowser.saveCommand(): %s", err.Error()))
	}
}

// onRequest records each page and frame the browser opens, and holds it to the command rules.
func (b *Browser) onRequest(sessionId string, ev *pausedRequest) {
	u, err := url.Parse(ev.Request.Url)
	if err != nil || !b.isSameOrigin(u) {
		command := b.newCommand(ev.Request.Method + " " + ev.Request.Url)
		command.Action = object.CommandActionBlock
		command.Error = fmt.Sprintf("only %s can be opened", b.getOrigin())
		command.StatusCode = http.StatusForbidden
		b.saveCommand(command)
		b.fulfill(sessionId, ev.RequestId, http.StatusForbidden, "The page can't be opened", fmt.Sprintf("Only %s can be opened in this browser.", b.getOrigin()), 0)
		return
	}

	start := time.Now()
	command := b.newCommand(ev.Request.Method + " " + u.RequestURI())
	rule, err := object.MatchCommandRule(b.user, b.asset, command.Text)
	if err != nil {
		logs.Error(fmt.Sprintf("rbrowser.onRequest(): failed to match the command rules: %s", err.Error()))
		b.block(sessionId, ev.RequestId, command, "the request is blocked because the command rules can't be read", "The command rules can't be read.")
		return
	}
	if rule != nil {
		command.Rule = rule.GetId()
		command.Action = rule.Action
		if rule.Action == object.CommandActionBlock {
			ruleName := rule.GetDisplayName()
			b.block(sessionId, ev.RequestId, command, fmt.Sprintf("the request is blocked by the rule: %s", ruleName), fmt.Sprintf("The request is blocked by the rule: %s.", ruleName))
			return
		}
		if rule.Action == object.CommandActionReview && !b.review(sessionId, ev, command, rule) {
			return
		}
	}

	err = b.cdp.call(sessionId, "Fetch.continueRequest", map[string]interface{}{"requestId": ev.RequestId}, nil)
	if err != nil {
		command.Error = err.Error()
	}
	command.Duration = time.Since(start).Milliseconds()
	b.saveCommand(command)
}

func (b *Browser) block(sessionId string, requestId string, command *object.Command, reason string, message string) {
	command.Action = object.CommandActionBlock
	command.Error = reason
	command.StatusCode = http.StatusForbidden
	b.saveCommand(command)
	b.fulfill(sessionId, requestId, http.StatusForbidden, "The request is blocked", message, 0)
}

// review lets a page that a Review rule matched open once an administrator has approved it, as
// the reverse proxy does: the browser is shown a page that reloads until the approval.
func (b *Browser) review(sessionId string, ev *pausedRequest, command *object.Command, rule *object.CommandRule) bool {
	review, err := object.FindCommandReview(b.asset.Owner, b.id, command.Text)
	if err != nil {
		logs.Error(fmt.Sprintf("rbrowser.review(): %s", err.Error()))
		b.block(sessionId, ev.RequestId, command, "the request is blocked because its review can't be read", "Its review can't be read.")
		return false
	}

	refresh := 0
	again := "Send it again once it is approved."
	if ev.Request.Method == http.MethodGet || ev.Request.Method == http.MethodHead {
		refresh = reviewRefresh
		again = "This page reloads by itself and opens once it is approved."
	}

	if review == nil {
		review, err = object.AddCommandReview(command, rule)
		if err != nil {
			logs.Error(fmt.Sprintf("rbrowser.review(): %s", err.Error()))
			b.block(sessionId, ev.RequestId, command, "the request is blocked because it can't be held for review", "It can't be held for review.")
			return false
		}
		command.Review = review.Name
		command.Error = "the request waits for review"
		command.StatusCode = http.StatusForbidden
		b.saveCommand(command)
	}

	switch review.Status {
	case object.CommandReviewPending:
		b.fulfill(sessionId, ev.RequestId, http.StatusForbidden, "The request waits for review", fmt.Sprintf("This request needs the approval of an administrator by the rule: %s. It waits for review (request %s) for up to %d minutes. %s", rule.GetDisplayName(), review.Name, int(object.CommandReviewTimeout.Minutes()), again), refresh)
		return false
	case object.CommandReviewApproved:
		used, err := object.ConsumeCommandReview(review.GetId())
		if err != nil {
			logs.Error(fmt.Sprintf("rbrowser.review(): %s", err.Error()))
		}
		if !used {
			b.fulfill(sessionId, ev.RequestId, http.StatusForbidden, "The request is not approved", "Its approval has just been used. Send it again to ask for a new review.", 0)
			return false
		}
		command.Review, command.Reviewer = review.Name, review.Reviewer
		return true
	default:
		outcome := review.GetReviewOutcome()
		b.fulfill(sessionId, ev.RequestId, http.StatusForbidden, "The request is not approved", strings.ToUpper(outcome[:1])+outcome[1:]+".", 0)
		return false
	}
}

func (b *Browser) fulfill(sessionId string, requestId string, status int, title string, message string, refresh int) {
	headers := []map[string]string{
		{"name": "Content-Type", "value": "text/html; charset=utf-8"},
		{"name": "Cache-Control", "value": "no-store"},
		{"name": "Content-Security-Policy", "value": "default-src 'none'; style-src 'unsafe-inline'"},
	}
	if refresh > 0 {
		headers = append(headers, map[string]string{"name": "Refresh", "value": fmt.Sprintf("%d", refresh)})
	}
	err := b.cdp.call(sessionId, "Fetch.fulfillRequest", map[string]interface{}{
		"requestId":       requestId,
		"responseCode":    status,
		"responseHeaders": headers,
		"body":            base64.StdEncoding.EncodeToString(webproxy.RenderPage(title, message, "")),
	}, nil)
	if err != nil {
		logs.Warning(fmt.Sprintf("rbrowser.fulfill(): %s", err.Error()))
	}
}
