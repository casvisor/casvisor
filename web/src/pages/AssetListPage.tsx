// Copyright 2023 The casbin Authors. All Rights Reserved.
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
import {FileUp, FolderInput} from "lucide-react";
import {Link, useSearchParams} from "react-router-dom";
import * as AssetBackend from "@/backend/AssetBackend";
import * as AssetNodeBackend from "@/backend/AssetNodeBackend";
import type {Asset, AssetNode} from "@/backend/types";
import {ConnectButton} from "@/components/access/ConnectButton";
import {AssetImportDialog} from "@/components/asset/AssetImportDialog";
import {AssetTree} from "@/components/asset/AssetTree";
import {MoveAssetsDialog} from "@/components/asset/MoveAssetsDialog";
import {Button} from "@/components/ui/button";
import {LocalClientButton} from "@/components/access/LocalClientButton";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {ExportButton} from "@/components/crud/ExportButton";
import {dateColumn, linkColumn, textColumn, valueFilters} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {useAccount} from "@/hooks/use-account";
import {Protocols} from "@/lib/asset";
import {NoNode, getNodePath} from "@/lib/assetNode";
import {useGateways} from "@/lib/gateway";
import * as Setting from "@/lib/setting";

export default function AssetListPage() {
  const {account} = useAccount();
  const isAdmin = Setting.isAdminUser(account);
  const owner = Setting.getRequestOrganization(account);
  const gateway = useGateways();
  const hasLocalClients = gateway !== null && (gateway.enabled || gateway.rdGateway === true);
  const [searchParams, setSearchParams] = useSearchParams();
  const node = searchParams.get("node") ?? "";
  const [nodes, setNodes] = React.useState<AssetNode[]>([]);
  const [counts, setCounts] = React.useState({total: 0, unassigned: 0});
  const [importing, setImporting] = React.useState(false);
  const [moving, setMoving] = React.useState<{assets: Asset[]; done: () => void} | null>(null);

  const loadNodes = React.useCallback(() => {
    if (!owner) {
      return;
    }
    AssetNodeBackend.getAssetNodes(owner).then((res) => {
      if (res.status === "ok") {
        setNodes(res.data ?? []);
        setCounts(res.data2 ?? {total: 0, unassigned: 0});
      }
    });
  }, [owner]);

  React.useEffect(loadNodes, [loadNodes]);

  const selectNode = (name: string) => {
    const next = new URLSearchParams(searchParams);
    if (name) {
      next.set("node", name);
    } else {
      next.delete("node");
    }
    setSearchParams(next, {replace: true});
  };

  const fetchAssets = React.useCallback(
    (query: TableQuery) =>
      AssetBackend.getAssets(owner, {
        page: query.page,
        pageSize: query.pageSize,
        field: query.searchedColumn,
        value: query.searchText,
        sortField: query.sortField,
        sortOrder: query.sortOrder,
      }, node),
    [owner, node],
  );

  const deleteAsset = React.useCallback(
    (asset: Asset) => AssetBackend.deleteAsset(asset).then((res: any) => {
      loadNodes();
      return res;
    }),
    [loadNodes],
  );

  const showTree = isAdmin || nodes.length > 0;

  const newAsset = (): Asset => ({
    owner: account!.owner,
    name: `asset_${Setting.getRandomName()}`,
    createdTime: dayjs().format(),
    updatedTime: dayjs().format(),
    displayName: `New Asset - ${Setting.getRandomName()}`,
    description: "",
    tag: "",
    os: "Linux",
    platform: "linux",
    protocol: "SSH",
    host: "127.0.0.1",
    port: 22,
    username: "root",
    password: "",
    database: "",
    privateKey: "",
    fingerprint: "",
    networkGateway: "",
    nodes: node && node !== NoNode ? [node] : [],
  });

  const columns: ColumnDef<Asset>[] = [
    textColumn<Asset>({
      dataIndex: "owner",
      title: i18next.t("general:Organization"),
      width: 120,
      searchable: true,
      link: (value) => Setting.getCasdoorUrl(account, `/organizations/${value}`),
      linkExternal: true,
    }),
    isAdmin
      ? linkColumn<Asset>({dataIndex: "name", width: 160, to: (asset) => `/assets/${asset.owner}/${asset.name}`})
      : textColumn<Asset>({dataIndex: "name", title: i18next.t("general:Name"), width: 160, searchable: true}),
    dateColumn<Asset>("createdTime"),
    textColumn<Asset>({dataIndex: "description", title: i18next.t("general:Description"), width: 200}),
    textColumn<Asset>({dataIndex: "protocol", title: i18next.t("general:Protocol"), width: 110, filters: valueFilters(Protocols)}),
    textColumn<Asset>({dataIndex: "platform", title: i18next.t("platform:Platform"), width: 120, searchable: true}),
    textColumn<Asset>({dataIndex: "host", title: i18next.t("asset:Host"), width: 140, searchable: true}),
    textColumn<Asset>({dataIndex: "port", title: i18next.t("general:Port"), width: 90}),
    {
      dataIndex: "username",
      title: i18next.t("general:Username"),
      width: 150,
      render: (value, asset) => {
        const usernames = (asset.accounts ?? []).map((info) => info.username).filter(Boolean);
        return <span className="font-mono text-xs">{usernames.length > 0 ? usernames.join(", ") : value}</span>;
      },
    },
    {
      dataIndex: "nodes",
      title: i18next.t("assetNode:Nodes"),
      width: 180,
      render: (_, asset) => (
        <div className="flex flex-col gap-0.5 text-xs">
          {(asset.nodes ?? []).map((name) => (
            <button key={name} type="button" className="truncate text-left text-muted-foreground hover:text-foreground hover:underline" onClick={() => selectNode(name)}>
              {getNodePath(nodes, name)}
            </button>
          ))}
        </div>
      ),
    },
  ];

  return (
    <>
      <CrudListPage<Asset>
        title={i18next.t("general:Assets")}
        description={i18next.t("general:Machines can be connected via RDP, VNC or SSH")}
        columns={columns}
        fetch={fetchAssets}
        deps={[owner, node]}
        newRecord={isAdmin ? newAsset : undefined}
        add={AssetBackend.addAsset}
        remove={isAdmin ? deleteAsset : undefined}
        editUrl={isAdmin ? (asset) => `/assets/${asset.owner}/${asset.name}` : undefined}
        deleteDisabled={(asset) => asset.owner !== account?.owner}
        toolbar={isAdmin ? ({refresh, query}) => (
          <>
            <Button variant="outline" onClick={() => setImporting(true)}>
              <FileUp />
              {i18next.t("assetImport:Import")}
            </Button>
            <ExportButton path="/api/export-assets" owner={owner} query={query} params={{node}} />
            <AssetImportDialog
              owner={owner}
              open={importing}
              onOpenChange={setImporting}
              onImported={() => {
                refresh();
                loadNodes();
              }}
            />
          </>
        ) : (
          <Button size="sm" variant="outline" asChild>
            <Link to="/access-requests?new=1">{i18next.t("accessRequest:Request access")}</Link>
          </Button>
        )}
        aside={showTree ? (
          <AssetTree
            owner={owner}
            isAdmin={isAdmin}
            nodes={nodes}
            total={counts.total}
            unassigned={counts.unassigned}
            selected={node}
            onSelect={selectNode}
            onChanged={loadNodes}
          />
        ) : undefined}
        selectionActions={isAdmin ? (records, {refresh, clearSelection}) => (
          <Button
            size="sm"
            variant="outline"
            onClick={() => setMoving({
              assets: records,
              done: () => {
                clearSelection();
                refresh();
                loadNodes();
              },
            })}
          >
            <FolderInput />
            {i18next.t("assetNode:Set nodes")}
          </Button>
        ) : undefined}
        actionColumnWidth={(isAdmin ? 240 : 110) + (hasLocalClients ? 40 : 0)}
        rowLeading={(asset) => (
          <>
            <ConnectButton asset={asset} disabled={asset.owner !== account?.owner} />
            {gateway && account && asset.owner === account.owner ? <LocalClientButton asset={asset} gateway={gateway} account={account} /> : null}
          </>
        )}
      />
      {moving ? (
        <MoveAssetsDialog
          owner={owner}
          assets={moving.assets}
          nodes={nodes}
          defaultNode={node && node !== NoNode ? node : ""}
          onClose={(changed) => {
            if (changed) {
              moving.done();
            }
            setMoving(null);
          }}
        />
      ) : null}
    </>
  );
}
