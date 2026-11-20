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
import {ChevronRight, Monitor, Plus, Search, X} from "lucide-react";
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
  id: string;
  targetKey: string;
  asset: Asset;
  account: string;
  seq: number;
}

function getTargetKey(asset: Asset, account: string) {
  return `${asset.owner}/${asset.name}@${account}`;
}

/** The accounts to list under an asset, one entry with no name when there is only the default one. */
function getAccountChoices(asset: Asset): string[] {
  const accounts = asset.accounts ?? [];
  if (accounts.length <= 1) {
    return [accounts.length === 1 && !accounts[0].isDefault ? accounts[0].username : ""];
  }
  return accounts.map((account) => (account.isDefault ? "" : account.username));
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

function AssetTree({assets, activeId, openCounts, onSelect}: {
  assets: Asset[];
  activeId: string;
  openCounts: Map<string, number>;
  onSelect: (asset: Asset, account: string, forceNew?: boolean) => void;
}) {
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
                ? items.flatMap((asset) => {
                  const choices = getAccountChoices(asset);
                  return choices.map((accountName) => {
                    const id = getTargetKey(asset, accountName);
                    const username = accountName || asset.username;
                    const openCount = openCounts.get(id) ?? 0;
                    return (
                      <div
                        key={id}
                        className={cn(
                          "group flex w-full items-center rounded hover:bg-accent",
                          activeId === id && "bg-accent font-medium",
                        )}
                      >
                        <button
                          type="button"
                          title={`${asset.protocol} ${username ? `${username}@` : ""}${asset.host}:${asset.port}`}
                          className="flex min-w-0 flex-1 items-center gap-2 py-1 pl-6 pr-2 text-left text-sm"
                          onClick={() => onSelect(asset, accountName)}
                        >
                          <span className="truncate">{asset.displayName || asset.name}</span>
                          {choices.length > 1 ? <span className="truncate font-mono text-xs text-muted-foreground">{username}</span> : null}
                          {openCount > 1 ? <span className="shrink-0 rounded bg-muted px-1 font-mono text-[10px] text-muted-foreground">{openCount}</span> : null}
                          <span className="ml-auto shrink-0 text-[10px] text-muted-foreground">{asset.protocol}</span>
                        </button>
                        {openCount > 0 ? (
                          <button
                            type="button"
                            title={i18next.t("asset:Open in new tab")}
                            aria-label={i18next.t("asset:Open in new tab")}
                            className="mr-1 shrink-0 rounded p-0.5 text-muted-foreground opacity-70 hover:bg-background hover:text-foreground hover:opacity-100"
                            onClick={() => onSelect(asset, accountName, true)}
                          >
                            <Plus className="h-3.5 w-3.5" />
                          </button>
                        ) : null}
                      </div>
                    );
                  });
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
  const [tabs, setTabs] = React.useState<{panes: Pane[]; activeId: string}>({panes: [], activeId: ""});
  const nextTabId = React.useRef(0);
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

  const openAsset = React.useCallback((asset: Asset, accountName: string, forceNew = false) => {
    const targetKey = getTargetKey(asset, accountName);
    const id = `tab-${++nextTabId.current}`;
    setTabs((previous) => {
      if (!forceNew) {
        const active = previous.panes.find((pane) => pane.id === previous.activeId);
        if (active?.targetKey === targetKey) {
          return previous;
        }
        const existing = previous.panes.find((pane) => pane.targetKey === targetKey);
        if (existing) {
          return {...previous, activeId: existing.id};
        }
      }
      const seq = previous.panes
        .filter((pane) => pane.targetKey === targetKey)
        .reduce((max, pane) => Math.max(max, pane.seq), 0) + 1;
      return {
        panes: [...previous.panes, {id, targetKey, asset, account: accountName, seq}],
        activeId: id,
      };
    });
  }, []);

  const closePane = React.useCallback((id: string) => {
    clients.current.get(id)?.disconnect();
    clients.current.delete(id);
    setTabs((previous) => {
      const index = previous.panes.findIndex((pane) => pane.id === id);
      if (index < 0) {
        return previous;
      }
      const nextPanes = previous.panes.filter((pane) => pane.id !== id);
      const nextActiveId = previous.activeId !== id
        ? previous.activeId
        : nextPanes.length > 0
          ? nextPanes[Math.max(0, index - 1)].id
          : "";
      return {panes: nextPanes, activeId: nextActiveId};
    });
  }, []);

  const {panes, activeId} = tabs;
  const activePane = panes.find((pane) => pane.id === activeId);
  const openCounts = React.useMemo(() => {
    const counts = new Map<string, number>();
    for (const pane of panes) {
      counts.set(pane.targetKey, (counts.get(pane.targetKey) ?? 0) + 1);
    }
    return counts;
  }, [panes]);

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
              activeId={activePane ? activePane.targetKey : ""}
              openCounts={openCounts}
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
                  key={pane.id}
                  className={cn(
                    "group flex h-8 items-center gap-1 rounded-t-md border border-b-0 px-3 text-sm",
                    pane.id === activeId ? "bg-[#1b1b1b] text-white" : "bg-muted text-muted-foreground hover:text-foreground",
                  )}
                >
                  <button
                    type="button"
                    className="flex items-center gap-1"
                    onClick={() => setTabs((prev) => ({...prev, activeId: pane.id}))}
                  >
                    <span className="max-w-[160px] truncate">{pane.asset.displayName || pane.asset.name}</span>
                    {pane.account ? <span className="shrink-0 font-mono text-xs opacity-70">{pane.account}</span> : null}
                    {(openCounts.get(pane.targetKey) ?? 0) > 1 ? <span className="shrink-0 font-mono text-xs opacity-70">#{pane.seq}</span> : null}
                  </button>
                  <button
                    type="button"
                    aria-label={i18next.t("general:Close")}
                    className="rounded p-0.5 opacity-60 hover:opacity-100"
                    onClick={() => closePane(pane.id)}
                  >
                    <X className="h-3.5 w-3.5" />
                  </button>
                </div>
              ))}
              {activePane ? (
                <button
                  type="button"
                  title={i18next.t("asset:Open in new tab")}
                  aria-label={i18next.t("asset:Open in new tab")}
                  className="mb-1 flex h-7 w-7 shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-foreground"
                  onClick={() => openAsset(activePane.asset, activePane.account, true)}
                >
                  <Plus className="h-4 w-4" />
                </button>
              ) : null}
            </div>
            {/* inactive connections stay mounted, so switching tabs does not drop them */}
            <div className="relative min-h-0 flex-1">
              {panes.map((pane) => (
                <div key={pane.id} className={cn("absolute inset-0", pane.id !== activeId && "invisible")}>
                  <AssetConnection
                    assetId={`${pane.asset.owner}/${pane.asset.name}`}
                    account={pane.account}
                    inTab
                    addClient={(client) => clients.current.set(pane.id, client)}
                    onClose={() => closePane(pane.id)}
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
