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
import * as PermissionBackend from "@/backend/PermissionBackend";
import type {Permission} from "@/backend/types";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {boolColumn, linkColumn, tagsColumn, textColumn} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";

export default function PermissionListPage() {
  const {account} = useAccount();
  const owner = Setting.getRequestOrganization(account);

  const fetchPermissions = React.useCallback(
    (query: TableQuery) =>
      PermissionBackend.getPermissions(owner, {
        page: query.page,
        pageSize: query.pageSize,
        field: query.searchedColumn,
        value: query.searchText,
        sortField: query.sortField,
        sortOrder: query.sortOrder,
      }),
    [owner],
  );

  const newPermission = (): Permission => ({
    owner: account!.owner,
    name: `permission_${Setting.getRandomName()}`,
    createdTime: dayjs().format(),
    updatedTime: dayjs().format(),
    displayName: `New Permission - ${Setting.getRandomName()}`,
    description: "",
    users: [],
    groups: [],
    assets: [],
    assetTags: [],
    actions: ["Connect", "Copy", "Paste"],
    clientIps: [],
    startTime: "",
    endTime: "",
    isEnabled: true,
  });

  const columns: ColumnDef<Permission>[] = [
    linkColumn<Permission>({dataIndex: "name", width: 160, to: (permission) => `/permissions/${permission.owner}/${permission.name}`}),
    textColumn<Permission>({dataIndex: "displayName", title: i18next.t("general:Display name"), width: 180, searchable: true}),
    tagsColumn<Permission>({dataIndex: "users", title: i18next.t("permission:Users")}),
    tagsColumn<Permission>({dataIndex: "groups", title: i18next.t("permission:Groups")}),
    tagsColumn<Permission>({dataIndex: "assets", title: i18next.t("general:Assets")}),
    tagsColumn<Permission>({dataIndex: "assetTags", title: i18next.t("permission:Asset tags")}),
    tagsColumn<Permission>({dataIndex: "actions", title: i18next.t("permission:Actions")}),
    tagsColumn<Permission>({dataIndex: "clientIps", title: i18next.t("permission:Client IPs")}),
    {
      dataIndex: "endTime",
      title: i18next.t("permission:End time"),
      width: 165,
      sortable: true,
      render: (value) => (
        <span className="whitespace-nowrap tabular-nums text-muted-foreground">
          {value ? Setting.getFormattedDate(value) : i18next.t("permission:Never")}
        </span>
      ),
    },
    boolColumn<Permission>({dataIndex: "isEnabled", title: i18next.t("permission:Is enabled"), width: 120}),
  ];

  return (
    <CrudListPage<Permission>
      title={i18next.t("general:Permissions")}
      description={i18next.t("general:Who can connect to which assets")}
      columns={columns}
      fetch={fetchPermissions}
      deps={[owner]}
      newRecord={newPermission}
      add={PermissionBackend.addPermission}
      remove={PermissionBackend.deletePermission}
      editUrl={(permission) => `/permissions/${permission.owner}/${permission.name}`}
    />
  );
}
