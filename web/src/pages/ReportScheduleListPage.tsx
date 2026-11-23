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
import * as ReportBackend from "@/backend/ReportBackend";
import type {Report} from "@/backend/types";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {boolColumn} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {Badge} from "@/components/ui/badge";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";
import {BackupRunBadge} from "@/pages/AccountBackupListPage";

export async function sendReportNow(report: Report, onDone?: () => void) {
  const res = await ReportBackend.runReport(report.owner, report.name);
  if (res.status === "ok") {
    Setting.showMessage("success", i18next.t("report:The report was sent"));
  } else {
    Setting.showMessage("error", res.msg);
  }
  onDone?.();
}

export async function downloadSavedReport(report: Report) {
  const error = await ReportBackend.downloadSavedReport(report.owner, report.name);
  if (error) {
    Setting.showMessage("error", error);
  }
}

export default function ReportScheduleListPage() {
  const {account} = useAccount();
  const owner = Setting.getRequestOrganization(account);
  const [nonce, setNonce] = React.useState(0);

  const fetchReports = React.useCallback(
    (query: TableQuery) =>
      ReportBackend.getReports(owner, {
        page: query.page,
        pageSize: query.pageSize,
        field: query.searchedColumn,
        value: query.searchText,
        sortField: query.sortField,
        sortOrder: query.sortOrder,
      }),
    [owner],
  );

  const newReport = (): Report => ({
    owner: account!.owner,
    name: `report_${Setting.getRandomName()}`,
    createdTime: dayjs().format(),
    updatedTime: dayjs().format(),
    displayName: "",
    description: "",
    period: "last-week",
    sections: [],
    language: i18next.language?.startsWith("zh") ? "zh" : "en",
    weekdays: [1, 2, 3, 4, 5],
    workStartTime: "09:00",
    workEndTime: "18:00",
    webhooks: [],
    cron: "0 8 * * 1",
    timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone ?? "",
    isEnabled: true,
    nextRunTime: "",
    lastRunTime: "",
    lastRunStatus: "",
    lastRunMessage: "",
  });

  const columns: ColumnDef<Report>[] = [
    {
      dataIndex: "displayName",
      title: i18next.t("general:Name"),
      width: 200,
      searchable: true,
      render: (value, report) => (
        <Link to={`/reports/${report.owner}/${report.name}`} className="font-medium underline-offset-4 hover:underline">
          {value || report.name}
        </Link>
      ),
    },
    {
      dataIndex: "period",
      title: i18next.t("report:Period"),
      width: 120,
      render: (value: string) => i18next.t(`report:${value}`),
    },
    {
      dataIndex: "webhooks",
      title: i18next.t("accountBackup:Send with"),
      width: 180,
      render: (value: string[] | null) => (
        <div className="flex flex-wrap gap-1">
          {(value ?? []).map((name) => <Badge key={name} variant="secondary">{name}</Badge>)}
        </div>
      ),
    },
    {
      dataIndex: "cron",
      title: i18next.t("job:Schedule"),
      width: 170,
      render: (value, report) => (value ? (
        <div className="text-xs">
          <code className="font-mono">{value}</code>
          {report.nextRunTime ? <div className="text-muted-foreground">{i18next.t("job:Next run")}: {dayjs(report.nextRunTime).format("MM-DD HH:mm")}</div> : null}
        </div>
      ) : <span className="text-xs text-muted-foreground">{i18next.t("accountBackup:By hand only")}</span>),
    },
    {
      dataIndex: "lastRunTime",
      title: i18next.t("job:Last run"),
      width: 260,
      render: (value: string, report) => (value ? (
        <div className="text-xs">
          <div className="flex items-center gap-1.5">
            <BackupRunBadge status={report.lastRunStatus} />
            <span className="tabular-nums text-muted-foreground">{dayjs(value).format("YYYY-MM-DD HH:mm")}</span>
          </div>
          {report.lastRunMessage ? <div className="mt-0.5 line-clamp-2 break-all text-muted-foreground" title={report.lastRunMessage}>{report.lastRunMessage}</div> : null}
        </div>
      ) : <span className="text-xs text-muted-foreground">{i18next.t("job:Not run yet")}</span>),
    },
    boolColumn<Report>({dataIndex: "isEnabled", title: i18next.t("permission:Is enabled"), width: 110}),
  ];

  return (
    <CrudListPage<Report>
      key={nonce}
      title={i18next.t("report:Scheduled reports")}
      description={i18next.t("report:Reports emailed as Excel workbooks on a cron schedule, or downloaded by hand")}
      columns={columns}
      fetch={fetchReports}
      deps={[owner]}
      newRecord={newReport}
      remove={ReportBackend.deleteReport}
      editUrl={(report) => `/reports/${report.owner}/${report.name}`}
      actionColumnWidth={240}
      rowActions={(report) => [
        {key: "download", label: i18next.t("report:Download"), onSelect: () => downloadSavedReport(report)},
        ...((report.webhooks ?? []).length > 0 ? [{key: "send", label: i18next.t("accountBackup:Send now"), onSelect: () => sendReportNow(report, () => setNonce((n) => n + 1))}] : []),
      ]}
    />
  );
}
