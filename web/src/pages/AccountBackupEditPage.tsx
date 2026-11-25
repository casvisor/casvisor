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
import * as AccountBackupBackend from "@/backend/AccountBackupBackend";
import * as JobScheduleBackend from "@/backend/JobScheduleBackend";
import * as WebhookBackend from "@/backend/WebhookBackend";
import type {AccountBackup, Webhook} from "@/backend/types";
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
import NotFoundPage from "@/pages/NotFoundPage";
import {BackupRunBadge, downloadBackup, sendBackupNow} from "@/pages/AccountBackupListPage";
import * as Setting from "@/lib/setting";

export default function AccountBackupEditPage() {
  const {owner = "", name = ""} = useParams();
  const navigate = useNavigate();
  const [saving, setSaving] = React.useState(false);
  const [busy, setBusy] = React.useState(false);
  const [webhooks, setWebhooks] = React.useState<Webhook[]>([]);
  const [runTimes, setRunTimes] = React.useState<string[]>([]);
  const [cronError, setCronError] = React.useState("");

  const {record: backup, updateField, loading, notFound, denied, mode, setMode, reload} = useEditRecord<AccountBackup>({
    fetch: () => AccountBackupBackend.getAccountBackup(owner, name),
    deps: [owner, name],
  });

  React.useEffect(() => {
    WebhookBackend.getWebhooks(owner).then((res) => {
      if (res.status === "ok") {
        setWebhooks((res.data ?? []).filter((webhook: Webhook) => webhook.type === "Email"));
      }
    });
  }, [owner]);

  const cron = backup?.cron ?? "";
  const timeZone = backup?.timeZone ?? "";
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
  if (notFound || !backup) {
    return <NotFoundPage />;
  }

  const save = (exitAfterSave: boolean) => {
    setSaving(true);
    return submitEdit({
      mode,
      record: backup,
      add: AccountBackupBackend.addAccountBackup,
      update: (record) => AccountBackupBackend.updateAccountBackup(owner, name, record),
      onSaved: (record) => {
        if (exitAfterSave) {
          navigate("/account-backups");
        } else if (record.name !== name) {
          navigate(`/account-backups/${Setting.getIdPath(record.owner, record.name)}`, {replace: true});
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
      title={i18next.t(getModeTitleKey("accountBackup:Edit Account Backup", mode))}
      description={i18next.t("general:Zip files of the passwords and private keys, encrypted, to download or to email on a schedule")}
      mode={mode}
      backTo="/account-backups"
      onSave={save}
      saving={saving}
      remove={() => AccountBackupBackend.deleteAccountBackup(backup)}
      extraActions={mode === "edit" ? (
        <>
          <Button variant="outline" disabled={busy || saving} onClick={() => act(() => downloadBackup(backup))}>
            <Download />
            {i18next.t("accountBackup:Download")}
          </Button>
          {(backup.webhooks ?? []).length > 0 ? (
            <Button variant="outline" disabled={busy || saving} onClick={() => act(() => sendBackupNow(backup, reload))}>
              <Send />
              {i18next.t("accountBackup:Send now")}
            </Button>
          ) : null}
        </>
      ) : null}
      grid
    >
      <FormRow labelKey="general:Organization">
        <Input value={backup.owner} disabled />
      </FormRow>
      <FormRow labelKey="general:Name">
        <Input value={backup.name} onChange={(e) => updateField("name", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Display name">
        <Input value={backup.displayName} onChange={(e) => updateField("displayName", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Description">
        <Input value={backup.description} onChange={(e) => updateField("description", e.target.value)} />
      </FormRow>
      <FormRow label={i18next.t("general:Password")} tooltip={i18next.t("accountBackup:Password - Tooltip")}>
        <Input type="password" autoComplete="new-password" value={backup.password} onChange={(e) => updateField("password", e.target.value)} />
      </FormRow>
      <FormRow label={i18next.t("accountBackup:Include assets")} tooltip={i18next.t("accountBackup:Include assets - Tooltip")}>
        <Switch checked={backup.includeAssets} onCheckedChange={(checked) => updateField("includeAssets", checked)} />
      </FormRow>
      <FormRow label={i18next.t("accountBackup:Send with")} tooltip={i18next.t("accountBackup:Send with - Tooltip")}>
        <div className="flex w-full flex-col gap-1.5">
          <MultiSelect
            value={backup.webhooks ?? []}
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
      <FormRow label={i18next.t("job:Schedule")} tooltip={i18next.t("accountBackup:Schedule - Tooltip")}>
        <div className="flex w-full flex-col gap-1.5">
          <div className="flex gap-2">
            <Input className="font-mono" value={backup.cron} onChange={(e) => updateField("cron", e.target.value)} />
            <SelectField
              className="w-48 shrink-0"
              value=""
              placeholder={i18next.t("job:Common schedules")}
              options={JobScheduleBackend.CronPresets.map((preset) => ({id: preset.cron, name: i18next.t(preset.label)}))}
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
      <FormRow label={i18next.t("loginRule:Time zone")} tooltip={i18next.t("job:Time zone - Tooltip")}>
        <Input value={backup.timeZone} placeholder="Asia/Shanghai" onChange={(e) => updateField("timeZone", e.target.value.trim())} />
      </FormRow>
      <FormRow labelKey="permission:Is enabled">
        <Switch checked={backup.isEnabled} onCheckedChange={(checked) => updateField("isEnabled", checked)} />
      </FormRow>
      {mode !== "add" ? (
        <FormRow label={i18next.t("job:Last run")}>
          <div className="flex min-h-9 flex-wrap items-center gap-2 text-sm">
            {backup.lastRunTime ? (
              <>
                <BackupRunBadge status={backup.lastRunStatus} />
                <span className="tabular-nums">{dayjs(backup.lastRunTime).format("YYYY-MM-DD HH:mm")}</span>
                {backup.lastRunMessage ? <span className="text-xs text-muted-foreground">{backup.lastRunMessage}</span> : null}
              </>
            ) : <span className="text-muted-foreground">{i18next.t("job:Not run yet")}</span>}
          </div>
        </FormRow>
      ) : null}
    </EditPageShell>
  );
}
