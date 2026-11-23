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
import {Download, Eraser} from "lucide-react";
import * as AssetBackend from "@/backend/AssetBackend";
import {Button} from "@/components/ui/button";

// the text kept on screen, the oldest lines dropped beyond it
const maxLogLength = 2 * 1024 * 1024;

const tailOptions = [100, 500, 1000, 5000, 0];

/** Reads the error of a response that is JSON instead of the stream asked for. */
async function getResponseError(res: Response) {
  if (!(res.headers.get("Content-Type") ?? "").includes("application/json")) {
    return res.ok ? "" : `HTTP ${res.status}`;
  }
  const data = await res.json().catch(() => null);
  return data?.msg || data?.message || `HTTP ${res.status}`;
}

export function saveBlob(blob: Blob, name: string) {
  const link = document.createElement("a");
  link.href = URL.createObjectURL(blob);
  link.download = name;
  link.click();
  setTimeout(() => URL.revokeObjectURL(link.href), 10000);
}

/** Shows the log of a container, following it as it grows. */
export default function KubernetesLogs({target, password}: {target: AssetBackend.KubernetesContainer; password: string}) {
  const [tailLines, setTailLines] = React.useState(500);
  const [follow, setFollow] = React.useState(true);
  const [timestamps, setTimestamps] = React.useState(false);
  const [previous, setPrevious] = React.useState(false);
  const [text, setText] = React.useState("");
  const [error, setError] = React.useState("");
  const [streaming, setStreaming] = React.useState(false);
  const [downloading, setDownloading] = React.useState(false);
  const ref = React.useRef<HTMLPreElement>(null);
  const atBottom = React.useRef(true);

  React.useEffect(() => {
    const controller = new AbortController();
    setText("");
    setError("");
    setStreaming(true);
    atBottom.current = true;

    (async() => {
      try {
        const res = await AssetBackend.fetchKubernetesLogs(target, {tailLines, follow: follow && !previous, timestamps, previous}, password, controller.signal);
        const message = await getResponseError(res);
        if (message || !res.body) {
          setError(message || `HTTP ${res.status}`);
          return;
        }
        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        for (;;) {
          const {done, value} = await reader.read();
          if (done) {
            break;
          }
          const chunk = decoder.decode(value, {stream: true});
          setText((text) => {
            const next = text + chunk;
            return next.length > maxLogLength ? next.slice(next.indexOf("\n", next.length - maxLogLength) + 1) : next;
          });
        }
      } catch (e) {
        if (!controller.signal.aborted) {
          setError(String(e));
        }
      } finally {
        if (!controller.signal.aborted) {
          setStreaming(false);
        }
      }
    })();

    return () => controller.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [target.assetId, target.account, target.namespace, target.pod, target.container, tailLines, follow, timestamps, previous, password]);

  React.useLayoutEffect(() => {
    if (atBottom.current && ref.current) {
      ref.current.scrollTop = ref.current.scrollHeight;
    }
  }, [text]);

  const download = async() => {
    setDownloading(true);
    try {
      const controller = new AbortController();
      const res = await AssetBackend.fetchKubernetesLogs(target, {tailLines: 0, follow: false, timestamps, previous}, password, controller.signal);
      const message = await getResponseError(res);
      if (message) {
        setError(message);
        return;
      }
      saveBlob(await res.blob(), `${target.pod}-${target.container}${previous ? "-previous" : ""}.log`);
    } finally {
      setDownloading(false);
    }
  };

  const checkbox = (label: string, checked: boolean, onChange: (checked: boolean) => void, disabled = false) => (
    <label className="flex items-center gap-1.5 whitespace-nowrap text-xs text-neutral-300">
      <input type="checkbox" className="h-3.5 w-3.5" checked={checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)} />
      {label}
    </label>
  );

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 flex-wrap items-center gap-3 border-b border-neutral-700 px-3 py-1.5">
        <label className="flex items-center gap-1.5 text-xs text-neutral-300">
          {i18next.t("kubernetes:Lines")}
          <select
            className="h-7 rounded-md border border-neutral-700 bg-neutral-900 px-1.5 text-xs outline-none"
            value={tailLines}
            onChange={(e) => setTailLines(Number(e.target.value))}
          >
            {tailOptions.map((value) => <option key={value} value={value}>{value === 0 ? i18next.t("kubernetes:All") : value}</option>)}
          </select>
        </label>
        {checkbox(i18next.t("kubernetes:Follow"), follow && !previous, setFollow, previous)}
        {checkbox(i18next.t("kubernetes:Timestamps"), timestamps, setTimestamps)}
        {checkbox(i18next.t("kubernetes:Previous run"), previous, setPrevious)}
        {streaming && follow && !previous ? <span className="text-xs text-emerald-400">{i18next.t("kubernetes:Following")}</span> : null}
        <div className="ml-auto flex items-center gap-1">
          <Button
            variant="ghost"
            size="sm"
            className="h-7 gap-1.5 px-2 text-xs text-neutral-300 hover:bg-neutral-800 hover:text-neutral-100"
            onClick={() => setText("")}
          >
            <Eraser className="h-3.5 w-3.5" />
            {i18next.t("kubernetes:Clear")}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className="h-7 gap-1.5 px-2 text-xs text-neutral-300 hover:bg-neutral-800 hover:text-neutral-100"
            disabled={downloading}
            onClick={download}
          >
            <Download className="h-3.5 w-3.5" />
            {i18next.t("kubernetes:Download")}
          </Button>
        </div>
      </div>
      {error ? <div className="shrink-0 break-words border-b border-neutral-700 px-3 py-2 text-sm text-red-400">{error}</div> : null}
      <pre
        ref={ref}
        className="min-h-0 flex-1 overflow-auto whitespace-pre-wrap break-all px-3 py-2 font-mono text-xs leading-5 text-neutral-200"
        onScroll={(e) => {
          const el = e.currentTarget;
          atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
        }}
      >
        {text || (!error && !streaming ? i18next.t("kubernetes:The log is empty") : "")}
      </pre>
    </div>
  );
}
