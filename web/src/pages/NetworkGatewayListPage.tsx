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
import * as NetworkGatewayBackend from "@/backend/NetworkGatewayBackend";
import type {NetworkGateway} from "@/backend/types";
import {Badge} from "@/components/ui/badge";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {boolColumn, dateColumn, linkColumn, textColumn} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";

export function NetworkGatewayStatus({gateway}: {gateway: NetworkGateway}) {
  if (gateway.type === "Agent") {
    if (gateway.isOnline) {
      return <Badge variant="success">{i18next.t("networkGateway:Online")}</Badge>;
    }
    return <Badge variant="secondary">{i18next.t("networkGateway:Offline")}</Badge>;
  }
  if (gateway.type !== "SSH" && gateway.type) {
    return null;
  }
  if (!gateway.isEnabled) {
    return <Badge variant="secondary">{i18next.t("networkGateway:Disabled")}</Badge>;
  }
  if (gateway.healthStatus === "Healthy") {
    return <Badge variant="success">{i18next.t("networkGateway:Healthy")}{gateway.latency ? ` ${gateway.latency} ms` : ""}</Badge>;
  }
  if (gateway.healthStatus === "Unhealthy") {
    return <Badge variant="destructive">{i18next.t("networkGateway:Unhealthy")}</Badge>;
  }
  return <Badge variant="secondary">{i18next.t("networkGateway:Not checked")}</Badge>;
}

export default function NetworkGatewayListPage() {
  const {account} = useAccount();
  const owner = Setting.getRequestOrganization(account);

  const fetchGateways = React.useCallback(
    (query: TableQuery) =>
      NetworkGatewayBackend.getNetworkGateways(owner, {
        page: query.page,
        pageSize: query.pageSize,
        field: query.searchedColumn,
        value: query.searchText,
        sortField: query.sortField,
        sortOrder: query.sortOrder,
      }),
    [owner],
  );

  const newGateway = (): NetworkGateway => ({
    owner: account!.owner,
    name: `network_gateway_${Setting.getRandomName()}`,
    createdTime: dayjs().format(),
    updatedTime: dayjs().format(),
    displayName: `New Network Gateway - ${Setting.getRandomName()}`,
    description: "",
    type: "SSH",
    host: "",
    port: 22,
    username: "root",
    password: "",
    privateKey: "",
    fingerprint: "",
    isEnabled: true,
    allowedNetworks: [],
    domain: "",
    priority: 0,
    weight: 1,
  });

  const columns: ColumnDef<NetworkGateway>[] = [
    linkColumn<NetworkGateway>({dataIndex: "name", width: 180, to: (gateway) => `/network-gateways/${Setting.getIdPath(gateway.owner, gateway.name)}`}),
    textColumn<NetworkGateway>({dataIndex: "displayName", title: i18next.t("general:Display name"), width: 180, searchable: true}),
    dateColumn<NetworkGateway>("createdTime"),
    textColumn<NetworkGateway>({dataIndex: "description", title: i18next.t("general:Description"), width: 200}),
    {
      dataIndex: "type",
      title: i18next.t("general:Type"),
      width: 130,
      sortable: true,
      render: (value) => (value === "Agent" ? i18next.t("networkGateway:Agent") : i18next.t("networkGateway:SSH jump host")),
    },
    {
      dataIndex: "isOnline",
      title: i18next.t("general:Status"),
      width: 110,
      sortable: false,
      render: (_, gateway) => <NetworkGatewayStatus gateway={gateway} />,
    },
    textColumn<NetworkGateway>({dataIndex: "domain", title: i18next.t("networkGateway:Network domain"), width: 140, searchable: true}),
    {
      dataIndex: "priority",
      title: i18next.t("networkGateway:Priority / weight"),
      width: 130,
      sortable: true,
      render: (_, gateway) => (gateway.domain ? `${gateway.priority} / ${gateway.weight || 1}` : ""),
    },
    {
      dataIndex: "lastError",
      title: i18next.t("networkGateway:Last error"),
      width: 240,
      sortable: false,
      render: (_, gateway) => (gateway.lastError ? (
        <div className="line-clamp-2 break-all text-xs text-destructive" title={gateway.lastError}>
          <span className="text-muted-foreground">{Setting.getFormattedDate(gateway.lastErrorTime ?? "")} </span>
          {gateway.lastError}
        </div>
      ) : ""),
    },
    textColumn<NetworkGateway>({dataIndex: "host", title: i18next.t("asset:Host"), width: 160, searchable: true}),
    {dataIndex: "port", title: i18next.t("general:Port"), width: 90, sortable: true, render: (value) => value || ""},
    textColumn<NetworkGateway>({dataIndex: "username", title: i18next.t("general:Username"), width: 130}),
    boolColumn<NetworkGateway>({dataIndex: "isEnabled", title: i18next.t("permission:Is enabled"), width: 120}),
  ];

  return (
    <CrudListPage<NetworkGateway>
      title={i18next.t("general:Network gateways")}
      description={i18next.t("general:Reach assets in other networks through SSH jump hosts or agents")}
      columns={columns}
      fetch={fetchGateways}
      deps={[owner]}
      newRecord={newGateway}
      add={NetworkGatewayBackend.addNetworkGateway}
      remove={NetworkGatewayBackend.deleteNetworkGateway}
      editUrl={(gateway) => `/network-gateways/${Setting.getIdPath(gateway.owner, gateway.name)}`}
    />
  );
}
