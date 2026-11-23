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

export const CloudProviders = ["Aliyun", "AWS", "Tencent", "Azure", "GCP"];

export const CloudAddressTypes = ["Public", "Private"];

export function getCloudAccounts(owner: string, params: ListParams = {}) {
  return get("/api/get-cloud-accounts", listQuery(owner, params));
}

export function getCloudAccount(owner: string, name: string) {
  return get("/api/get-cloud-account", {id: `${owner}/${name}`});
}

export function updateCloudAccount(owner: string, name: string, account: any) {
  return post("/api/update-cloud-account", {id: `${owner}/${name}`}, account);
}

export function addCloudAccount(account: any) {
  return post("/api/add-cloud-account", {}, account);
}

export function deleteCloudAccount(account: any) {
  return post("/api/delete-cloud-account", {}, account);
}

/** Lists the regions of the account as it is in the form; id is the saved account, whose masked secret is used. */
export function getCloudRegions(id: string, account: any) {
  return post("/api/get-cloud-regions", id ? {id} : {}, account);
}

export function syncCloudAccount(id: string, dryRun: boolean) {
  return post("/api/sync-cloud-account", {id, dryRun: String(dryRun)});
}
