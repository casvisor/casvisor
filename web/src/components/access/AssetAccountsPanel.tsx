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
import {KeyRound, Pencil, Plus, RefreshCw, Trash2, Upload} from "lucide-react";
import * as AssetAccountBackend from "@/backend/AssetAccountBackend";
import type {AssetAccount} from "@/backend/types";
import {Badge} from "@/components/ui/badge";
import {Button} from "@/components/ui/button";
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle} from "@/components/ui/dialog";
import {Input} from "@/components/ui/input";
import {Switch} from "@/components/ui/switch";
import {Table, TableBody, TableCell, TableHead, TableHeader, TableRow} from "@/components/ui/table";
import {Textarea} from "@/components/ui/textarea";
import {Tooltip, TooltipContent, TooltipTrigger} from "@/components/ui/tooltip";
import {ConfirmButton} from "@/components/common/ConfirmButton";
import {PasswordInput} from "@/components/common/PasswordInput";
import * as Setting from "@/lib/setting";

function newAccount(owner: string, asset: string): AssetAccount {
  return {
    owner,
    name: "",
    createdTime: "",
    updatedTime: "",
    asset,
    username: "",
    displayName: "",
    description: "",
    password: "",
    privateKey: "",
    isEnabled: true,
    autoRotate: false,
    rotateIntervalDays: 30,
    lastRotatedTime: "",
    rotateStatus: "",
    rotateMessage: "",
  };
}

function RotateStatus({account}: {account: AssetAccount}) {
  if (!account.rotateStatus) {
    return <span className="text-muted-foreground">-</span>;
  }
  const variant = account.rotateStatus === "Succeeded" ? "success" : account.rotateStatus === "Failed" ? "destructive" : "info";
  const badge = <Badge variant={variant}>{i18next.t(`account:${account.rotateStatus}`)}</Badge>;
  if (!account.rotateMessage) {
    return badge;
  }
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span>{badge}</span>
      </TooltipTrigger>
      <TooltipContent className="max-w-md break-words">{account.rotateMessage}</TooltipContent>
    </Tooltip>
  );
}

/**
 * The system accounts of an asset besides its own account. Users connect as them
 * without seeing their passwords, and Casvisor can change those passwords on SSH
 * assets through the asset's own account.
 */
