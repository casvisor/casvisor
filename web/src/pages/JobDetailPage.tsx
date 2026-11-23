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
import {Link, useNavigate, useParams} from "react-router-dom";
import {ArrowLeft, Copy, RotateCw, Square} from "lucide-react";
import * as JobBackend from "@/backend/JobBackend";
import type {Job, JobResult, PlaybookTaskResult} from "@/backend/types";
import {ConfirmButton} from "@/components/common/ConfirmButton";
import {PageHeader} from "@/components/crud/PageHeader";
import {Badge} from "@/components/ui/badge";
import {Button} from "@/components/ui/button";
import {Card, CardContent} from "@/components/ui/card";
import {Table, TableBody, TableCell, TableHead, TableHeader, TableRow} from "@/components/ui/table";
import * as Setting from "@/lib/setting";
import {JobStatusBadge, getJobTypeLabel} from "@/pages/JobListPage";
import NotFoundPage from "@/pages/NotFoundPage";

function Field({label, children}: {label: React.ReactNode; children: React.ReactNode}) {
  return (
    <div className="min-w-0">
      <div className="text-muted-foreground">{label}</div>
      <div className="mt-1 break-all">{children}</div>
    </div>
  );
}

function ResultOutput({result}: {result: JobResult}) {
  const ref = React.useRef<HTMLPreElement>(null);
  React.useEffect(() => {
    if (ref.current) {
      ref.current.scrollTop = ref.current.scrollHeight;
    }
  }, [result.output]);

  return (
    <pre ref={ref} className="max-h-96 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted p-3 font-mono text-xs">
      {result.output || i18next.t("job:No output")}
    </pre>
  );
}

function TaskStatusBadge({task}: {task: PlaybookTaskResult}) {
  if (task.status === "changed") {
    return <Badge variant="warning">{i18next.t("job:changed")}</Badge>;
  } else if (task.status === "failed") {
    return <Badge variant={task.ignored ? "secondary" : "destructive"}>{task.ignored ? i18next.t("job:failed, ignored") : i18next.t("job:failed")}</Badge>;
  } else if (task.status === "skipped") {
    return <Badge variant="secondary">{i18next.t("job:skipped")}</Badge>;
  } else if (task.status === "ok") {
    return <Badge variant="success">{i18next.t("job:ok")}</Badge>;
  }
  return <Badge variant="info">{i18next.t("job:Running")}</Badge>;
}

/** The counts of a playbook on an asset, as the PLAY RECAP of Ansible gives them. */
function getRecap(tasks: PlaybookTaskResult[]) {
  const recap = {ok: 0, changed: 0, failed: 0, skipped: 0, ignored: 0};
  for (const task of tasks) {
    if (task.status === "failed" && task.ignored) {
      recap.ignored++;
    } else if (task.status === "ok" || task.status === "changed") {
      recap.ok++;
      if (task.status === "changed") {
        recap.changed++;
      }
    } else if (task.status === "failed") {
      recap.failed++;
    } else if (task.status === "skipped") {
      recap.skipped++;
    }
  }
  return recap;
}

function Recap({tasks}: {tasks: PlaybookTaskResult[]}) {
  const recap = getRecap(tasks);
  return (
    <span className="whitespace-nowrap text-xs tabular-nums">
      <span className="text-success">ok={recap.ok}</span>{" "}
      <span className={recap.changed ? "text-warning" : "text-muted-foreground"}>changed={recap.changed}</span>{" "}
      <span className={recap.failed ? "text-destructive" : "text-muted-foreground"}>failed={recap.failed}</span>{" "}
      <span className="text-muted-foreground">skipped={recap.skipped}</span>
      {recap.ignored ? <span className="text-muted-foreground"> ignored={recap.ignored}</span> : null}
    </span>
  );
}

