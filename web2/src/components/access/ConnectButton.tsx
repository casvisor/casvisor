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

import * as React from "react";
import i18next from "i18next";
import {Lock, User} from "lucide-react";
import {Button} from "@/components/ui/button";
import {Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle} from "@/components/ui/dialog";
import {Input} from "@/components/ui/input";
import {PasswordInput} from "@/components/common/PasswordInput";
import type {Asset} from "@/backend/types";

const credentialsKeyPrefix = "casvisorConnect:";
const credentialsTtlMs = 60 * 1000;
const takenCredentials = new Map<string, {username: string; password: string}>();

/**
 * Hands the credentials typed here to the access page in the new tab. They go
 * through localStorage for a minute rather than the URL, so a password never
 * lands in the browser history or a server log.
 */
function saveCredentials(assetId: string, username: string, password: string) {
  try {
    localStorage.setItem(credentialsKeyPrefix + assetId, JSON.stringify({username, password, time: Date.now()}));
  } catch {
    // a blocked store leaves the asset's own credentials to the backend
  }
}

/** Reads the credentials once; StrictMode renders twice, so the first read is cached. */
export function takeCredentials(assetId: string): {username: string; password: string} {
  const cached = takenCredentials.get(assetId);
  if (cached) {
    return cached;
  }

  let result = {username: "", password: ""};
  try {
    const raw = localStorage.getItem(credentialsKeyPrefix + assetId);
    localStorage.removeItem(credentialsKeyPrefix + assetId);
    if (raw) {
      const saved = JSON.parse(raw);
      if (Date.now() - saved.time < credentialsTtlMs) {
        result = {username: saved.username ?? "", password: saved.password ?? ""};
      }
    }
  } catch {
    // fall back to the asset's own credentials
  }
  takenCredentials.set(assetId, result);
  return result;
}

function openAccessPage(asset: Asset, username: string, password: string) {
  if (username !== "" && password !== "") {
    saveCredentials(`${asset.owner}/${asset.name}`, username, password);
  }
  window.open(`/access/${asset.owner}/${asset.name}`, "_blank");
}

/**
 * The "Connect" button of an asset. An asset that keeps both a username and a
 * password connects straight away; otherwise it asks for what is missing.
 */
export function ConnectButton({asset, disabled}: {asset: Asset; disabled?: boolean}) {
  const [open, setOpen] = React.useState(false);
  const [username, setUsername] = React.useState("");
  const [password, setPassword] = React.useState("");
  const hasCredentials = Boolean(asset.username && asset.password);

  const start = () => {
    if (hasCredentials) {
      openAccessPage(asset, "", "");
      return;
    }
    setUsername(asset.username || "");
    setPassword("");
    setOpen(true);
  };

  const submit = () => {
    if (!username || !password) {
      return;
    }
    setOpen(false);
    openAccessPage(asset, username, password);
  };

  return (
    <>
      <Button size="sm" variant={hasCredentials ? "default" : "outline"} disabled={disabled} onClick={start}>
        {i18next.t("general:Connect")}
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>{i18next.t("general:Connect")}: {asset.displayName || asset.name}</DialogTitle>
          </DialogHeader>
          <form
            className="space-y-3"
            onSubmit={(e) => {
              e.preventDefault();
              submit();
            }}
          >
            <div className="relative">
              <User className="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                className="pl-8"
                placeholder={i18next.t("general:Username")}
                value={username}
                disabled={Boolean(asset.username)}
                autoComplete="username"
                onChange={(e) => setUsername(e.target.value)}
              />
            </div>
            <div className="relative">
              <Lock className="absolute left-2.5 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <PasswordInput
                className="[&_input]:pl-8"
                placeholder={i18next.t("general:Password")}
                value={password}
                autoFocus
                autoComplete="current-password"
                onChange={(e) => setPassword(e.target.value)}
              />
            </div>
            <DialogFooter>
              <Button type="submit" className="w-full" disabled={!username || !password}>
                {i18next.t("general:Connect")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}
