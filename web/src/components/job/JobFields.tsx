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
import {CheckCircle2, Loader2, Upload, X} from "lucide-react";
import * as AssetBackend from "@/backend/AssetBackend";
import * as AssetNodeBackend from "@/backend/AssetNodeBackend";
import * as JobBackend from "@/backend/JobBackend";
import type {Asset, AssetNode, Job} from "@/backend/types";
import {MultiSelect} from "@/components/common/MultiSelect";
import {SelectField} from "@/components/common/SelectField";
import {FormGrid, FormRow} from "@/components/crud/FormRow";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import {Switch} from "@/components/ui/switch";
import {Textarea} from "@/components/ui/textarea";
import {getDescendantNames, getNodeOptions} from "@/lib/assetNode";
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

/** The nodes of the asset tree, which jobs can pick assets by. */
export function useAssetNodes(owner: string) {
  const [nodes, setNodes] = React.useState<AssetNode[]>([]);
  React.useEffect(() => {
    AssetNodeBackend.getAssetNodes(owner).then((res) => {
      if (res.status === "ok") {
        setNodes((res.data ?? []) as AssetNode[]);
      }
    });
  }, [owner]);
  return nodes;
}

/** The assets a job runs on: the ones named, the ones with the tags and the ones in the nodes or below them. */
export function getPickedAssets(assets: Asset[], job: Partial<Job>, nodes: AssetNode[] = []) {
  const nodeNames = new Set<string>();
  for (const name of job.assetNodes ?? []) {
    getDescendantNames(nodes, name).forEach((item) => nodeNames.add(item));
  }
  return assets.filter((asset) => (job.assets ?? []).includes(asset.name)
    || (asset.tag && (job.assetTags ?? []).includes(asset.tag))
    || (asset.nodes ?? []).some((node) => nodeNames.has(node)));
}

const playbookPlaceholder = [
  "- name: Install nginx",
  "  hosts: all",
  "  become: true",
  "  tasks:",
  "    - name: Install the package",
  "      apt:",
  "        name: nginx",
  "        state: present",
  "        update_cache: true",
  "    - name: Write the site",
  "      template:",
  "        src: site.conf.j2",
  "        dest: /etc/nginx/conf.d/site.conf",
  "      notify: Reload nginx",
  "  handlers:",
  "    - name: Reload nginx",
  "      service:",
  "        name: nginx",
  "        state: reloaded",
].join("\n");

