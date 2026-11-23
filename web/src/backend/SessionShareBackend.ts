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

import {get, post} from "@/backend/request";

export function addSessionShare(form: {session: string; users: string[]; mode: string; expireMinutes: number}) {
  return post("/api/add-session-share", {}, form);
}

export function getSessionShares(session: string) {
  return get("/api/get-session-shares", {session});
}

export function revokeSessionShare(id: string) {
  return post("/api/revoke-session-share", {id});
}

/** What a share is, for a user it is for, without its code. */
export function getSessionShare(id: string) {
  return get("/api/get-session-share", {id});
}
