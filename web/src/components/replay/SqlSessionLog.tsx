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
import {useSearchParams} from "react-router-dom";
import {Database, Globe} from "lucide-react";
import * as CommandBackend from "@/backend/CommandBackend";
import * as SessionBackend from "@/backend/SessionBackend";
import type {Command, Session} from "@/backend/types";
import {WebProtocol} from "@/lib/asset";
import * as Setting from "@/lib/setting";

const maxStatements = 1000;
const refreshInterval = 3000;

/** The statements of a database session, or the requests of a web session, in the
 *  order they were sent, with how each turned out; a connected session is followed as it goes. */
export function SqlSessionLog({session: initialSession}: {session: Session}) {
  const [searchParams] = useSearchParams();
  const atParam = searchParams.get("at");
  const [session, setSession] = React.useState(initialSession);
  const [commands, setCommands] = React.useState<Command[]>([]);
  const [total, setTotal] = React.useState(0);
  const scrolledAt = React.useRef<string | null>(null);
  const sessionId = `${session.owner}/${session.name}`;
  const live = session.status === SessionBackend.Connected;
  const isWeb = session.protocol === WebProtocol;

  React.useEffect(() => {
    document.title = `${i18next.t("session:Replay")} | ${session.name}`;
  }, [session.name]);

  React.useEffect(() => {
    let active = true;
    const load = () => {
      CommandBackend.getCommands(session.owner, {page: 1, pageSize: maxStatements, field: "session", value: sessionId, sortField: "createdTime", sortOrder: "ascend"}).then((res) => {
        if (active && res.status === "ok") {
          setCommands((res.data ?? []).filter((command: Command) => command.session === sessionId));
          setTotal(res.data2 ?? 0);
        }
      });
    };
    load();
    if (!live) {
      return () => {
        active = false;
      };
    }

    const timer = setInterval(() => {
      load();
      SessionBackend.getSession(session.owner, session.name).then((res) => {
        if (active && res.status === "ok" && res.data) {
          setSession(res.data);
        }
      });
    }, refreshInterval);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [session.owner, session.name, sessionId, live]);

  const targetName = React.useMemo(() => {
    if (!atParam) {
      return "";
    }
    const target = dayjs(atParam);
    return (
      commands.find((c) => c.createdTime === atParam) ??
      (target.isValid() ? commands.find((c) => Math.abs(dayjs(c.createdTime).diff(target)) <= 1000) : undefined)
    )?.name ?? "";
  }, [commands, atParam]);

  const targetRowRef = React.useCallback((el: HTMLTableRowElement | null) => {
    if (el && scrolledAt.current !== atParam) {
      scrolledAt.current = atParam;
      el.scrollIntoView({block: "center"});
    }
  }, [atParam]);

  return (
    <div className="flex h-screen w-screen flex-col bg-[#1b1b1b] text-neutral-200">
      <div className="flex flex-wrap items-center gap-3 border-b border-neutral-700 px-4 py-2 text-sm">
        {isWeb ? <Globe className="h-4 w-4" /> : <Database className="h-4 w-4" />}
        <span className="font-medium">{isWeb ? i18next.t("web:Requests") : session.protocol === "Redis" || session.protocol === "MongoDB" ? i18next.t("database:Commands") : i18next.t("database:SQL statements")}</span>
        <span className="truncate text-neutral-400">
          {session.asset} | {session.creator} | {Setting.getFormattedDate(session.startTime)}
        </span>
        {live ? <span className="rounded bg-emerald-700 px-1.5 py-0.5 text-xs text-white">{i18next.t("session:Online")}</span> : null}
        <span className="ml-auto text-neutral-400">
          {total > commands.length
            ? i18next.t(isWeb ? "web:The first {{count}} of {{total}} requests" : "database:The first {{count}} of {{total}} statements", {count: commands.length, total})
            : i18next.t(isWeb ? "web:{{n}} request(s)" : "database:{{n}} statement(s)", {n: commands.length})}
        </span>
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        <table className="w-full border-collapse text-xs">
          <thead className="sticky top-0 bg-[#1b1b1b] text-left text-neutral-400">
            <tr className="border-b border-neutral-700">
              <th className="w-24 px-4 py-2 font-medium">{i18next.t("general:Time")}</th>
              <th className="px-4 py-2 font-medium">{isWeb ? i18next.t("web:Request") : i18next.t("database:Statement")}</th>
              <th className="w-72 px-4 py-2 font-medium">{i18next.t("database:Result")}</th>
              <th className="w-24 px-4 py-2 text-right font-medium">{i18next.t("database:Duration")}</th>
            </tr>
          </thead>
          <tbody>
            {commands.map((command) => (
              <tr
                key={command.name}
                ref={command.name === targetName ? targetRowRef : undefined}
                className={`border-b border-neutral-800 align-top hover:bg-neutral-800/60 ${command.name === targetName ? "bg-neutral-800" : ""}`}
              >
                <td className="whitespace-nowrap px-4 py-1.5 tabular-nums text-neutral-500" title={command.createdTime}>
                  {dayjs(command.createdTime).format("HH:mm:ss")}
                </td>
                <td className="px-4 py-1.5">
                  <pre className={`whitespace-pre-wrap break-all font-mono ${command.action === "Block" ? "text-red-400" : command.action === "Alert" ? "text-amber-300" : ""}`}>
                    {command.text}
                  </pre>
                </td>
                <td className="px-4 py-1.5">
                  {command.action === "Block" ? (
                    <span className="break-all text-red-400">
                      {i18next.t("command:Block")}: {command.rule ? Setting.getShortName(command.rule) : command.error}
                    </span>
                  ) : command.error ? (
                    <span className="break-all text-red-400">{isWeb && command.statusCode ? `HTTP ${command.statusCode}, ` : ""}{command.error}</span>
                  ) : isWeb ? (
                    <span className={`tabular-nums ${(command.statusCode ?? 0) >= 400 ? "text-amber-300" : ""}`}>HTTP {command.statusCode ?? 0}</span>
                  ) : (
                    <span className="tabular-nums">{i18next.t("database:{{n}} row(s)", {n: command.rowCount ?? 0})}</span>
                  )}
                  {command.action === "Alert" ? (
                    <span className="ml-2 text-amber-300">{i18next.t("command:Alert")}: {Setting.getShortName(command.rule)}</span>
                  ) : null}
                </td>
                <td className="whitespace-nowrap px-4 py-1.5 text-right tabular-nums text-neutral-400">
                  {command.action === "Block" ? "" : `${command.duration ?? 0} ms`}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {commands.length === 0 ? (
          <div className="p-8 text-center text-sm text-neutral-500">{isWeb ? i18next.t("web:No requests were sent in this session") : i18next.t("database:No statements were run in this session")}</div>
        ) : null}
      </div>
    </div>
  );
}
