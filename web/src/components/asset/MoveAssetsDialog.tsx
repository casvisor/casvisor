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
import * as AssetBackend from "@/backend/AssetBackend";
import type {Asset, AssetNode} from "@/backend/types";
import {MultiSelect} from "@/components/common/MultiSelect";
import {SelectField} from "@/components/common/SelectField";
import {Button} from "@/components/ui/button";
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle} from "@/components/ui/dialog";
import {getNodeOptions} from "@/lib/assetNode";
import * as Setting from "@/lib/setting";

type MoveMode = "add" | "set" | "remove";

/** Puts the selected assets in nodes, or takes them out. */
export function MoveAssetsDialog({owner, assets, nodes, defaultNode, onClose}: {owner: string; assets: Asset[]; nodes: AssetNode[]; defaultNode: string; onClose: (changed: boolean) => void}) {
  const [mode, setMode] = React.useState<MoveMode>("add");
  const [selected, setSelected] = React.useState<string[]>(defaultNode ? [defaultNode] : []);
  const [saving, setSaving] = React.useState(false);

  const submit = async() => {
    setSaving(true);
    try {
      const res = await AssetBackend.moveAssets(owner, assets.map((asset) => asset.name), selected, mode);
      if (res.status === "ok") {
        Setting.showMessage("success", i18next.t("assetNode:{{count}} assets changed", {count: res.data}));
        onClose(true);
      } else {
        Setting.showMessage("error", res.msg);
      }
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose(false)}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{i18next.t("assetNode:Set nodes")}</DialogTitle>
          <DialogDescription>
            {assets.slice(0, 5).map((asset) => asset.displayName || asset.name).join(", ")}
            {assets.length > 5 ? ` … (${assets.length})` : ""}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <SelectField
            value={mode}
            onChange={(value) => setMode(value as MoveMode)}
            options={[
              {id: "add", name: i18next.t("assetNode:Add to these nodes, keeping the current ones")},
              {id: "set", name: i18next.t("assetNode:Put only in these nodes")},
              {id: "remove", name: i18next.t("assetNode:Take out of these nodes")},
            ]}
          />
          <MultiSelect value={selected} options={getNodeOptions(nodes)} placeholder={i18next.t("assetNode:Nodes")} onChange={setSelected} />
          {mode === "set" && selected.length === 0 ? (
            <p className="text-xs text-muted-foreground">{i18next.t("assetNode:With no node chosen, the assets are taken out of all nodes")}</p>
          ) : null}
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onClose(false)}>{i18next.t("general:Cancel")}</Button>
          <Button loading={saving} disabled={mode !== "set" && selected.length === 0} onClick={submit}>{i18next.t("general:Save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
