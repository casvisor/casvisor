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
import {FileUp} from "lucide-react";
import * as AccountBackupBackend from "@/backend/AccountBackupBackend";
import type {AccountRestoreReport, AssetImportResult, AssetImportSummary} from "@/backend/types";
import {Badge} from "@/components/ui/badge";
import {Button} from "@/components/ui/button";
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle} from "@/components/ui/dialog";
import {Input} from "@/components/ui/input";
import {Switch} from "@/components/ui/switch";
import {Table, TableBody, TableCell, TableHead, TableHeader, TableRow} from "@/components/ui/table";
import * as Setting from "@/lib/setting";

const actionVariants: Record<AssetImportResult["action"], "success" | "info" | "secondary" | "destructive"> = {
  create: "success",
  update: "info",
  skip: "secondary",
  error: "destructive",
};

function SummaryBadges({label, summary}: {label: string; summary: AssetImportSummary}) {
  return (
    <div className="flex flex-wrap items-center gap-2 text-sm">
      <span className="w-20 font-medium">{label}</span>
      <Badge variant="success">{i18next.t("assetImport:create")} {summary.created}</Badge>
      <Badge variant="info">{i18next.t("assetImport:update")} {summary.updated}</Badge>
      <Badge variant="secondary">{i18next.t("assetImport:skip")} {summary.skipped}</Badge>
      <Badge variant="destructive">{i18next.t("assetImport:error")} {summary.failed}</Badge>
    </div>
  );
}

function ResultTable({kind, results}: {kind: string; results: AssetImportResult[]}) {
  return results.map((result) => (
    <TableRow key={`${kind}-${result.row}`}>
      <TableCell className="text-xs text-muted-foreground">{kind}</TableCell>
      <TableCell className="font-mono text-xs">{result.name}</TableCell>
      <TableCell><Badge variant={actionVariants[result.action]}>{i18next.t(`assetImport:${result.action}`)}</Badge></TableCell>
      <TableCell className="text-xs">
        {result.message ? <div className={result.action === "error" ? "text-destructive" : "text-muted-foreground"}>{result.message}</div> : null}
        {(result.changes ?? []).map((change) => <div key={change} className="text-muted-foreground">{change}</div>)}
      </TableCell>
    </TableRow>
  ));
}

/** Restores a backup's zip file: a preview first, then the restore. */
export function RestoreBackupDialog({owner, open, onOpenChange}: {owner: string; open: boolean; onOpenChange: (open: boolean) => void}) {
  const [file, setFile] = React.useState<File | null>(null);
  const [password, setPassword] = React.useState("");
  const [overwrite, setOverwrite] = React.useState(false);
  const [report, setReport] = React.useState<AccountRestoreReport | null>(null);
  const [restored, setRestored] = React.useState(false);
  const [busy, setBusy] = React.useState(false);
  const fileRef = React.useRef<HTMLInputElement>(null);

  React.useEffect(() => {
    if (open) {
      setFile(null);
      setPassword("");
      setOverwrite(false);
      setReport(null);
      setRestored(false);
    }
  }, [open]);

  const resetPreview = () => {
    setReport(null);
    setRestored(false);
  };

  const run = async(dryRun: boolean) => {
    if (!file) {
      return;
    }
    setBusy(true);
    try {
      const res = await AccountBackupBackend.restoreAccountBackup(owner, file, password, overwrite, dryRun);
      if (res.status !== "ok") {
        Setting.showMessage("error", res.msg);
        return;
      }
      setReport(res.data);
      if (!dryRun) {
        setRestored(true);
        const failed = res.data.assetSummary.failed + res.data.accountSummary.failed;
        Setting.showMessage(failed > 0 ? "warning" : "success", i18next.t("accountBackup:Restored"));
      }
    } catch (error) {
      Setting.showMessage("error", `${i18next.t("general:Failed to connect to server")}: ${error}`);
    } finally {
      setBusy(false);
    }
  };

  const toApply = report ? report.assetSummary.created + report.assetSummary.updated + report.accountSummary.created + report.accountSummary.updated : 0;
  const hasAssets = Boolean(report && (report.assets ?? []).length > 0);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{i18next.t("accountBackup:Restore a backup")}</DialogTitle>
          <DialogDescription>
            {i18next.t("accountBackup:Restore - Description")}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            <input
              ref={fileRef}
              type="file"
              accept=".zip,application/zip"
              className="hidden"
              onChange={(e) => {
                setFile(e.target.files?.[0] ?? null);
                resetPreview();
                e.target.value = "";
              }}
            />
            <Button variant="outline" size="sm" onClick={() => fileRef.current?.click()}>
              <FileUp />
              {i18next.t("accountBackup:Choose the zip file")}
            </Button>
            {file ? <span className="truncate text-xs text-muted-foreground">{file.name}</span> : null}
          </div>
          <Input
            type="password"
            autoComplete="off"
            value={password}
            placeholder={i18next.t("accountBackup:Password of the backup")}
            onChange={(e) => {
              setPassword(e.target.value);
              resetPreview();
            }}
          />
          <label className="flex items-center gap-2 text-sm">
            <Switch
              checked={overwrite}
              onCheckedChange={(checked) => {
                setOverwrite(checked);
                resetPreview();
              }}
            />
            {i18next.t("accountBackup:Overwrite the assets and accounts that already exist")}
          </label>
          <p className="text-xs text-muted-foreground">{i18next.t("accountBackup:Overwrite - Tooltip")}</p>

          {report ? (
            <div className="space-y-1.5">
              <div className="text-sm font-medium">{restored ? i18next.t("assetImport:Result") : i18next.t("assetImport:Preview")}</div>
              {hasAssets ? <SummaryBadges label={i18next.t("general:Assets")} summary={report.assetSummary} /> : null}
              <SummaryBadges label={i18next.t("accountBackup:Accounts")} summary={report.accountSummary} />
            </div>
          ) : null}

          {report && toApply + report.assetSummary.failed + report.accountSummary.failed + report.assetSummary.skipped + report.accountSummary.skipped > 0 ? (
            <div className="max-h-72 overflow-y-auto rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-20">{i18next.t("general:Type")}</TableHead>
                    <TableHead className="w-48">{i18next.t("general:Name")}</TableHead>
                    <TableHead className="w-24">{i18next.t("general:Action")}</TableHead>
                    <TableHead>{i18next.t("assetImport:Details")}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  <ResultTable kind={i18next.t("general:Assets")} results={report.assets ?? []} />
                  <ResultTable kind={i18next.t("accountBackup:Accounts")} results={report.accounts ?? []} />
                </TableBody>
              </Table>
            </div>
          ) : null}
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>{i18next.t(restored ? "general:Close" : "general:Cancel")}</Button>
          {restored ? null : (
            <>
              <Button variant="outline" loading={busy && !report} disabled={!file || !password} onClick={() => run(true)}>
                {i18next.t("assetImport:Preview")}
              </Button>
              <Button loading={busy && Boolean(report)} disabled={!report || toApply === 0} onClick={() => run(false)}>
                {i18next.t("accountBackup:Restore {{count}} items", {count: toApply})}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
