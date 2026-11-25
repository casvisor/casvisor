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
import * as ConnectRuleBackend from "@/backend/ConnectRuleBackend";
import type {ConnectRule} from "@/backend/types";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {boolColumn, linkColumn, tagsColumn, textColumn} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {Badge} from "@/components/ui/badge";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";

export function ConnectActionBadge({action}: {action: string}) {
  return <Badge variant={action === "Reject" ? "destructive" : "success"}>{i18next.t(`connectRule:${action}`)}</Badge>;
}

export default function ConnectRuleListPage() {
  const {account} = useAccount();
  const owner = Setting.getRequestOrganization(account);

  const fetchRules = React.useCallback(
    (query: TableQuery) =>
      ConnectRuleBackend.getConnectRules(owner, {
        page: query.page,
        pageSize: query.pageSize,
        field: query.searchedColumn,
        value: query.searchText,
        sortField: query.sortField,
        sortOrder: query.sortOrder,
      }),
    [owner],
  );

  const newRule = (): ConnectRule => ({
    owner: account!.owner,
    name: `connect_rule_${Setting.getRandomName()}`,
    createdTime: dayjs().format(),
    updatedTime: dayjs().format(),
    displayName: `New Connect Rule - ${Setting.getRandomName()}`,
    description: "",
    priority: 100,
    users: [],
    groups: [],
    assets: [],
    assetTags: [],
    accounts: [],
    methods: ["Port forwarding"],
    action: "Reject",
    isEnabled: false,
  });

  const columns: ColumnDef<ConnectRule>[] = [
    textColumn<ConnectRule>({dataIndex: "priority", title: i18next.t("loginRule:Priority"), width: 90}),
    linkColumn<ConnectRule>({dataIndex: "name", width: 170, to: (rule) => `/connect-rules/${Setting.getIdPath(rule.owner, rule.name)}`}),
    textColumn<ConnectRule>({dataIndex: "displayName", title: i18next.t("general:Display name"), width: 180, searchable: true}),
    {
      dataIndex: "methods",
      title: i18next.t("connectRule:Methods"),
      width: 240,
      render: (value: string[] | null) => (value && value.length > 0 ? (
        <div className="flex flex-wrap gap-1">{value.map((method) => <Badge key={method} variant="secondary">{i18next.t(`connectRule:${method}`)}</Badge>)}</div>
      ) : <span className="text-muted-foreground">{i18next.t("connectRule:Every method")}</span>),
    },
    {
      dataIndex: "action",
      title: i18next.t("command:Action"),
      width: 100,
      render: (value) => <ConnectActionBadge action={value} />,
    },
    tagsColumn<ConnectRule>({dataIndex: "users", title: i18next.t("permission:Users")}),
    tagsColumn<ConnectRule>({dataIndex: "groups", title: i18next.t("permission:Groups")}),
    tagsColumn<ConnectRule>({dataIndex: "assets", title: i18next.t("general:Assets")}),
    tagsColumn<ConnectRule>({dataIndex: "assetTags", title: i18next.t("permission:Asset tags")}),
    tagsColumn<ConnectRule>({dataIndex: "accounts", title: i18next.t("connectRule:Accounts")}),
    boolColumn<ConnectRule>({dataIndex: "isEnabled", title: i18next.t("permission:Is enabled"), width: 110}),
  ];

  return (
    <CrudListPage<ConnectRule>
      title={i18next.t("general:Connect rules")}
      description={i18next.t("general:Allow or reject ways to connect, such as the browser, SSH clients, SFTP, port forwarding and Remote Desktop Connection")}
      columns={columns}
      fetch={fetchRules}
      deps={[owner]}
      newRecord={newRule}
      add={ConnectRuleBackend.addConnectRule}
      remove={ConnectRuleBackend.deleteConnectRule}
      editUrl={(rule) => `/connect-rules/${Setting.getIdPath(rule.owner, rule.name)}`}
    />
  );
}
