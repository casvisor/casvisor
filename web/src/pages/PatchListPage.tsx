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
import {Link} from "react-router-dom";
import {Download, Loader2, RefreshCw, RotateCw, ScanSearch} from "lucide-react";
import * as PatchBackend from "@/backend/PatchBackend";
import type {PatchJob, PatchStatus} from "@/backend/types";
import {ConfirmButton} from "@/components/common/ConfirmButton";
import {PageHeader} from "@/components/crud/PageHeader";
import {Badge} from "@/components/ui/badge";
import {Button} from "@/components/ui/button";
import {Card, CardContent, CardHeader, CardTitle} from "@/components/ui/card";
import {Checkbox} from "@/components/ui/checkbox";
import {Table, TableBody, TableCell, TableHead, TableHeader, TableRow} from "@/components/ui/table";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";

/** Starts a scan, install or restart; resolves to the jobs started, or null when none did. */
export async function startPatchJob(owner: string, assets: string[], type: PatchBackend.PatchJobType, updates: string[] = []): Promise<PatchJob[] | null> {
  try {
    const res = await PatchBackend.startPatchJob(owner, assets, type, updates);
    if (res.status !== "ok") {
      Setting.showMessage("error", res.msg);
      return null;
    }
    for (const error of (res.data2 ?? []) as string[]) {
      Setting.showMessage("warning", error);
    }
    Setting.showMessage("success", i18next.t("patch:Started"));
    return res.data as PatchJob[];
  } catch (error) {
    Setting.showMessage("error", `${i18next.t("general:Failed to connect to server")}: ${error}`);
    return null;
  }
}

export function getJobTypeLabel(type: string) {
  if (type === "Scan") {
    return i18next.t("patch:Scanning");
  } else if (type === "Install") {
    return i18next.t("patch:Installing");
  } else if (type === "Reboot") {
    return i18next.t("patch:Restarting");
  }
  return type;
}

export function PatchStateCell({status}: {status: PatchStatus}) {
  if (status.job) {
    return (
      <Badge variant="info">
        <Loader2 className="h-3 w-3 animate-spin" />
        {getJobTypeLabel(status.jobType)}
      </Badge>
    );
  }
  if (status.message) {
    return <span className="line-clamp-2 break-all text-xs text-destructive" title={status.message}>{status.message}</span>;
  }
  if (!status.scannedTime) {
    return <span className="text-muted-foreground">{i18next.t("patch:Not scanned")}</span>;
  }
  if (status.updateCount === 0 && !status.rebootRequired) {
    return <Badge variant="success">{i18next.t("patch:Up to date")}</Badge>;
  }
  return null;
}

export function UpdateCountCell({status}: {status: PatchStatus}) {
  if (!status.scannedTime) {
    return <span className="text-muted-foreground">-</span>;
  }
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className="tabular-nums">{status.updateCount}</span>
      {status.securityCount > 0 ? (
        <Badge variant="destructive">{i18next.t("patch:{{count}} security", {count: status.securityCount})}</Badge>
      ) : null}
      {status.rebootRequired ? <Badge variant="warning">{i18next.t("patch:Restart required")}</Badge> : null}
    </div>
  );
}

function Stat({title, value}: {title: string; value: number}) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-normal text-muted-foreground">{title}</CardTitle>
      </CardHeader>
      <CardContent className="text-2xl font-semibold tabular-nums">{value}</CardContent>
    </Card>
  );
}

