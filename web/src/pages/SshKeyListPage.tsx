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
import {Copy, Plus} from "lucide-react";
import * as SshKeyBackend from "@/backend/SshKeyBackend";
import type {SshKey} from "@/backend/types";
import {ConfirmButton} from "@/components/common/ConfirmButton";
import {PageHeader} from "@/components/crud/PageHeader";
import {Button} from "@/components/ui/button";
import {Card, CardContent, CardDescription, CardHeader, CardTitle} from "@/components/ui/card";
import {Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle} from "@/components/ui/dialog";
import {Input} from "@/components/ui/input";
import {Table, TableBody, TableCell, TableHead, TableHeader, TableRow} from "@/components/ui/table";
import {Textarea} from "@/components/ui/textarea";
import {useAccount} from "@/hooks/use-account";
import {copyText, getGatewayHost, useSshGateway} from "@/lib/gateway";
import * as Setting from "@/lib/setting";

function CommandLine({command}: {command: string}) {
  return (
    <div className="flex items-center gap-2 rounded-md bg-muted px-3 py-2 font-mono text-sm">
      <span className="min-w-0 flex-1 break-all">{command}</span>
      <Button size="icon" variant="ghost" className="h-7 w-7 shrink-0" aria-label={i18next.t("general:Copy")} onClick={() => copyText(command)}>
        <Copy />
      </Button>
    </div>
  );
}

/** The public keys the signed-in user reaches SSH assets with through the SSH gateway. */
export default function SshKeyListPage() {
  const {account} = useAccount();
  const gateway = useSshGateway();
  const [sshKeys, setSshKeys] = React.useState<SshKey[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [open, setOpen] = React.useState(false);
  const [displayName, setDisplayName] = React.useState("");
  const [publicKey, setPublicKey] = React.useState("");
  const [adding, setAdding] = React.useState(false);

  const refresh = React.useCallback(() => {
    setLoading(true);
    SshKeyBackend.getSshKeys().then((res) => {
      setLoading(false);
      if (res.status === "ok") {
        setSshKeys(res.data ?? []);
      } else {
        Setting.showMessage("error", res.msg);
      }
    });
  }, []);

  React.useEffect(() => {
    refresh();
  }, [refresh]);

  const add = () => {
    setAdding(true);
    SshKeyBackend.addSshKey({owner: account!.owner, displayName: displayName.trim(), publicKey: publicKey.trim()}).then((res) => {
      setAdding(false);
      if (res.status === "ok") {
        setOpen(false);
        setDisplayName("");
        setPublicKey("");
        refresh();
      } else {
        Setting.showMessage("error", res.msg);
      }
    });
  };

  const remove = (sshKey: SshKey) => SshKeyBackend.deleteSshKey(sshKey).then((res) => {
    if (res.status === "ok") {
      refresh();
    } else {
      Setting.showMessage("error", res.msg);
    }
  });

  const host = gateway ? getGatewayHost(gateway) : "";
  const port = gateway?.port && gateway.port !== "22" ? gateway.port : "";
  const sshPort = port ? `-p ${port} ` : "";
  const scpPort = port ? `-P ${port} ` : "";

  return (
    <div className="space-y-4">
      <PageHeader
        title={i18next.t("general:SSH keys")}
        description={i18next.t("general:Connect to SSH assets with ssh, scp, sftp, Xshell or VS Code")}
        actions={(
          <Button onClick={() => setOpen(true)}>
            <Plus />
            {i18next.t("sshKey:Add SSH key")}
          </Button>
        )}
      />

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{i18next.t("sshKey:How to connect")}</CardTitle>
          <CardDescription>
            {gateway
              ? i18next.t("sshKey:Add your public key below, then connect with the asset name as the username")
              : i18next.t("sshKey:The SSH gateway is not enabled, set sshGatewayPort in conf/app.conf to enable it")}
          </CardDescription>
        </CardHeader>
        {gateway ? (
          <CardContent className="space-y-2">
            <CommandLine command={`ssh ${sshPort}<asset>@${host}`} />
            <CommandLine command={`ssh ${sshPort}${account?.name ?? "<user>"}@${host}`} />
            <CommandLine command={`sftp ${scpPort}<asset>@${host}`} />
            <CommandLine command={`scp ${scpPort}<file> <asset>@${host}:<path>`} />
            <p className="pt-1 text-sm text-muted-foreground">
              {i18next.t("sshKey:Connecting with your own username shows a menu of your SSH assets")}
            </p>
            <p className="text-sm text-muted-foreground">
              {i18next.t("sshKey:Host key fingerprint")}: <span className="font-mono">{gateway.fingerprint}</span>
            </p>
          </CardContent>
        ) : null}
      </Card>

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{i18next.t("general:Display name")}</TableHead>
              <TableHead>{i18next.t("sshKey:Fingerprint")}</TableHead>
              <TableHead className="w-44">{i18next.t("general:Created time")}</TableHead>
              <TableHead className="w-44">{i18next.t("sshKey:Last used time")}</TableHead>
              <TableHead className="w-24" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {sshKeys.length === 0 ? (
              <TableRow>
                <TableCell colSpan={5} className="h-20 text-center text-muted-foreground">
                  {loading ? i18next.t("general:Loading") : i18next.t("sshKey:No SSH keys yet")}
                </TableCell>
              </TableRow>
            ) : sshKeys.map((sshKey) => (
              <TableRow key={sshKey.name}>
                <TableCell className="font-medium">{sshKey.displayName || "-"}</TableCell>
                <TableCell className="break-all font-mono text-xs">{sshKey.fingerprint}</TableCell>
                <TableCell>{Setting.getFormattedDate(sshKey.createdTime)}</TableCell>
                <TableCell>{sshKey.lastUsedTime ? Setting.getFormattedDate(sshKey.lastUsedTime) : "-"}</TableCell>
                <TableCell className="text-right">
                  <ConfirmButton size="sm" variant="destructive" onConfirm={() => remove(sshKey)}>
                    {i18next.t("general:Delete")}
                  </ConfirmButton>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{i18next.t("sshKey:Add SSH key")}</DialogTitle>
          </DialogHeader>
          <form
            className="space-y-3"
            onSubmit={(e) => {
              e.preventDefault();
              add();
            }}
          >
            <Input
              placeholder={i18next.t("general:Display name")}
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
            />
            <Textarea
              rows={6}
              className="font-mono text-xs"
              placeholder="ssh-ed25519 AAAA... user@laptop"
              value={publicKey}
              autoFocus
              onChange={(e) => setPublicKey(e.target.value)}
            />
            <p className="text-sm text-muted-foreground">
              {i18next.t("sshKey:Paste the content of ~/.ssh/id_ed25519.pub, or create a key with ssh-keygen -t ed25519")}
            </p>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setOpen(false)}>{i18next.t("general:Cancel")}</Button>
              <Button type="submit" disabled={!publicKey.trim()} loading={adding}>{i18next.t("general:Add")}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
