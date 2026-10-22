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
import * as AssetBackend from "@/backend/AssetBackend";
import type {Asset} from "@/backend/types";
import {ConnectButton} from "@/components/access/ConnectButton";
import {SshCommandButton} from "@/components/access/SshCommandButton";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {dateColumn, linkColumn, textColumn, valueFilters} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {useAccount} from "@/hooks/use-account";
import {Protocols} from "@/lib/asset";
import {useSshGateway} from "@/lib/gateway";
import * as Setting from "@/lib/setting";

export default function AssetListPage() {
  const {account} = useAccount();
  const isAdmin = Setting.isAdminUser(account);
  const owner = Setting.getRequestOrganization(account);
  const gateway = useSshGateway();

  const fetchAssets = React.useCallback(
    (query: TableQuery) =>
      AssetBackend.getAssets(owner, {
        page: query.page,
        pageSize: query.pageSize,
        field: query.searchedColumn,
        value: query.searchText,
        sortField: query.sortField,
        sortOrder: query.sortOrder,
      }),
    [owner],
  );

  const newAsset = (): Asset => ({
    owner: account!.owner,
    name: `asset_${Setting.getRandomName()}`,
    createdTime: dayjs().format(),
    updatedTime: dayjs().format(),
    displayName: `New Asset - ${Setting.getRandomName()}`,
    description: "",
    tag: "",
    os: "Linux",
    protocol: "SSH",
    host: "127.0.0.1",
    port: 22,
    username: "root",
    password: "",
    privateKey: "",
    fingerprint: "",
    networkGateway: "",
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
    textColumn<Asset>({dataIndex: "host", title: i18next.t("asset:Host"), width: 140, searchable: true}),
    textColumn<Asset>({dataIndex: "port", title: i18next.t("general:Port"), width: 90}),
    textColumn<Asset>({dataIndex: "username", title: i18next.t("general:Username"), width: 130}),
  ];

  return (
    <CrudListPage<Asset>
      title={i18next.t("general:Assets")}
      description={i18next.t("general:Machines can be connected via RDP, VNC or SSH")}
      columns={columns}
      fetch={fetchAssets}
      deps={[owner]}
      newRecord={isAdmin ? newAsset : undefined}
      add={AssetBackend.addAsset}
      remove={isAdmin ? AssetBackend.deleteAsset : undefined}
      editUrl={isAdmin ? (asset) => `/assets/${asset.owner}/${asset.name}` : undefined}
      deleteDisabled={(asset) => asset.owner !== account?.owner}
      actionColumnWidth={(isAdmin ? 240 : 110) + (gateway ? 40 : 0)}
      rowLeading={(asset) => (
        <>
          <ConnectButton asset={asset} disabled={asset.owner !== account?.owner} />
          {gateway && asset.protocol === "SSH" ? <SshCommandButton asset={asset} gateway={gateway} account={account} /> : null}
        </>
      )}
    />
  );
}