/** The tasks of a playbook on an asset, each with its result and output. */
function PlaybookTasks({tasks}: {tasks: PlaybookTaskResult[]}) {
  const [open, setOpen] = React.useState<Set<number>>(new Set());
  if (tasks.length === 0) {
    return null;
  }
  const toggle = (index: number) => {
    const next = new Set(open);
    if (next.has(index)) {
      next.delete(index);
    } else {
      next.add(index);
    }
    setOpen(next);
  };

  return (
    <div className="mb-3 overflow-x-auto rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="min-w-40">{i18next.t("job:Play")}</TableHead>
            <TableHead className="min-w-48">{i18next.t("job:Task")}</TableHead>
            <TableHead className="w-28">{i18next.t("job:Module")}</TableHead>
            <TableHead className="w-32">{i18next.t("general:Status")}</TableHead>
            <TableHead className="min-w-56">{i18next.t("job:Message")}</TableHead>
            <TableHead className="w-24" />
          </TableRow>
        </TableHeader>
        <TableBody>
          {tasks.map((task, index) => (
            <React.Fragment key={index}>
              <TableRow>
                <TableCell className="text-xs text-muted-foreground">{task.play}</TableCell>
                <TableCell className="text-sm">
                  {task.handler ? <span className="text-muted-foreground">{i18next.t("job:Handler")}: </span> : null}
                  {task.name}
                  {task.items && task.items.length > 0 ? (
                    <div className="mt-1 flex flex-wrap gap-1">
                      {task.items.map((item, i) => (
                        <span key={i} className="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px]" title={item.msg}>
                          {item.label}: {item.status}
                        </span>
                      ))}
                    </div>
                  ) : null}
                </TableCell>
                <TableCell className="font-mono text-xs">{task.module}</TableCell>
                <TableCell><TaskStatusBadge task={task} /></TableCell>
                <TableCell className="break-all text-xs">
                  {task.msg}
                  {task.rc !== undefined && task.rc !== null && task.rc !== 0 ? <span className="text-muted-foreground"> (rc={task.rc})</span> : null}
                </TableCell>
                <TableCell className="text-right">
                  {task.output ? (
                    <Button size="sm" variant="ghost" onClick={() => toggle(index)}>
                      {open.has(index) ? i18next.t("job:Hide output") : i18next.t("job:Output")}
                    </Button>
                  ) : null}
                </TableCell>
              </TableRow>
              {open.has(index) ? (
                <TableRow>
                  <TableCell colSpan={6}>
                    <pre className="max-h-72 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted p-3 font-mono text-xs">{task.output}</pre>
                  </TableCell>
                </TableRow>
              ) : null}
            </React.Fragment>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

/** One job: what it runs and how it went on each asset, live while it runs. */
export default function JobDetailPage() {
  const {owner = "", name = ""} = useParams();
  const navigate = useNavigate();
  const [job, setJob] = React.useState<Job | null>(null);
  const [notFound, setNotFound] = React.useState(false);
  const [open, setOpen] = React.useState<Set<string>>(new Set());

  const refresh = React.useCallback(() => {
    return JobBackend.getJob(owner, name).then((res) => {
      if (res.status !== "ok") {
        Setting.showMessage("error", res.msg);
      } else if (!res.data) {
        setNotFound(true);
      } else {
        setJob(res.data);
      }
    });
  }, [owner, name]);

  React.useEffect(() => {
    refresh();
  }, [refresh]);

  const running = job?.status === "Running";
  React.useEffect(() => {
    if (!running) {
      return;
    }
    const timer = window.setTimeout(refresh, 2000);
    return () => window.clearTimeout(timer);
  }, [running, job, refresh]);

  if (notFound) {
    return <NotFoundPage />;
  }
  if (!job) {
    return <div className="text-muted-foreground">{i18next.t("general:Loading")}</div>;
  }

  const results = job.results ?? [];
  const failedCount = results.filter((result) => result.status !== "Succeeded").length;

  const act = async(action: () => Promise<any>, success: string) => {
    try {
      const res = await action();
      if (res.status === "ok") {
        Setting.showMessage("success", success);
        await refresh();
      } else {
        Setting.showMessage("error", res.msg);
      }
    } catch (error) {
      Setting.showMessage("error", `${i18next.t("general:Failed to connect to server")}: ${error}`);
    }
  };

  const toggle = (asset: string) => {
    const next = new Set(open);
    if (next.has(asset)) {
      next.delete(asset);
    } else {
      next.add(asset);
    }
    setOpen(next);
  };

  return (
    <div className="space-y-4">
      <PageHeader
        title={(
          <span className="flex items-center gap-3">
            <Button variant="ghost" size="iconSm" onClick={() => navigate("/jobs")} aria-label={i18next.t("general:Go back")}>
              <ArrowLeft />
            </Button>
            {job.displayName || job.name.slice(0, 8)}
            <JobStatusBadge status={job.status} />
          </span>
        )}
        description={job.message || undefined}
        actions={(
          <>
            <Button variant="outline" onClick={() => navigate("/jobs/new", {state: {job}})}>
              <Copy />
              {i18next.t("job:Run as new job")}
            </Button>
            <ConfirmButton
              variant="outline"
              destructive={false}
              disabled={running || failedCount === 0}
              title={i18next.t("job:Run again on the {{count}} assets that did not succeed?", {count: failedCount})}
              onConfirm={() => act(() => JobBackend.rerunJob(owner, name), i18next.t("job:Started"))}
            >
              <RotateCw />
              {i18next.t("job:Rerun failed")}
            </ConfirmButton>
            <ConfirmButton
              variant="outline"
              disabled={!running}
              title={i18next.t("job:Cancel the job?")}
              description={i18next.t("job:What runs now is stopped, and the assets not reached yet are skipped.")}
              onConfirm={() => act(() => JobBackend.cancelJob(owner, name), i18next.t("job:Canceled"))}
            >
              <Square />
              {i18next.t("job:Cancel job")}
            </ConfirmButton>
          </>
        )}
      />

      <Card>
        <CardContent className="grid grid-cols-1 gap-4 pt-6 text-sm sm:grid-cols-4">
          <Field label={i18next.t("general:Type")}>
            {getJobTypeLabel(job.type)}{job.type === "Script" ? ` (${job.scriptType})` : ""}
            {job.type === "Playbook" && job.checkMode ? ` (${i18next.t("job:Check mode")})` : ""}
          </Field>
          <Field label={i18next.t("general:User")}>{job.user}</Field>
          <Field label={i18next.t("general:Time")}>
            <span className="tabular-nums">{Setting.getFormattedDate(job.startTime)}</span>
          </Field>
          <Field label={i18next.t("job:Duration")}>
            <span className="tabular-nums">{Setting.getDurationSince(job.startTime, running ? null : job.endTime)}</span>
          </Field>
          <Field label={i18next.t("general:Assets")}>
            <span className="tabular-nums">
              {job.total}
              {job.succeeded > 0 ? <span className="text-success"> / {i18next.t("job:{{count}} succeeded", {count: job.succeeded})}</span> : null}
              {job.failed > 0 ? <span className="text-destructive"> / {i18next.t("job:{{count}} failed", {count: job.failed})}</span> : null}
            </span>
          </Field>
          <Field label={i18next.t("job:Timeout (seconds)")}>{job.timeout}</Field>
          <Field label={i18next.t("job:Concurrency")}>{job.concurrency}</Field>
          <Field label={i18next.t("permission:Asset tags")}>{(job.assetTags ?? []).join(", ") || "-"}</Field>
          {(job.assetNodes ?? []).length > 0 ? (
            <Field label={i18next.t("assetNode:Nodes")}>{(job.assetNodes ?? []).join(", ")}</Field>
          ) : null}
          <div className="sm:col-span-4">
            {job.type === "Playbook" ? (
              <div className="grid grid-cols-1 gap-4">
                <Field label={i18next.t("job:Playbook")}>
                  <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted p-3 font-mono text-xs">{job.content}</pre>
                </Field>
                {job.variables ? (
                  <Field label={i18next.t("job:Variables")}>
                    <pre className="max-h-40 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted p-3 font-mono text-xs">{job.variables}</pre>
                  </Field>
                ) : null}
                {(job.files ?? []).length > 0 ? (
                  <Field label={i18next.t("job:Files")}>
                    <span className="font-mono text-xs">{(job.files ?? []).map((file) => file.name).join(", ")}</span>
                  </Field>
                ) : null}
              </div>
            ) : job.type === "File" ? (
              <Field label={i18next.t("job:File")}>
                <span className="font-mono text-xs">{job.fileName} {"→"} {job.filePath}</span>
              </Field>
            ) : (
              <Field label={job.type === "Script" ? i18next.t("job:Script") : i18next.t("job:Command")}>
                <pre className="max-h-60 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted p-3 font-mono text-xs">{job.content}</pre>
              </Field>
            )}
          </div>
        </CardContent>
      </Card>

      <Card className="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="min-w-40">{i18next.t("general:Asset")}</TableHead>
              <TableHead className="w-32">{i18next.t("general:Status")}</TableHead>
              <TableHead className="w-24">{job.type === "Playbook" ? i18next.t("job:Tasks") : i18next.t("job:Exit code")}</TableHead>
              <TableHead className="w-32">{i18next.t("job:Duration")}</TableHead>
              <TableHead className="min-w-56">{i18next.t("job:Error")}</TableHead>
              <TableHead className="w-48" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {results.map((result) => (
              <React.Fragment key={result.asset}>
                <TableRow>
                  <TableCell>
                    <Link to={`/assets/${result.owner}/${result.asset}`} className="font-medium underline-offset-4 hover:underline">
                      {result.displayName || result.asset}
                    </Link>
                    <div className="text-xs text-muted-foreground">{result.host}</div>
                  </TableCell>
                  <TableCell><JobStatusBadge status={result.status} /></TableCell>
                  <TableCell className="tabular-nums">
                    {job.type === "Playbook"
                      ? (result.tasks ?? []).length > 0 ? <Recap tasks={result.tasks ?? []} /> : "-"
                      : job.type !== "File" && (result.status === "Succeeded" || result.status === "Failed") && result.exitCode >= 0 ? result.exitCode : "-"}
                  </TableCell>
                  <TableCell className="whitespace-nowrap tabular-nums text-muted-foreground">
                    {result.startTime ? Setting.getDurationSince(result.startTime, result.status === "Running" ? null : result.endTime) : "-"}
                  </TableCell>
                  <TableCell className="break-all text-xs">
                    <span className="text-destructive">{result.error}</span>
                    {result.rule ? (
                      <div>
                        {i18next.t("command:Rule")}:{" "}
                        <Link to={`/command-rules/${result.rule}`} className="underline-offset-4 hover:underline">{Setting.getShortName(result.rule)}</Link>
                      </div>
                    ) : null}
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-1">
                      {result.session ? (
                        <Button size="sm" variant="ghost" asChild>
                          <Link to={`/commands?session=${encodeURIComponent(result.session)}`}>{i18next.t("general:Commands")}</Link>
                        </Button>
                      ) : null}
                      <Button size="sm" variant="ghost" disabled={result.status === "Pending"} onClick={() => toggle(result.asset)}>
                        {open.has(result.asset) ? i18next.t("job:Hide output") : i18next.t("job:Output")}
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
                {open.has(result.asset) ? (
                  <TableRow>
                    <TableCell colSpan={6}>
                      {job.type === "Playbook" ? <PlaybookTasks tasks={result.tasks ?? []} /> : null}
                      <ResultOutput result={result} />
                    </TableCell>
                  </TableRow>
                ) : null}
              </React.Fragment>
            ))}
          </TableBody>
        </Table>
      </Card>
    </div>
  );
}
