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
import {Link, useLocation, useNavigate, useParams} from "react-router-dom";
import {ArrowLeft, Loader2, Play, Save, Trash2} from "lucide-react";
import * as JobScheduleBackend from "@/backend/JobScheduleBackend";
import type {Job, JobSchedule} from "@/backend/types";
import {Loading} from "@/components/common/Loading";
import {SelectField} from "@/components/common/SelectField";
import {FormGrid, FormRow} from "@/components/crud/FormRow";
import {PageHeader} from "@/components/crud/PageHeader";
import {JobFields, useJobAssets} from "@/components/job/JobFields";
import {Button} from "@/components/ui/button";
import {Card, CardContent} from "@/components/ui/card";
import {Input} from "@/components/ui/input";
import {Switch} from "@/components/ui/switch";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";
import {runScheduleNow, ScheduleRunBadge} from "@/pages/JobScheduleListPage";

function getBrowserTimeZone() {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone ?? "";
  } catch {
    return "";
  }
}

/** A new job schedule, or one to edit; a job passed in the router state, as the new job page's
 *  "Run on a schedule" does, fills the form. */
export default function JobScheduleEditPage() {
  const {owner: ownerParam, name} = useParams();
  const {account} = useAccount();
  const owner = ownerParam ?? Setting.getRequestOrganization(account);
  const isNew = !name;
  const navigate = useNavigate();
  const location = useLocation();
  const from = (location.state as {job?: Partial<Job>} | null)?.job;
  const assets = useJobAssets(owner);

  const [schedule, setSchedule] = React.useState<Partial<JobSchedule> | null>(() => (isNew ? {
    owner,
    name: `schedule_${Setting.getRandomName()}`,
    displayName: from?.displayName ?? "",
    description: "",
    type: from?.type ?? "Command",
    assets: from?.assets ?? [],
    assetTags: from?.assetTags ?? [],
    assetNodes: from?.assetNodes ?? [],
    content: from?.content ?? "",
    scriptType: from?.scriptType || "Bash",
    fileKey: from?.fileKey ?? "",
    fileName: from?.fileName ?? "",
    fileSize: from?.fileSize ?? 0,
    filePath: from?.filePath ?? "",
    variables: from?.variables ?? "",
    files: from?.files ?? [],
    checkMode: from?.checkMode ?? false,
    timeout: from?.timeout ?? 60,
    concurrency: from?.concurrency ?? 10,
    cron: "0 3 * * *",
    timeZone: getBrowserTimeZone(),
    isEnabled: true,
  } : null));
  const [notFound, setNotFound] = React.useState(false);
  const [saving, setSaving] = React.useState(false);
  const [uploading, setUploading] = React.useState(false);
  const [runTimes, setRunTimes] = React.useState<string[]>([]);
  const [cronError, setCronError] = React.useState("");

  React.useEffect(() => {
    if (isNew) {
      return;
    }
    JobScheduleBackend.getJobSchedule(owner, name!).then((res) => {
      if (res.status === "ok" && res.data) {
        setSchedule(res.data);
      } else {
        setNotFound(true);
      }
    });
  }, [isNew, owner, name]);

  const cron = schedule?.cron ?? "";
  const timeZone = schedule?.timeZone ?? "";
  React.useEffect(() => {
    if (!cron.trim()) {
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

  if (notFound) {
    return <div className="p-6 text-sm text-muted-foreground">{i18next.t("job:The job schedule does not exist")}</div>;
  }
  if (!schedule) {
    return <Loading />;
  }

  const update = (patch: Partial<JobSchedule>) => setSchedule((current) => ({...current, ...patch}));

  const save = async() => {
    setSaving(true);
    try {
      const record = {...schedule, owner, timeout: Number(schedule.timeout) || 0, concurrency: Number(schedule.concurrency) || 0};
      const res = isNew ? await JobScheduleBackend.addJobSchedule(record) : await JobScheduleBackend.updateJobSchedule(owner, name!, record);
      if (res.status === "ok") {
        Setting.showMessage("success", i18next.t("general:Successfully saved"));
        if (isNew || record.name !== name) {
          navigate(`/job-schedules/${owner}/${encodeURIComponent(record.name ?? "")}`, {replace: true});
        } else {
          const fresh = await JobScheduleBackend.getJobSchedule(owner, name!);
          if (fresh.status === "ok") {
            setSchedule(fresh.data);
          }
        }
      } else {
        Setting.showMessage("error", res.msg);
      }
    } finally {
      setSaving(false);
    }
  };

  const remove = async() => {
    const res = await JobScheduleBackend.deleteJobSchedule(schedule);
    if (res.status === "ok") {
      navigate("/job-schedules");
    } else {
      Setting.showMessage("error", res.msg);
    }
  };

  return (
    <div className="space-y-4">
      <PageHeader
        title={isNew ? i18next.t("job:New schedule") : i18next.t("job:Edit schedule")}
        description={i18next.t("general:Run jobs at set times, such as backups every night or checks every hour")}
        actions={(
          <>
            <Button variant="ghost" onClick={() => navigate("/job-schedules")}>
              <ArrowLeft />
              {i18next.t("general:Back")}
            </Button>
            {!isNew ? (
              <>
                <Button variant="ghost" className="text-destructive" onClick={remove}>
                  <Trash2 />
                  {i18next.t("general:Delete")}
                </Button>
                <Button variant="outline" onClick={() => runScheduleNow(schedule as JobSchedule, navigate)}>
                  <Play />
                  {i18next.t("job:Run now")}
                </Button>
              </>
            ) : null}
            <Button onClick={save} disabled={saving || uploading}>
              {saving ? <Loader2 className="animate-spin" /> : <Save />}
              {i18next.t("general:Save")}
            </Button>
          </>
        )}
      />

      <Card>
        <CardContent className="pt-4">
          <FormGrid>
            <FormRow labelKey="general:Name">
              <Input value={schedule.name} onChange={(e) => update({name: e.target.value})} />
            </FormRow>
            <FormRow label={i18next.t("job:Schedule")} tooltip={i18next.t("job:Schedule - Tooltip")}>
              <div className="flex w-full flex-col gap-1.5">
                <div className="flex gap-2">
                  <Input className="font-mono" value={schedule.cron} onChange={(e) => update({cron: e.target.value})} />
                  <SelectField
                    className="w-48 shrink-0"
                    value=""
                    placeholder={i18next.t("job:Common schedules")}
                    options={JobScheduleBackend.CronPresets.map((preset) => ({id: preset.cron, name: i18next.t(preset.label)}))}
                    onChange={(value) => update({cron: value})}
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
              <Input value={schedule.timeZone} placeholder="Asia/Shanghai" onChange={(e) => update({timeZone: e.target.value.trim()})} />
            </FormRow>
            <FormRow labelKey="permission:Is enabled">
              <Switch checked={Boolean(schedule.isEnabled)} onCheckedChange={(checked) => update({isEnabled: checked})} />
            </FormRow>
            {!isNew ? (
              <FormRow label={i18next.t("job:Last run")}>
                <div className="flex min-h-9 flex-wrap items-center gap-2 text-sm">
                  {schedule.lastRunTime ? (
                    <>
                      <ScheduleRunBadge status={schedule.lastRunStatus ?? ""} />
                      <span className="tabular-nums">{dayjs(schedule.lastRunTime).format("YYYY-MM-DD HH:mm")}</span>
                      {schedule.lastRunMessage ? <span className="text-xs text-muted-foreground">{schedule.lastRunMessage}</span> : null}
                    </>
                  ) : <span className="text-muted-foreground">{i18next.t("job:Not run yet")}</span>}
                  <Link to={`/jobs?schedule=${encodeURIComponent(schedule.name ?? "")}`} className="text-primary underline-offset-4 hover:underline">{i18next.t("job:All runs")}</Link>
                </div>
              </FormRow>
            ) : null}
            {!isNew && schedule.user ? (
              <FormRow label={i18next.t("job:Runs as")} tooltip={i18next.t("job:Runs as - Tooltip")}>
                <div className="flex h-9 items-center text-sm">{schedule.user}</div>
              </FormRow>
            ) : null}
          </FormGrid>
          <div className="my-4 border-t" />
          <JobFields owner={owner} job={schedule as Partial<Job>} update={(patch) => update(patch as Partial<JobSchedule>)} assets={assets} onUploading={setUploading} />
        </CardContent>
      </Card>
    </div>
  );
}
