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
import * as CloudAccountBackend from "@/backend/CloudAccountBackend";
import type {CloudAccount} from "@/backend/types";
import {CloudSyncDialog} from "@/components/asset/CloudSyncDialog";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {boolColumn, linkColumn, textColumn} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {Badge} from "@/components/ui/badge";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";

export default function CloudAccountListPage() {
  const {account} = useAccount();
  const owner = Setting.getRequestOrganization(account);
  const [syncing, setSyncing] = React.useState<{account: CloudAccount; refresh: () => void} | null>(null);

  const fetchAccounts = React.useCallback(
    (query: TableQuery) =>
      CloudAccountBackend.getCloudAccounts(owner, {
        page: query.page,
        pageSize: query.pageSize,
        field: query.searchedColumn,
        value: query.searchText,
        sortField: query.sortField,
        sortOrder: query.sortOrder,
      }),
    [owner],
  );

  const newAccount = (): CloudAccount => ({
    owner: account!.owner,
    name: `cloud_${Setting.getRandomName()}`,
    createdTime: dayjs().format(),
    updatedTime: dayjs().format(),
    displayName: `New Cloud Account - ${Setting.getRandomName()}`,
    description: "",
    provider: "Aliyun",
    accessKey: "",
    accessSecret: "",
    tenantId: "",
    subscriptionId: "",
    projectId: "",
    regions: [],
    addressType: "Public",
    linuxUsername: "root",
    windowsUsername: "Administrator",
    nodePath: "",
    tag: "",
    networkGateway: "",
    syncInterval: 0,
    deleteMissing: false,
    isEnabled: true,
    lastSyncTime: "",
    lastSyncStatus: "",
    lastSyncMessage: "",
  });

  const columns: ColumnDef<CloudAccount>[] = [
    linkColumn<CloudAccount>({dataIndex: "name", width: 160, to: (item) => `/cloud-accounts/${item.owner}/${item.name}`}),
    textColumn<CloudAccount>({dataIndex: "displayName", title: i18next.t("general:Display name"), width: 170, searchable: true}),
    {
      dataIndex: "provider",
      title: i18next.t("cloudAccount:Provider"),
      width: 120,
      render: (value) => i18next.t(`cloudAccount:${value}`),
    },
    {
      dataIndex: "regions",
      title: i18next.t("cloudAccount:Regions"),
      width: 180,
      render: (value: string[] | null) => (value && value.length > 0 ? value.join(", ") : <span className="text-muted-foreground">{i18next.t("cloudAccount:All regions")}</span>),
    },
    {
      dataIndex: "syncInterval",
      title: i18next.t("cloudAccount:Sync interval"),
      width: 120,
      render: (value: number) => (value > 0 ? i18next.t("cloudAccount:Every {{n}} min", {n: value}) : i18next.t("cloudAccount:By hand")),
    },
    {
      dataIndex: "lastSyncTime",
      title: i18next.t("cloudAccount:Last sync"),
      width: 260,
      render: (value: string, item) => (value ? (
        <div className="text-xs">
          <div className="flex items-center gap-1.5">
            <Badge variant={item.lastSyncStatus === "Succeeded" ? "success" : "destructive"}>{i18next.t(`cloudAccount:${item.lastSyncStatus}`)}</Badge>
            <span className="text-muted-foreground">{dayjs(value).format("YYYY-MM-DD HH:mm")}</span>
          </div>
          <div className="mt-0.5 line-clamp-2 break-all text-muted-foreground" title={item.lastSyncMessage}>{item.lastSyncMessage}</div>
        </div>
      ) : <span className="text-xs text-muted-foreground">{i18next.t("cloudAccount:Never")}</span>),
    },
    boolColumn<CloudAccount>({dataIndex: "isEnabled", title: i18next.t("permission:Is enabled"), width: 110}),
  ];

  return (
    <>
      <CrudListPage<CloudAccount>
        title={i18next.t("general:Cloud accounts")}
        description={i18next.t("general:Keep the servers of Alibaba Cloud, AWS, Tencent Cloud, Azure, GCP and Huawei Cloud accounts as assets")}
        columns={columns}
        fetch={fetchAccounts}
        deps={[owner]}
        newRecord={newAccount}
        add={CloudAccountBackend.addCloudAccount}
        remove={CloudAccountBackend.deleteCloudAccount}
        editUrl={(item) => `/cloud-accounts/${item.owner}/${item.name}`}
        actionColumnWidth={220}
        rowActions={(item, _index, {refresh}) => [
          {key: "sync", label: i18next.t("cloudAccount:Sync"), onSelect: () => setSyncing({account: item, refresh})},
        ]}
      />
      {syncing ? (
        <CloudSyncDialog
          account={syncing.account}
          onClose={(synced) => {
            setSyncing(null);
            if (synced) {
              syncing.refresh();
            }
          }}
        />
      ) : null}
    </>
  );
}
