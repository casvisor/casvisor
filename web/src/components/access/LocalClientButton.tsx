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
import {AppWindow, Cable, Copy, FolderOpen, KeyRound, MonitorUp, Terminal} from "lucide-react";
import * as SessionBackend from "@/backend/SessionBackend";
import type {Asset, SshGateway} from "@/backend/types";
import {Button} from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type {Account} from "@/hooks/use-account";
import {copyText, getGatewayHost, getSshCommand, getTunnelCommand, getTunnelLocalPort} from "@/lib/gateway";
import * as Setting from "@/lib/setting";

/** The system account to connect as: none for the asset's own account, as in the SSH command. */
function getAccountName(asset: Asset) {
  const accounts = asset.accounts ?? [];
  return accounts.length > 0 && !accounts[0].isDefault ? accounts[0].username : "";
}

function getPortOption(gateway: SshGateway) {
  return gateway.port && gateway.port !== "22" ? `-p ${gateway.port} ` : "";
}

function getTarget(asset: Asset, account: Account) {
  return asset.owner === account.owner ? asset.name : `${asset.owner}/${asset.name}`;
}

async function addToken(asset: Asset, kind: string) {
  const res = await SessionBackend.addClientToken(`${asset.owner}/${asset.name}`, kind, getAccountName(asset));
  if (res.status !== "ok") {
    Setting.showMessage("error", res.msg);
    return null;
  }
  return res.data as {token: string; username: string; expires: number};
}

/** A .rdp file that has mstsc reach the asset through Casvisor's Remote Desktop Gateway, written in UTF-16 as mstsc saves them. */
function downloadRdpFile(asset: Asset, token: string, username: string) {
  const lines = [
    `full address:s:${asset.name}`,
    `username:s:${username}`,
    `gatewayhostname:s:${window.location.host}`,
    "gatewayusagemethod:i:1",
    "gatewayprofileusagemethod:i:1",
    "gatewaycredentialssource:i:5",
    `gatewayaccesstoken:s:${token}`,
    "promptcredentialonce:i:0",
    "authentication level:i:2",
    "screen mode id:i:2",
  ];
  const text = `\ufeff${lines.join("\r\n")}\r\n`;
  const units = new Uint16Array(text.length);
  for (let i = 0; i < text.length; i++) {
    units[i] = text.charCodeAt(i);
  }
  const link = document.createElement("a");
  link.href = URL.createObjectURL(new Blob([units], {type: "application/x-rdp"}));
  link.download = `${asset.name}.rdp`;
  link.click();
  setTimeout(() => URL.revokeObjectURL(link.href), 10000);
}

/**
 * Opens the asset in a client on the user's machine: SSH clients such as Xshell and WinSCP through the SSH gateway,
 * Remote Desktop Connection through the Remote Desktop Gateway, or any client through a forwarded port. The links and
 * commands carry a one-time token that is good for two minutes, so no public key needs to be added first.
 */
export function LocalClientButton({asset, gateway, account}: {asset: Asset; gateway: SshGateway; account: Account}) {
  const isSsh = asset.protocol === "SSH";
  const hasSsh = gateway.enabled;
  const hasRdp = asset.protocol === "RDP" && gateway.rdGateway && window.location.protocol === "https:";
  if (!hasSsh && !hasRdp) {
    return null;
  }

  const host = getGatewayHost(gateway);
  const port = gateway.port || "22";

  const openUrl = async(scheme: string) => {
    const res = await addToken(asset, "ssh");
    if (res) {
      window.location.href = `${scheme}://${res.token}@${host}:${port}`;
    }
  };

  const copySshCommand = async() => {
    const res = await addToken(asset, "ssh");
    if (res) {
      copyText(`ssh ${getPortOption(gateway)}${res.token}@${host}`);
    }
  };

  const copyTunnelCommand = async() => {
    const res = await addToken(asset, "tunnel");
    if (res) {
      copyText(`ssh ${getPortOption(gateway)}-N -L ${getTunnelLocalPort(asset)}:${getTarget(asset, account)}:${asset.port} ${res.token}@${host}`);
    }
  };

  const openRdp = async() => {
    const res = await addToken(asset, "rdp");
    if (res) {
      downloadRdpFile(asset, res.token, res.username);
      Setting.showMessage("success", i18next.t("asset:Open the downloaded file to connect, within two minutes"));
    }
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button size="icon" variant="outline" className="h-8 w-8" aria-label={i18next.t("asset:Open in a local client")} title={i18next.t("asset:Open in a local client")}>
          <MonitorUp />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-80">
        <DropdownMenuLabel className="font-normal text-xs text-muted-foreground">
          {i18next.t("asset:Links and commands work once, within two minutes")}
        </DropdownMenuLabel>
        {hasRdp ? (
          <DropdownMenuItem onSelect={openRdp}>
            <AppWindow />
            {i18next.t("asset:Open in Remote Desktop Connection")}
          </DropdownMenuItem>
        ) : null}
        {hasSsh && isSsh ? (
          <>
            <DropdownMenuItem onSelect={() => openUrl("ssh")}>
              <Terminal />
              {i18next.t("asset:Open in SSH client (Xshell, SecureCRT, Termius)")}
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => openUrl("sftp")}>
              <FolderOpen />
              {i18next.t("asset:Open in SFTP client (WinSCP, FileZilla)")}
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={copySshCommand}>
              <Copy />
              {i18next.t("asset:Copy one-time SSH command")}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => copyText(getSshCommand(gateway, asset, account))}>
              <KeyRound />
              {i18next.t("asset:Copy SSH command for your public key")}
            </DropdownMenuItem>
          </>
        ) : null}
        {hasSsh && !isSsh ? (
          <>
            <DropdownMenuItem onSelect={copyTunnelCommand}>
              <Cable />
              {i18next.t("asset:Copy one-time port forwarding command")}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => copyText(getTunnelCommand(gateway, asset, account))}>
              <KeyRound />
              {i18next.t("asset:Copy port forwarding command for your public key")}
            </DropdownMenuItem>
            <DropdownMenuLabel className="font-normal text-xs text-muted-foreground">
              {i18next.t("asset:Keep it running, then connect your client to")} localhost:{getTunnelLocalPort(asset)}
            </DropdownMenuLabel>
          </>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
