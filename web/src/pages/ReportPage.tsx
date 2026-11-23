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
import {Link, useSearchParams} from "react-router-dom";
import {CalendarClock, FileSpreadsheet, Printer, RefreshCw} from "lucide-react";
import * as ReportBackend from "@/backend/ReportBackend";
import type {ReportData} from "@/backend/types";
import {Loading} from "@/components/common/Loading";
import {MultiSelect} from "@/components/common/MultiSelect";
import {SelectField} from "@/components/common/SelectField";
import {PageHeader} from "@/components/crud/PageHeader";
import {ReportView, getReportRangeText} from "@/components/report/ReportView";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import {useAccount} from "@/hooks/use-account";
import {getWeekdayNames} from "@/lib/loginRule";
import * as Setting from "@/lib/setting";

/** Reads what a report covers from the query of the page, the same for the page and its print view. */
export function getReportQuery(params: URLSearchParams, owner: string): ReportBackend.ReportQuery {
  const weekdays = (params.get("weekdays") ?? "").split(",").filter((day) => day !== "").map(Number);
  return {
    owner,
    period: params.get("period") || "this-week",
    startDate: params.get("startDate") ?? "",
    endDate: params.get("endDate") ?? "",
    timeZone: params.get("timeZone") ?? (Intl.DateTimeFormat().resolvedOptions().timeZone || ""),
    weekdays: weekdays.length > 0 ? weekdays : [1, 2, 3, 4, 5],
    workStartTime: params.get("workStartTime") || "09:00",
    workEndTime: params.get("workEndTime") || "18:00",
  };
}

export function getPeriodOptions() {
  return ReportBackend.ReportPeriods.map((period) => ({id: period, name: i18next.t(`report:${period}`)}));
}

export function ReportMeta({data}: {data: ReportData}) {
  const names = getWeekdayNames();
  const days = [1, 2, 3, 4, 5, 6, 0].filter((day) => (data.weekdays ?? []).includes(day)).map((day) => names[day]).join(", ");
  return (
    <div className="flex flex-wrap gap-x-5 gap-y-1 text-sm text-muted-foreground">
      <span>{i18next.t("general:Organization")}: <span className="text-foreground">{data.owner}</span></span>
      <span>{i18next.t("report:Period")}: <span className="tabular-nums text-foreground">{getReportRangeText(data)}</span></span>
      <span>{i18next.t("loginRule:Time zone")}: <span className="text-foreground">{data.timeZone}</span></span>
      <span>{i18next.t("report:Working hours")}: <span className="text-foreground">{days} {data.workStartTime}-{data.workEndTime}</span></span>
      <span>{i18next.t("report:Generated at")}: <span className="tabular-nums text-foreground">{dayjs(data.generatedTime).format("YYYY-MM-DD HH:mm")}</span></span>
    </div>
  );
}

