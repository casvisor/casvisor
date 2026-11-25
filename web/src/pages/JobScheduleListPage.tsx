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
import {Link, useNavigate} from "react-router-dom";
import {Plus} from "lucide-react";
import * as JobScheduleBackend from "@/backend/JobScheduleBackend";
import type {JobSchedule} from "@/backend/types";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {boolColumn} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {Badge} from "@/components/ui/badge";
import {Button} from "@/components/ui/button";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";
import {getJobTypeLabel} from "@/pages/JobListPage";

export function ScheduleRunBadge({status}: {status: string}) {
  if (!status) {
    return null;
  }
  const variant = status === "Started" ? "success" : status === "Skipped" ? "warning" : "destructive";
  return <Badge variant={variant}>{i18next.t(`job:Run ${status}`)}</Badge>;
}

export async function runScheduleNow(schedule: JobSchedule, navigate: (to: string) => void) {
  const res = await JobScheduleBackend.runJobSchedule(schedule.owner, schedule.name);
  if (res.status === "ok") {
    Setting.showMessage("success", i18next.t("job:Started"));
    navigate(`/jobs/${Setting.getIdPath(res.data.owner, res.data.name)}`);
  } else {
    Setting.showMessage("error", res.msg);
  }
}

export default function JobScheduleListPage() {
  const {account} = useAccount();
  const owner = Setting.getRequestOrganization(account);
  const navigate = useNavigate();

  const fetchSchedules = React.useCallback(
    (query: TableQuery) =>
      JobScheduleBackend.getJobSchedules(owner, {
        page: query.page,
        pageSize: query.pageSize,
        field: query.searchedColumn,
        value: query.searchText,
        sortField: query.sortField,
        sortOrder: query.sortOrder,
      }),
    [owner],
  );

  const columns: ColumnDef<JobSchedule>[] = [
    {
      dataIndex: "displayName",
      title: i18next.t("general:Name"),
      width: 200,
      searchable: true,
      render: (value, schedule) => (
        <Link to={`/job-schedules/${Setting.getIdPath(schedule.owner, schedule.name)}`} className="font-medium underline-offset-4 hover:underline">
          {value || schedule.name}
        </Link>
      ),
    },
    {
      dataIndex: "type",
      title: i18next.t("general:Type"),
      width: 110,
      render: (value) => getJobTypeLabel(value),
    },
    {
      dataIndex: "cron",
      title: i18next.t("job:Schedule"),
      width: 170,
      render: (value, schedule) => (
        <div className="text-xs">
          <code className="font-mono">{value}</code>
          {schedule.timeZone ? <div className="text-muted-foreground">{schedule.timeZone}</div> : null}
        </div>
      ),
    },
    {
      dataIndex: "nextRunTime",
      title: i18next.t("job:Next run"),
      width: 150,
      render: (value) => (value ? <span className="tabular-nums">{dayjs(value).format("YYYY-MM-DD HH:mm")}</span> : null),
    },
    {
      dataIndex: "lastRunTime",
      title: i18next.t("job:Last run"),
      width: 260,
      render: (value: string, schedule) => (value ? (
        <div className="text-xs">
          <div className="flex items-center gap-1.5">
            <ScheduleRunBadge status={schedule.lastRunStatus} />
            {schedule.lastJob && schedule.lastRunStatus === "Started" ? (
              <Link to={`/jobs/${Setting.getIdPath(schedule.owner, schedule.lastJob)}`} className="tabular-nums text-primary underline-offset-4 hover:underline">{dayjs(value).format("YYYY-MM-DD HH:mm")}</Link>
            ) : <span className="tabular-nums text-muted-foreground">{dayjs(value).format("YYYY-MM-DD HH:mm")}</span>}
          </div>
          {schedule.lastRunMessage ? <div className="mt-0.5 line-clamp-2 break-all text-muted-foreground" title={schedule.lastRunMessage}>{schedule.lastRunMessage}</div> : null}
        </div>
      ) : <span className="text-xs text-muted-foreground">{i18next.t("job:Not run yet")}</span>),
    },
    {
      dataIndex: "user",
      title: i18next.t("job:Runs as"),
      width: 140,
      render: (value) => Setting.getShortName(value),
    },
    boolColumn<JobSchedule>({dataIndex: "isEnabled", title: i18next.t("permission:Is enabled"), width: 110}),
  ];

  return (
    <CrudListPage<JobSchedule>
      title={i18next.t("general:Job schedules")}
      description={i18next.t("general:Run jobs at set times, such as backups every night or checks every hour")}
      columns={columns}
      fetch={fetchSchedules}
      deps={[owner]}
      remove={JobScheduleBackend.deleteJobSchedule}
      editUrl={(schedule) => `/job-schedules/${Setting.getIdPath(schedule.owner, schedule.name)}`}
      actionColumnWidth={220}
      rowActions={(schedule) => [
        {key: "run", label: i18next.t("job:Run now"), onSelect: () => runScheduleNow(schedule, navigate)},
        {key: "runs", label: i18next.t("job:Runs"), onSelect: () => navigate(`/jobs?schedule=${encodeURIComponent(schedule.name)}`)},
      ]}
      toolbar={
        <Button onClick={() => navigate("/job-schedules/new")}>
          <Plus />
          {i18next.t("job:New schedule")}
        </Button>
      }
    />
  );
}
