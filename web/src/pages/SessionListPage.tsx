// Copyright 2024 The casbin Authors. All Rights Reserved.
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
import {Link} from "react-router-dom";
import * as SessionBackend from "@/backend/SessionBackend";
import type {Session} from "@/backend/types";
import {Tabs, TabsList, TabsTrigger} from "@/components/ui/tabs";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {textColumn, valueFilters} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";

export default function SessionListPage() {
  const {account} = useAccount();
  const owner = Setting.getRequestOrganization(account);
  const [status, setStatus] = React.useState(SessionBackend.Connected);
  const isOnline = status === SessionBackend.Connected;

  const fetchSessions = React.useCallback(
    (query: TableQuery) =>
      SessionBackend.getSessions(owner, {
        page: query.page,
        pageSize: query.pageSize,
        field: query.searchedColumn,
        value: query.searchText,
        sortField: query.sortField,
        sortOrder: query.sortOrder,
      }, status),
    [owner, status],
  );

  const stopSession = async(session: Session, refresh: () => void) => {
    try {
      const res = await SessionBackend.disconnect(Setting.getIdFromObject(session));
      if (res.status === "ok") {
        Setting.showMessage("success", i18next.t("general:Successfully stopped"));
        refresh();
      } else {
        Setting.showMessage("error", `${i18next.t("general:Failed to stop")}: ${res.msg}`);
      }
    } catch (error) {
      Setting.showMessage("error", `${i18next.t("general:Failed to connect to server")}: ${error}`);
    }
  };

  const columns: ColumnDef<Session>[] = [
    {
      dataIndex: "asset",
      title: i18next.t("general:Asset"),
      width: 200,
      sortable: true,
      searchable: true,
      render: (value) =>
        value ? (
          <Link to={`/assets/${value}`} className="font-medium underline-offset-4 hover:underline">
            {value}
          </Link>
        ) : null,
    },
    textColumn<Session>({dataIndex: "protocol", title: i18next.t("general:Protocol"), width: 120, filters: valueFilters(["RDP", "VNC", "SSH", "Telnet"])}),
    textColumn<Session>({
      dataIndex: "creator",
      title: i18next.t("general:User"),
      width: 160,
      searchable: true,
      link: (value) => (value ? Setting.getUserProfileUrl(value, account) : undefined),
      linkExternal: true,
    }),
    {
      dataIndex: "clientIp",
      title: i18next.t("general:Client IP"),
      width: 170,
      sortable: true,
      searchable: true,
      render: (value, session) =>
        value ? (
          <a href={`https://db-ip.com/${value}`} target="_blank" rel="noreferrer" className="underline-offset-4 hover:underline">
            <div>{value}</div>
            {session.clientIpDesc ? <div className="text-xs text-muted-foreground">{session.clientIpDesc}</div> : null}
          </a>
        ) : null,
    },
    {
      dataIndex: "userAgent",
      title: i18next.t("general:User agent"),
      width: 200,
      searchable: true,
      render: (value, session) => {
        if (!session.userAgentDesc) {
          return <span className="line-clamp-2 break-all text-xs">{value}</span>;
        }
        return session.userAgentDesc
          .split("|")
          .filter((text: string) => !text.includes("Other") && !text.includes("Generic Smartphone"))
          .map((text: string) => <div key={text}>{text}</div>);
      },
    },
    {
      dataIndex: "startTime",
      title: i18next.t("general:Connection start time"),
      width: 180,
      sortable: true,
      render: (value) => <span className="whitespace-nowrap tabular-nums text-muted-foreground">{Setting.getFormattedDate(value)}</span>,
    },
    {
      key: "duration",
      dataIndex: "startTimeDur",
      title: i18next.t("general:Connection duration"),
      width: 160,
      render: (_value, session) => (
        <span className="whitespace-nowrap tabular-nums">
          {Setting.getDurationSince(session.startTime, isOnline ? null : session.endTime)}
        </span>
      ),
    },
  ];

  return (
    <CrudListPage<Session>
      title={i18next.t("general:Sessions")}
      description={i18next.t("general:Active or history connection sessions")}
      columns={columns}
      fetch={fetchSessions}
      deps={[owner, status]}
      tableId={`/sessions/${status}`}
      remove={isOnline ? undefined : SessionBackend.deleteSession}
      actionColumnWidth={120}
      rowActions={(session, _index, {refresh}) => [
        isOnline
          ? {
            key: "stop",
            label: i18next.t("general:Stop"),
            destructive: true,
            confirm: {title: i18next.t("general:Sure to disconnect?"), confirmText: i18next.t("general:Stop")},
            onSelect: () => stopSession(session, refresh),
          }
          : null,
      ]}
      toolbar={
        <Tabs value={status} onValueChange={setStatus}>
          <TabsList>
            <TabsTrigger value={SessionBackend.Connected}>{i18next.t("session:Online")}</TabsTrigger>
            <TabsTrigger value={SessionBackend.Disconnected}>{i18next.t("session:History")}</TabsTrigger>
          </TabsList>
        </Tabs>
      }
    />
  );
}
