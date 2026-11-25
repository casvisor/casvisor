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
import * as LoginRuleBackend from "@/backend/LoginRuleBackend";
import type {LoginRule} from "@/backend/types";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {boolColumn, linkColumn, tagsColumn, textColumn} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {useAccount} from "@/hooks/use-account";
import {LoginActionBadge, describeLoginTime} from "@/lib/loginRule";
import * as Setting from "@/lib/setting";

export default function LoginRuleListPage() {
  const {account} = useAccount();
  const owner = Setting.getRequestOrganization(account);

  const fetchRules = React.useCallback(
    (query: TableQuery) =>
      LoginRuleBackend.getLoginRules(owner, {
        page: query.page,
        pageSize: query.pageSize,
        field: query.searchedColumn,
        value: query.searchText,
        sortField: query.sortField,
        sortOrder: query.sortOrder,
      }),
    [owner],
  );

  const newRule = (): LoginRule => ({
    owner: account!.owner,
    name: `login_rule_${Setting.getRandomName()}`,
    createdTime: dayjs().format(),
    updatedTime: dayjs().format(),
    displayName: `New Login Rule - ${Setting.getRandomName()}`,
    description: "",
    priority: 50,
    users: ["*"],
    groups: [],
    clientIps: [],
    weekdays: [],
    startTime: "",
    endTime: "",
    timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone ?? "",
    methods: [],
    action: "Reject",
    reviewers: [],
    isEnabled: false,
  });

  const columns: ColumnDef<LoginRule>[] = [
    {dataIndex: "priority", title: i18next.t("loginRule:Priority"), width: 90, sortable: true},
    linkColumn<LoginRule>({dataIndex: "name", width: 170, to: (rule) => `/login-rules/${Setting.getIdPath(rule.owner, rule.name)}`}),
    textColumn<LoginRule>({dataIndex: "displayName", title: i18next.t("general:Display name"), width: 180, searchable: true}),
    {
      dataIndex: "action",
      title: i18next.t("command:Action"),
      width: 130,
      render: (value) => <LoginActionBadge action={value} />,
    },
    tagsColumn<LoginRule>({dataIndex: "users", title: i18next.t("permission:Users")}),
    tagsColumn<LoginRule>({dataIndex: "groups", title: i18next.t("permission:Groups")}),
    tagsColumn<LoginRule>({dataIndex: "clientIps", title: i18next.t("permission:Client IPs")}),
    {
      dataIndex: "weekdays",
      title: i18next.t("loginRule:Time"),
      width: 200,
      render: (_, rule) => <span className="text-xs">{describeLoginTime(rule)}</span>,
    },
    tagsColumn<LoginRule>({dataIndex: "methods", title: i18next.t("loginRule:Sign-in methods")}),
    boolColumn<LoginRule>({dataIndex: "isEnabled", title: i18next.t("permission:Is enabled"), width: 120}),
  ];

  return (
    <CrudListPage<LoginRule>
      title={i18next.t("general:Login rules")}
      description={i18next.t("general:Allow, reject, review or ask for two-step verification when users sign in, by user, IP and time")}
      columns={columns}
      fetch={fetchRules}
      deps={[owner]}
      newRecord={newRule}
      add={LoginRuleBackend.addLoginRule}
      remove={LoginRuleBackend.deleteLoginRule}
      editUrl={(rule) => `/login-rules/${Setting.getIdPath(rule.owner, rule.name)}`}
      initialQuery={{sortField: "priority", sortOrder: "ascend"}}
    />
  );
}
