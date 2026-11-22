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

import * as React from "react";
import i18next from "i18next";
import {ExternalLink, Globe, Loader2} from "lucide-react";
import * as SessionBackend from "@/backend/SessionBackend";
import type {ConnectionProps} from "@/components/access/util";
import {Button} from "@/components/ui/button";
import * as Setting from "@/lib/setting";

/** Starts a web session and returns the address that opens the website on its subdomain. */
async function openWebSession(assetId: string, account?: string): Promise<string> {
  const res = await SessionBackend.addAssetTunnel(assetId, account);
  if (res.status !== "ok") {
    throw new Error(res.msg);
  }
  const session = res.data;
  const urlRes = await SessionBackend.getWebAssetUrl(`${session.owner}/${session.name}`);
  if (urlRes.status !== "ok") {
    throw new Error(urlRes.msg);
  }
  return urlRes.data as string;
}

/**
 * Opens the website of a Web asset. A window of its own goes on to the website, so that the
 * website has the whole window and its own address; a workbench tab opens it in a new window.
 */
export default function WebView(props: ConnectionProps) {
  const {assetId, account, inTab} = props;
  const assetName = Setting.getShortName(assetId);
  const [error, setError] = React.useState("");
  const [opening, setOpening] = React.useState(!inTab);
  const startedRef = React.useRef(false);

  React.useEffect(() => {
    document.title = assetName;
    // StrictMode runs effects twice, and a session can only be signed in to once
    if (inTab || startedRef.current) {
      return;
    }
    startedRef.current = true;
    openWebSession(assetId, account)
      .then((url) => window.location.replace(url))
      .catch((e) => {
        setOpening(false);
        setError(String(e?.message ?? e));
      });
  }, [assetId, account, inTab, assetName]);

  // the window is opened before the session is started, while the click still allows it
  const openInWindow = () => {
    const win = window.open("about:blank", "_blank");
    setError("");
    setOpening(true);
    openWebSession(assetId, account)
      .then((url) => {
        if (win) {
          win.opener = null;
          win.location.href = url;
        } else {
          window.location.href = url;
        }
      })
      .catch((e) => {
        win?.close();
        setError(String(e?.message ?? e));
      })
      .finally(() => setOpening(false));
  };

  return (
    <div className="flex h-full w-full items-center justify-center bg-[#1b1b1b] p-4 text-neutral-200">
      <div className="flex w-full max-w-md flex-col items-center gap-4 text-center">
        <Globe className="h-10 w-10 text-neutral-400" />
        <div className="text-base font-medium">{assetName}</div>
        {error ? (
          <div className="break-all text-sm text-red-400">{i18next.t("asset:Failed to connect")}: {error}</div>
        ) : opening ? (
          <div className="flex items-center gap-2 text-sm text-neutral-400">
            <Loader2 className="h-4 w-4 animate-spin" />
            {i18next.t("web:Opening the website...")}
          </div>
        ) : (
          <div className="text-sm text-neutral-400">{i18next.t("web:The website opens in a window of its own, through Casvisor")}</div>
        )}
        {inTab || error ? (
          <Button size="sm" disabled={opening} onClick={openInWindow}>
            <ExternalLink />
            {i18next.t("web:Open the website")}
          </Button>
        ) : null}
      </div>
    </div>
  );
}
