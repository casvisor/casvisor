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
import {ChevronDown, User} from "lucide-react";
import {Button} from "@/components/ui/button";
import {DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuTrigger} from "@/components/ui/dropdown-menu";
import {ConnectCredentialsDialog} from "@/components/access/ConnectCredentialsDialog";
import * as SessionBackend from "@/backend/SessionBackend";
import type {Asset} from "@/backend/types";
import {getAssetCredentialRequirements} from "@/lib/asset";

function buildAccessUrl(asset: Asset, account = "", ticket = "") {
  const query = account ? `?account=${encodeURIComponent(account)}` : "";
  const hash = ticket ? `#ticket=${encodeURIComponent(ticket)}` : "";
  return `/access/${encodeURIComponent(asset.owner)}/${encodeURIComponent(asset.name)}${query}${hash}`;
}

function openAccessPage(asset: Asset, account = "") {
  window.open(buildAccessUrl(asset, account), "_blank", "noopener");
}

/**
 * The "Connect" button of an asset. It asks only for credentials the protocol
 * needs that are not saved. When the user may connect as several accounts of
 * the asset, it lists them first.
 */
export function ConnectButton({asset, disabled}: {asset: Asset; disabled?: boolean}) {
  const [open, setOpen] = React.useState(false);
  const {needsUsername, needsPassword} = getAssetCredentialRequirements(asset);
  const hasCredentials = !needsUsername && !needsPassword;
  const accounts = asset.accounts ?? [];

  const start = () => {
    if (accounts.length === 1 && !accounts[0].isDefault) {
      openAccessPage(asset, accounts[0].username);
      return;
    }
    if (hasCredentials) {
      openAccessPage(asset);
      return;
    }
    setOpen(true);
  };

  const submit = async (username: string, password: string) => {
    let popup: Window | null = null;
    let ticket: string;
    try {
      // the tab is opened before the request, while the click still allows a popup
      popup = window.open("", "_blank");
      if (popup) {
        popup.opener = null;
      }
      const res = await SessionBackend.createConnectTicket(`${asset.owner}/${asset.name}`, username, password);
      const issuedTicket = res?.data?.ticket;
      if (res?.status !== "ok" || typeof issuedTicket !== "string" || !issuedTicket) {
        throw new Error(res?.msg || i18next.t("asset:Failed to connect"));
      }
      ticket = issuedTicket;
    } catch (err) {
      if (popup && !popup.closed) {
        popup.close();
      }
      throw err;
    }

    const url = buildAccessUrl(asset, "", ticket);
    if (popup && !popup.closed) {
      try {
        popup.location.replace(new URL(url, window.location.origin).href);
        return true;
      } catch {
        // The tab may have closed during navigation.
      }
    }
    window.location.assign(url);
    return true;
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
            onSelect={() => (account.isDefault ? start() : openAccessPage(asset, account.username))}
          >
            <User />
            <span className="font-mono">
              {account.username || i18next.t(account.isDefault && hasCredentials ? "account:Default" : "account:Enter an account")}
            </span>
            {account.displayName ? <span className="text-xs text-muted-foreground">{account.displayName}</span> : null}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  ) : (
    <Button size="sm" variant={hasCredentials || (accounts.length === 1 && !accounts[0].isDefault) ? "default" : "outline"} disabled={disabled} onClick={start}>
      {i18next.t("general:Connect")}
    </Button>
  );

  return (
    <>
      {trigger}
      <ConnectCredentialsDialog asset={asset} open={open} onOpenChange={setOpen} onSubmit={submit} />
    </>
  );
}
