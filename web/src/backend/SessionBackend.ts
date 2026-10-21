// Copyright 2023 The casbin Authors. All Rights Reserved.
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

export const Connected = "connected";
export const Disconnected = "disconnected";

export function getSessions(owner: string, params: ListParams = {}, status = Connected) {
  return get("/api/get-sessions", {...listQuery(owner, params), status});
}

export function getSession(owner: string, name: string) {
  return get("/api/get-session", {id: `${owner}/${name}`});
}

export function updateSession(owner: string, name: string, session: any) {
  return post("/api/update-session", {id: `${owner}/${name}`}, session);
}

export function deleteSession(session: any) {
  return post("/api/delete-session", {}, session);
}

/** Creates the session a connection runs in; "native" is the SSH and VNC path that skips guacd. */
export function addAssetTunnel(assetId: string, mode: "guacd" | "native" = "guacd") {
  return post("/api/add-asset-tunnel", {assetId, mode});
}

export function connect(sessionId: string) {
  return post("/api/start-session", {id: sessionId});
}

export function disconnect(sessionId: string) {
  return post("/api/stop-session", {id: sessionId});
}
