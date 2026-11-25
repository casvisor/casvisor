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
import dayjs from "dayjs";
import * as AccountTemplateBackend from "@/backend/AccountTemplateBackend";
import type {AccountTemplate, AssetImportResult, AssetImportSummary} from "@/backend/types";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {boolColumn, linkColumn, tagsColumn, textColumn} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {Badge} from "@/components/ui/badge";
import {Button} from "@/components/ui/button";
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle} from "@/components/ui/dialog";
import {Table, TableBody, TableCell, TableHead, TableHeader, TableRow} from "@/components/ui/table";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";

const actionVariants: Record<string, "success" | "info" | "secondary" | "destructive"> = {
  create: "success",
  update: "info",
  skip: "secondary",
  error: "destructive",
};

/** Applies a template to its assets: a preview first, then the changes. */
export function ApplyTemplateDialog({template, onClose}: {template: AccountTemplate; onClose: () => void}) {
  const id = `${template.owner}/${template.name}`;
  const [busy, setBusy] = React.useState(false);
  const [applied, setApplied] = React.useState(false);
  const [results, setResults] = React.useState<AssetImportResult[]>([]);
  const [summary, setSummary] = React.useState<AssetImportSummary | null>(null);
  const [error, setError] = React.useState("");

  const run = React.useCallback(async(dryRun: boolean) => {
    setBusy(true);
    setError("");
    try {
      const res = await AccountTemplateBackend.applyAccountTemplate(id, dryRun);
      if (res.status !== "ok") {
        setError(res.msg);
        return;
      }
      setResults(res.data.results ?? []);
      setSummary(res.data.summary);
      if (!dryRun) {
        setApplied(true);
        Setting.showMessage("success", i18next.t("accountTemplate:The template has been applied"));
      }
    } finally {
      setBusy(false);
    }
  }, [id]);

  React.useEffect(() => {
    run(true);
  }, [run]);

  const toApply = summary ? summary.created + summary.updated : 0;

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="flex max-h-[85vh] flex-col sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{i18next.t("accountTemplate:Apply to assets")}: {template.displayName || template.name}</DialogTitle>
          <DialogDescription>
            {applied ? i18next.t("accountTemplate:Applied, this is what changed") : i18next.t("accountTemplate:A preview of what applying changes, nothing is saved yet")}
          </DialogDescription>
        </DialogHeader>
        {error ? <p className="break-all text-sm text-destructive">{error}</p> : null}
        {summary ? (
          <div className="flex flex-wrap gap-1.5 text-xs">
            <Badge variant="success">{i18next.t("assetImport:create")} {summary.created}</Badge>
            <Badge variant="info">{i18next.t("assetImport:update")} {summary.updated}</Badge>
            <Badge variant="secondary">{i18next.t("assetImport:skip")} {summary.skipped}</Badge>
            <Badge variant="destructive">{i18next.t("assetImport:error")} {summary.failed}</Badge>
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
                    {busy ? i18next.t("general:Loading") : i18next.t("accountTemplate:The template covers no assets")}
                  </TableCell>
                </TableRow>
              ) : results.map((result) => (
                <TableRow key={result.row}>
                  <TableCell className="tabular-nums text-muted-foreground">{result.row}</TableCell>
                  <TableCell className="break-all font-mono text-xs">{result.name}</TableCell>
                  <TableCell><Badge variant={actionVariants[result.action]}>{i18next.t(`assetImport:${result.action}`)}</Badge></TableCell>
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
          <Button variant="ghost" onClick={onClose}>{i18next.t("general:Close")}</Button>
          {!applied ? (
            <Button loading={busy} disabled={!summary || toApply === 0} onClick={() => run(false)}>
              {i18next.t("accountTemplate:Apply")}
            </Button>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export default function AccountTemplateListPage() {
  const {account} = useAccount();
  const owner = Setting.getRequestOrganization(account);
  const [applying, setApplying] = React.useState<AccountTemplate | null>(null);

  const fetchTemplates = React.useCallback(
    (query: TableQuery) =>
      AccountTemplateBackend.getAccountTemplates(owner, {
        page: query.page,
        pageSize: query.pageSize,
        field: query.searchedColumn,
        value: query.searchText,
        sortField: query.sortField,
        sortOrder: query.sortOrder,
      }),
    [owner],
  );

  const newTemplate = (): AccountTemplate => ({
    owner: account!.owner,
    name: `account_template_${Setting.getRandomName()}`,
    createdTime: dayjs().format(),
    updatedTime: dayjs().format(),
    displayName: `New Account Template - ${Setting.getRandomName()}`,
    description: "",
    username: "ops",
    password: "",
    privateKey: "",
    randomPassword: true,
    autoRotate: true,
    rotateIntervalDays: 30,
    push: true,
    assets: [],
    assetTags: [],
  });

  const columns: ColumnDef<AccountTemplate>[] = [
    linkColumn<AccountTemplate>({dataIndex: "name", width: 180, to: (item) => `/account-templates/${Setting.getIdPath(item.owner, item.name)}`}),
    textColumn<AccountTemplate>({dataIndex: "displayName", title: i18next.t("general:Display name"), width: 180, searchable: true}),
    {
      dataIndex: "username",
      title: i18next.t("general:Username"),
      width: 130,
      render: (value) => <span className="font-mono">{value}</span>,
    },
    {
      dataIndex: "randomPassword",
      title: i18next.t("accountTemplate:Secret"),
      width: 170,
      render: (_value, item) => (item.randomPassword ? i18next.t("accountTemplate:A random password for each asset") : item.privateKey ? i18next.t("asset:Private key") : i18next.t("general:Password")),
    },
    boolColumn<AccountTemplate>({dataIndex: "push", title: i18next.t("accountTemplate:Push"), width: 90}),
    boolColumn<AccountTemplate>({dataIndex: "autoRotate", title: i18next.t("accountTemplate:Rotate"), width: 90}),
    tagsColumn<AccountTemplate>({dataIndex: "assets", title: i18next.t("general:Assets")}),
    tagsColumn<AccountTemplate>({dataIndex: "assetTags", title: i18next.t("permission:Asset tags")}),
  ];

  return (
    <>
      <CrudListPage<AccountTemplate>
        title={i18next.t("general:Account templates")}
        description={i18next.t("general:Give many assets the same system account at once, and keep them in line")}
        columns={columns}
        fetch={fetchTemplates}
        deps={[owner]}
        newRecord={newTemplate}
        add={AccountTemplateBackend.addAccountTemplate}
        remove={AccountTemplateBackend.deleteAccountTemplate}
        editUrl={(item) => `/account-templates/${Setting.getIdPath(item.owner, item.name)}`}
        actionColumnWidth={200}
        rowActions={(item) => [
          {key: "apply", label: i18next.t("accountTemplate:Apply"), onSelect: () => setApplying(item)},
        ]}
      />
      {applying ? <ApplyTemplateDialog template={applying} onClose={() => setApplying(null)} /> : null}
    </>
  );
}
