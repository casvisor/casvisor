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

export const ConnectMethods = ["Web", "SSH client", "SFTP client", "Port forwarding", "RDP client", "kubectl", "Database client"];

export const ConnectActions = ["Reject", "Allow"];

export function getConnectRules(owner: string, params: ListParams = {}) {
  return get("/api/get-connect-rules", listQuery(owner, params));
}

export function getConnectRule(owner: string, name: string) {
  return get("/api/get-connect-rule", {id: `${owner}/${name}`});
}

export function updateConnectRule(owner: string, name: string, rule: any) {
  return post("/api/update-connect-rule", {id: `${owner}/${name}`}, rule);
}

export function addConnectRule(rule: any) {
  return post("/api/add-connect-rule", {}, rule);
}

export function deleteConnectRule(rule: any) {
  return post("/api/delete-connect-rule", {}, rule);
}
