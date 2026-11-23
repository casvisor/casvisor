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
import {Download} from "lucide-react";
import * as AssetBackend from "@/backend/AssetBackend";
import type {KubernetesToken} from "@/backend/types";
import {saveBlob} from "@/components/access/KubernetesLogs";
import {ConfirmButton} from "@/components/common/ConfirmButton";
import {Button} from "@/components/ui/button";
import {Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle} from "@/components/ui/dialog";
import * as Setting from "@/lib/setting";

/** Hands out kubeconfigs for kubectl, and lists the ones still valid to revoke them. */
export default function KubeconfigDialog({assetId, account, onClose}: {assetId: string; account?: string; onClose: () => void}) {
  const assetName = Setting.getShortName(assetId);
  const fileName = `casvisor-${assetName}.yaml`;
  const [tokens, setTokens] = React.useState<KubernetesToken[] | null>(null);
  const [busy, setBusy] = React.useState(false);
  const [issued, setIssued] = React.useState(false);

  const refresh = () => {
    AssetBackend.getKubernetesTokens(assetId).then((res) => {
      if (res.status === "ok") {
        setTokens(res.data ?? []);
      } else {
        Setting.showMessage("error", res.msg);
      }
    });
  };

  React.useEffect(refresh, [assetId]);

  const issue = () => {
    setBusy(true);
    AssetBackend.addKubernetesToken(assetId, account).then((res) => {
      setBusy(false);
      if (res.status !== "ok") {
        Setting.showMessage("error", res.msg);
        return;
      }
      saveBlob(new Blob([res.data.kubeconfig], {type: "application/yaml"}), fileName);
      setIssued(true);
      refresh();
    });
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{i18next.t("kubernetes:kubectl")}</DialogTitle>
          <DialogDescription>{i18next.t("kubernetes:Kubeconfig - Description")}</DialogDescription>
        </DialogHeader>
        <div className="space-y-4 text-sm">
          <div className="flex flex-wrap items-center gap-3">
            <Button onClick={issue} disabled={busy} className="gap-1.5">
              <Download className="h-4 w-4" />
              {i18next.t("kubernetes:Download kubeconfig")}
            </Button>
            <span className="text-xs text-muted-foreground">{i18next.t("kubernetes:Valid for 8 hours")}</span>
          </div>
          {issued ? (
            <pre className="overflow-x-auto rounded-md bg-muted px-3 py-2 font-mono text-xs">{`export KUBECONFIG=~/Downloads/${fileName}\nkubectl get pods\nkubectl exec -it <pod> -- sh`}</pre>
          ) : null}
          <div>
            <div className="mb-2 font-medium">{i18next.t("kubernetes:Valid kubeconfigs")}</div>
            {tokens === null ? (
              <div className="text-muted-foreground">{i18next.t("general:Loading")}...</div>
            ) : tokens.length === 0 ? (
              <div className="text-muted-foreground">{i18next.t("kubernetes:No valid kubeconfigs")}</div>
            ) : (
              <div className="max-h-64 overflow-auto rounded-md border">
                <table className="w-full text-xs">
                  <thead className="bg-muted/50 text-left text-muted-foreground">
                    <tr>
                      <th className="px-3 py-2 font-medium">{i18next.t("general:User")}</th>
                      <th className="px-3 py-2 font-medium">{i18next.t("account:Account")}</th>
                      <th className="px-3 py-2 font-medium">{i18next.t("general:Client IP")}</th>
                      <th className="px-3 py-2 font-medium">{i18next.t("kubernetes:Issued")}</th>
                      <th className="px-3 py-2 font-medium">{i18next.t("kubernetes:Expires")}</th>
                      <th className="px-3 py-2" />
                    </tr>
                  </thead>
                  <tbody>
                    {tokens.map((token) => (
                      <tr key={token.name} className="border-t">
                        <td className="px-3 py-2">{token.user}</td>
                        <td className="px-3 py-2 font-mono">{token.account || i18next.t("account:Default")}</td>
                        <td className="px-3 py-2">{token.clientIp}</td>
                        <td className="whitespace-nowrap px-3 py-2 tabular-nums">{Setting.getFormattedDate(token.createdTime)}</td>
                        <td className="whitespace-nowrap px-3 py-2 tabular-nums">{Setting.getFormattedDate(token.expireTime)}</td>
                        <td className="px-3 py-2 text-right">
                          <ConfirmButton
                            size="sm"
                            variant="ghost"
                            className="h-7 px-2 text-xs text-destructive"
                            title={i18next.t("kubernetes:Sure to revoke this kubeconfig?")}
                            description={i18next.t("kubernetes:Revoke - Description")}
                            confirmText={i18next.t("kubernetes:Revoke")}
                            onConfirm={async() => {
                              const res = await AssetBackend.revokeKubernetesToken(`${token.owner}/${token.name}`);
                              if (res.status === "ok") {
                                refresh();
                              } else {
                                Setting.showMessage("error", res.msg);
                              }
                            }}
                          >
                            {i18next.t("kubernetes:Revoke")}
                          </ConfirmButton>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
