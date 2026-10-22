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
import {useParams} from "react-router-dom";
import {Download, RotateCw, ScanSearch} from "lucide-react";
import * as PatchBackend from "@/backend/PatchBackend";
import type {PatchJob, PatchStatus} from "@/backend/types";
import {ConfirmButton} from "@/components/common/ConfirmButton";
import {PageHeader} from "@/components/crud/PageHeader";
import {Badge} from "@/components/ui/badge";
import {Button} from "@/components/ui/button";
import {Card, CardContent} from "@/components/ui/card";
import {Checkbox} from "@/components/ui/checkbox";
import {Table, TableBody, TableCell, TableHead, TableHeader, TableRow} from "@/components/ui/table";
import {Tabs, TabsContent, TabsList, TabsTrigger} from "@/components/ui/tabs";
import * as Setting from "@/lib/setting";
import {PatchStateCell, UpdateCountCell, getJobTypeLabel, startPatchJob} from "@/pages/PatchListPage";

function JobStatusBadge({job}: {job: PatchJob}) {
  if (job.status === "Running") {
    return <Badge variant="info">{getJobTypeLabel(job.type)}</Badge>;
  } else if (job.status === "Succeeded") {
    return <Badge variant="success">{i18next.t("patch:Succeeded")}</Badge>;
  }
  return <Badge variant="destructive">{i18next.t("patch:Failed")}</Badge>;
}

function getJobTitle(job: PatchJob) {
  if (job.type === "Scan") {
    return i18next.t("patch:Scan");
  } else if (job.type === "Reboot") {
    return i18next.t("patch:Restart");
  } else if ((job.updates ?? []).length > 0) {
    return i18next.t("patch:Install {{count}} updates", {count: job.updates!.length});
  }
  return i18next.t("patch:Install all");
}

