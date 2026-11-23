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

import {type ListParams, get, listQuery, post} from "@/backend/request";
import type {Job} from "@/backend/types";
import * as Setting from "@/lib/setting";

export const JobTypes = ["Command", "Script", "File", "Playbook"];
export const ScriptTypes = ["Bash", "PowerShell"];

export function getJobs(owner: string, params: ListParams = {}) {
  return get("/api/get-jobs", listQuery(owner, params));
}

export function getJob(owner: string, name: string) {
  return get("/api/get-job", {id: `${owner}/${name}`});
}

export function startJob(job: Partial<Job>) {
  return post("/api/start-job", {}, job);
}

/** Checks a playbook before it runs; resolves to {plays, tasks, handlers, modules, files}. */
export function checkPlaybook(job: Partial<Job>) {
  return post("/api/check-playbook", {}, job);
}

export function rerunJob(owner: string, name: string) {
  return post("/api/rerun-job", {id: `${owner}/${name}`});
}

export function cancelJob(owner: string, name: string) {
  return post("/api/cancel-job", {id: `${owner}/${name}`});
}

export function deleteJob(job: Job) {
  return post("/api/delete-job", {}, {owner: job.owner, name: job.name});
}

/** Uploads the file a job sends; resolves to {key, name, size}. */
export function uploadJobFile(owner: string, file: File) {
  const form = new FormData();
  form.append("file", file);
  return fetch(`${Setting.ServerUrl}/api/upload-job-file?owner=${encodeURIComponent(owner)}`, {
    method: "POST",
    credentials: "include",
    body: form,
  }).then(Setting.handleFetchResponse);
}
