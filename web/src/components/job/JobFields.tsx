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
import {Loader2, Upload} from "lucide-react";
import * as AssetBackend from "@/backend/AssetBackend";
import * as JobBackend from "@/backend/JobBackend";
import type {Asset, Job} from "@/backend/types";
import {MultiSelect} from "@/components/common/MultiSelect";
import {SelectField} from "@/components/common/SelectField";
import {FormGrid, FormRow} from "@/components/crud/FormRow";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import {Textarea} from "@/components/ui/textarea";
import * as Setting from "@/lib/setting";
import {getJobTypeLabel} from "@/pages/JobListPage";

function formatSize(size: number) {
  if (size < 1024) {
    return `${size} B`;
  } else if (size < 1024 * 1024) {
    return `${(size / 1024).toFixed(1)} KB`;
  }
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
}

/** The SSH assets of the organization, which jobs run on. */
export function useJobAssets(owner: string) {
  const [assets, setAssets] = React.useState<Asset[]>([]);
  React.useEffect(() => {
    AssetBackend.getAssets(owner).then((res) => {
      if (res.status === "ok") {
        setAssets(((res.data ?? []) as Asset[]).filter((asset) => asset.protocol?.toUpperCase() === "SSH"));
      } else {
        Setting.showMessage("error", res.msg);
      }
    });
  }, [owner]);
  return assets;
}

/** The assets a job runs on: the ones named, and the ones with the tags. */
export function getPickedAssets(assets: Asset[], job: Partial<Job>) {
  return assets.filter((asset) => (job.assets ?? []).includes(asset.name) || (asset.tag && (job.assetTags ?? []).includes(asset.tag)));
}

/** What a job runs and where: its type, assets, command, script or file, timeout and concurrency.
 *  Used by the new job page and by job schedules. */
export function JobFields({owner, job, update, assets, onUploading, children}: {
  owner: string;
  job: Partial<Job>;
  update: (patch: Partial<Job>) => void;
  assets: Asset[];
  onUploading?: (uploading: boolean) => void;
  children?: React.ReactNode;
}) {
  const [uploading, setUploading] = React.useState(false);
  const fileInput = React.useRef<HTMLInputElement>(null);

  const assetOptions = assets.map((asset) => ({
    value: asset.name,
    label: `${asset.displayName || asset.name} (${asset.host})`,
    keywords: `${asset.name} ${asset.displayName} ${asset.host}`,
  }));
  const tagOptions = [...new Set(assets.map((asset) => asset.tag).filter(Boolean))].map((tag) => ({value: tag, label: tag}));
  const picked = getPickedAssets(assets, job);

  const upload = async(file: File) => {
    setUploading(true);
    onUploading?.(true);
    try {
      const res = await JobBackend.uploadJobFile(owner, file);
      if (res.status === "ok") {
        update({fileKey: res.data.key, fileName: res.data.name, fileSize: res.data.size});
      } else {
        Setting.showMessage("error", res.msg);
      }
    } catch (error) {
      Setting.showMessage("error", `${i18next.t("general:Failed to connect to server")}: ${error}`);
    } finally {
      setUploading(false);
      onUploading?.(false);
    }
  };

  return (
    <>
      <FormGrid>
        <FormRow labelKey="general:Display name">
          <Input value={job.displayName} placeholder={i18next.t("job:Optional, such as Check disk space")} onChange={(e) => update({displayName: e.target.value})} />
        </FormRow>
        <FormRow labelKey="general:Type">
          <SelectField
            value={job.type}
            options={JobBackend.JobTypes.map((type) => ({id: type, name: getJobTypeLabel(type)}))}
            onChange={(value) => update({type: value})}
          />
        </FormRow>
        <FormRow label={i18next.t("general:Assets")} tooltip={i18next.t("job:Only SSH assets can run jobs")}>
          <MultiSelect value={job.assets ?? []} options={assetOptions} onChange={(value) => update({assets: value})} />
        </FormRow>
        <FormRow label={i18next.t("permission:Asset tags")} tooltip={i18next.t("job:Every SSH asset with one of these tags")}>
          <MultiSelect value={job.assetTags ?? []} options={tagOptions} onChange={(value) => update({assetTags: value})} />
        </FormRow>

        {job.type === "Script" ? (
          <FormRow label={i18next.t("job:Script type")} tooltip={i18next.t("job:Bash runs on Linux assets, PowerShell on Windows assets and on Linux assets with pwsh")}>
            <SelectField
              value={job.scriptType}
              options={JobBackend.ScriptTypes.map((type) => ({id: type, name: type}))}
              onChange={(value) => update({scriptType: value})}
            />
          </FormRow>
        ) : null}

        {job.type === "File" ? (
          <>
            <FormRow label={i18next.t("job:File")}>
              <div className="flex items-center gap-3">
                <input
                  ref={fileInput}
                  type="file"
                  className="hidden"
                  onChange={(e) => {
                    const file = e.target.files?.[0];
                    if (file) {
                      upload(file);
                    }
                    e.target.value = "";
                  }}
                />
                <Button variant="outline" onClick={() => fileInput.current?.click()} disabled={uploading}>
                  {uploading ? <Loader2 className="animate-spin" /> : <Upload />}
                  {job.fileKey ? i18next.t("job:Replace file") : i18next.t("job:Upload file")}
                </Button>
                {job.fileKey ? (
                  <span className="min-w-0 truncate text-sm">
                    {job.fileName} <span className="text-muted-foreground">({formatSize(job.fileSize ?? 0)})</span>
                  </span>
                ) : null}
              </div>
            </FormRow>
            <FormRow label={i18next.t("job:Target path")} tooltip={i18next.t("job:A path ending with / is a folder, and the file keeps its name in it")}>
              <Input className="font-mono" value={job.filePath} placeholder="/tmp/  C:\Temp\" onChange={(e) => update({filePath: e.target.value})} />
            </FormRow>
          </>
        ) : (
          <FormRow label={job.type === "Script" ? i18next.t("job:Script") : i18next.t("job:Command")} block>
            <Textarea
              className="min-h-[220px] font-mono text-xs"
              spellCheck={false}
              value={job.content}
              placeholder={job.type === "Script" ? (job.scriptType === "PowerShell" ? "Get-Volume" : "#!/bin/bash\ndf -h") : "df -h"}
              onChange={(e) => update({content: e.target.value})}
            />
          </FormRow>
        )}

        <FormRow label={i18next.t("job:Timeout (seconds)")} tooltip={i18next.t("job:An asset still running after this is stopped")}>
          <Input type="number" min={1} value={job.timeout} onChange={(e) => update({timeout: Number(e.target.value)})} />
        </FormRow>
        <FormRow label={i18next.t("job:Concurrency")} tooltip={i18next.t("job:How many assets run at the same time")}>
          <Input type="number" min={1} max={100} value={job.concurrency} onChange={(e) => update({concurrency: Number(e.target.value)})} />
        </FormRow>
      </FormGrid>
      {children}
      <div className="mt-2 text-sm text-muted-foreground">
        {picked.length === 0
          ? i18next.t("job:Pick assets by name or by tag")
          : `${i18next.t("job:Runs on")}: ${picked.map((asset) => asset.displayName || asset.name).join(", ")}`}
      </div>
    </>
  );
}
