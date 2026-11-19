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

export interface AccessReview {
  comment?: string;
  actions?: string[];
  endTime?: string;
}

export function getAccessRequests(owner: string, params: ListParams = {}) {
  return get("/api/get-access-requests", listQuery(owner, params));
}

export function getRequestableAssets() {
  return get("/api/get-requestable-assets", {});
}

export function addAccessRequest(request: any) {
  return post("/api/add-access-request", {}, request);
}

export function cancelAccessRequest(owner: string, name: string) {
  return post("/api/cancel-access-request", {id: `${owner}/${name}`}, {});
}

export function approveAccessRequest(owner: string, name: string, review: AccessReview) {
  return post("/api/approve-access-request", {id: `${owner}/${name}`}, review);
}

export function rejectAccessRequest(owner: string, name: string, review: AccessReview) {
  return post("/api/reject-access-request", {id: `${owner}/${name}`}, review);
}

export function revokeAccessRequest(owner: string, name: string, review: AccessReview) {
  return post("/api/revoke-access-request", {id: `${owner}/${name}`}, review);
}

export function deleteAccessRequest(request: any) {
  return post("/api/delete-access-request", {}, request);
}
