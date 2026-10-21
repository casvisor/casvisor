// Copyright 2024 The casbin Authors. All Rights Reserved.
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
import {ChevronRight, Monitor, Search, X} from "lucide-react";
import * as AssetBackend from "@/backend/AssetBackend";
import type {Asset} from "@/backend/types";
import {Input} from "@/components/ui/input";
import {Loading} from "@/components/common/Loading";
import {Logo} from "@/components/common/Logo";
import {AssetConnection} from "@/components/access/AssetConnection";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";
import {cn} from "@/lib/utils";

interface Pane {
  key: string;
  asset: Asset;
}

/** Assets grouped by their tag, else their OS, so a "windows" tag and the "Windows" OS share a group. */
function groupAssets(assets: Asset[]): [string, Asset[]][] {
  const groups = new Map<string, {label: string; items: Asset[]}>();
  for (const asset of assets) {
    const label = asset.tag || asset.os || "default";
    const group = groups.get(label.toLowerCase()) ?? {label, items: []};
    group.items.push(asset);
    groups.set(label.toLowerCase(), group);
  }
  return [...groups.values()].map((group) => [group.label, group.items]);
}

function AssetTree({assets, activeId, onSelect}: {assets: Asset[]; activeId: string; onSelect: (asset: Asset) => void}) {
  const [search, setSearch] = React.useState("");
  const [collapsed, setCollapsed] = React.useState<Set<string>>(new Set());

  const keyword = search.trim().toLowerCase();
  const groups = groupAssets(
    keyword ? assets.filter((asset) => `${asset.name} ${asset.displayName} ${asset.host}`.toLowerCase().includes(keyword)) : assets,
  );

  const toggle = (group: string) => {
    setCollapsed((previous) => {
      const next = new Set(previous);
      if (next.has(group)) {
        next.delete(group);
      } else {
        next.add(group);
      }
      return next;
    });
  };

  return (
    <div className="flex h-full flex-col">
      <div className="relative p-2">
        <Search className="absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          className="h-8 pl-8"
          placeholder={i18next.t("general:Search")}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-2 scrollbar-thin">
        {groups.length === 0 ? (
          <p className="px-2 py-4 text-center text-sm text-muted-foreground">{i18next.t("general:No data")}</p>
        ) : null}
        {groups.map(([group, items]) => {
          const open = keyword !== "" || !collapsed.has(group);
          return (
            <div key={group} className="mb-1">
              <button
                type="button"
                className="flex w-full items-center gap-1 rounded px-1.5 py-1 text-left text-xs font-medium uppercase tracking-wide text-muted-foreground hover:bg-accent"
                onClick={() => toggle(group)}
              >
                <ChevronRight className={cn("h-3.5 w-3.5 transition-transform", open && "rotate-90")} />
                {group}
                <span className="ml-auto tabular-nums">{items.length}</span>
              </button>
              {open
                ? items.map((asset) => {
                  const id = `${asset.owner}/${asset.name}`;
                  return (
                    <button
                      key={id}
                      type="button"
                      title={`${asset.protocol} ${asset.host}:${asset.port}`}
                      className={cn(
                        "flex w-full items-center gap-2 rounded py-1 pl-6 pr-2 text-left text-sm hover:bg-accent",
                        activeId === id && "bg-accent font-medium",
                      )}
                      onClick={() => onSelect(asset)}
                    >
                      <span className="truncate">{asset.displayName || asset.name}</span>
                      <span className="ml-auto shrink-0 text-[10px] text-muted-foreground">{asset.protocol}</span>
                    </button>
                  );
                })
                : null}
            </div>
          );
        })}
      </div>
    </div>
  );
}

