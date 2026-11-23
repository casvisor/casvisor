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
import {Box, ChevronDown, ChevronRight, FileKey, RefreshCw, SquareTerminal, X} from "lucide-react";
import * as AssetBackend from "@/backend/AssetBackend";
import type {KubernetesPod, KubernetesPods} from "@/backend/types";
import KubeconfigDialog from "@/components/access/KubeconfigDialog";
import KubernetesFiles from "@/components/access/KubernetesFiles";
import KubernetesLogs from "@/components/access/KubernetesLogs";
import SshView, {type KubernetesTarget} from "@/components/access/SshView";
import type {ConnectionProps} from "@/components/access/util";
import {Button} from "@/components/ui/button";
import {cn} from "@/lib/utils";
import * as Setting from "@/lib/setting";

function getPhaseColor(phase: string) {
  switch (phase) {
  case "Running":
    return "bg-emerald-500";
  case "Pending":
    return "bg-amber-400";
  case "Succeeded":
    return "bg-neutral-500";
  default:
    return "bg-red-500";
  }
}

type ContainerView = "terminal" | "logs" | "files";

/** Browses the namespaces and pods of a Kubernetes cluster, and opens a shell in a container, its log or its files. */
export default function KubernetesView(props: ConnectionProps) {
  const {assetId, account, password, inTab, addClient} = props;
  const assetName = Setting.getShortName(assetId);

  const [data, setData] = React.useState<KubernetesPods | null>(null);
  const [namespace, setNamespace] = React.useState("");
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState("");
  const [filter, setFilter] = React.useState("");
  const [expanded, setExpanded] = React.useState<Record<string, boolean>>({});
  const [target, setTarget] = React.useState<KubernetesTarget | null>(null);
  const [targetRunning, setTargetRunning] = React.useState(false);
  const [targetReadOnly, setTargetReadOnly] = React.useState(false);
  const [view, setView] = React.useState<ContainerView>("terminal");
  const [shell, setShell] = React.useState(0);
  const [terminalOpened, setTerminalOpened] = React.useState(false);
  const [showKubeconfig, setShowKubeconfig] = React.useState(false);

  const load = (name: string) => {
    setLoading(true);
    AssetBackend.getKubernetesPods(assetId, account, name, password).then((res) => {
      setLoading(false);
      if (res.status === "ok" && res.data) {
        setData(res.data);
        setNamespace(res.data.namespace);
        setError("");
      } else {
        setError(res.msg || i18next.t("kubernetes:Failed to list the pods"));
      }
    });
  };

  React.useEffect(() => {
    if (!inTab) {
      document.title = assetName;
    }
    addClient?.({disconnect: () => setTarget(null)});
    load("");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [assetId]);

  const open = (pod: KubernetesPod, container: string) => {
    const running = pod.phase === "Running";
    const readOnly = data?.readOnly ?? false;
    setTarget({namespace, pod: pod.name, container});
    setTargetRunning(running);
    setTargetReadOnly(readOnly);
    setView(running && !readOnly ? "terminal" : "logs");
    setTerminalOpened(running && !readOnly);
    setShell((shell) => shell + 1);
  };

  const selectView = (next: ContainerView) => {
    setView(next);
    if (next === "terminal") {
      setTerminalOpened(true);
    }
  };

  const containerTarget = target ? {assetId, account, ...target} : null;

  const pods = (data?.pods ?? []).filter((pod) => pod.name.includes(filter.trim()));

  return (
    <div className="flex h-full w-full bg-[#1b1b1b] text-neutral-200">
      <div className="flex w-72 shrink-0 flex-col border-r border-neutral-700 text-sm">
        <div className="flex h-10 shrink-0 items-center gap-2 border-b border-neutral-700 px-3">
          <Box className="h-4 w-4 shrink-0 text-sky-400" />
          <span className="truncate font-medium" title={assetId}>{assetName}</span>
          <Button
            variant="ghost"
            size="icon"
            className="ml-auto h-7 w-7 text-neutral-300 hover:bg-neutral-800 hover:text-neutral-100"
            onClick={() => setShowKubeconfig(true)}
            title={i18next.t("kubernetes:kubectl")}
            aria-label={i18next.t("kubernetes:kubectl")}
          >
            <FileKey className="h-3.5 w-3.5" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7 text-neutral-300 hover:bg-neutral-800 hover:text-neutral-100"
            disabled={loading}
            onClick={() => load(namespace)}
            aria-label={i18next.t("general:Refresh")}
          >
            <RefreshCw className={cn("h-3.5 w-3.5", loading && "animate-spin")} />
          </Button>
        </div>
        <div className="space-y-2 border-b border-neutral-700 p-3">
          <label className="block text-xs text-neutral-400">{i18next.t("kubernetes:Namespace")}</label>
          <select
            className="h-8 w-full rounded-md border border-neutral-700 bg-neutral-900 px-2 text-sm outline-none"
            value={namespace}
            disabled={loading || !data}
            aria-label={i18next.t("kubernetes:Namespace")}
            onChange={(e) => {
              setExpanded({});
              load(e.target.value);
            }}
          >
            {(data?.namespaces ?? []).map((name) => <option key={name} value={name}>{name}</option>)}
          </select>
          <input
            className="h-8 w-full rounded-md border border-neutral-700 bg-neutral-900 px-2 text-sm outline-none placeholder:text-neutral-500"
            placeholder={i18next.t("kubernetes:Filter pods")}
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
        </div>
        <div className="min-h-0 flex-1 overflow-auto py-1">
          {error ? (
            <div className="break-words px-3 py-2 text-red-400">{error}</div>
          ) : !data ? (
            <div className="px-3 py-2 text-neutral-400">{i18next.t("general:Loading")}...</div>
          ) : pods.length === 0 ? (
            <div className="px-3 py-2 text-neutral-400">{i18next.t("kubernetes:No pods")}</div>
          ) : (
            pods.map((pod) => {
              const running = pod.phase === "Running";
              const single = pod.containers.length === 1;
              const isOpen = expanded[pod.name] ?? false;
              return (
                <div key={pod.name}>
                  <button
                    type="button"
                    className={cn(
                      "flex w-full items-center gap-1.5 px-2 py-1 text-left hover:bg-neutral-800",
                      target?.pod === pod.name && target.namespace === namespace && "bg-neutral-800",
                      single && !running && "opacity-60",
                    )}
                    title={running ? `${pod.name}\n${pod.node}` : `${pod.name}: ${pod.phase}\n${i18next.t("kubernetes:Only running pods can be opened")}`}
                    onClick={() => {
                      if (!single) {
                        setExpanded({...expanded, [pod.name]: !isOpen});
                      } else {
                        open(pod, pod.containers[0]);
                      }
                    }}
                  >
                    {single ? (
                      <SquareTerminal className="h-3.5 w-3.5 shrink-0 text-neutral-400" />
                    ) : isOpen ? (
                      <ChevronDown className="h-3.5 w-3.5 shrink-0 text-neutral-400" />
                    ) : (
                      <ChevronRight className="h-3.5 w-3.5 shrink-0 text-neutral-400" />
                    )}
                    <span className={cn("h-2 w-2 shrink-0 rounded-full", getPhaseColor(pod.phase))} />
                    <span className="min-w-0 flex-1 truncate font-mono text-xs">{pod.name}</span>
                    {!single ? <span className="shrink-0 text-xs text-neutral-500">{pod.containers.length}</span> : null}
                  </button>
                  {!single && isOpen
                    ? pod.containers.map((container) => (
                      <button
                        key={container}
                        type="button"
                        className={cn(
                          "flex w-full items-center gap-1.5 py-1 pl-8 pr-2 text-left hover:bg-neutral-800",
                          !running && "opacity-60",
                          target?.pod === pod.name && target.container === container && target.namespace === namespace && "bg-neutral-800",
                        )}
                        title={running ? container : i18next.t("kubernetes:Only running pods can be opened")}
                        onClick={() => open(pod, container)}
                      >
                        <SquareTerminal className="h-3.5 w-3.5 shrink-0 text-neutral-400" />
                        <span className="min-w-0 flex-1 truncate font-mono text-xs">{container}</span>
                      </button>
                    ))
                    : null}
                </div>
              );
            })
          )}
        </div>
      </div>

      <div className="flex min-w-0 flex-1 flex-col">
        {target && containerTarget ? (
          <>
            <div className="flex h-10 shrink-0 items-center gap-2 border-b border-neutral-700 px-3 text-sm">
              <SquareTerminal className="h-4 w-4 shrink-0 text-neutral-400" />
              <span className="truncate font-mono text-xs">{`${target.namespace}/${target.pod}/${target.container}`}</span>
              <div className="ml-3 flex items-center gap-0.5">
                {(["terminal", "logs", "files"] as const).map((name) => (
                  <button
                    key={name}
                    type="button"
                    disabled={name !== "logs" && (!targetRunning || targetReadOnly)}
                    className={cn(
                      "rounded px-2 py-1 text-xs text-neutral-400 hover:bg-neutral-800 hover:text-neutral-100 disabled:cursor-not-allowed disabled:opacity-50",
                      view === name && "bg-neutral-800 text-neutral-100",
                    )}
                    title={name === "logs" ? undefined : targetReadOnly ? i18next.t("kubernetes:Your permission only reads this namespace") : !targetRunning ? i18next.t("kubernetes:Only running pods can be opened") : undefined}
                    onClick={() => selectView(name)}
                  >
                    {name === "terminal" ? i18next.t("kubernetes:Terminal") : name === "logs" ? i18next.t("kubernetes:Logs") : i18next.t("kubernetes:Files")}
                  </button>
                ))}
              </div>
              <Button
                variant="ghost"
                size="icon"
                className="ml-auto h-7 w-7 text-neutral-300 hover:bg-neutral-800 hover:text-neutral-100"
                onClick={() => setTarget(null)}
                aria-label={i18next.t("general:Close")}
              >
                <X className="h-4 w-4" />
              </Button>
            </div>
            <div className={cn("min-h-0 flex-1", view !== "terminal" && "hidden")}>
              {terminalOpened ? <SshView key={shell} {...props} addClient={undefined} protocol="Kubernetes" kubernetes={target} onClose={() => setTarget(null)} /> : null}
            </div>
            {view === "logs" ? (
              <div className="min-h-0 flex-1">
                <KubernetesLogs key={shell} target={containerTarget} password={password ?? ""} />
              </div>
            ) : null}
            {view === "files" ? (
              <div className="min-h-0 flex-1">
                <KubernetesFiles key={shell} target={containerTarget} password={password ?? ""} />
              </div>
            ) : null}
          </>
        ) : (
          <div className="flex flex-1 items-center justify-center p-6 text-center text-sm text-neutral-400">
            {i18next.t("kubernetes:Select a container to open a shell in it")}
          </div>
        )}
      </div>
      {showKubeconfig ? <KubeconfigDialog assetId={assetId} account={account} onClose={() => setShowKubeconfig(false)} /> : null}
    </div>
  );
}
