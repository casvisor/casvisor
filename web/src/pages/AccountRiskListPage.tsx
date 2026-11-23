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
import {Link} from "react-router-dom";
import {Loader2, ScanSearch} from "lucide-react";
import * as AccountTemplateBackend from "@/backend/AccountTemplateBackend";
import type {AccountRisk, AccountRiskScan} from "@/backend/types";
import {SelectField} from "@/components/common/SelectField";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {valueFilters} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {Badge} from "@/components/ui/badge";
import {Button} from "@/components/ui/button";
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle} from "@/components/ui/dialog";
import {Textarea} from "@/components/ui/textarea";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";

const levelVariants: Record<string, "destructive" | "warning" | "secondary"> = {
  High: "destructive",
  Medium: "warning",
  Low: "secondary",
};

const statusVariants: Record<string, "warning" | "secondary" | "success"> = {
  Open: "warning",
  Ignored: "secondary",
  Resolved: "success",
};

const allStatuses = "@all";

function IgnoreDialog({risk, onClose}: {risk: AccountRisk; onClose: (changed: boolean) => void}) {
  const [comment, setComment] = React.useState(risk.comment ?? "");
  const [saving, setSaving] = React.useState(false);
  const submit = async() => {
    setSaving(true);
    try {
      const res = await AccountTemplateBackend.setAccountRiskStatus(`${risk.owner}/${risk.name}`, "Ignored", comment);
      if (res.status === "ok") {
        onClose(true);
      } else {
        Setting.showMessage("error", res.msg);
      }
    } finally {
      setSaving(false);
    }
  };
  return (
    <Dialog open onOpenChange={(open) => !open && onClose(false)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{i18next.t("accountRisk:Ignore the risk")}</DialogTitle>
          <DialogDescription>{risk.asset} / {risk.username}: {risk.detail}</DialogDescription>
        </DialogHeader>
        <Textarea value={comment} rows={3} placeholder={i18next.t("accountRisk:Why it is fine, for the next one to read")} onChange={(e) => setComment(e.target.value)} />
        <DialogFooter>
          <Button variant="ghost" onClick={() => onClose(false)}>{i18next.t("general:Cancel")}</Button>
          <Button loading={saving} onClick={submit}>{i18next.t("accountRisk:Ignore")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export default function AccountRiskListPage() {
  const {account} = useAccount();
  const owner = Setting.getRequestOrganization(account);
  const [status, setStatus] = React.useState("Open");
  const [scan, setScan] = React.useState<AccountRiskScan | null>(null);
  const [ignoring, setIgnoring] = React.useState<{risk: AccountRisk; refresh: () => void} | null>(null);
  const refreshRef = React.useRef<() => void>(() => {});

  const loadScan = React.useCallback(() => {
    AccountTemplateBackend.getAccountRiskScan(owner).then((res) => res.status === "ok" && setScan(res.data));
  }, [owner]);

  React.useEffect(() => {
    loadScan();
  }, [loadScan]);

  // while a scan runs, follow it, and show what it found when it ends
  React.useEffect(() => {
    if (!scan?.running) {
      return;
    }
    const timer = setInterval(() => {
      AccountTemplateBackend.getAccountRiskScan(owner).then((res) => {
        if (res.status === "ok") {
          setScan(res.data);
          if (!res.data.running) {
            refreshRef.current();
          }
        }
      });
    }, 2000);
    return () => clearInterval(timer);
  }, [scan?.running, owner]);

  const startScan = async() => {
    const res = await AccountTemplateBackend.startAccountRiskScan(owner);
    if (res.status === "ok") {
      loadScan();
    } else {
      Setting.showMessage("error", res.msg);
    }
  };

  const reopen = async(risk: AccountRisk, refresh: () => void) => {
    const res = await AccountTemplateBackend.setAccountRiskStatus(`${risk.owner}/${risk.name}`, "Open", "");
    if (res.status === "ok") {
      refresh();
    } else {
      Setting.showMessage("error", res.msg);
    }
  };

  const fetchRisks = React.useCallback(
    (query: TableQuery) =>
      AccountTemplateBackend.getAccountRisks(owner, status === allStatuses ? "" : status, {
        page: query.page,
        pageSize: query.pageSize,
        field: query.searchedColumn,
        value: query.searchText,
        sortField: query.sortField,
        sortOrder: query.sortOrder,
      }),
    [owner, status],
  );

  const columns: ColumnDef<AccountRisk>[] = [
    {
      dataIndex: "level",
      title: i18next.t("accountRisk:Level"),
      width: 90,
      filters: valueFilters(["High", "Medium", "Low"]).map((filter) => ({...filter, label: i18next.t(`accountRisk:${filter.value}`)})),
      render: (value) => <Badge variant={levelVariants[value] ?? "secondary"}>{i18next.t(`accountRisk:${value}`)}</Badge>,
    },
    {
      dataIndex: "kind",
      title: i18next.t("accountRisk:Risk"),
      width: 170,
      filters: AccountTemplateBackend.RiskKinds.map((kind) => ({value: kind, label: i18next.t(`accountRisk:${kind}`)})),
      render: (value) => i18next.t(`accountRisk:${value}`),
    },
    {
      dataIndex: "asset",
      title: i18next.t("general:Asset"),
      width: 150,
      searchable: true,
      render: (value, risk) => <Link to={`/assets/${risk.owner}/${value}`} className="underline-offset-4 hover:underline">{value}</Link>,
    },
    {
      dataIndex: "username",
      title: i18next.t("general:Username"),
      width: 130,
      searchable: true,
      render: (value, risk) => (
        <span className="font-mono">
          {value}
          {!risk.managed ? <Badge variant="outline" className="ml-1.5 font-sans">{i18next.t("accountRisk:Not managed")}</Badge> : null}
        </span>
      ),
    },
    {
      dataIndex: "detail",
      title: i18next.t("accountRisk:Detail"),
      width: 320,
      render: (value, risk) => (
        <div className="text-xs">
          <div>{value}</div>
          {risk.comment ? <div className="mt-0.5 text-muted-foreground">{i18next.t("accountRisk:Comment")}: {risk.comment}</div> : null}
        </div>
      ),
    },
    {
      dataIndex: "status",
      title: i18next.t("general:Status"),
      width: 100,
      render: (value) => <Badge variant={statusVariants[value] ?? "secondary"}>{i18next.t(`accountRisk:${value}`)}</Badge>,
    },
    {
      dataIndex: "lastSeenTime",
      title: i18next.t("accountRisk:Last seen"),
      width: 140,
      render: (value, risk) => (
        <span className="text-xs tabular-nums" title={`${i18next.t("accountRisk:First seen")}: ${dayjs(risk.firstSeenTime).format("YYYY-MM-DD HH:mm")}`}>
          {dayjs(risk.status === "Resolved" && risk.resolvedTime ? risk.resolvedTime : value).format("YYYY-MM-DD HH:mm")}
        </span>
      ),
    },
  ];

  return (
    <>
      <CrudListPage<AccountRisk>
        title={i18next.t("general:Account risks")}
        description={i18next.t("general:Weak, old and reused passwords, and accounts on the assets that Casvisor does not manage")}
        columns={columns}
        fetch={fetchRisks}
        deps={[owner, status]}
        tableId="account-risks"
        actionColumnWidth={140}
        toolbar={({refresh}) => {
          refreshRef.current = refresh;
          return (
            <div className="flex flex-wrap items-center gap-2">
              <SelectField
                className="w-32"
                value={status}
                onChange={setStatus}
                options={[{id: allStatuses, name: i18next.t("accountRisk:All")}, ...AccountTemplateBackend.RiskStatuses.map((item) => ({id: item, name: i18next.t(`accountRisk:${item}`)}))]}
              />
              <Button onClick={startScan} disabled={scan?.running}>
                {scan?.running ? <Loader2 className="animate-spin" /> : <ScanSearch />}
                {scan?.running ? i18next.t("accountRisk:Scanning {{done}}/{{total}}", {done: scan.done, total: scan.total}) : i18next.t("accountRisk:Scan now")}
              </Button>
              {scan && !scan.running && scan.endTime ? (
                <span className="text-xs text-muted-foreground" title={(scan.errors ?? []).join("\n")}>
                  {i18next.t("accountRisk:Last scan {{time}}, {{found}} open", {time: dayjs(scan.endTime).format("MM-DD HH:mm"), found: scan.found})}
                  {(scan.errors ?? []).length > 0 ? `, ${i18next.t("accountRisk:{{n}} assets could not be read", {n: scan.errors.length})}` : ""}
                </span>
              ) : null}
            </div>
          );
        }}
        rowActions={(risk, _index, {refresh}) => [
          risk.status === "Open" ? {key: "ignore", label: i18next.t("accountRisk:Ignore"), onSelect: () => setIgnoring({risk, refresh})} : null,
          risk.status === "Ignored" ? {key: "reopen", label: i18next.t("accountRisk:Reopen"), onSelect: () => reopen(risk, refresh)} : null,
        ]}
      />
      {ignoring ? (
        <IgnoreDialog
          risk={ignoring.risk}
          onClose={(changed) => {
            const refresh = ignoring.refresh;
            setIgnoring(null);
            if (changed) {
              refresh();
            }
          }}
        />
      ) : null}
    </>
  );
}
