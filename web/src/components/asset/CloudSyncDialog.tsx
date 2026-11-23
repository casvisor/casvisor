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
import * as CloudAccountBackend from "@/backend/CloudAccountBackend";
import type {CloudAccount, CloudSyncResult, CloudSyncSummary} from "@/backend/types";
import {Badge} from "@/components/ui/badge";
import {Button} from "@/components/ui/button";
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle} from "@/components/ui/dialog";
import {Table, TableBody, TableCell, TableHead, TableHeader, TableRow} from "@/components/ui/table";
import * as Setting from "@/lib/setting";

const actionVariants: Record<CloudSyncResult["action"], "success" | "info" | "secondary" | "destructive" | "warning"> = {
  create: "success",
  update: "info",
  skip: "secondary",
  error: "destructive",
  missing: "warning",
  delete: "destructive",
};

/** Syncs the assets of a cloud account with its servers: a preview first, then the sync. */
export function CloudSyncDialog({account, onClose}: {account: CloudAccount; onClose: (synced: boolean) => void}) {
  const id = `${account.owner}/${account.name}`;
  const [busy, setBusy] = React.useState(false);
  const [applied, setApplied] = React.useState(false);
  const [results, setResults] = React.useState<CloudSyncResult[]>([]);
  const [summary, setSummary] = React.useState<CloudSyncSummary | null>(null);
  const [error, setError] = React.useState("");

  const run = React.useCallback(async(dryRun: boolean) => {
    setBusy(true);
    setError("");
    try {
      const res = await CloudAccountBackend.syncCloudAccount(id, dryRun);
      if (res.status !== "ok") {
        setError(res.msg);
        return;
      }
      setResults(res.data.results ?? []);
      setSummary(res.data.summary);
      if (!dryRun) {
        setApplied(true);
        Setting.showMessage("success", i18next.t("cloudAccount:The assets have been synced"));
      }
    } catch (e: any) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }, [id]);

  React.useEffect(() => {
    run(true);
  }, [run]);

  const toApply = summary ? summary.created + summary.updated + summary.deleted : 0;

  return (
    <Dialog open onOpenChange={(open) => !open && onClose(applied)}>
      <DialogContent className="flex max-h-[85vh] flex-col sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{i18next.t("cloudAccount:Sync assets")}: {account.displayName || account.name}</DialogTitle>
          <DialogDescription>
            {applied ? i18next.t("cloudAccount:The sync is done, this is what changed") : i18next.t("cloudAccount:A preview of what the sync changes, nothing is saved yet")}
          </DialogDescription>
        </DialogHeader>
        {error ? <p className="break-all text-sm text-destructive">{error}</p> : null}
        {summary ? (
          <div className="flex flex-wrap gap-1.5 text-xs">
            <Badge variant="outline">{i18next.t("cloudAccount:Servers")} {summary.instances}</Badge>
            <Badge variant="success">{i18next.t("assetImport:create")} {summary.created}</Badge>
            <Badge variant="info">{i18next.t("assetImport:update")} {summary.updated}</Badge>
            <Badge variant="secondary">{i18next.t("assetImport:skip")} {summary.skipped}</Badge>
            <Badge variant="destructive">{i18next.t("assetImport:error")} {summary.failed}</Badge>
            {summary.missing > 0 ? <Badge variant="warning">{i18next.t("cloudAccount:missing")} {summary.missing}</Badge> : null}
            {summary.deleted > 0 ? <Badge variant="destructive">{i18next.t("cloudAccount:delete")} {summary.deleted}</Badge> : null}
            {summary.createdNodes > 0 ? <Badge variant="outline">{i18next.t("assetImport:New nodes")} {summary.createdNodes}</Badge> : null}
          </div>
        ) : null}
        <div className="min-h-0 flex-1 overflow-auto rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-12">#</TableHead>
                <TableHead className="w-44">{i18next.t("general:Asset")}</TableHead>
                <TableHead className="w-24">{i18next.t("general:Action")}</TableHead>
                <TableHead>{i18next.t("assetImport:Details")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {results.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={4} className="py-8 text-center text-sm text-muted-foreground">
                    {busy ? i18next.t("cloudAccount:Listing the servers...") : i18next.t("cloudAccount:No servers found")}
                  </TableCell>
                </TableRow>
              ) : results.map((result) => (
                <TableRow key={result.row}>
                  <TableCell className="tabular-nums text-muted-foreground">{result.row}</TableCell>
                  <TableCell className="break-all font-mono text-xs">{result.name}</TableCell>
                  <TableCell>
                    <Badge variant={actionVariants[result.action]}>{i18next.t(result.action === "missing" || result.action === "delete" ? `cloudAccount:${result.action}` : `assetImport:${result.action}`)}</Badge>
                  </TableCell>
                  <TableCell className="break-all text-xs">
                    {result.message ? <div className={result.action === "error" ? "text-destructive" : "text-muted-foreground"}>{result.message}</div> : null}
                    {(result.changes ?? []).map((change, i) => <div key={i}>{change}</div>)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onClose(applied)}>{i18next.t("general:Close")}</Button>
          {!applied ? (
            <>
              <Button variant="outline" disabled={busy} onClick={() => run(true)}>{i18next.t("cloudAccount:Preview again")}</Button>
              <Button loading={busy} disabled={!summary || toApply === 0} onClick={() => run(false)}>
                {i18next.t("cloudAccount:Sync now")}
              </Button>
            </>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
