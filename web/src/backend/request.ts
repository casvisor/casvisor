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

import * as Setting from "@/lib/setting";

export interface ListParams {
  page?: number | string;
  pageSize?: number | string;
  field?: string;
  value?: string;
  sortField?: string;
  sortOrder?: string;
}

function toQuery(params: Record<string, any>) {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    search.set(key, value === undefined || value === null ? "" : String(value));
  }
  return search.toString();
}

export function get(path: string, params: Record<string, any> = {}): Promise<any> {
  const query = toQuery(params);
  return fetch(`${Setting.ServerUrl}${path}${query ? `?${query}` : ""}`, {
    method: "GET",
    credentials: "include",
  }).then(Setting.handleFetchResponse);
}

export function post(path: string, params: Record<string, any> = {}, body?: any): Promise<any> {
  const query = toQuery(params);
  return fetch(`${Setting.ServerUrl}${path}${query ? `?${query}` : ""}`, {
    method: "POST",
    credentials: "include",
    body: body === undefined ? undefined : JSON.stringify(body),
  }).then(Setting.handleFetchResponse);
}

/** The (owner, p, pageSize, field, value, sortField, sortOrder) query every list API takes. */
export function listQuery(owner: string, params: ListParams = {}) {
  return {
    owner,
    p: params.page ?? "",
    pageSize: params.pageSize ?? "",
    field: params.field ?? "",
    value: params.value ?? "",
    sortField: params.sortField ?? "",
    sortOrder: params.sortOrder ?? "",
  };
}
