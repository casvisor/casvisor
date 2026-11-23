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

import * as Setting from "@/lib/setting";

export const XlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet";

/** Saves the file an API answers with; resolves to an error message, or "" when the file was saved. */
export async function downloadFile(path: string, query: URLSearchParams, contentType: string, fallbackName: string): Promise<string> {
  const res = await fetch(`${Setting.ServerUrl}${path}?${query.toString()}`, {credentials: "include"});
  if (!res.ok || !(res.headers.get("Content-Type") ?? "").includes(contentType)) {
    const data = await Setting.handleFetchResponse(res);
    return data?.msg || `HTTP ${res.status}`;
  }
  const match = /filename="?([^";]+)"?/.exec(res.headers.get("Content-Disposition") ?? "");
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = match ? match[1] : fallbackName;
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 10000);
  return "";
}
