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
import {Link, useNavigate, useParams} from "react-router-dom";
import {Download, Send} from "lucide-react";
import * as JobScheduleBackend from "@/backend/JobScheduleBackend";
import * as ReportBackend from "@/backend/ReportBackend";
import * as WebhookBackend from "@/backend/WebhookBackend";
import type {Report, ReportSection, Webhook} from "@/backend/types";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import {Switch} from "@/components/ui/switch";
import {Loading} from "@/components/common/Loading";
import {MultiSelect} from "@/components/common/MultiSelect";
import {SelectField} from "@/components/common/SelectField";
import {UnauthorizedPage} from "@/components/common/UnauthorizedPage";
import {EditPageShell} from "@/components/crud/EditPageShell";
import {FormRow} from "@/components/crud/FormRow";
import {useEditRecord} from "@/hooks/use-edit-record";
import {getModeTitleKey, submitEdit} from "@/lib/crud";
import {getWeekdayNames} from "@/lib/loginRule";
import NotFoundPage from "@/pages/NotFoundPage";
import {BackupRunBadge} from "@/pages/AccountBackupListPage";
import {getPeriodOptions} from "@/pages/ReportPage";
import {downloadSavedReport, sendReportNow} from "@/pages/ReportScheduleListPage";

export function getSectionOptions() {
  return ReportBackend.ReportSections.map((section) => ({value: section, label: i18next.t(`report:section-${section}`)}));
}

