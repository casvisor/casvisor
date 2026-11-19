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
import {ChevronDown, Lock, User} from "lucide-react";
import {Button} from "@/components/ui/button";
import {DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuTrigger} from "@/components/ui/dropdown-menu";
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
export function saveCredentials(assetId: string, username: string, password: string) {
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

function openAccessPage(asset: Asset, username: string, password: string, account = "") {
  if (username !== "" && password !== "") {
    saveCredentials(`${asset.owner}/${asset.name}`, username, password);
  }
  const query = account ? `?account=${encodeURIComponent(account)}` : "";
  window.open(`/access/${asset.owner}/${asset.name}${query}`, "_blank");
}

/**
 * The "Connect" button of an asset. An asset that keeps both a username and a
 * password connects straight away; otherwise it asks for what is missing. When
 * the user may connect as several accounts of the asset, it lists them first.
 */
export function ConnectButton({asset, disabled}: {asset: Asset; disabled?: boolean}) {
  const [open, setOpen] = React.useState(false);
  const [username, setUsername] = React.useState("");
  const [password, setPassword] = React.useState("");
  const hasCredentials = Boolean(asset.username && asset.password);
  const accounts = asset.accounts ?? [];

  const start = () => {
    if (accounts.length === 1 && !accounts[0].isDefault) {
      openAccessPage(asset, "", "", accounts[0].username);
      return;
    }
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

  const trigger = accounts.length > 1 ? (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button size="sm" disabled={disabled}>
          {i18next.t("general:Connect")}
          <ChevronDown />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">{i18next.t("account:Connect as")}</DropdownMenuLabel>
        {accounts.map((account) => (
          <DropdownMenuItem
            key={`${account.isDefault}:${account.username}`}
            onSelect={() => (account.isDefault ? start() : openAccessPage(asset, "", "", account.username))}
          >
            <User />
            <span className="font-mono">{account.username || i18next.t("account:Enter an account")}</span>
            {account.displayName ? <span className="text-xs text-muted-foreground">{account.displayName}</span> : null}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  ) : (
    <Button size="sm" variant={hasCredentials || accounts.length === 1 && !accounts[0].isDefault ? "default" : "outline"} disabled={disabled} onClick={start}>
      {i18next.t("general:Connect")}
    </Button>
  );

  return (
    <>
      {trigger}
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
