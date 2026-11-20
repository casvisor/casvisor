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
import dayjs from "dayjs";
import {ChevronDown, ChevronRight, Folder, FolderOpen, FolderPlus, Inbox, Layers, MoreHorizontal} from "lucide-react";
import * as AssetNodeBackend from "@/backend/AssetNodeBackend";
import type {AssetNode} from "@/backend/types";
import {SelectField} from "@/components/common/SelectField";
import {Button} from "@/components/ui/button";
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle} from "@/components/ui/dialog";
import {DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger} from "@/components/ui/dropdown-menu";
import {Input} from "@/components/ui/input";
import {NoNode, getChildNodes, getDescendantNames, getNodePath, getNodeTitle} from "@/lib/assetNode";
import * as Setting from "@/lib/setting";
import {cn} from "@/lib/utils";

interface AssetTreeProps {
  owner: string;
  isAdmin: boolean;
  nodes: AssetNode[];
  total: number;
  unassigned: number;
  /** "" for all assets, NoNode for the assets in no node, or a node name */
  selected: string;
  onSelect: (node: string) => void;
  onChanged: () => void;
}

type NodeAction = {kind: "add"; parent: string} | {kind: "rename" | "move" | "delete"; node: AssetNode};

const expandedKey = "casvisor-asset-tree-collapsed";

// a Radix select item can not have "" as its value
const topLevel = "@top";

function loadCollapsed(): Set<string> {
  try {
    return new Set(JSON.parse(localStorage.getItem(expandedKey) ?? "[]"));
  } catch {
    return new Set();
  }
}

function TreeRow({
  depth,
  icon,
  title,
  count,
  active,
  onClick,
  toggle,
  menu,
}: {
  depth: number;
  icon: React.ReactNode;
  title: React.ReactNode;
  count?: number;
  active: boolean;
  onClick: () => void;
  toggle?: React.ReactNode;
  menu?: React.ReactNode;
}) {
  return (
    <div
      className={cn(
        "group flex h-8 cursor-pointer items-center gap-1 rounded-md pr-1 text-sm hover:bg-accent",
        active && "bg-accent font-medium text-accent-foreground",
      )}
      style={{paddingLeft: depth * 14 + 4}}
      onClick={onClick}
    >
      <span className="flex h-5 w-5 shrink-0 items-center justify-center">{toggle}</span>
      <span className="shrink-0 text-muted-foreground [&_svg]:h-4 [&_svg]:w-4">{icon}</span>
      <span className="min-w-0 flex-1 truncate">{title}</span>
      {count !== undefined ? <span className="shrink-0 text-xs tabular-nums text-muted-foreground">{count}</span> : null}
      {menu ? <span className="shrink-0 opacity-0 group-hover:opacity-100 has-[[data-state=open]]:opacity-100" onClick={(e) => e.stopPropagation()}>{menu}</span> : null}
    </div>
  );
}

/** The folders of the assets, with how many assets each has; an admin can add, rename, move and delete them. */
export function AssetTree({owner, isAdmin, nodes, total, unassigned, selected, onSelect, onChanged}: AssetTreeProps) {
  const [collapsed, setCollapsed] = React.useState<Set<string>>(loadCollapsed);
  const [action, setAction] = React.useState<NodeAction | null>(null);

  const toggle = (name: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(name)) {
        next.delete(name);
      } else {
        next.add(name);
      }
      try {
        localStorage.setItem(expandedKey, JSON.stringify([...next]));
      } catch {
        // the tree just opens fully next time
      }
      return next;
    });
  };

  const renderNodes = (parent: string, depth: number): React.ReactNode =>
    getChildNodes(nodes, parent).map((node) => {
      const children = getChildNodes(nodes, node.name);
      const isCollapsed = collapsed.has(node.name);
      return (
        <React.Fragment key={node.name}>
          <TreeRow
            depth={depth}
            icon={selected === node.name ? <FolderOpen /> : <Folder />}
            title={<span title={node.description || node.name}>{getNodeTitle(node)}</span>}
            count={node.assetCount}
            active={selected === node.name}
            onClick={() => onSelect(node.name)}
            toggle={children.length > 0 ? (
              <button
                type="button"
                className="flex h-5 w-5 items-center justify-center rounded hover:bg-muted"
                aria-label={isCollapsed ? "Expand" : "Collapse"}
                onClick={(e) => {
                  e.stopPropagation();
                  toggle(node.name);
                }}
              >
                {isCollapsed ? <ChevronRight className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
              </button>
            ) : null}
            menu={isAdmin ? (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" size="iconSm" className="h-6 w-6" aria-label={i18next.t("general:Action")}>
                    <MoreHorizontal className="h-4 w-4" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem onSelect={() => setAction({kind: "add", parent: node.name})}>{i18next.t("assetNode:Add child node")}</DropdownMenuItem>
                  <DropdownMenuItem onSelect={() => setAction({kind: "rename", node})}>{i18next.t("assetNode:Rename")}</DropdownMenuItem>
                  <DropdownMenuItem onSelect={() => setAction({kind: "move", node})}>{i18next.t("assetNode:Move to")}</DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem className="text-destructive focus:text-destructive" onSelect={() => setAction({kind: "delete", node})}>
                    {i18next.t("general:Delete")}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            ) : null}
          />
          {isCollapsed ? null : renderNodes(node.name, depth + 1)}
        </React.Fragment>
      );
    });

  return (
    <div className="w-full shrink-0 rounded-lg border bg-card p-2 lg:sticky lg:top-4 lg:w-64">
      <div className="mb-1 flex items-center justify-between px-1">
        <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{i18next.t("assetNode:Asset tree")}</span>
        {isAdmin ? (
          <Button variant="ghost" size="iconSm" className="h-7 w-7" aria-label={i18next.t("assetNode:Add node")} title={i18next.t("assetNode:Add node")} onClick={() => setAction({kind: "add", parent: ""})}>
            <FolderPlus className="h-4 w-4" />
          </Button>
        ) : null}
      </div>
      <div className="max-h-[70vh] overflow-y-auto">
        <TreeRow depth={0} icon={<Layers />} title={i18next.t("assetNode:All assets")} count={total} active={selected === ""} onClick={() => onSelect("")} />
        {renderNodes("", 0)}
        {unassigned > 0 || selected === NoNode ? (
          <TreeRow depth={0} icon={<Inbox />} title={i18next.t("assetNode:Not in any node")} count={unassigned} active={selected === NoNode} onClick={() => onSelect(NoNode)} />
        ) : null}
        {isAdmin && nodes.length === 0 ? (
          <p className="px-2 py-2 text-xs text-muted-foreground">{i18next.t("assetNode:Group the assets into nodes, such as data centers, environments or teams, and grant access to a whole node")}</p>
        ) : null}
      </div>
      {action ? (
        <NodeActionDialog
          owner={owner}
          nodes={nodes}
          action={action}
          onClose={(changed, name) => {
            setAction(null);
            if (changed) {
              onChanged();
              if (action.kind === "delete" && selected === action.node.name) {
                onSelect("");
              } else if (action.kind === "add" && name) {
                onSelect(name);
              }
            }
          }}
        />
      ) : null}
    </div>
  );
}

