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
import * as RecordBackend from "@/backend/RecordBackend";
import type {Record} from "@/backend/types";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {clientIpColumn, dateColumn, linkColumn, textColumn, valueFilters} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";

const Methods = ["GET", "HEAD", "POST", "PUT", "DELETE", "CONNECT", "OPTIONS", "TRACE", "PATCH"];

export default function RecordListPage() {
  const {account} = useAccount();
  const owner = Setting.getRequestOrganization(account);

  const fetchRecords = React.useCallback(
    (query: TableQuery) =>
      RecordBackend.getRecords(owner, {
        page: query.page,
        pageSize: query.pageSize,
        field: query.searchedColumn,
        value: query.searchText,
        sortField: query.sortField,
        sortOrder: query.sortOrder,
      }),
    [owner],
  );

  // a record's owner is its organization, but older rows may have only one of the two
  const viewUrl = (record: Record) => `/records/${record.owner || record.organization || account?.owner}/${record.name}`;

  const columns: ColumnDef<Record>[] = [
    textColumn<Record>({
      dataIndex: "organization",
      title: i18next.t("general:Organization"),
      width: 120,
      searchable: true,
      link: (value) => (value ? Setting.getCasdoorUrl(account, `/organizations/${value}`) : undefined),
      linkExternal: true,
    }),
    textColumn<Record>({dataIndex: "id", title: i18next.t("general:ID"), width: 90, searchable: true}),
    linkColumn<Record>({dataIndex: "name", width: 300, to: viewUrl, fixed: false}),
    clientIpColumn<Record>(),
    dateColumn<Record>("createdTime"),
    {
      dataIndex: "user",
      title: i18next.t("general:User"),
      width: 130,
      sortable: true,
      searchable: true,
      render: (value, record) =>
        value ? (
          <a
            href={Setting.getCasdoorUrl(account, `/users/${record.organization}/${value}`)}
            target="_blank"
            rel="noreferrer"
            className="underline-offset-4 hover:underline"
          >
            {value}
          </a>
        ) : null,
    },
    textColumn<Record>({dataIndex: "method", title: i18next.t("general:Method"), width: 110, filters: valueFilters(Methods)}),
    textColumn<Record>({dataIndex: "requestUri", title: i18next.t("general:Request URI"), width: 220, searchable: true}),
    textColumn<Record>({dataIndex: "language", title: i18next.t("user:Language"), width: 100, searchable: true}),
    {
      dataIndex: "response",
      title: i18next.t("record:Response"),
      width: 200,
      sortable: true,
      searchable: true,
      render: (value) => <span className="line-clamp-2 break-all font-mono text-xs">{value}</span>,
    },
    {
      dataIndex: "object",
      title: i18next.t("record:Object"),
      width: 200,
      sortable: true,
      searchable: true,
      render: (value) => <span className="line-clamp-2 break-all font-mono text-xs">{value}</span>,
    },
    textColumn<Record>({dataIndex: "action", title: i18next.t("general:Action"), width: 150, searchable: true}),
  ];

  return (
    <CrudListPage<Record>
      title={i18next.t("general:Records")}
      description={i18next.t("general:Audit logs")}
      columns={columns}
      fetch={fetchRecords}
      deps={[owner]}
      rowKey={(record) => `${record.id}`}
      editUrl={viewUrl}
      readOnly
      actionColumnWidth={100}
    />
  );
}