function JobOutput({owner, name, running}: {owner: string; name: string; running: boolean}) {
  const [output, setOutput] = React.useState("");
  const ref = React.useRef<HTMLPreElement>(null);

  React.useEffect(() => {
    let timer = 0;
    let cancelled = false;
    const load = () => {
      PatchBackend.getPatchJob(owner, name).then((res) => {
        if (cancelled || res.status !== "ok" || !res.data) {
          return;
        }
        setOutput(res.data.output ?? "");
        if (res.data.status === "Running") {
          timer = window.setTimeout(load, 2000);
        }
      });
    };
    load();
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [owner, name, running]);

  React.useEffect(() => {
    if (ref.current) {
      ref.current.scrollTop = ref.current.scrollHeight;
    }
  }, [output]);

  return (
    <pre ref={ref} className="max-h-96 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted p-3 font-mono text-xs">
      {output || i18next.t("general:Loading")}
    </pre>
  );
}

/** The pending updates of one SSH asset and the jobs run on it. */
export default function PatchDetailPage() {
  const {owner = "", name = ""} = useParams();
  const assetId = `${owner}/${name}`;
  const [status, setStatus] = React.useState<PatchStatus | null>(null);
  const [jobs, setJobs] = React.useState<PatchJob[]>([]);
  const [selected, setSelected] = React.useState<Set<string>>(new Set());
  const [openJob, setOpenJob] = React.useState("");
  const [tab, setTab] = React.useState("updates");

  const refresh = React.useCallback(() => {
    PatchBackend.getPatchStatus(owner, name).then((res) => {
      if (res.status === "ok") {
        setStatus(res.data);
      } else {
        Setting.showMessage("error", res.msg);
      }
    });
    PatchBackend.getPatchJobs(owner, assetId).then((res) => {
      if (res.status === "ok") {
        setJobs(((res.data ?? []) as PatchJob[]).filter((job) => job.asset === assetId));
      }
    });
  }, [owner, name, assetId]);

  React.useEffect(() => {
    refresh();
  }, [refresh]);

  const running = Boolean(status?.job);
  React.useEffect(() => {
    if (!running) {
      return;
    }
    const timer = window.setTimeout(refresh, 3000);
    return () => window.clearTimeout(timer);
  }, [running, status, refresh]);

  // a new scan replaces the updates, so drop picks that are gone
  const updates = status?.updates ?? [];
  React.useEffect(() => {
    setSelected((current) => new Set([...current].filter((id) => updates.some((update) => update.id === id))));
  }, [status?.scannedTime]); // eslint-disable-line react-hooks/exhaustive-deps

  const start = async(type: PatchBackend.PatchJobType, ids: string[] = []) => {
    const started = await startPatchJob(owner, [name], type, ids);
    if (started && started.length > 0) {
      refresh();
      setOpenJob(started[0].name);
      setTab("history");
    }
  };

  const toggle = (id: string, checked: boolean) => {
    const next = new Set(selected);
    if (checked) {
      next.add(id);
    } else {
      next.delete(id);
    }
    setSelected(next);
  };

  if (!status) {
    return <div className="text-muted-foreground">{i18next.t("general:Loading")}</div>;
  }

  const isWindows = status.platform === "Windows";
  const shownJob = openJob || (jobs.length > 0 && jobs[0].status === "Running" ? jobs[0].name : "");

  return (
    <div className="space-y-4">
      <PageHeader
        title={status.displayName || status.name}
        description={[status.host, status.os].filter(Boolean).join(" | ")}
        actions={(
          <>
            <Button variant="outline" disabled={running} onClick={() => start("Scan")}>
              <ScanSearch />
              {i18next.t("patch:Scan")}
            </Button>
            <ConfirmButton
              variant="outline"
              destructive={false}
              disabled={running || (selected.size === 0 && status.updateCount === 0)}
              title={selected.size > 0
                ? i18next.t("patch:Install {{count}} updates", {count: selected.size}) + "?"
                : i18next.t("patch:Install all updates on {{scope}}?", {scope: status.displayName || status.name})}
              description={i18next.t("patch:Assets are not restarted automatically. Restart them afterwards if an update needs it.")}
              onConfirm={() => start("Install", [...selected])}
            >
              <Download />
              {selected.size > 0 ? i18next.t("patch:Install {{count}} updates", {count: selected.size}) : i18next.t("patch:Install all")}
            </ConfirmButton>
            <ConfirmButton
              variant={status.rebootRequired ? "default" : "outline"}
              disabled={running}
              title={i18next.t("patch:Restart {{scope}}?", {scope: status.displayName || status.name})}
              description={i18next.t("patch:Connections to the assets drop while they restart. Each asset is scanned again once it is back.")}
              onConfirm={() => start("Reboot")}
            >
              <RotateCw />
              {i18next.t("patch:Restart")}
            </ConfirmButton>
          </>
        )}
      />

      <Card>
        <CardContent className="grid grid-cols-1 gap-4 pt-6 text-sm sm:grid-cols-4">
          <div>
            <div className="text-muted-foreground">{i18next.t("patch:Pending updates")}</div>
            <div className="mt-1"><UpdateCountCell status={status} /></div>
          </div>
          <div>
            <div className="text-muted-foreground">{i18next.t("patch:Package manager")}</div>
            <div className="mt-1">{status.manager || "-"}</div>
          </div>
          <div>
            <div className="text-muted-foreground">{i18next.t("patch:Last scanned")}</div>
            <div className="mt-1 tabular-nums">{status.scannedTime ? Setting.getFormattedDate(status.scannedTime) : "-"}</div>
          </div>
          <div>
            <div className="text-muted-foreground">{i18next.t("general:Status")}</div>
            <div className="mt-1"><PatchStateCell status={status} /></div>
          </div>
        </CardContent>
      </Card>

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList>
          <TabsTrigger value="updates">{i18next.t("patch:Updates")} ({updates.length})</TabsTrigger>
          <TabsTrigger value="history">{i18next.t("patch:History")}</TabsTrigger>
        </TabsList>
        <TabsContent value="updates">
          <Card className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-10">
                    <Checkbox
                      checked={updates.length > 0 && selected.size === updates.length ? true : selected.size > 0 ? "indeterminate" : false}
                      onCheckedChange={(checked) => setSelected(checked === true ? new Set(updates.map((update) => update.id)) : new Set())}
                      aria-label={i18next.t("general:Select all")}
                    />
                  </TableHead>
                  <TableHead className="min-w-72">{isWindows ? i18next.t("patch:Update") : i18next.t("patch:Package")}</TableHead>
                  <TableHead className="min-w-36">{isWindows ? i18next.t("patch:Classification") : i18next.t("patch:Source")}</TableHead>
                  <TableHead className="w-32">{i18next.t("patch:Severity")}</TableHead>
                  <TableHead className="min-w-56">{isWindows ? i18next.t("patch:Restart") : i18next.t("patch:Version")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {updates.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={5} className="h-20 text-center text-muted-foreground">
                      {status.scannedTime ? i18next.t("patch:No pending updates") : i18next.t("patch:Not scanned yet")}
                    </TableCell>
                  </TableRow>
                ) : updates.map((update) => (
                  <TableRow key={update.id}>
                    <TableCell>
                      <Checkbox checked={selected.has(update.id)} onCheckedChange={(checked) => toggle(update.id, checked === true)} />
                    </TableCell>
                    <TableCell>
                      <div className="font-medium">{update.title}</div>
                      {update.kb ? <div className="text-xs text-muted-foreground">{update.kb}</div> : null}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {isWindows && update.category ? i18next.t(`patch:${update.category}`) : update.category}
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {update.isSecurity ? <Badge variant="destructive">{i18next.t("patch:Security")}</Badge> : null}
                        {update.severity ? <Badge variant="outline">{update.severity}</Badge> : null}
                      </div>
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {isWindows ? (update.rebootRequired ? <Badge variant="warning">{i18next.t("patch:May restart")}</Badge> : null) : (
                        <span className="break-all">{update.currentVersion || "?"} {"→"} {update.newVersion}</span>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Card>
        </TabsContent>
        <TabsContent value="history">
          <Card>
            <CardContent className="pt-6">
              {jobs.length === 0 ? (
                <div className="text-sm text-muted-foreground">{i18next.t("patch:Nothing has run on this asset yet")}</div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-44">{i18next.t("general:Time")}</TableHead>
                      <TableHead className="w-44">{i18next.t("patch:Job")}</TableHead>
                      <TableHead className="w-28">{i18next.t("general:Status")}</TableHead>
                      <TableHead className="w-40">{i18next.t("general:User")}</TableHead>
                      <TableHead>{i18next.t("patch:Message")}</TableHead>
                      <TableHead className="w-24" />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {jobs.map((job) => (
                      <React.Fragment key={job.name}>
                        <TableRow>
                          <TableCell className="whitespace-nowrap tabular-nums text-muted-foreground">{Setting.getFormattedDate(job.createdTime)}</TableCell>
                          <TableCell>{getJobTitle(job)}</TableCell>
                          <TableCell><JobStatusBadge job={job} /></TableCell>
                          <TableCell className="text-muted-foreground">{job.user || i18next.t("patch:Scheduled")}</TableCell>
                          <TableCell className="break-all text-xs text-destructive">{job.message}</TableCell>
                          <TableCell className="text-right">
                            <Button size="sm" variant="ghost" onClick={() => setOpenJob(shownJob === job.name ? "-" : job.name)}>
                              {shownJob === job.name ? i18next.t("patch:Hide output") : i18next.t("patch:Output")}
                            </Button>
                          </TableCell>
                        </TableRow>
                        {shownJob === job.name ? (
                          <TableRow>
                            <TableCell colSpan={6}>
                              <JobOutput owner={job.owner} name={job.name} running={job.status === "Running"} />
                            </TableCell>
                          </TableRow>
                        ) : null}
                      </React.Fragment>
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}
