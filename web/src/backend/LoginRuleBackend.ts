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

export const LoginActions = ["Allow", "Reject", "Review", "Mfa"];

export const LoginMethods = ["Web", "SSH", "Database"];

export function getLoginRules(owner: string, params: ListParams = {}) {
  return get("/api/get-login-rules", listQuery(owner, params));
}

export function getLoginRule(owner: string, name: string) {
  return get("/api/get-login-rule", {id: `${owner}/${name}`});
}

export function updateLoginRule(owner: string, name: string, rule: any) {
  return post("/api/update-login-rule", {id: `${owner}/${name}`}, rule);
}

export function addLoginRule(rule: any) {
  return post("/api/add-login-rule", {}, rule);
}

export function deleteLoginRule(rule: any) {
  return post("/api/delete-login-rule", {}, rule);
}

export function getLoginReviews(owner: string, status: string, params: ListParams = {}) {
  return get("/api/get-login-reviews", {...listQuery(owner, params), status});
}

export function reviewLogin(id: string, approve: boolean, comment: string) {
  return post("/api/review-login", {}, {id, approve, comment});
}

/** For the browser whose sign-in a login rule holds: the review's status, which signs it in once approved. */
export function getLoginReviewStatus() {
  return get("/api/get-login-review-status");
}

export function cancelLoginReview() {
  return post("/api/cancel-login-review");
}
