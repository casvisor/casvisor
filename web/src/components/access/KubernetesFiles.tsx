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
import {Download, Upload} from "lucide-react";
import * as AssetBackend from "@/backend/AssetBackend";
import {saveBlob} from "@/components/access/KubernetesLogs";
import {Button} from "@/components/ui/button";
import * as Setting from "@/lib/setting";

function getFileName(res: Response, path: string) {
  const match = /filename\*=UTF-8''([^;]+)/.exec(res.headers.get("Content-Disposition") ?? "");
  return match ? decodeURIComponent(match[1]) : path.split("/").pop() || "download";
}

interface Transfer {
  time: string;
  text: string;
  ok: boolean;
}

/** Copies files into and out of a container, the way kubectl cp does. */
export default function KubernetesFiles({target, password}: {target: AssetBackend.KubernetesContainer; password: string}) {
  const [downloadPath, setDownloadPath] = React.useState("");
  const [uploadDir, setUploadDir] = React.useState("/tmp");
  const [busy, setBusy] = React.useState(false);
  const [transfers, setTransfers] = React.useState<Transfer[]>([]);
  const fileInput = React.useRef<HTMLInputElement>(null);

  const note = (text: string, ok: boolean) => {
    setTransfers((transfers) => [{time: new Date().toLocaleTimeString(), text, ok}, ...transfers].slice(0, 50));
    if (!ok) {
      Setting.showMessage("error", text);
    }
  };

  const download = async() => {
    const path = downloadPath.trim();
    if (!path.startsWith("/")) {
      Setting.showMessage("error", i18next.t("kubernetes:Enter an absolute path"));
      return;
    }
    setBusy(true);
    try {
      const res = await AssetBackend.fetchKubernetesFile(target, path, password);
      if ((res.headers.get("Content-Type") ?? "").includes("application/json") || !res.ok) {
        const data = await Setting.handleFetchResponse(res);
        note(`${path}: ${data?.msg || `HTTP ${res.status}`}`, false);
        return;
      }
      const name = getFileName(res, path);
      saveBlob(await res.blob(), name);
      note(`${i18next.t("kubernetes:Downloaded")} ${path} → ${name}`, true);
    } catch (e) {
      note(`${path}: ${String(e)}`, false);
    } finally {
      setBusy(false);
    }
  };

  const upload = async(files: File[]) => {
    const dir = uploadDir.trim();
    if (files.length === 0) {
      return;
    }
    if (!dir.startsWith("/")) {
      Setting.showMessage("error", i18next.t("kubernetes:Enter an absolute path"));
      return;
    }
    setBusy(true);
    try {
      const res = await AssetBackend.uploadKubernetesFiles(target, dir, files, password);
      const names = files.map((file) => file.name).join(", ");
      if (res.status === "ok") {
        note(`${i18next.t("kubernetes:Uploaded")} ${names} → ${dir}`, true);
      } else {
        note(`${names}: ${res.msg}`, false);
      }
    } finally {
      setBusy(false);
    }
  };

  const inputClass = "h-8 min-w-0 flex-1 rounded-md border border-neutral-700 bg-neutral-900 px-2 font-mono text-xs outline-none placeholder:text-neutral-500";
  const buttonClass = "h-8 gap-1.5 border-neutral-700 bg-neutral-900 text-xs text-neutral-200 hover:bg-neutral-800 hover:text-neutral-100";

  return (
    <div className="flex h-full min-h-0 flex-col gap-4 overflow-auto p-4 text-sm">
      <p className="text-xs text-neutral-400">{i18next.t("kubernetes:Files - Description")}</p>
      <form
        className="space-y-1.5"
        onSubmit={(e) => {
          e.preventDefault();
          download();
        }}
      >
        <label className="block text-xs text-neutral-400">{i18next.t("kubernetes:Download a file or a directory")}</label>
        <div className="flex gap-2">
          <input className={inputClass} placeholder="/var/log/app.log" value={downloadPath} onChange={(e) => setDownloadPath(e.target.value)} />
          <Button type="submit" variant="outline" className={buttonClass} disabled={busy || !downloadPath.trim()}>
            <Download className="h-3.5 w-3.5" />
            {i18next.t("kubernetes:Download")}
          </Button>
        </div>
      </form>
      <div className="space-y-1.5">
        <label className="block text-xs text-neutral-400">{i18next.t("kubernetes:Upload files into a directory")}</label>
        <div className="flex gap-2">
          <input className={inputClass} placeholder="/tmp" value={uploadDir} onChange={(e) => setUploadDir(e.target.value)} />
          <Button variant="outline" className={buttonClass} disabled={busy || !uploadDir.trim()} onClick={() => fileInput.current?.click()}>
            <Upload className="h-3.5 w-3.5" />
            {i18next.t("kubernetes:Upload")}
          </Button>
          <input
            ref={fileInput}
            type="file"
            multiple
            className="hidden"
            onChange={(e) => {
              upload(Array.from(e.target.files ?? []));
              e.target.value = "";
            }}
          />
        </div>
      </div>
      {transfers.length > 0 ? (
        <div className="space-y-1 border-t border-neutral-700 pt-3">
          {transfers.map((transfer, i) => (
            <div key={i} className="flex gap-2 font-mono text-xs">
              <span className="shrink-0 text-neutral-500">{transfer.time}</span>
              <span className={transfer.ok ? "break-all text-neutral-300" : "break-all text-red-400"}>{transfer.text}</span>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}
