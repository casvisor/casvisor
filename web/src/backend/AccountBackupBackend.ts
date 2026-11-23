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
import * as Setting from "@/lib/setting";

export function getAccountBackups(owner: string, params: ListParams = {}) {
  return get("/api/get-account-backups", listQuery(owner, params));
}

export function getAccountBackup(owner: string, name: string) {
  return get("/api/get-account-backup", {id: `${owner}/${name}`});
}

export function updateAccountBackup(owner: string, name: string, backup: any) {
  return post("/api/update-account-backup", {id: `${owner}/${name}`}, backup);
}

export function addAccountBackup(backup: any) {
  return post("/api/add-account-backup", {}, backup);
}

export function deleteAccountBackup(backup: any) {
  return post("/api/delete-account-backup", {}, backup);
}

/** Makes the backup now and emails it with its webhooks. */
export function runAccountBackup(owner: string, name: string) {
  return post("/api/run-account-backup", {id: `${owner}/${name}`});
}

/** Downloads the backup made now; resolves to an error message, or "" when the file was saved. */
export async function downloadAccountBackup(owner: string, name: string): Promise<string> {
  const res = await fetch(`${Setting.ServerUrl}/api/download-account-backup?id=${encodeURIComponent(`${owner}/${name}`)}`, {
    method: "POST",
    credentials: "include",
  });
  if (!res.ok || !(res.headers.get("Content-Type") ?? "").includes("application/zip")) {
    const data = await Setting.handleFetchResponse(res);
    return data?.msg || `HTTP ${res.status}`;
  }
  const match = /filename="?([^";]+)"?/.exec(res.headers.get("Content-Disposition") ?? "");
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = match ? match[1] : "casvisor-accounts.zip";
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 10000);
  return "";
}

/** Restores the assets and the accounts of a backup's zip file, or only tells what it would do. */
export function restoreAccountBackup(owner: string, file: File, password: string, overwrite: boolean, dryRun: boolean) {
  const form = new FormData();
  form.append("owner", owner);
  form.append("file", file);
  form.append("password", password);
  form.append("overwrite", String(overwrite));
  form.append("dryRun", String(dryRun));
  return fetch(`${Setting.ServerUrl}/api/restore-account-backup?owner=${encodeURIComponent(owner)}`, {
    method: "POST",
    credentials: "include",
    body: form,
  }).then(Setting.handleFetchResponse);
}