function NodeActionDialog({owner, nodes, action, onClose}: {owner: string; nodes: AssetNode[]; action: NodeAction; onClose: (changed: boolean, name?: string) => void}) {
  const node = action.kind === "add" ? null : action.node;
  const [title, setTitle] = React.useState(node ? getNodeTitle(node) : "");
  const [parent, setParent] = React.useState(node?.parent || topLevel);
  const [saving, setSaving] = React.useState(false);

  const parentOptions = React.useMemo(() => {
    if (!node) {
      return [];
    }
    const excluded = getDescendantNames(nodes, node.name);
    return [
      {id: topLevel, name: i18next.t("assetNode:(Top level)")},
      ...nodes.filter((n) => !excluded.has(n.name)).map((n) => ({id: n.name, name: getNodePath(nodes, n.name)})).sort((a, b) => a.name.localeCompare(b.name)),
    ];
  }, [nodes, node]);

  const submit = async() => {
    setSaving(true);
    try {
      let res: any;
      let name: string | undefined;
      if (action.kind === "add") {
        name = `node_${Setting.getRandomName()}`;
        res = await AssetNodeBackend.addAssetNode({
          owner,
          name,
          createdTime: dayjs().format(),
          updatedTime: dayjs().format(),
          displayName: title.trim(),
          description: "",
          parent: action.parent,
        });
      } else if (action.kind === "delete") {
        res = await AssetNodeBackend.deleteAssetNode(action.node);
      } else {
        res = await AssetNodeBackend.updateAssetNode(owner, action.node.name, {
          ...action.node,
          displayName: action.kind === "rename" ? title.trim() : action.node.displayName,
          parent: action.kind === "move" ? (parent === topLevel ? "" : parent) : action.node.parent,
        });
      }
      if (res.status === "ok") {
        onClose(true, name);
      } else {
        Setting.showMessage("error", res.msg);
      }
    } finally {
      setSaving(false);
    }
  };

  const dialogTitle = {
    add: i18next.t(action.kind === "add" && action.parent ? "assetNode:Add child node" : "assetNode:Add node"),
    rename: i18next.t("assetNode:Rename"),
    move: i18next.t("assetNode:Move to"),
    delete: i18next.t("general:Sure to delete") + "?",
  }[action.kind];

  return (
    <Dialog open onOpenChange={(open) => !open && onClose(false)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{dialogTitle}</DialogTitle>
          <DialogDescription>
            {action.kind === "add"
              ? (action.parent ? getNodePath(nodes, action.parent) : i18next.t("assetNode:(Top level)"))
              : getNodePath(nodes, action.node.name)}
          </DialogDescription>
        </DialogHeader>
        {action.kind === "add" || action.kind === "rename" ? (
          <Input
            autoFocus
            value={title}
            placeholder={i18next.t("assetNode:Such as Beijing, Production or Databases")}
            onChange={(e) => setTitle(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && title.trim() && !title.includes("/")) {
                submit();
              }
            }}
          />
        ) : null}
        {action.kind === "move" ? <SelectField value={parent} options={parentOptions} onChange={setParent} /> : null}
        {action.kind === "delete" ? (
          <p className="text-sm text-muted-foreground">{i18next.t("assetNode:Only a node without child nodes, assets and permissions can be deleted")}</p>
        ) : null}
        {(action.kind === "add" || action.kind === "rename") && title.includes("/") ? (
          <p className="text-sm text-destructive">{i18next.t("assetNode:A node name can not contain \"/\"")}</p>
        ) : null}
        <DialogFooter>
          <Button variant="ghost" onClick={() => onClose(false)}>{i18next.t("general:Cancel")}</Button>
          <Button
            variant={action.kind === "delete" ? "destructive" : "default"}
            loading={saving}
            disabled={(action.kind === "add" || action.kind === "rename") && (!title.trim() || title.includes("/"))}
            onClick={submit}
          >
            {action.kind === "delete" ? i18next.t("general:Delete") : i18next.t("general:Save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
