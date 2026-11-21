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
import {Link, useSearchParams} from "react-router-dom";
import * as CommandBackend from "@/backend/CommandBackend";
import type {Command} from "@/backend/types";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {ExportCsvButton} from "@/components/crud/ExportCsvButton";
import {dateColumn, textColumn} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {useAccount} from "@/hooks/use-account";
import {isDatabaseProtocol} from "@/lib/asset";
import * as Setting from "@/lib/setting";
import {CommandActionBadge} from "@/pages/CommandRuleListPage";

/** The commands run in SSH sessions and the statements run in database sessions;
 *  /commands?session=<owner>/<name> shows one session's. */
export default function CommandListPage() {
  const {account} = useAccount();
  const owner = Setting.getRequestOrganization(account);
  const [searchParams] = useSearchParams();
  const session = searchParams.get("session") ?? "";

  const fetchCommands = React.useCallback(
    (query: TableQuery) =>
      CommandBackend.getCommands(owner, {
        page: query.page,
        pageSize: query.pageSize,
        field: query.searchedColumn,
        value: query.searchText,
        sortField: query.sortField,
        sortOrder: query.sortOrder,
      }),
    [owner],
  );

  const columns: ColumnDef<Command>[] = [
    dateColumn<Command>("createdTime", i18next.t("general:Time")),
    {
      dataIndex: "text",
      title: i18next.t("command:Command"),
      width: 360,
      searchable: true,
      render: (value) => <code className="break-all font-mono text-xs">{value}</code>,
    },
    {
      dataIndex: "action",
      title: i18next.t("command:Action"),
      width: 100,
      render: (value) => <CommandActionBadge action={value} />,
    },
    {
      dataIndex: "rowCount",
      title: i18next.t("database:Result"),
      width: 160,
      render: (_value, command) => {
        if (!isDatabaseProtocol(command.protocol) || command.action === "Block") {
          return null;
        }
        if (command.error) {
          return <span className="line-clamp-2 break-all text-xs text-destructive" title={command.error}>{command.error}</span>;
        }
        return <span className="text-xs tabular-nums">{i18next.t("database:{{n}} row(s)", {n: command.rowCount ?? 0})}</span>;
      },
    },
    textColumn<Command>({dataIndex: "user", title: i18next.t("general:User"), width: 150, searchable: true}),
    textColumn<Command>({dataIndex: "operator", title: i18next.t("command:Operator"), width: 150, searchable: true}),
    {
      dataIndex: "asset",
      title: i18next.t("general:Asset"),
      width: 170,
      searchable: true,
      render: (value) => (
        <Link to={`/assets/${value}`} className="underline-offset-4 hover:underline">{value}</Link>
      ),
    },
    {
      dataIndex: "rule",
      title: i18next.t("command:Rule"),
      width: 170,
      render: (value) => (value ? (
        <Link to={`/command-rules/${value}`} className="underline-offset-4 hover:underline">{Setting.getShortName(value)}</Link>
      ) : null),
    },
    textColumn<Command>({dataIndex: "clientIp", title: i18next.t("general:Client IP"), width: 140, searchable: true}),
    {
      dataIndex: "session",
      title: i18next.t("general:Session"),
      width: 120,
      searchable: true,
      render: (value) => <span className="font-mono text-xs text-muted-foreground">{Setting.getShortName(value).slice(0, 8)}</span>,
    },
  ];

  return (
    <CrudListPage<Command>
      key={session}
      title={i18next.t("general:Commands")}
      description={i18next.t("general:Commands run in SSH sessions and SQL run in database sessions")}
      columns={columns}
      fetch={fetchCommands}
      deps={[owner]}
      tableId="/commands"
      initialQuery={session ? {searchedColumn: "session", searchText: session} : undefined}
      showActionColumn={false}
      toolbar={({query}) => <ExportCsvButton path="/api/export-commands" owner={owner} query={query} />}
    />
  );
}