/** Pending OS updates of every SSH asset, scanned, installed and restarted from here instead of signing in to each machine. */
export default function PatchListPage() {
  const {account} = useAccount();
  const owner = Setting.getRequestOrganization(account);
  const [statuses, setStatuses] = React.useState<PatchStatus[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [selected, setSelected] = React.useState<Set<string>>(new Set());

  const refresh = React.useCallback(() => {
    return PatchBackend.getPatchStatuses(owner).then((res) => {
      setLoading(false);
      if (res.status === "ok") {
        setStatuses(res.data ?? []);
      } else {
        Setting.showMessage("error", res.msg);
      }
    });
  }, [owner]);

  React.useEffect(() => {
    setLoading(true);
    refresh();
  }, [refresh]);

  const running = statuses.some((status) => status.job);
  React.useEffect(() => {
    if (!running) {
      return;
    }
    const timer = window.setTimeout(refresh, 3000);
    return () => window.clearTimeout(timer);
  }, [running, statuses, refresh]);

  const start = async(assets: string[], type: PatchBackend.PatchJobType) => {
    if (await startPatchJob(owner, assets, type)) {
      setSelected(new Set());
      await refresh();
    }
  };

  const toggle = (name: string, checked: boolean) => {
    const next = new Set(selected);
    if (checked) {
      next.add(name);
    } else {
      next.delete(name);
    }
    setSelected(next);
  };

  const allSelected = statuses.length > 0 && selected.size === statuses.length;
  const targets = selected.size > 0 ? [...selected] : statuses.map((status) => status.name);
  const scopeText = selected.size > 0
    ? i18next.t("patch:{{count}} selected assets", {count: selected.size})
    : i18next.t("patch:all {{count}} assets", {count: statuses.length});

  return (
    <div className="space-y-4">
      <PageHeader
        title={i18next.t("general:Patches")}
        description={i18next.t("general:Scan and install system updates on SSH assets")}
        actions={(
          <>
            <Button variant="outline" onClick={() => refresh()}>
              <RefreshCw />
              {i18next.t("general:Refresh")}
            </Button>
            <Button variant="outline" disabled={statuses.length === 0} onClick={() => start(targets, "Scan")}>
              <ScanSearch />
              {selected.size > 0 ? i18next.t("patch:Scan selected") : i18next.t("patch:Scan all")}
            </Button>
            <ConfirmButton
              variant="outline"
              destructive={false}
              disabled={statuses.length === 0}
              title={i18next.t("patch:Install all updates on {{scope}}?", {scope: scopeText})}
              description={i18next.t("patch:Assets are not restarted automatically. Restart them afterwards if an update needs it.")}
              onConfirm={() => start(targets, "Install")}
            >
              <Download />
              {selected.size > 0 ? i18next.t("patch:Install on selected") : i18next.t("patch:Install on all")}
            </ConfirmButton>
            <ConfirmButton
              variant="outline"
              disabled={selected.size === 0}
              title={i18next.t("patch:Restart {{scope}}?", {scope: scopeText})}
              description={i18next.t("patch:Connections to the assets drop while they restart. Each asset is scanned again once it is back.")}
              onConfirm={() => start([...selected], "Reboot")}
            >
              <RotateCw />
              {i18next.t("patch:Restart selected")}
            </ConfirmButton>
          </>
        )}
      />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <Stat title={i18next.t("patch:Assets with updates")} value={statuses.filter((status) => status.updateCount > 0).length} />
        <Stat title={i18next.t("patch:Security updates")} value={statuses.reduce((sum, status) => sum + status.securityCount, 0)} />
        <Stat title={i18next.t("patch:Assets to restart")} value={statuses.filter((status) => status.rebootRequired).length} />
      </div>

      <Card>
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-10">
                  <Checkbox
                    checked={allSelected ? true : selected.size > 0 ? "indeterminate" : false}
                    onCheckedChange={(checked) => setSelected(checked === true ? new Set(statuses.map((status) => status.name)) : new Set())}
                    aria-label={i18next.t("general:Select all")}
                  />
                </TableHead>
                <TableHead className="min-w-36">{i18next.t("general:Asset")}</TableHead>
                <TableHead className="min-w-48">{i18next.t("patch:System")}</TableHead>
                <TableHead className="min-w-36">{i18next.t("patch:Pending updates")}</TableHead>
                <TableHead className="w-40">{i18next.t("patch:Last scanned")}</TableHead>
                <TableHead className="min-w-28">{i18next.t("general:Status")}</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {statuses.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} className="h-20 text-center text-muted-foreground">
                    {loading ? i18next.t("general:Loading") : i18next.t("patch:No SSH assets yet. Add the SSH asset of a machine to manage its updates.")}
                  </TableCell>
                </TableRow>
              ) : statuses.map((status) => (
                <TableRow key={status.name}>
                  <TableCell>
                    <Checkbox checked={selected.has(status.name)} onCheckedChange={(checked) => toggle(status.name, checked === true)} />
                  </TableCell>
                  <TableCell>
                    <Link to={`/patches/${status.owner}/${status.name}`} className="font-medium underline-offset-4 hover:underline">
                      {status.displayName || status.name}
                    </Link>
                    <div className="text-xs text-muted-foreground">{status.host}</div>
                  </TableCell>
                  <TableCell>
                    {status.os ? <div>{status.os}</div> : <span className="text-muted-foreground">-</span>}
                    {status.manager ? <div className="text-xs text-muted-foreground">{status.manager}</div> : null}
                  </TableCell>
                  <TableCell><UpdateCountCell status={status} /></TableCell>
                  <TableCell className="whitespace-nowrap tabular-nums text-muted-foreground">
                    {status.scannedTime ? Setting.getFormattedDate(status.scannedTime) : "-"}
                  </TableCell>
                  <TableCell><PatchStateCell status={status} /></TableCell>
                  <TableCell>
                    <div className="flex justify-end gap-1">
                      <Button size="sm" variant="ghost" disabled={Boolean(status.job)} onClick={() => start([status.name], "Scan")}>
                        {i18next.t("patch:Scan")}
                      </Button>
                      <ConfirmButton
                        size="sm"
                        variant="ghost"
                        destructive={false}
                        disabled={Boolean(status.job) || status.updateCount === 0}
                        title={i18next.t("patch:Install all updates on {{scope}}?", {scope: status.displayName || status.name})}
                        description={i18next.t("patch:Assets are not restarted automatically. Restart them afterwards if an update needs it.")}
                        onConfirm={() => start([status.name], "Install")}
                      >
                        {i18next.t("patch:Install")}
                      </ConfirmButton>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </Card>
    </div>
  );
}