export default function ReportEditPage() {
  const {owner = "", name = ""} = useParams();
  const navigate = useNavigate();
  const [saving, setSaving] = React.useState(false);
  const [busy, setBusy] = React.useState(false);
  const [webhooks, setWebhooks] = React.useState<Webhook[]>([]);
  const [runTimes, setRunTimes] = React.useState<string[]>([]);
  const [cronError, setCronError] = React.useState("");

  const {record: report, updateField, loading, notFound, denied, mode, setMode, reload} = useEditRecord<Report>({
    fetch: () => ReportBackend.getReport(owner, name),
    deps: [owner, name],
  });

  React.useEffect(() => {
    WebhookBackend.getWebhooks(owner).then((res) => {
      if (res.status === "ok") {
        setWebhooks((res.data ?? []).filter((webhook: Webhook) => webhook.type === "Email"));
      }
    });
  }, [owner]);

  const cron = report?.cron ?? "";
  const timeZone = report?.timeZone ?? "";
  React.useEffect(() => {
    if (!cron.trim()) {
      setRunTimes([]);
      setCronError("");
      return;
    }
    const timer = setTimeout(() => {
      JobScheduleBackend.getJobScheduleRunTimes(cron, timeZone).then((res) => {
        if (res.status === "ok") {
          setRunTimes(res.data ?? []);
          setCronError("");
        } else {
          setRunTimes([]);
          setCronError(res.msg);
        }
      });
    }, 300);
    return () => clearTimeout(timer);
  }, [cron, timeZone]);

  if (loading) {
    return <Loading />;
  }
  if (denied) {
    return <UnauthorizedPage />;
  }
  if (notFound || !report) {
    return <NotFoundPage />;
  }

  const save = (exitAfterSave: boolean) => {
    setSaving(true);
    return submitEdit({
      mode,
      record: report,
      add: ReportBackend.addReport,
      update: (record) => ReportBackend.updateReport(owner, name, record),
      onSaved: (record) => {
        if (exitAfterSave) {
          navigate("/reports/schedules");
        } else if (record.name !== name) {
          navigate(`/reports/${record.owner}/${encodeURIComponent(record.name)}`, {replace: true});
        } else {
          setMode("edit");
          reload();
        }
      },
      onFailed: () => updateField("name", name),
    }).finally(() => setSaving(false));
  };

  const act = async(action: () => Promise<void>) => {
    setBusy(true);
    try {
      await action();
    } finally {
      setBusy(false);
    }
  };

  return (
    <EditPageShell
      title={i18next.t(getModeTitleKey("report:Edit Report", mode))}
      description={i18next.t("report:Reports emailed as Excel workbooks on a cron schedule, or downloaded by hand")}
      mode={mode}
      backTo="/reports/schedules"
      onSave={save}
      saving={saving}
      remove={() => ReportBackend.deleteReport(report)}
      extraActions={mode === "edit" ? (
        <>
          <Button variant="outline" disabled={busy || saving} onClick={() => act(() => downloadSavedReport(report))}>
            <Download />
            {i18next.t("report:Download")}
          </Button>
          {(report.webhooks ?? []).length > 0 ? (
            <Button variant="outline" disabled={busy || saving} onClick={() => act(() => sendReportNow(report, reload))}>
              <Send />
              {i18next.t("accountBackup:Send now")}
            </Button>
          ) : null}
        </>
      ) : null}
      grid
    >
      <FormRow labelKey="general:Organization">
        <Input value={report.owner} disabled />
      </FormRow>
      <FormRow labelKey="general:Name">
        <Input value={report.name} onChange={(e) => updateField("name", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Display name">
        <Input value={report.displayName} onChange={(e) => updateField("displayName", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Description">
        <Input value={report.description} onChange={(e) => updateField("description", e.target.value)} />
      </FormRow>
      <FormRow label={i18next.t("report:Period")} tooltip={i18next.t("report:Period - Tooltip")}>
        <SelectField value={report.period} options={getPeriodOptions()} onChange={(value) => updateField("period", value)} />
      </FormRow>
      <FormRow label={i18next.t("report:Sections")} tooltip={i18next.t("report:Sections - Tooltip")}>
        <MultiSelect
          value={report.sections ?? []}
          options={getSectionOptions()}
          placeholder={i18next.t("report:All sections")}
          onChange={(value) => updateField("sections", value as ReportSection[])}
        />
      </FormRow>
      <FormRow label={i18next.t("report:Language")}>
        <SelectField value={report.language || "en"} options={[{id: "en", name: "English"}, {id: "zh", name: "中文"}]} onChange={(value) => updateField("language", value)} />
      </FormRow>
      <FormRow label={i18next.t("report:Working days")} tooltip={i18next.t("report:Working hours - Tooltip")}>
        <MultiSelect
          value={(report.weekdays ?? []).map(String)}
          options={[1, 2, 3, 4, 5, 6, 0].map((day) => ({value: String(day), label: getWeekdayNames()[day]}))}
          placeholder={i18next.t("report:Monday to Friday")}
          onChange={(value) => updateField("weekdays", value.map(Number).sort())}
        />
      </FormRow>
      <FormRow label={i18next.t("report:Working hours")} tooltip={i18next.t("report:Working hours - Tooltip")}>
        <div className="flex w-full items-center gap-2">
          <Input type="time" value={report.workStartTime} onChange={(e) => updateField("workStartTime", e.target.value)} />
          <span className="text-muted-foreground">-</span>
          <Input type="time" value={report.workEndTime} onChange={(e) => updateField("workEndTime", e.target.value)} />
        </div>
      </FormRow>
      <FormRow label={i18next.t("accountBackup:Send with")} tooltip={i18next.t("report:Send with - Tooltip")}>
        <div className="flex w-full flex-col gap-1.5">
          <MultiSelect
            value={report.webhooks ?? []}
            options={webhooks.map((webhook) => ({value: webhook.name, label: `${webhook.displayName || webhook.name} (${(webhook.recipients ?? []).join(", ")})`}))}
            onChange={(value) => updateField("webhooks", value)}
          />
          {webhooks.length === 0 ? (
            <span className="text-xs text-muted-foreground">
              {i18next.t("accountBackup:No webhook of the Email type yet")} <Link to="/webhooks" className="text-primary underline-offset-4 hover:underline">{i18next.t("general:Webhooks")}</Link>
            </span>
          ) : null}
        </div>
      </FormRow>
      <FormRow label={i18next.t("job:Schedule")} tooltip={i18next.t("report:Schedule - Tooltip")}>
        <div className="flex w-full flex-col gap-1.5">
          <div className="flex gap-2">
            <Input className="font-mono" value={report.cron} onChange={(e) => updateField("cron", e.target.value)} />
            <SelectField
              className="w-48 shrink-0"
              value=""
              placeholder={i18next.t("job:Common schedules")}
              options={[
                {id: "0 8 * * 1", name: i18next.t("report:Every Monday at 08:00")},
                {id: "0 8 1 * *", name: i18next.t("report:The 1st of every month at 08:00")},
                {id: "0 8 * * *", name: i18next.t("report:Every day at 08:00")},
              ]}
              onChange={(value) => updateField("cron", value)}
            />
          </div>
          {cronError ? (
            <span className="text-xs text-destructive">{cronError}</span>
          ) : runTimes.length > 0 ? (
            <span className="text-xs text-muted-foreground">
              {i18next.t("job:Next runs")}: {runTimes.map((time) => dayjs(time).format("MM-DD HH:mm")).join(", ")}
            </span>
          ) : null}
        </div>
      </FormRow>
      <FormRow label={i18next.t("loginRule:Time zone")} tooltip={i18next.t("report:Time zone - Tooltip")}>
        <Input value={report.timeZone} placeholder="Asia/Shanghai" onChange={(e) => updateField("timeZone", e.target.value.trim())} />
      </FormRow>
      <FormRow labelKey="permission:Is enabled">
        <Switch checked={report.isEnabled} onCheckedChange={(checked) => updateField("isEnabled", checked)} />
      </FormRow>
      {mode !== "add" ? (
        <FormRow label={i18next.t("job:Last run")}>
          <div className="flex min-h-9 flex-wrap items-center gap-2 text-sm">
            {report.lastRunTime ? (
              <>
                <BackupRunBadge status={report.lastRunStatus} />
                <span className="tabular-nums">{dayjs(report.lastRunTime).format("YYYY-MM-DD HH:mm")}</span>
                {report.lastRunMessage ? <span className="text-xs text-muted-foreground">{report.lastRunMessage}</span> : null}
              </>
            ) : <span className="text-muted-foreground">{i18next.t("job:Not run yet")}</span>}
          </div>
        </FormRow>
      ) : null}
    </EditPageShell>
  );
}
