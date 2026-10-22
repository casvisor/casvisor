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
import * as Setting from "@/lib/setting";

/** Lists a directory on the asset of an SSH session; an empty path is the home directory. */
export function getSessionFiles(sessionId: string, path: string) {
  return get("/api/get-session-files", {id: sessionId, path});
}

export function getDownloadUrl(sessionId: string, path: string) {
  return `${Setting.ServerUrl}/api/download-session-file?${new URLSearchParams({id: sessionId, path}).toString()}`;
}

export function uploadSessionFiles(sessionId: string, dir: string, files: File[]) {
  const form = new FormData();
  for (const file of files) {
    form.append("files", file, file.name);
  }
  return fetch(`${Setting.ServerUrl}/api/upload-session-file?${new URLSearchParams({id: sessionId, path: dir}).toString()}`, {
    method: "POST",
    credentials: "include",
    body: form,
  }).then(Setting.handleFetchResponse);
}

export function createSessionDir(sessionId: string, path: string) {
  return post("/api/create-session-dir", {id: sessionId, path});
}

export function renameSessionFile(sessionId: string, path: string, newPath: string) {
  return post("/api/rename-session-file", {id: sessionId, path, newPath});
}

export function deleteSessionFile(sessionId: string, path: string) {
  return post("/api/delete-session-file", {id: sessionId, path});
}
