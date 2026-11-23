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
import {useLocation, useNavigate} from "react-router-dom";
import {ArrowLeft, CalendarClock, Loader2, Play} from "lucide-react";
import * as JobBackend from "@/backend/JobBackend";
import type {Job} from "@/backend/types";
import {getPickedAssets, JobFields, useAssetNodes, useJobAssets} from "@/components/job/JobFields";
import {PageHeader} from "@/components/crud/PageHeader";
import {Button} from "@/components/ui/button";
import {Card, CardContent} from "@/components/ui/card";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";

/** A new job; a job passed in the router state, as "Run as new job" does, fills the form. */
export default function JobEditPage() {
  const {account} = useAccount();
  const owner = Setting.getRequestOrganization(account);
  const navigate = useNavigate();
  const location = useLocation();
  const from = (location.state as {job?: Job} | null)?.job;

  const assets = useJobAssets(owner);
  const nodes = useAssetNodes(owner);
  const [uploading, setUploading] = React.useState(false);
  const [starting, setStarting] = React.useState(false);
  const [job, setJob] = React.useState<Partial<Job>>(() => ({
    owner,
    displayName: from?.displayName ?? "",
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
  }));

  const update = (patch: Partial<Job>) => setJob((current) => ({...current, ...patch}));

  const picked = getPickedAssets(assets, job, nodes);

  const start = async() => {
    setStarting(true);
    try {
      const res = await JobBackend.startJob({...job, owner, timeout: Number(job.timeout) || 0, concurrency: Number(job.concurrency) || 0});
      if (res.status === "ok") {
        Setting.showMessage("success", i18next.t("job:Started"));
        navigate(`/jobs/${res.data.owner}/${res.data.name}`);
      } else {
        Setting.showMessage("error", res.msg);
      }
    } catch (error) {
      Setting.showMessage("error", `${i18next.t("general:Failed to connect to server")}: ${error}`);
    } finally {
      setStarting(false);
    }
  };

  const ready = picked.length > 0 && (job.type === "File" ? Boolean(job.fileKey && job.filePath) : Boolean(job.content?.trim()));

  return (
    <div className="space-y-4">
      <PageHeader
        title={i18next.t("job:New job")}
        description={i18next.t("general:Run commands, scripts, file transfers and Ansible playbooks on many SSH assets at once")}
        actions={(
          <>
            <Button variant="ghost" onClick={() => navigate("/jobs")}>
              <ArrowLeft />
              {i18next.t("general:Cancel")}
            </Button>
            <Button variant="outline" disabled={uploading} onClick={() => navigate("/job-schedules/new", {state: {job}})}>
              <CalendarClock />
              {i18next.t("job:Run on a schedule")}
            </Button>
            <Button onClick={start} disabled={!ready || starting || uploading}>
              {starting ? <Loader2 className="animate-spin" /> : <Play />}
              {i18next.t("job:Run on {{count}} assets", {count: picked.length})}
            </Button>
          </>
        )}
      />

      <Card>
        <CardContent className="pt-4">
          <JobFields owner={owner} job={job} update={update} assets={assets} onUploading={setUploading} />
        </CardContent>
      </Card>
    </div>
  );
}
