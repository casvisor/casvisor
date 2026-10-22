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
import * as Setting from "@/lib/setting";

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

/** Creates the session a connection runs in. */
export function addAssetTunnel(assetId: string) {
  return post("/api/add-asset-tunnel", {assetId});
}

export function connect(sessionId: string) {
  return post("/api/start-session", {id: sessionId});
}

export function disconnect(sessionId: string) {
  return post("/api/stop-session", {id: sessionId});
}

/** Downloads a recording of a session, reporting the bytes received so far. */
export async function downloadRecording(sessionId: string, index: number, onProgress?: (loaded: number) => void): Promise<ArrayBuffer> {
  const query = new URLSearchParams({id: sessionId, index: String(index)});
  const res = await fetch(`${Setting.ServerUrl}/api/get-session-recording?${query}`, {credentials: "include"});
  if ((res.headers.get("Content-Type") ?? "").includes("json")) {
    const body = await res.json();
    throw new Error(body.msg ?? "failed to download the recording");
  }
  if (!res.ok || !res.body) {
    throw new Error(res.statusText);
  }

  const reader = res.body.getReader();
  const chunks: Uint8Array[] = [];
  let loaded = 0;
  for (;;) {
    const {done, value} = await reader.read();
    if (done) {
      break;
    }
    chunks.push(value);
    loaded += value.length;
    onProgress?.(loaded);
  }

  const buffer = new Uint8Array(loaded);
  let offset = 0;
  for (const chunk of chunks) {
    buffer.set(chunk, offset);
    offset += chunk.length;
  }
  return buffer.buffer;
}