export default function ReportPage() {
  const {account} = useAccount();
  const owner = Setting.getRequestOrganization(account);
  const [params, setParams] = useSearchParams();
  const query = getReportQuery(params, owner);
  const [data, setData] = React.useState<ReportData | null>(null);
  const [error, setError] = React.useState("");
  const [loading, setLoading] = React.useState(false);
  const [downloading, setDownloading] = React.useState(false);
  const [nonce, setNonce] = React.useState(0);

  const search = ReportBackend.toSearchParams(query);
  search.delete("owner");
  const searchText = search.toString();

  React.useEffect(() => {
    if (!owner) {
      return;
    }
    const current = getReportQuery(new URLSearchParams(searchText), owner);
    if (current.period === "custom" && (!current.startDate || !current.endDate)) {
      setData(null);
      setError("");
      return;
    }
    setLoading(true);
    ReportBackend.getReportData(current).then((res) => {
      if (res.status === "ok") {
        setData(res.data);
        setError("");
      } else {
        setData(null);
        setError(res.msg);
      }
    }).finally(() => setLoading(false));
  }, [owner, searchText, nonce]);

  const update = (changes: Record<string, string>) => {
    const next = new URLSearchParams(searchText);
    for (const [key, value] of Object.entries(changes)) {
      next.set(key, value);
    }
    setParams(next, {replace: true});
  };

  const download = async() => {
    setDownloading(true);
    try {
      const message = await ReportBackend.downloadReport(query);
      if (message) {
        Setting.showMessage("error", message);
      }
    } finally {
      setDownloading(false);
    }
  };

  return (
    <div className="space-y-4">
      <PageHeader
        title={i18next.t("general:Reports")}
        description={i18next.t("general:Sign-ins, sessions, commands, file transfers, approvals and changes of a period, to print or export")}
        actions={
          <>
            <Button variant="outline" onClick={() => setNonce((n) => n + 1)} loading={loading}>
              <RefreshCw />
              {i18next.t("general:Refresh")}
            </Button>
            <Button variant="outline" disabled={!data} onClick={() => window.open(`/reports/print?${searchText}&autoPrint=1`, "_blank")}>
              <Printer />
              {i18next.t("report:Print or save as PDF")}
            </Button>
            <Button variant="outline" disabled={!data} loading={downloading} onClick={download}>
              <FileSpreadsheet />
              {i18next.t("report:Export Excel")}
            </Button>
            <Button variant="outline" asChild>
              <Link to="/reports/schedules">
                <CalendarClock />
                {i18next.t("report:Scheduled reports")}
              </Link>
            </Button>
          </>
        }
      />

      <div className="flex flex-wrap items-end gap-3 rounded-xl border p-3">
        <label className="space-y-1">
          <span className="text-xs text-muted-foreground">{i18next.t("report:Period")}</span>
          <SelectField
            className="w-44"
            value={query.period}
            options={[...getPeriodOptions(), {id: "custom", name: i18next.t("report:custom")}]}
            onChange={(value) => update(value === "custom" && !query.startDate ? {period: value, startDate: dayjs().startOf("month").format("YYYY-MM-DD"), endDate: dayjs().format("YYYY-MM-DD")} : {period: value})}
          />
        </label>
        {query.period === "custom" ? (
          <>
            <label className="space-y-1">
              <span className="text-xs text-muted-foreground">{i18next.t("report:From")}</span>
              <Input type="date" className="w-40" value={query.startDate} onChange={(e) => update({startDate: e.target.value})} />
            </label>
            <label className="space-y-1">
              <span className="text-xs text-muted-foreground">{i18next.t("report:To")}</span>
              <Input type="date" className="w-40" value={query.endDate} onChange={(e) => update({endDate: e.target.value})} />
            </label>
          </>
        ) : null}
        <label className="min-w-64 flex-1 space-y-1">
          <span className="text-xs text-muted-foreground">{i18next.t("report:Working days")}</span>
          <MultiSelect
            value={(query.weekdays ?? []).map(String)}
            options={[1, 2, 3, 4, 5, 6, 0].map((day) => ({value: String(day), label: getWeekdayNames()[day]}))}
            onChange={(value) => update({weekdays: value.map(Number).sort().join(",")})}
          />
        </label>
        <label className="space-y-1">
          <span className="text-xs text-muted-foreground">{i18next.t("report:Working hours")}</span>
          <div className="flex items-center gap-1.5">
            <Input type="time" className="w-28" value={query.workStartTime} onChange={(e) => update({workStartTime: e.target.value})} />
            <span className="text-muted-foreground">-</span>
            <Input type="time" className="w-28" value={query.workEndTime} onChange={(e) => update({workEndTime: e.target.value})} />
          </div>
        </label>
        <label className="space-y-1">
          <span className="text-xs text-muted-foreground">{i18next.t("loginRule:Time zone")}</span>
          <Input className="w-44" value={query.timeZone} placeholder="Asia/Shanghai" onChange={(e) => update({timeZone: e.target.value.trim()})} />
        </label>
      </div>

      {error ? <div className="rounded-md border border-destructive/50 px-3 py-2 text-sm text-destructive">{error}</div> : null}
      {data ? (
        <>
          <ReportMeta data={data} />
          <ReportView data={data} />
        </>
      ) : loading ? <Loading /> : null}
    </div>
  );
}
