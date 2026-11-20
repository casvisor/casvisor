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
	"slices"

	"github.com/casvisor/casvisor/webhook"
)

func setNonEmpty(data map[string]any, key, value string) {
	if value != "" {
		data[key] = value
	}
}

func notifyCommand(command *Command) {
	if command == nil || !webhook.Enabled() || (command.Action != CommandActionBlock && command.Action != CommandActionAlert) {
		return
	}
	action := "blocked"
	if command.Action == CommandActionAlert {
		action = "alerted"
	}
	webhook.Send(webhook.Event{
		Type:         "command." + action,
		Time:         parseTime(command.CreatedTime),
		Organization: command.Owner,
		User:         command.User,
		Summary:      "Command " + action,
		Data: map[string]any{
			"asset":    command.Asset,
			"session":  command.Session,
			"clientIp": command.ClientIp,
			"command":  command.Text,
			"action":   command.Action,
			"rule":     command.Rule,
			"protocol": command.Protocol,
		},
	})
}

func notifyAccessRequest(action string, request *AccessRequest) {
	if request == nil || !webhook.Enabled() {
		return
	}
	when := request.ReviewedTime
	if action == "created" {
		when = request.CreatedTime
	}
	data := map[string]any{
		"request": request.Name,
		"status":  request.Status,
		"assets":  slices.Clone(request.Assets),
		"actions": slices.Clone(request.Actions),
		"endTime": request.EndTime,
		"reason":  request.Reason,
	}
	if len(request.Accounts) > 0 {
		data["accounts"] = slices.Clone(request.Accounts)
	}
	setNonEmpty(data, "startTime", request.StartTime)
	setNonEmpty(data, "reviewer", request.Reviewer)
	setNonEmpty(data, "reviewComment", request.ReviewComment)
	setNonEmpty(data, "permission", request.Permission)
	webhook.Send(webhook.Event{
		Type:         "access_request." + action,
		Time:         parseTime(when),
		Organization: request.Owner,
		User:         request.User,
		Summary:      "Access request " + action,
		Data:         data,
	})
}

func notifyLoginReview(action string, review *LoginReview) {
	if review == nil || !webhook.Enabled() {
		return
	}
	webhook.Send(loginReviewEvent(action, review))
}

func loginReviewEvent(action string, review *LoginReview) webhook.Event {
	when := review.ReviewedTime
	if action == "created" {
		when = review.CreatedTime
	}
	data := map[string]any{
		"review":   review.Name,
		"status":   review.Status,
		"clientIp": review.ClientIp,
		"method":   review.Method,
		"rule":     review.Rule,
	}
	setNonEmpty(data, "reviewer", review.Reviewer)
	setNonEmpty(data, "comment", review.Comment)
	return webhook.Event{
		Type:         "login_review." + action,
		Time:         parseTime(when),
		Organization: review.Owner,
		User:         review.User,
		Summary:      "Login review " + action,
		Data:         data,
	}
}
