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

import {get, listQuery, post} from "@/backend/request";

export type PatchJobType = "Scan" | "Install" | "Reboot";

export function getPatchStatuses(owner: string) {
  return get("/api/get-patch-statuses", {owner});
}

export function getPatchStatus(owner: string, name: string) {
  return get("/api/get-patch-status", {id: `${owner}/${name}`});
}

export function getPatchJobs(owner: string, asset: string, pageSize = 20) {
  return get("/api/get-patch-jobs", listQuery(owner, {page: 1, pageSize, field: "asset", value: asset}));
}

export function getPatchJob(owner: string, name: string) {
  return get("/api/get-patch-job", {id: `${owner}/${name}`});
}

/** `updates` picks some of the updates of a single asset to install; leave it out to install all. */
export function startPatchJob(owner: string, assets: string[], type: PatchJobType, updates: string[] = []) {
  return post("/api/start-patch-job", {}, {owner, assets, type, updates});
}