/** The playbook of a job: its YAML, the files its tasks use, its extra variables and check mode. */
function PlaybookFields({owner, job, update, onUploading}: {
  owner: string;
  job: Partial<Job>;
  update: (patch: Partial<Job>) => void;
  onUploading?: (uploading: boolean) => void;
}) {
  const [uploading, setUploading] = React.useState(false);
  const [checking, setChecking] = React.useState(false);
  const [checked, setChecked] = React.useState("");
  const fileInput = React.useRef<HTMLInputElement>(null);
  const playbookInput = React.useRef<HTMLInputElement>(null);
  const files = job.files ?? [];

  const upload = async(list: File[]) => {
    setUploading(true);
    onUploading?.(true);
    try {
      let next = [...files];
      for (const file of list) {
        const res = await JobBackend.uploadJobFile(owner, file);
        if (res.status !== "ok") {
          Setting.showMessage("error", res.msg);
          break;
        }
        next = [...next.filter((item) => item.name !== res.data.name), res.data];
      }
      update({files: next});
    } catch (error) {
      Setting.showMessage("error", `${i18next.t("general:Failed to connect to server")}: ${error}`);
    } finally {
      setUploading(false);
      onUploading?.(false);
    }
  };

  const check = async() => {
    setChecking(true);
    setChecked("");
    try {
      const res = await JobBackend.checkPlaybook({owner, content: job.content, variables: job.variables, files});
      if (res.status === "ok") {
        setChecked(i18next.t("job:{{plays}} plays, {{tasks}} tasks and {{handlers}} handlers, with the modules {{modules}}", {
          plays: res.data.plays, tasks: res.data.tasks, handlers: res.data.handlers, modules: (res.data.modules ?? []).join(", "),
        }));
      } else {
        Setting.showMessage("error", res.msg);
      }
    } catch (error) {
      Setting.showMessage("error", `${i18next.t("general:Failed to connect to server")}: ${error}`);
    } finally {
      setChecking(false);
    }
  };

  return (
    <>
      <FormRow label={i18next.t("job:Playbook")} tooltip={i18next.t("job:Playbook - Tooltip")} block>
        <div className="flex w-full flex-col gap-2">
          <Textarea
            className="min-h-[320px] font-mono text-xs"
            spellCheck={false}
            value={job.content}
            placeholder={playbookPlaceholder}
            onChange={(e) => {
              setChecked("");
              update({content: e.target.value});
            }}
          />
          <div className="flex flex-wrap items-center gap-2">
            <input
              ref={playbookInput}
              type="file"
              accept=".yml,.yaml"
              className="hidden"
              onChange={(e) => {
                const file = e.target.files?.[0];
                if (file) {
                  file.text().then((text) => update({content: text}));
                }
                e.target.value = "";
              }}
            />
            <Button variant="outline" size="sm" onClick={() => playbookInput.current?.click()}>
              <Upload />
              {i18next.t("job:Open playbook file")}
            </Button>
            <Button variant="outline" size="sm" onClick={check} disabled={checking || !job.content?.trim()}>
              {checking ? <Loader2 className="animate-spin" /> : <CheckCircle2 />}
              {i18next.t("job:Check playbook")}
            </Button>
            {checked ? <span className="text-xs text-success">{checked}</span> : null}
          </div>
        </div>
      </FormRow>
      <FormRow label={i18next.t("job:Files")} tooltip={i18next.t("job:Files - Tooltip")} block>
        <div className="flex w-full flex-col gap-2">
          <input
            ref={fileInput}
            type="file"
            multiple
            className="hidden"
            onChange={(e) => {
              const list = Array.from(e.target.files ?? []);
              if (list.length > 0) {
                upload(list);
              }
              e.target.value = "";
            }}
          />
          {files.length > 0 ? (
            <div className="flex flex-col gap-1">
              {files.map((file) => (
                <div key={file.key} className="flex items-center gap-2 text-sm">
                  <span className="font-mono text-xs">{file.name}</span>
                  <span className="text-xs text-muted-foreground">({formatSize(file.size)})</span>
                  <Button variant="ghost" size="iconSm" aria-label={i18next.t("general:Delete")} onClick={() => update({files: files.filter((item) => item.key !== file.key)})}>
                    <X />
                  </Button>
                </div>
              ))}
            </div>
          ) : null}
          <div>
            <Button variant="outline" size="sm" onClick={() => fileInput.current?.click()} disabled={uploading}>
              {uploading ? <Loader2 className="animate-spin" /> : <Upload />}
              {i18next.t("job:Attach files")}
            </Button>
          </div>
        </div>
      </FormRow>
      <FormRow label={i18next.t("job:Variables")} tooltip={i18next.t("job:Variables - Tooltip")} block>
        <Textarea
          className="min-h-[80px] font-mono text-xs"
          spellCheck={false}
          value={job.variables ?? ""}
          placeholder={"port: 8080\nusers: [alice, bob]"}
          onChange={(e) => {
            setChecked("");
            update({variables: e.target.value});
          }}
        />
      </FormRow>
      <FormRow label={i18next.t("job:Check mode")} tooltip={i18next.t("job:Check mode - Tooltip")}>
        <Switch checked={Boolean(job.checkMode)} onCheckedChange={(checkMode) => update({checkMode})} />
      </FormRow>
    </>
  );
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
  const nodes = useAssetNodes(owner);

  const assetOptions = assets.map((asset) => ({
    value: asset.name,
    label: `${asset.displayName || asset.name} (${asset.host})`,
    keywords: `${asset.name} ${asset.displayName} ${asset.host}`,
  }));
  const tagOptions = [...new Set(assets.map((asset) => asset.tag).filter(Boolean))].map((tag) => ({value: tag, label: tag}));
  const picked = getPickedAssets(assets, job, nodes);

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
            onChange={(value) => update(value === "Playbook" && (!job.timeout || job.timeout === 60) ? {type: value, timeout: 1800} : {type: value})}
          />
        </FormRow>
        <FormRow label={i18next.t("general:Assets")} tooltip={i18next.t("job:Only SSH assets can run jobs")}>
          <MultiSelect value={job.assets ?? []} options={assetOptions} onChange={(value) => update({assets: value})} />
        </FormRow>
        <FormRow label={i18next.t("permission:Asset tags")} tooltip={i18next.t("job:Every SSH asset with one of these tags")}>
          <MultiSelect value={job.assetTags ?? []} options={tagOptions} onChange={(value) => update({assetTags: value})} />
        </FormRow>
        <FormRow label={i18next.t("assetNode:Nodes")} tooltip={i18next.t("job:Every SSH asset in one of these nodes or below them")}>
          <MultiSelect value={job.assetNodes ?? []} options={getNodeOptions(nodes)} onChange={(value) => update({assetNodes: value})} />
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

        {job.type === "Playbook" ? (
          <PlaybookFields owner={owner} job={job} update={update} onUploading={onUploading} />
        ) : job.type === "File" ? (
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
          ? i18next.t("job:Pick assets by name, tag or node")
          : `${i18next.t("job:Runs on")}: ${picked.map((asset) => asset.displayName || asset.name).join(", ")}`}
      </div>
    </>
  );
}