/** Every asset on the left, its open connections as tabs on the right. */
export default function WorkbenchPage() {
  const {account} = useAccount();
  const [assets, setAssets] = React.useState<Asset[] | null>(null);
  const [panes, setPanes] = React.useState<Pane[]>([]);
  const [activeKey, setActiveKey] = React.useState("");
  const clients = React.useRef(new Map<string, {disconnect: () => void}>());

  React.useEffect(() => {
    document.title = i18next.t("general:Workbench");
    AssetBackend.getAssets(account!.owner).then((res) => {
      if (res.status === "ok") {
        setAssets(res.data ?? []);
      } else {
        setAssets([]);
        Setting.showMessage("error", `${i18next.t("asset:Failed to get assets")}: ${res.msg}`);
      }
    });
  }, [account]);

  const openAsset = (asset: Asset) => {
    const existing = panes.find((pane) => pane.asset.owner === asset.owner && pane.asset.name === asset.name);
    if (existing) {
      setActiveKey(existing.key);
      return;
    }
    const key = `asset-${Date.now()}`;
    setPanes((previous) => [...previous, {key, asset}]);
    setActiveKey(key);
  };

  const closePane = (key: string) => {
    clients.current.get(key)?.disconnect();
    clients.current.delete(key);
    setPanes((previous) => {
      const index = previous.findIndex((pane) => pane.key === key);
      const next = previous.filter((pane) => pane.key !== key);
      if (key === activeKey && next.length > 0) {
        setActiveKey(next[Math.max(0, index - 1)].key);
      }
      return next;
    });
  };

  const activeAssetId = panes.find((pane) => pane.key === activeKey)?.asset;

  return (
    <div className="flex h-screen bg-muted/40">
      <aside className="flex w-60 shrink-0 flex-col border-r bg-background">
        <Link to="/" className="flex h-10 items-center border-b px-3">
          <Logo className="h-5 text-sm" />
        </Link>
        <div className="min-h-0 flex-1">
          {assets === null ? (
            <Loading />
          ) : (
            <AssetTree
              assets={assets}
              activeId={activeAssetId ? `${activeAssetId.owner}/${activeAssetId.name}` : ""}
              onSelect={openAsset}
            />
          )}
        </div>
      </aside>

      <main className="flex min-w-0 flex-1 flex-col">
        {panes.length === 0 ? (
          <div className="flex flex-1 flex-col items-center justify-center gap-2 text-muted-foreground">
            <Monitor className="h-10 w-10 opacity-50" />
            <p className="text-lg">{i18next.t("asset:Please select a asset")}</p>
          </div>
        ) : (
          <>
            <div className="flex h-10 shrink-0 items-end gap-1 overflow-x-auto border-b bg-background px-2 scrollbar-thin">
              {panes.map((pane) => (
                <div
                  key={pane.key}
                  className={cn(
                    "group flex h-8 items-center gap-1 rounded-t-md border border-b-0 px-3 text-sm",
                    pane.key === activeKey ? "bg-[#1b1b1b] text-white" : "bg-muted text-muted-foreground hover:text-foreground",
                  )}
                >
                  <button type="button" className="max-w-[160px] truncate" onClick={() => setActiveKey(pane.key)}>
                    {pane.asset.displayName || pane.asset.name}
                  </button>
                  <button
                    type="button"
                    aria-label={i18next.t("general:Close")}
                    className="rounded p-0.5 opacity-60 hover:opacity-100"
                    onClick={() => closePane(pane.key)}
                  >
                    <X className="h-3.5 w-3.5" />
                  </button>
                </div>
              ))}
            </div>
            {/* inactive connections stay mounted, so switching tabs does not drop them */}
            <div className="relative min-h-0 flex-1">
              {panes.map((pane) => (
                <div key={pane.key} className={cn("absolute inset-0", pane.key !== activeKey && "invisible")}>
                  <AssetConnection
                    assetId={`${pane.asset.owner}/${pane.asset.name}`}
                    inTab
                    addClient={(client) => clients.current.set(pane.key, client)}
                    onClose={() => closePane(pane.key)}
                  />
                </div>
              ))}
            </div>
          </>
        )}
      </main>
    </div>
  );
}