export function AssetAccountsPanel({owner, asset, protocol, ownUsername}: {owner: string; asset: string; protocol: string; ownUsername: string}) {
  const [accounts, setAccounts] = React.useState<AssetAccount[]>([]);
  const [editing, setEditing] = React.useState<AssetAccount | null>(null);
  const [saving, setSaving] = React.useState(false);
  const [busy, setBusy] = React.useState("");
  const canRotate = protocol === "SSH";

  const load = React.useCallback(() => {
    AssetAccountBackend.getAssetAccounts(owner, asset).then((res) => {
      if (res.status === "ok") {
        setAccounts(res.data ?? []);
      } else {
        Setting.showMessage("error", res.msg);
      }
    });
  }, [owner, asset]);

  React.useEffect(load, [load]);

  const save = async() => {
    if (!editing) {
      return;
    }
    setSaving(true);
    try {
      const res = editing.name === "" ?
        await AssetAccountBackend.addAssetAccount(editing) :
        await AssetAccountBackend.updateAssetAccount(editing.owner, editing.name, editing);
      if (res.status === "ok") {
        Setting.showMessage("success", i18next.t("general:Successfully saved"));
        setEditing(null);
        load();
      } else {
        Setting.showMessage("error", `${i18next.t("general:Failed to save")}: ${res.msg}`);
      }
    } finally {
      setSaving(false);
    }
  };

  const run = async(account: AssetAccount, isPush: boolean) => {
    setBusy(`${account.name}:${isPush}`);
    try {
      const res = isPush ?
        await AssetAccountBackend.pushAssetAccount(account.owner, account.name) :
        await AssetAccountBackend.rotateAssetAccount(account.owner, account.name);
      if (res.status === "ok") {
        Setting.showMessage("success", isPush ? i18next.t("account:The account is set on the asset") : i18next.t("account:The password has been changed"));
      } else {
        Setting.showMessage("error", res.msg);
      }
    } finally {
      setBusy("");
      load();
    }
  };

  const remove = async(account: AssetAccount) => {
    const res = await AssetAccountBackend.deleteAssetAccount(account);
    if (res.status === "ok") {
      Setting.showMessage("success", i18next.t("general:Successfully deleted"));
      load();
    } else {
      Setting.showMessage("error", `${i18next.t("general:Failed to delete")}: ${res.msg}`);
    }
  };

  const update = (patch: Partial<AssetAccount>) => setEditing((prev) => (prev ? {...prev, ...patch} : prev));

  return (
    <div className="w-full space-y-2">
      <div className="flex items-start justify-between gap-2">
        <p className="text-xs text-muted-foreground">
          {i18next.t("account:Users connect as these accounts without seeing their passwords. The asset's own account above is the default account, and Casvisor uses it to change the passwords of the others")}
          {ownUsername ? ` (${ownUsername})` : ""}
        </p>
        <Button size="sm" variant="outline" onClick={() => setEditing(newAccount(owner, asset))}>
          <Plus />
          {i18next.t("general:Add")}
        </Button>
      </div>
      {accounts.length > 0 ? (
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{i18next.t("general:Username")}</TableHead>
                <TableHead>{i18next.t("account:Credential")}</TableHead>
                <TableHead>{i18next.t("general:Is enabled")}</TableHead>
                <TableHead>{i18next.t("account:Auto rotate")}</TableHead>
                <TableHead>{i18next.t("account:Last rotated")}</TableHead>
                <TableHead className="text-right">{i18next.t("general:Action")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {accounts.map((account) => (
                <TableRow key={account.name}>
                  <TableCell>
                    <div className="font-mono">
                      {account.username}
                      {account.template ? <Badge variant="outline" className="ml-1.5 font-sans" title={i18next.t("accountTemplate:From the template")}>{account.template}</Badge> : null}
                    </div>
                    {account.displayName ? <div className="text-xs text-muted-foreground">{account.displayName}</div> : null}
                  </TableCell>
                  <TableCell>
                    <div className="flex gap-1">
                      {account.password ? <Badge variant="secondary">{i18next.t("general:Password")}</Badge> : null}
                      {account.privateKey ? <Badge variant="secondary">{i18next.t("asset:Private key")}</Badge> : null}
                    </div>
                  </TableCell>
                  <TableCell>{account.isEnabled ? i18next.t("general:Yes") : i18next.t("general:No")}</TableCell>
                  <TableCell>
                    {account.autoRotate ? i18next.t("account:Every {{days}} days", {days: account.rotateIntervalDays}) : i18next.t("general:No")}
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center gap-2 whitespace-nowrap">
                      <span className="tabular-nums">{account.lastRotatedTime ? Setting.getFormattedDate(account.lastRotatedTime) : "-"}</span>
                      {account.rotateStatus ? <RotateStatus account={account} /> : null}
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className="flex justify-end gap-1">
                      <Button size="iconSm" variant="ghost" aria-label={i18next.t("general:Edit")} onClick={() => setEditing({...account})}>
                        <Pencil />
                      </Button>
                      {canRotate ? (
                        <>
                          <ConfirmButton
                            size="iconSm"
                            variant="ghost"
                            destructive={false}
                            aria-label={i18next.t("account:Change password now")}
                            title={i18next.t("account:Change password now")}
                            description={i18next.t("account:Casvisor sets a new random password for this account on the asset and keeps it. Connected sessions are not affected")}
                            loading={busy === `${account.name}:false`}
                            disabled={account.password === "" || account.privateKey !== ""}
                            onConfirm={() => run(account, false)}
                          >
                            <RefreshCw />
                          </ConfirmButton>
                          <ConfirmButton
                            size="iconSm"
                            variant="ghost"
                            destructive={false}
                            aria-label={i18next.t("account:Push to asset")}
                            title={i18next.t("account:Push to asset")}
                            description={i18next.t("account:Casvisor creates this account on the asset if it does not exist and sets the saved password there")}
                            loading={busy === `${account.name}:true`}
                            disabled={account.password === ""}
                            onConfirm={() => run(account, true)}
                          >
                            <Upload />
                          </ConfirmButton>
                        </>
                      ) : null}
                      <ConfirmButton size="iconSm" variant="ghost" aria-label={i18next.t("general:Delete")} onConfirm={() => remove(account)}>
                        <Trash2 />
                      </ConfirmButton>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : null}

      <Dialog open={editing !== null} onOpenChange={(open) => !open && setEditing(null)}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <KeyRound className="h-4 w-4" />
              {editing?.name ? i18next.t("account:Edit Account") : i18next.t("account:New Account")}
            </DialogTitle>
            <DialogDescription>{asset}</DialogDescription>
          </DialogHeader>
          {editing ? (
            <form
              className="space-y-3"
              onSubmit={(e) => {
                e.preventDefault();
                save();
              }}
            >
              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-1.5">
                  <label className="text-sm font-medium">{i18next.t("general:Username")}</label>
                  <Input value={editing.username} autoComplete="off" onChange={(e) => update({username: e.target.value})} />
                </div>
                <div className="space-y-1.5">
                  <label className="text-sm font-medium">{i18next.t("general:Display name")}</label>
                  <Input value={editing.displayName} onChange={(e) => update({displayName: e.target.value})} />
                </div>
              </div>
              <div className="space-y-1.5">
                <label className="text-sm font-medium">{protocol === "Kubernetes" ? i18next.t("asset:Token") : i18next.t("general:Password")}</label>
                <PasswordInput value={editing.password} autoComplete="new-password" onChange={(e) => update({password: e.target.value})} />
              </div>
              {protocol === "SSH" ? (
                <div className="space-y-1.5">
                  <label className="text-sm font-medium">{i18next.t("asset:Private key")}</label>
                  <Textarea
                    value={editing.privateKey}
                    rows={3}
                    className="font-mono text-xs"
                    placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                    onChange={(e) => update({privateKey: e.target.value})}
                  />
                </div>
              ) : null}
              <div className="space-y-1.5">
                <label className="text-sm font-medium">{i18next.t("general:Description")}</label>
                <Input value={editing.description} onChange={(e) => update({description: e.target.value})} />
              </div>
              <div className="flex items-center justify-between">
                <label className="text-sm font-medium">{i18next.t("general:Is enabled")}</label>
                <Switch checked={editing.isEnabled} onCheckedChange={(isEnabled) => update({isEnabled})} />
              </div>
              {canRotate ? (
                <div className="space-y-2 rounded-md border p-3">
                  <div className="flex items-center justify-between">
                    <label className="text-sm font-medium">{i18next.t("account:Auto rotate")}</label>
                    <Switch checked={editing.autoRotate} onCheckedChange={(autoRotate) => update({autoRotate})} />
                  </div>
                  {editing.autoRotate ? (
                    <div className="flex items-center gap-2 text-sm">
                      <span>{i18next.t("account:Change the password every")}</span>
                      <Input
                        type="number"
                        min={1}
                        className="w-20"
                        value={editing.rotateIntervalDays}
                        onChange={(e) => update({rotateIntervalDays: Setting.myParseInt(e.target.value)})}
                      />
                      <span>{i18next.t("account:days")}</span>
                    </div>
                  ) : null}
                  <p className="text-xs text-muted-foreground">
                    {i18next.t("account:Auto rotate - Tooltip")}
                  </p>
                </div>
              ) : null}
              <DialogFooter>
                <Button type="button" variant="ghost" onClick={() => setEditing(null)}>{i18next.t("general:Cancel")}</Button>
                <Button type="submit" loading={saving} disabled={!editing.username}>{i18next.t("general:Save")}</Button>
              </DialogFooter>
            </form>
          ) : null}
        </DialogContent>
      </Dialog>
    </div>
  );
}
