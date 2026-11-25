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
import {useNavigate} from "react-router-dom";
import * as PlatformBackend from "@/backend/PlatformBackend";
import type {Platform} from "@/backend/types";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {boolColumn, linkColumn, textColumn} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {Badge} from "@/components/ui/badge";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";

/** A copy of the platform under a new name, to save in the organization and change. */
export function copyPlatform(platform: Platform): Platform {
  return {
    ...platform,
    name: `${platform.name}_${Setting.getRandomName()}`,
    displayName: `${platform.displayName || platform.name} (${i18next.t("platform:Copy")})`,
    createdTime: dayjs().format(),
    updatedTime: dayjs().format(),
    isBuiltIn: false,
    protocols: (platform.protocols ?? []).map((protocol) => ({...protocol})),
  };
}

export function getMethodName(method: string) {
  return method ? i18next.t(`platform:${method}`) : i18next.t("platform:None");
}

export default function PlatformListPage() {
  const {account} = useAccount();
  const navigate = useNavigate();
  const owner = Setting.getRequestOrganization(account);

  const fetchPlatforms = React.useCallback(
    (query: TableQuery) =>
      PlatformBackend.getPlatforms(owner, {
        page: query.page,
        pageSize: query.pageSize,
        value: query.searchText,
      }),
    [owner],
  );

  const newPlatform = (): Platform => ({
    owner: account!.owner,
    name: `platform_${Setting.getRandomName()}`,
    createdTime: dayjs().format(),
    updatedTime: dayjs().format(),
    displayName: `New Platform - ${Setting.getRandomName()}`,
    description: "",
    category: "Host",
    protocols: [{name: "SSH", port: 22}],
    charset: "UTF-8",
    elevation: "sudo",
    elevationCommand: "",
    method: "Shell",
    changeCommands: "",
    pushCommands: "",
    promptPattern: "",
    errorPattern: "",
    passwordLength: 24,
    passwordUppercase: true,
    passwordLowercase: true,
    passwordDigits: true,
    passwordSymbols: "!#%+-=_.",
    passwordExclude: "",
    canChangePassword: true,
    canPush: true,
    canGather: true,
  });

  const editUrl = (item: Platform) => `/platforms/${Setting.getIdPath(item.owner, item.name)}`;

  const columns: ColumnDef<Platform>[] = [
    linkColumn<Platform>({dataIndex: "name", width: 170, sortable: false, to: editUrl}),
    {
      dataIndex: "displayName",
      title: i18next.t("general:Display name"),
      width: 170,
      render: (value, item) => (
        <span className="flex items-center gap-1.5">
          {value}
          {item.isBuiltIn ? <Badge variant="secondary">{i18next.t("platform:Built-in")}</Badge> : null}
        </span>
      ),
    },
    {
      dataIndex: "category",
      title: i18next.t("platform:Category"),
      width: 110,
      render: (value) => i18next.t(`platform:${value}`),
    },
    {
      dataIndex: "protocols",
      title: i18next.t("platform:Protocols"),
      width: 220,
      render: (_value, item) => (
        <div className="flex flex-wrap gap-1">
          {(item.protocols ?? []).map((protocol) => (
            <Badge key={protocol.name} variant="outline" className="font-mono">{protocol.name}{protocol.port ? `/${protocol.port}` : ""}</Badge>
          ))}
        </div>
      ),
    },
    {
      dataIndex: "method",
      title: i18next.t("platform:Method"),
      width: 120,
      render: (value) => getMethodName(value),
    },
    textColumn<Platform>({dataIndex: "charset", title: i18next.t("platform:Charset"), width: 100, sortable: false}),
    boolColumn<Platform>({dataIndex: "canChangePassword", title: i18next.t("platform:Change passwords"), width: 110}),
    boolColumn<Platform>({dataIndex: "canPush", title: i18next.t("platform:Push accounts"), width: 110}),
    boolColumn<Platform>({dataIndex: "canGather", title: i18next.t("platform:Gather accounts"), width: 110}),
  ];

  return (
    <CrudListPage<Platform>
      title={i18next.t("general:Platforms")}
      description={i18next.t("general:Ports, terminal charsets, elevation and password changes of each kind of system")}
      columns={columns}
      fetch={fetchPlatforms}
      deps={[owner]}
      newRecord={newPlatform}
      add={PlatformBackend.addPlatform}
      remove={PlatformBackend.deletePlatform}
      editUrl={editUrl}
      rowReadOnly={(item) => Boolean(item.isBuiltIn)}
      actionColumnWidth={200}
      rowActions={(item) => [
        {key: "copy", label: i18next.t("platform:Copy"), onSelect: () => {
          const copy = copyPlatform(item);
          navigate(editUrl(copy), {state: {mode: "add", record: copy}});
        }},
      ]}
    />
  );
}
