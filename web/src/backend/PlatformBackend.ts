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

export const PlatformCategories = ["Host", "Network", "Database", "Cloud", "Web"];

export const PlatformMethods = ["", "Shell", "PowerShell", "CLI", "MySQL", "PostgreSQL", "SQLServer", "Oracle", "Redis", "MongoDB"];

export const PlatformCharsets = ["UTF-8", "GB18030", "GBK", "Big5", "Shift_JIS", "EUC-JP", "EUC-KR", "ISO-8859-1", "windows-1252"];

export const Elevations = ["", "sudo", "su", "enable"];

/** What each method can do; a platform may only turn these off. */
export const MethodAbilities: Record<string, {change: boolean; push: boolean; gather: boolean}> = {
  "": {change: false, push: false, gather: false},
  Shell: {change: true, push: true, gather: true},
  PowerShell: {change: true, push: true, gather: true},
  CLI: {change: true, push: true, gather: false},
  MySQL: {change: true, push: true, gather: true},
  PostgreSQL: {change: true, push: true, gather: true},
  SQLServer: {change: true, push: false, gather: true},
  Oracle: {change: true, push: false, gather: true},
  Redis: {change: true, push: false, gather: true},
  MongoDB: {change: true, push: false, gather: true},
};

export function getPlatforms(owner: string, params: ListParams = {}) {
  return get("/api/get-platforms", listQuery(owner, params));
}

export function getAllPlatforms(owner: string) {
  return get("/api/get-platforms", {owner});
}

export function getPlatform(owner: string, name: string) {
  return get("/api/get-platform", {id: `${owner}/${name}`});
}

export function updatePlatform(owner: string, name: string, platform: any) {
  return post("/api/update-platform", {id: `${owner}/${name}`}, platform);
}

export function addPlatform(platform: any) {
  return post("/api/add-platform", {}, platform);
}

export function deletePlatform(platform: any) {
  return post("/api/delete-platform", {}, platform);
}
