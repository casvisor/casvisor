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

import i18next from "i18next";
import * as Setting from "@/lib/setting";

const releaseUrl = "https://github.com/casvisor/casvisor/releases/latest";

/** "casvisor://launch?type=ssh&host=...&port=...&user=<token>&name=...", which Casvisor Launcher opens in a client on the user's machine. */
export function getLauncherLink(params: Record<string, string>) {
  return `casvisor://launch?${new URLSearchParams(params).toString()}`;
}

/** Hands the link to Casvisor Launcher. A browser cannot tell whether it is installed, so the message says where to get it. */
export function openLauncherLink(params: Record<string, string>) {
  window.location.href = getLauncherLink(params);
  Setting.showMessage("success", i18next.t("asset:Opening in Casvisor Launcher, download it first if nothing happens"));
}

/** The launcher for the user's system: the Windows executable itself, or the release page for the others. */
export function getLauncherDownloadUrl() {
  return /Windows/i.test(navigator.userAgent) ? `${releaseUrl}/download/casvisor-launcher_windows_amd64.exe` : releaseUrl;
}
