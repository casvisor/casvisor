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

/**
 * Creates the session a connection runs in. What the user typed for an asset that
 * keeps no credentials goes in the body, for the session's tunnel to use once.
 */
export function addAssetTunnel(assetId: string, account = "", username = "", password = "") {
  const query = account ? {assetId, account} : {assetId};
  return post("/api/add-asset-tunnel", query, username || password ? {username, password} : undefined);
}

/**
 * A token for a client on the user's machine: "ssh", "tunnel" or "rdp", good for two minutes and once, or "db",
 * the user and the password of a database client, good for hours and many connections.
 */
export function addClientToken(assetId: string, kind: string, account = "") {
  const query = account ? {assetId, kind, account} : {assetId, kind};
  return post("/api/add-client-token", query);
}

/** The valid database tokens: the user's own, or those of the organization for an administrator. */
export function getDatabaseTokens() {
  return get("/api/get-database-tokens");
}

/** Revokes a database token, which also disconnects the sessions opened with it. */
export function revokeDatabaseToken(tokenId: string) {
  return post("/api/revoke-database-token", {tokenId});
}

/** Connects a session of a Web asset, and returns the address that opens the website, once and within a minute. */
export function getWebAssetUrl(sessionId: string) {
  return post("/api/get-web-asset-url", {sessionId});
}

/** Whether Web assets can be opened in Casvisor's browser, which needs Chrome, Chromium or Edge on the server. */
export function getRemoteBrowser() {
  return get("/api/get-remote-browser");
}

/** The address of a file the browser of a session has downloaded. */
export function getBrowserDownloadUrl(sessionId: string, download: string) {
  return `${Setting.ServerUrl}/api/get-browser-download?${new URLSearchParams({sessionId, download})}`;
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
