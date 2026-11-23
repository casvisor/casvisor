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

import {get, listQuery, post, type ListParams} from "@/backend/request";

export const WebhookTypes = ["JSON", "DingTalk", "Feishu", "WeCom", "Slack", "Email"];

export const WebhookEvents = [
  "command.blocked",
  "command.alerted",
  "access_request.created",
  "access_request.reviewed",
  "login_review.created",
  "login_review.reviewed",
  "command_review.created",
  "command_review.reviewed",
  "job.succeeded",
  "job.failed",
  "job_schedule.missed",
  "account_risk.found",
  "account_backup.failed",
  "report.failed",
];

export function getWebhooks(owner: string, params: ListParams = {}) {
  return get("/api/get-webhooks", listQuery(owner, params));
}

export function getWebhook(owner: string, name: string) {
  return get("/api/get-webhook", {id: `${owner}/${name}`});
}

export function updateWebhook(owner: string, name: string, webhook: any) {
  return post("/api/update-webhook", {id: `${owner}/${name}`}, webhook);
}

export function addWebhook(webhook: any) {
  return post("/api/add-webhook", {}, webhook);
}

export function deleteWebhook(webhook: any) {
  return post("/api/delete-webhook", {}, webhook);
}

/** Sends a test message with the webhook as it is in the form; id is the saved webhook, whose masked secret is used. */
export function testWebhook(id: string, webhook: any) {
  return post("/api/test-webhook", id ? {id} : {}, webhook);
}
