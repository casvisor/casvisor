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
import * as AssetBackend from "@/backend/AssetBackend";
import type {AssetImportResult, AssetImportSummary} from "@/backend/types";
import {Badge} from "@/components/ui/badge";
import {Button} from "@/components/ui/button";
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle} from "@/components/ui/dialog";
import {Switch} from "@/components/ui/switch";
import {Table, TableBody, TableCell, TableHead, TableHeader, TableRow} from "@/components/ui/table";
import {Textarea} from "@/components/ui/textarea";
import {AssetCsvColumns, AssetCsvTemplate, downloadText, readCsvFile} from "@/lib/assetNode";
import * as Setting from "@/lib/setting";

const actionVariants: Record<AssetImportResult["action"], "success" | "info" | "secondary" | "destructive"> = {
  create: "success",
  update: "info",
  skip: "secondary",
  error: "destructive",
};

function ActionBadge({action}: {action: AssetImportResult["action"]}) {
  return <Badge variant={actionVariants[action]}>{i18next.t(`assetImport:${action}`)}</Badge>;
}

/** Adds or updates assets from a CSV file: a preview first, then the import. */
export function AssetImportDialog({owner, open, onOpenChange, onImported}: {owner: string; open: boolean; onOpenChange: (open: boolean) => void; onImported: () => void}) {
  const [csv, setCsv] = React.useState("");
  const [fileName, setFileName] = React.useState("");
  const [update, setUpdate] = React.useState(false);
  const [results, setResults] = React.useState<AssetImportResult[] | null>(null);
  const [summary, setSummary] = React.useState<AssetImportSummary | null>(null);
  const [imported, setImported] = React.useState(false);
  const [busy, setBusy] = React.useState(false);
  const fileRef = React.useRef<HTMLInputElement>(null);

  React.useEffect(() => {
    if (open) {
      setCsv("");
      setFileName("");
      setUpdate(false);
      setResults(null);
      setSummary(null);
      setImported(false);
    }
  }, [open]);

  const resetPreview = () => {
    setResults(null);
    setSummary(null);
    setImported(false);
  };

  const pickFile = async(file: File | undefined) => {
    if (!file) {
      return;
    }
    try {
      setCsv(await readCsvFile(file));
      setFileName(file.name);
      resetPreview();
    } catch (error) {
      Setting.showMessage("error", `${error}`);
    }
  };

  const run = async(dryRun: boolean) => {
    setBusy(true);
    try {
      const res = await AssetBackend.importAssets(owner, csv, update, dryRun);
      if (res.status !== "ok") {
        Setting.showMessage("error", res.msg);
        return;
      }
      setResults(res.data ?? []);
      setSummary(res.data2 ?? null);
      if (!dryRun) {
        setImported(true);
        const s: AssetImportSummary = res.data2;
        Setting.showMessage(s.failed > 0 ? "warning" : "success",
          i18next.t("assetImport:Created {{created}}, updated {{updated}}, skipped {{skipped}}, failed {{failed}}", {...s}));
        onImported();
      }
    } finally {
      setBusy(false);
    }
  };

  const toApply = summary ? summary.created + summary.updated : 0;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{i18next.t("assetImport:Import assets")}</DialogTitle>
          <DialogDescription>
            {i18next.t("assetImport:A CSV file whose first line is the header. Only the name column is required; the protocol defaults to SSH and the port to the protocol's. Nodes are paths such as Beijing/Production, separated by \";\", and missing ones are created")}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            <input
              ref={fileRef}
              type="file"
              accept=".csv,.txt,text/csv"
              className="hidden"
              onChange={(e) => {
                pickFile(e.target.files?.[0]);
                e.target.value = "";
              }}
            />
            <Button variant="outline" size="sm" onClick={() => fileRef.current?.click()}>
              <FileUp />
              {i18next.t("assetImport:Choose a CSV file")}
            </Button>
            <Button variant="link" size="sm" onClick={() => downloadText("casvisor-assets-template.csv", AssetCsvTemplate)}>
              {i18next.t("assetImport:Download the template")}
            </Button>
            {fileName ? <span className="truncate text-xs text-muted-foreground">{fileName}</span> : null}
          </div>
          <Textarea
            value={csv}
            rows={7}
            spellCheck={false}
            className="font-mono text-xs"
            placeholder={AssetCsvColumns.join(",")}
            onChange={(e) => {
              setCsv(e.target.value);
              resetPreview();
            }}
          />
          <label className="flex items-center gap-2 text-sm">
            <Switch
              checked={update}
              onCheckedChange={(checked) => {
                setUpdate(checked);
                resetPreview();
              }}
            />
            {i18next.t("assetImport:Update the assets that already exist")}
          </label>
          <p className="text-xs text-muted-foreground">
            {i18next.t("assetImport:An existing asset keeps the values of the columns the file leaves out, and its password and private key when those cells are empty. Exported files have no passwords, so they can be edited and imported back")}
          </p>

          {summary ? (
            <div className="flex flex-wrap items-center gap-2 text-sm">
              <span className="font-medium">{imported ? i18next.t("assetImport:Result") : i18next.t("assetImport:Preview")}:</span>
              <Badge variant="success">{i18next.t("assetImport:create")} {summary.created}</Badge>
              <Badge variant="info">{i18next.t("assetImport:update")} {summary.updated}</Badge>
              <Badge variant="secondary">{i18next.t("assetImport:skip")} {summary.skipped}</Badge>
              <Badge variant="destructive">{i18next.t("assetImport:error")} {summary.failed}</Badge>
              {summary.createdNodes > 0 ? <Badge variant="outline">{i18next.t("assetImport:New nodes")} {summary.createdNodes}</Badge> : null}
            </div>
          ) : null}

          {results && results.length > 0 ? (
            <div className="max-h-72 overflow-y-auto rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-14">{i18next.t("assetImport:Row")}</TableHead>
                    <TableHead className="w-40">{i18next.t("general:Name")}</TableHead>
                    <TableHead className="w-24">{i18next.t("general:Action")}</TableHead>
                    <TableHead>{i18next.t("assetImport:Details")}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {results.map((result) => (
                    <TableRow key={result.row}>
                      <TableCell className="tabular-nums text-muted-foreground">{result.row}</TableCell>
                      <TableCell className="font-mono text-xs">{result.name}</TableCell>
                      <TableCell><ActionBadge action={result.action} /></TableCell>
                      <TableCell className="text-xs">
                        {result.message ? <div className={result.action === "error" ? "text-destructive" : "text-muted-foreground"}>{result.message}</div> : null}
                        {(result.changes ?? []).map((change) => <div key={change} className="text-muted-foreground">{change}</div>)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          ) : null}
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>{i18next.t(imported ? "general:Close" : "general:Cancel")}</Button>
          {imported ? null : (
            <>
              <Button variant="outline" loading={busy && !summary} disabled={!csv.trim()} onClick={() => run(true)}>
                {i18next.t("assetImport:Preview")}
              </Button>
              <Button loading={busy && Boolean(summary)} disabled={!summary || toApply === 0} onClick={() => run(false)}>
                {i18next.t("assetImport:Import {{count}} assets", {count: toApply})}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
