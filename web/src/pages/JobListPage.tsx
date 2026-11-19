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
import {Link, useNavigate} from "react-router-dom";
import {Loader2, Plus} from "lucide-react";
import * as JobBackend from "@/backend/JobBackend";
import type {Job} from "@/backend/types";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {dateColumn, textColumn, valueFilters} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {Badge} from "@/components/ui/badge";
import {Button} from "@/components/ui/button";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";

export function getJobTypeLabel(type: string) {
  if (type === "Command") {
    return i18next.t("job:Command");
  } else if (type === "Script") {
    return i18next.t("job:Script");
  } else if (type === "File") {
    return i18next.t("job:File transfer");
  }
  return type;
}

/** The status of a job, or of a job on one asset. */
export function JobStatusBadge({status}: {status: string}) {
  switch (status) {
  case "Running":
    return (
      <Badge variant="info">
        <Loader2 className="h-3 w-3 animate-spin" />
        {i18next.t("job:Running")}
      </Badge>
    );
  case "Pending":
    return <Badge variant="outline">{i18next.t("job:Pending")}</Badge>;
  case "Succeeded":
    return <Badge variant="success">{i18next.t("job:Succeeded")}</Badge>;
  case "Timeout":
    return <Badge variant="warning">{i18next.t("job:Timed out")}</Badge>;
  case "Blocked":
    return <Badge variant="destructive">{i18next.t("job:Blocked")}</Badge>;
  case "Canceled":
    return <Badge variant="secondary">{i18next.t("job:Canceled")}</Badge>;
  case "Interrupted":
    return <Badge variant="warning">{i18next.t("job:Interrupted")}</Badge>;
  case "Failed":
    return <Badge variant="destructive">{i18next.t("job:Failed")}</Badge>;
  default:
    return <Badge variant="outline">{status}</Badge>;
  }
}

/** Commands, scripts and file transfers run on many SSH assets at once. */
export default function JobListPage() {
  const {account} = useAccount();
  const owner = Setting.getRequestOrganization(account);
  const navigate = useNavigate();

  const fetchJobs = React.useCallback(
    (query: TableQuery) =>
      JobBackend.getJobs(owner, {
        page: query.page,
        pageSize: query.pageSize,
        field: query.searchedColumn,
        value: query.searchText,
        sortField: query.sortField,
        sortOrder: query.sortOrder,
      }),
    [owner],
  );

  const columns: ColumnDef<Job>[] = [
    {
      dataIndex: "displayName",
      title: i18next.t("general:Name"),
      width: 200,
      searchable: true,
      render: (value, job) => (
        <Link to={`/jobs/${job.owner}/${job.name}`} className="font-medium underline-offset-4 hover:underline">
          {value || job.name.slice(0, 8)}
        </Link>
      ),
    },
    {
      dataIndex: "type",
      title: i18next.t("general:Type"),
      width: 120,
      filters: JobBackend.JobTypes.map((type) => ({label: getJobTypeLabel(type), value: type})),
      render: (value, job) => (
        <span>{getJobTypeLabel(value)}{value === "Script" ? <span className="text-muted-foreground"> ({job.scriptType})</span> : null}</span>
      ),
    },
    {
      dataIndex: "status",
      title: i18next.t("general:Status"),
      width: 120,
      filters: valueFilters(["Running", "Succeeded", "Failed", "Canceled", "Interrupted"]).map((filter) => ({...filter, label: <JobStatusBadge status={filter.value} />})),
      render: (value) => <JobStatusBadge status={value} />,
    },
    {
      dataIndex: "total",
      title: i18next.t("general:Assets"),
      width: 150,
      render: (value, job) => (
        <span className="tabular-nums">
          {value}
          {job.succeeded > 0 ? <span className="text-success"> / {i18next.t("job:{{count}} succeeded", {count: job.succeeded})}</span> : null}
          {job.failed > 0 ? <span className="text-destructive"> / {i18next.t("job:{{count}} failed", {count: job.failed})}</span> : null}
        </span>
      ),
    },
    textColumn<Job>({dataIndex: "user", title: i18next.t("general:User"), width: 150, searchable: true}),
    dateColumn<Job>("createdTime", i18next.t("general:Time")),
    {
      key: "duration",
      dataIndex: "endTime",
      title: i18next.t("job:Duration"),
      width: 130,
      render: (_value, job) => (
        <span className="whitespace-nowrap tabular-nums">{Setting.getDurationSince(job.startTime, job.status === "Running" ? null : job.endTime)}</span>
      ),
    },
  ];

  return (
    <CrudListPage<Job>
      title={i18next.t("general:Jobs")}
      description={i18next.t("general:Run commands, scripts and file transfers on many SSH assets at once")}
      columns={columns}
      fetch={fetchJobs}
      deps={[owner]}
      remove={JobBackend.deleteJob}
      deleteDisabled={(job) => job.status === "Running"}
      actionColumnWidth={120}
      toolbar={
        <Button onClick={() => navigate("/jobs/new")}>
          <Plus />
          {i18next.t("job:New job")}
        </Button>
      }
    />
  );
}
