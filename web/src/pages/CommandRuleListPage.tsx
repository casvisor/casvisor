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
import * as CommandBackend from "@/backend/CommandBackend";
import type {CommandRule} from "@/backend/types";
import {Badge} from "@/components/ui/badge";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {boolColumn, linkColumn, tagsColumn, textColumn} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";

export function CommandActionBadge({action}: {action: string}) {
  if (!action) {
    return null;
  }
  return (
    <Badge variant={action === "Block" ? "destructive" : "secondary"}>
      {i18next.t(`command:${action}`)}
    </Badge>
  );
}

export default function CommandRuleListPage() {
  const {account} = useAccount();
  const owner = Setting.getRequestOrganization(account);

  const fetchRules = React.useCallback(
    (query: TableQuery) =>
      CommandBackend.getCommandRules(owner, {
        page: query.page,
        pageSize: query.pageSize,
        field: query.searchedColumn,
        value: query.searchText,
        sortField: query.sortField,
        sortOrder: query.sortOrder,
      }),
    [owner],
  );

  const newRule = (): CommandRule => ({
    owner: account!.owner,
    name: `command_rule_${Setting.getRandomName()}`,
    createdTime: dayjs().format(),
    updatedTime: dayjs().format(),
    displayName: `New Command Rule - ${Setting.getRandomName()}`,
    description: "",
    pattern: "^\\s*rm\\s+-rf\\s+/\\s*$",
    action: "Block",
    users: [],
    groups: [],
    assets: [],
    assetTags: [],
    isEnabled: true,
  });

  const columns: ColumnDef<CommandRule>[] = [
    linkColumn<CommandRule>({dataIndex: "name", width: 180, to: (rule) => `/command-rules/${rule.owner}/${rule.name}`}),
    textColumn<CommandRule>({dataIndex: "displayName", title: i18next.t("general:Display name"), width: 180, searchable: true}),
    {
      dataIndex: "pattern",
      title: i18next.t("command:Pattern"),
      width: 260,
      searchable: true,
      render: (value) => <code className="break-all rounded bg-muted px-1.5 py-0.5 text-xs">{value}</code>,
    },
    {
      dataIndex: "action",
      title: i18next.t("command:Action"),
      width: 110,
      render: (value) => <CommandActionBadge action={value} />,
    },
    tagsColumn<CommandRule>({dataIndex: "users", title: i18next.t("permission:Users")}),
    tagsColumn<CommandRule>({dataIndex: "groups", title: i18next.t("permission:Groups")}),
    tagsColumn<CommandRule>({dataIndex: "assets", title: i18next.t("general:Assets")}),
    tagsColumn<CommandRule>({dataIndex: "assetTags", title: i18next.t("permission:Asset tags")}),
    boolColumn<CommandRule>({dataIndex: "isEnabled", title: i18next.t("permission:Is enabled"), width: 120}),
  ];

  return (
    <CrudListPage<CommandRule>
      title={i18next.t("general:Command rules")}
      description={i18next.t("general:Block or flag dangerous commands in SSH sessions")}
      columns={columns}
      fetch={fetchRules}
      deps={[owner]}
      newRecord={newRule}
      add={CommandBackend.addCommandRule}
      remove={CommandBackend.deleteCommandRule}
      editUrl={(rule) => `/command-rules/${rule.owner}/${rule.name}`}
    />
  );
}
