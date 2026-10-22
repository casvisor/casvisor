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

export function getLocalUsers(owner: string, params: ListParams = {}) {
  return get("/api/get-local-users", listQuery(owner, params));
}

export function getLocalUser(owner: string, name: string) {
  return get("/api/get-local-user", {id: `${owner}/${name}`});
}

export function updateLocalUser(owner: string, name: string, user: any) {
  return post("/api/update-local-user", {id: `${owner}/${name}`}, user);
}

export function addLocalUser(user: any) {
  return post("/api/add-local-user", {}, user);
}

export function deleteLocalUser(user: any) {
  return post("/api/delete-local-user", {}, user);
}
