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
import * as LocalUserBackend from "@/backend/LocalUserBackend";
import type {LocalUser} from "@/backend/types";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {boolColumn, dateColumn, linkColumn, tagsColumn, textColumn} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";

/** The accounts Casvisor keeps itself, when it runs without Casdoor. */
export default function UserListPage() {
  const {account} = useAccount();
  const owner = account?.owner ?? "";

  const fetchUsers = React.useCallback(
    (query: TableQuery) =>
      LocalUserBackend.getLocalUsers(owner, {
        page: query.page,
        pageSize: query.pageSize,
        field: query.searchedColumn,
        value: query.searchText,
        sortField: query.sortField,
        sortOrder: query.sortOrder,
      }),
    [owner],
  );

  const newUser = (): LocalUser => ({
    owner,
    name: `user_${Setting.getRandomName()}`,
    createdTime: dayjs().format(),
    updatedTime: dayjs().format(),
    displayName: "",
    email: "",
    groups: [],
    isAdmin: false,
    isForbidden: false,
    password: "",
    lastSigninTime: "",
  });

  const columns: ColumnDef<LocalUser>[] = [
    linkColumn<LocalUser>({dataIndex: "name", width: 160, to: (user) => `/users/${user.owner}/${user.name}`}),
    textColumn<LocalUser>({dataIndex: "displayName", title: i18next.t("general:Display name"), width: 160, searchable: true}),
    textColumn<LocalUser>({dataIndex: "email", title: i18next.t("user:Email"), width: 200, searchable: true}),
    tagsColumn<LocalUser>({dataIndex: "groups", title: i18next.t("permission:Groups")}),
    boolColumn<LocalUser>({dataIndex: "isAdmin", title: i18next.t("user:Is admin"), width: 110}),
    boolColumn<LocalUser>({dataIndex: "isForbidden", title: i18next.t("user:Is forbidden"), width: 110, invertColor: true}),
    dateColumn<LocalUser>("lastSigninTime", i18next.t("user:Last sign-in time")),
    dateColumn<LocalUser>("createdTime"),
  ];

  return (
    <CrudListPage<LocalUser>
      title={i18next.t("general:Users")}
      description={i18next.t("general:Accounts that sign in to Casvisor")}
      columns={columns}
      fetch={fetchUsers}
      deps={[owner]}
      newRecord={newUser}
      remove={LocalUserBackend.deleteLocalUser}
      deleteDisabled={(user) => user.name === account?.name}
      editUrl={(user) => `/users/${user.owner}/${user.name}`}
    />
  );
}
