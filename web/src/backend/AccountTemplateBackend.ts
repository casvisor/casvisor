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

export function getAccountTemplates(owner: string, params: ListParams = {}) {
  return get("/api/get-account-templates", listQuery(owner, params));
}

export function getAccountTemplate(owner: string, name: string) {
  return get("/api/get-account-template", {id: `${owner}/${name}`});
}

export function updateAccountTemplate(owner: string, name: string, template: any) {
  return post("/api/update-account-template", {id: `${owner}/${name}`}, template);
}

export function addAccountTemplate(template: any) {
  return post("/api/add-account-template", {}, template);
}

export function deleteAccountTemplate(template: any) {
  return post("/api/delete-account-template", {}, template);
}

export function applyAccountTemplate(id: string, dryRun: boolean) {
  return post("/api/apply-account-template", {id, dryRun: String(dryRun)});
}

export const RiskKinds = ["weak-password", "stale-password", "reused-password", "empty-password", "unmanaged-privileged", "unmanaged-account"];

export const RiskStatuses = ["Open", "Ignored", "Resolved"];

export function getAccountRisks(owner: string, status: string, params: ListParams = {}) {
  return get("/api/get-account-risks", {...listQuery(owner, params), status});
}

export function startAccountRiskScan(owner: string) {
  return post("/api/start-account-risk-scan", {owner});
}

export function getAccountRiskScan(owner: string) {
  return get("/api/get-account-risk-scan", {owner});
}

export function setAccountRiskStatus(id: string, status: string, comment: string) {
  return post("/api/set-account-risk-status", {}, {id, status, comment});
}
