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
import qs from "qs";
import {
  ChevronDown,
  ChevronRight,
  Copy,
  Database,
  Download,
  Eye,
  KeyRound,
  Loader2,
  Play,
  RefreshCw,
  ShieldAlert,
  Table2,
  TriangleAlert,
} from "lucide-react";
import * as SessionBackend from "@/backend/SessionBackend";
import type {DatabaseColumn, DatabaseTable, SqlResult} from "@/backend/types";
import {FailureDialog, useConnectionToast} from "@/components/access/ConnectionChrome";
import {closeView, holdPageLock, reloadIfUpdated, type ConnectionProps} from "@/components/access/util";
import {Button} from "@/components/ui/button";
import {Tooltip, TooltipContent, TooltipTrigger} from "@/components/ui/tooltip";
import {cn} from "@/lib/utils";
import * as Setting from "@/lib/setting";

const NotReachable = "Remote service exception, please check if the target device can be accessed normally.";

interface ConnectionInfo {
  protocol: string;
  version: string;
  database: string;
  maxRows: number;
}

interface Reply {
  type: string;
  id?: number;
  code?: number;
  message?: string;
  error?: string;
  data?: any;
  result?: SqlResult;
}

type Loadable<T> = {loading: true} | {error: string} | {items: T[]};

function quoteIdentifier(name: string, protocol: string) {
  return protocol === "MySQL" ? `\`${name.replace(/`/g, "``")}\`` : `"${name.replace(/"/g, "\"\"")}"`;
}

function getTableKey(database: string, table: DatabaseTable) {
  return `${database}\u0000${table.schema}\u0000${table.name}`;
}

/** The rows of a result as tab separated text, to paste into a spreadsheet. */
function toTsv(result: SqlResult) {
  const clean = (value: string | null) => (value ?? "").replace(/[\t\r\n]+/g, " ");
  const lines = [(result.columns ?? []).map((column) => clean(column.name)).join("\t")];
  for (const row of result.rows ?? []) {
    lines.push(row.map(clean).join("\t"));
  }
  return lines.join("\n");
}

/** A SQL console for a MySQL or PostgreSQL asset: the databases and tables on
 *  the left, the editor and the results on the right. Every statement runs on
 *  the server, which checks it against the command rules and records it. */
export default function DatabaseView(props: ConnectionProps) {
  const {assetId, inTab, addClient, username, password} = props;
  const assetName = Setting.getShortName(assetId);

  const wsRef = React.useRef<WebSocket | null>(null);
  const nextIdRef = React.useRef(1);
  const pendingRef = React.useRef(new Map<number, (reply: Reply) => void>());
  const runIdRef = React.useRef(0);
  const editorRef = React.useRef<HTMLTextAreaElement>(null);

  const [failure, setFailure] = React.useState<string | null>(null);
  const [closed, setClosed] = React.useState(false);
  const [info, setInfo] = React.useState<ConnectionInfo | null>(null);
  const [operations, setOperations] = React.useState<string[]>([]);
  const [sessionId, setSessionId] = React.useState("");
  const [databases, setDatabases] = React.useState<Loadable<string>>({loading: true});
  const [tables, setTables] = React.useState<Record<string, Loadable<DatabaseTable>>>({});
  const [columns, setColumns] = React.useState<Record<string, Loadable<DatabaseColumn>>>({});
  const [sql, setSql] = React.useState("");
  const [running, setRunning] = React.useState(false);
  const [results, setResults] = React.useState<SqlResult[]>([]);
  const [runError, setRunError] = React.useState("");
  const [activeResult, setActiveResult] = React.useState(0);
  const [editorHeight, setEditorHeight] = React.useState(220);
  const status = useConnectionToast(assetId);

  const connected = info !== null && !closed;
  const protocol = info?.protocol ?? "";

  const request = React.useCallback((type: string, payload: Record<string, any> = {}): Promise<Reply> => {
    const ws = wsRef.current;
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      return Promise.resolve({type: "reply", error: i18next.t("asset:Connection closed")});
    }
    const id = nextIdRef.current++;
    return new Promise((resolve) => {
      pendingRef.current.set(id, resolve);
      ws.send(JSON.stringify({type, id, ...payload}));
    });
  }, []);

  const loadDatabases = React.useCallback(() => {
    setDatabases({loading: true});
    request("databases").then((reply) => {
      if (reply.error) {
        setDatabases({error: reply.error});
        return;
      }
      setDatabases({items: reply.data?.databases ?? []});
      setInfo((info) => (info ? {...info, database: reply.data?.current ?? info.database} : info));
    });
  }, [request]);

  const loadTables = (database: string) => {
    setTables((tables) => ({...tables, [database]: {loading: true}}));
    request("tables", {database}).then((reply) => {
      setTables((tables) => ({...tables, [database]: reply.error ? {error: reply.error} : {items: reply.data ?? []}}));
    });
  };

  const loadColumns = (database: string, table: DatabaseTable) => {
    const key = getTableKey(database, table);
    setColumns((columns) => ({...columns, [key]: {loading: true}}));
    request("columns", {database, schema: table.schema, table: table.name}).then((reply) => {
      setColumns((columns) => ({...columns, [key]: reply.error ? {error: reply.error} : {items: reply.data ?? []}}));
    });
  };

  const connect = () => {
    status.loading(i18next.t("asset:Connecting..."));
    setInfo(null);
    setClosed(false);
    setResults([]);
    setTables({});
    setColumns({});

    SessionBackend.addAssetTunnel(assetId, props.account, username, password).then((res) => {
      if (res.status !== "ok") {
        status.dismiss();
        Setting.showMessage("error", `${i18next.t("asset:Failed to connect")}: ${res.msg}`);
        return;
      }

      const session = res.data;
      const id = `${session.owner}/${session.name}`;
      const params: Record<string, any> = {sessionId: id};

      let errorMessage = "";
      let isConnected = false;
      const ws = new WebSocket(Setting.getWebSocketUrl(`/api/get-asset-database?${qs.stringify(params)}`));
      wsRef.current = ws;

      ws.onmessage = (event) => {
        const reply: Reply = JSON.parse(event.data);
        if (reply.type === "connected") {
          isConnected = true;
          setSessionId(id);
          setOperations(session.operations ?? []);
          setInfo(reply.data);
          status.success(i18next.t("asset:Connection successful"));
          editorRef.current?.focus();
        } else if (reply.type === "error") {
          errorMessage = reply.message ?? "";
        } else if (reply.type === "result" && reply.result) {
          if (reply.id === runIdRef.current) {
            setResults((results) => [...results, reply.result!]);
            if (reply.result.error) {
              // the batch stops at a failed statement, which is shown
              setActiveResult(Number.MAX_SAFE_INTEGER);
            }
          }
        } else if (reply.type === "reply" && reply.id !== undefined) {
          const resolve = pendingRef.current.get(reply.id);
          pendingRef.current.delete(reply.id);
          resolve?.(reply);
        }
      };

      ws.onclose = () => {
        if (wsRef.current !== ws) {
          return;
        }
        status.dismiss();
        for (const resolve of pendingRef.current.values()) {
          resolve({type: "reply", error: errorMessage || i18next.t("asset:Connection closed")});
        }
        pendingRef.current.clear();
        if (!isConnected) {
          setFailure(errorMessage || NotReachable);
        } else {
          setClosed(true);
          if (errorMessage) {
            setFailure(errorMessage);
          } else {
            status.error(i18next.t("asset:Connection closed"));
          }
        }
      };
    });
  };

  React.useEffect(() => {
    if (!inTab) {
      document.title = assetName;
    }
    addClient?.({disconnect: () => wsRef.current?.close()});
    const releaseLock = holdPageLock();
    connect();

    return () => {
      releaseLock();
      const ws = wsRef.current;
      wsRef.current = null;
      ws?.close();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [assetId]);

  React.useEffect(() => {
    if (connected) {
      loadDatabases();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected]);

  React.useEffect(() => {
    if (info?.database) {
      loadTables(info.database);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [info?.database, protocol]);

  const reconnect = () => {
    setFailure(null);
    reloadIfUpdated(props).then((reloaded) => {
      if (!reloaded) {
        connect();
      }
    });
  };

  const run = (text: string) => {
    if (!connected || running || text.trim() === "") {
      return;
    }
    const id = nextIdRef.current;
    runIdRef.current = id;
    setRunning(true);
    setResults([]);
    setRunError("");
    setActiveResult(0);
    request("query", {sql: text}).then((reply) => {
      setRunning(false);
      if (reply.error) {
        setRunError(reply.error);
      }
    });
  };

  const runEditor = () => {
    const editor = editorRef.current;
    if (editor && editor.selectionStart !== editor.selectionEnd) {
      run(sql.slice(editor.selectionStart, editor.selectionEnd));
    } else {
      run(sql);
    }
  };

  const switchDatabase = (database: string) => {
    request("use", {database}).then((reply) => {
      if (reply.error) {
        Setting.showMessage("error", reply.error);
        return;
      }
      setInfo((info) => (info ? {...info, database: reply.data?.current ?? database} : info));
      if (protocol === "PostgreSQL") {
        setTables({});
        setColumns({});
      }
    });
  };

  const queryTable = (database: string, table: DatabaseTable) => {
    let name = quoteIdentifier(table.name, protocol);
    if (protocol === "MySQL" && database !== info?.database) {
      name = `${quoteIdentifier(database, protocol)}.${name}`;
    } else if (protocol === "PostgreSQL" && table.schema !== "public") {
      name = `${quoteIdentifier(table.schema, protocol)}.${name}`;
    }
    const text = `SELECT * FROM ${name} LIMIT 100;`;
    setSql(text);
    run(text);
  };

  const onEditorKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if ((e.ctrlKey || e.metaKey) && e.key === "Enter") {
      e.preventDefault();
      runEditor();
    } else if (e.key === "Tab" && !e.shiftKey && !e.ctrlKey && !e.altKey) {
      e.preventDefault();
      const editor = e.currentTarget;
      const {selectionStart, selectionEnd} = editor;
      const next = `${sql.slice(0, selectionStart)}  ${sql.slice(selectionEnd)}`;
      setSql(next);
      requestAnimationFrame(() => editor.setSelectionRange(selectionStart + 2, selectionStart + 2));
    }
  };

  const startResize = (e: React.MouseEvent) => {
    e.preventDefault();
    const startY = e.clientY;
    const startHeight = editorHeight;
    const onMove = (event: MouseEvent) => setEditorHeight(Math.max(80, Math.min(window.innerHeight - 200, startHeight + event.clientY - startY)));
    const onUp = () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
  };

  const canCopy = operations.includes("copy");
  const canExport = operations.includes("download");
  const result = results[Math.min(activeResult, results.length - 1)];

  return (
    <div className="flex h-full w-full flex-col bg-background text-foreground">
      <div className="flex h-11 shrink-0 items-center gap-3 border-b px-3 text-sm">
        <Database className="h-4 w-4 text-muted-foreground" />
        <span className="font-medium">{assetName}</span>
        {info ? <span className="hidden truncate text-xs text-muted-foreground md:inline">{protocol} {info.version}</span> : null}
        {info && "items" in databases ? (
          <select
            className="h-8 max-w-[220px] rounded-md border bg-background px-2 text-sm"
            value={info.database}
            disabled={!connected}
            aria-label={i18next.t("database:Database")}
            onChange={(e) => switchDatabase(e.target.value)}
          >
            {info.database === "" ? <option value="">({i18next.t("database:No database selected")})</option> : null}
            {databases.items.map((database) => <option key={database} value={database}>{database}</option>)}
          </select>
        ) : null}
        <span className="ml-auto flex items-center gap-2">
          {closed ? (
            <Button size="sm" variant="outline" className="h-8" onClick={reconnect}>
              <RefreshCw />
              {i18next.t("asset:Reconnect")}
            </Button>
          ) : null}
          <span className="hidden text-xs text-muted-foreground sm:inline">Ctrl+Enter</span>
          <Button size="sm" className="h-8" disabled={!connected || running} onClick={runEditor}>
            {running ? <Loader2 className="animate-spin" /> : <Play />}
            {i18next.t("database:Run")}
          </Button>
        </span>
      </div>

      <div className="flex min-h-0 flex-1">
        <div className="flex w-64 shrink-0 flex-col border-r">
          <div className="flex h-9 shrink-0 items-center justify-between border-b px-3 text-xs font-medium text-muted-foreground">
            {i18next.t("database:Databases")}
            <Button variant="ghost" size="icon" className="h-6 w-6" disabled={!connected} onClick={loadDatabases} aria-label={i18next.t("general:Refresh")}>
              <RefreshCw className="h-3.5 w-3.5" />
            </Button>
          </div>
          <div className="min-h-0 flex-1 overflow-auto py-1 text-sm">
            {"loading" in databases ? (
              connected ? <div className="px-3 py-1 text-muted-foreground">{i18next.t("general:Loading")}...</div> : null
            ) : "error" in databases ? (
              <div className="break-words px-3 py-1 text-destructive">{databases.error}</div>
            ) : (
              databases.items.map((database) => (
                <DatabaseNode
                  key={database}
                  database={database}
                  protocol={protocol}
                  current={database === info?.database}
                  tables={tables[database]}
                  columns={columns}
                  onExpand={() => {
                    if (protocol === "PostgreSQL" && database !== info?.database) {
                      switchDatabase(database);
                    } else {
                      loadTables(database);
                    }
                  }}
                  onCollapse={() => setTables((tables) => {
                    const next = {...tables};
                    delete next[database];
                    return next;
                  })}
                  onExpandTable={(table) => loadColumns(database, table)}
                  onCollapseTable={(table) => setColumns((columns) => {
                    const next = {...columns};
                    delete next[getTableKey(database, table)];
                    return next;
                  })}
                  onQueryTable={(table) => queryTable(database, table)}
                />
              ))
            )}
          </div>
        </div>

        <div className="flex min-w-0 flex-1 flex-col">
          <textarea
            ref={editorRef}
            value={sql}
            spellCheck={false}
            placeholder={i18next.t("database:Type SQL here, Ctrl+Enter runs the selected text or all of it")}
            style={{height: editorHeight}}
            className="w-full shrink-0 resize-none bg-background p-3 font-mono text-sm outline-none"
            onChange={(e) => setSql(e.target.value)}
            onKeyDown={onEditorKeyDown}
          />
          <div className="h-1.5 shrink-0 cursor-row-resize border-y bg-muted hover:bg-accent" onMouseDown={startResize} />
          <ResultPanel
            results={results}
            active={result}
            activeIndex={Math.min(activeResult, results.length - 1)}
            onSelect={setActiveResult}
            running={running}
            runError={runError}
            maxRows={info?.maxRows ?? 1000}
            canCopy={canCopy}
            canExport={canExport}
            sessionId={sessionId}
          />
        </div>
      </div>

      <FailureDialog assetName={assetName} message={failure} onReconnect={reconnect} onClose={() => closeView(props)} />
    </div>
  );
}

function DatabaseNode({
  database,
  protocol,
  current,
  tables,
  columns,
  onExpand,
  onCollapse,
  onExpandTable,
  onCollapseTable,
  onQueryTable,
}: {
  database: string;
  protocol: string;
  current: boolean;
  tables?: Loadable<DatabaseTable>;
  columns: Record<string, Loadable<DatabaseColumn>>;
  onExpand: () => void;
  onCollapse: () => void;
  onExpandTable: (table: DatabaseTable) => void;
  onCollapseTable: (table: DatabaseTable) => void;
  onQueryTable: (table: DatabaseTable) => void;
}) {
  const expanded = tables !== undefined;
  return (
    <div>
      <button
        type="button"
        className="flex w-full items-center gap-1 px-2 py-1 text-left hover:bg-accent"
        title={protocol === "PostgreSQL" && !current ? i18next.t("database:Switch to this database") : database}
        onClick={expanded ? onCollapse : onExpand}
      >
        {expanded ? <ChevronDown className="h-3.5 w-3.5 shrink-0" /> : <ChevronRight className="h-3.5 w-3.5 shrink-0" />}
        <Database className={cn("h-3.5 w-3.5 shrink-0", current ? "text-primary" : "text-muted-foreground")} />
        <span className={cn("truncate", current && "font-semibold")}>{database}</span>
      </button>
      {tables === undefined ? null : "loading" in tables ? (
        <div className="py-1 pl-9 text-xs text-muted-foreground">{i18next.t("general:Loading")}...</div>
      ) : "error" in tables ? (
        <div className="break-words py-1 pl-9 pr-2 text-xs text-destructive">{tables.error}</div>
      ) : tables.items.length === 0 ? (
        <div className="py-1 pl-9 text-xs text-muted-foreground">{i18next.t("database:No tables")}</div>
      ) : (
        tables.items.map((table) => {
          const key = getTableKey(database, table);
          const tableColumns = columns[key];
          const isView = table.type.toUpperCase().includes("VIEW");
          const label = protocol === "PostgreSQL" && table.schema !== "public" ? `${table.schema}.${table.name}` : table.name;
          return (
            <div key={key}>
              <div className="group flex items-center hover:bg-accent">
                <button
                  type="button"
                  className="flex min-w-0 flex-1 items-center gap-1 py-1 pl-5 text-left"
                  title={label}
                  onClick={() => (tableColumns ? onCollapseTable(table) : onExpandTable(table))}
                >
                  {tableColumns ? <ChevronDown className="h-3.5 w-3.5 shrink-0" /> : <ChevronRight className="h-3.5 w-3.5 shrink-0" />}
                  {isView ? <Eye className="h-3.5 w-3.5 shrink-0 text-muted-foreground" /> : <Table2 className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />}
                  <span className="truncate">{label}</span>
                </button>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      type="button"
                      className="mr-1 hidden rounded p-1 text-muted-foreground hover:text-foreground group-hover:block"
                      aria-label={i18next.t("database:Query this table")}
                      onClick={() => onQueryTable(table)}
                    >
                      <Play className="h-3 w-3" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent side="right">{i18next.t("database:Query this table")}</TooltipContent>
                </Tooltip>
              </div>
              {tableColumns === undefined ? null : "loading" in tableColumns ? (
                <div className="py-0.5 pl-12 text-xs text-muted-foreground">{i18next.t("general:Loading")}...</div>
              ) : "error" in tableColumns ? (
                <div className="break-words py-0.5 pl-12 pr-2 text-xs text-destructive">{tableColumns.error}</div>
              ) : (
                tableColumns.items.map((column) => (
                  <div key={column.name} className="flex items-center gap-1 py-0.5 pl-12 pr-2 text-xs" title={`${column.name} ${column.type}${column.nullable ? "" : " NOT NULL"}`}>
                    {column.key === "PRI" ? <KeyRound className="h-3 w-3 shrink-0 text-amber-500" /> : <span className="w-3 shrink-0" />}
                    <span className="truncate">{column.name}</span>
                    <span className="ml-auto shrink-0 pl-2 text-muted-foreground">{column.type}</span>
                  </div>
                ))
              )}
            </div>
          );
        })
      )}
    </div>
  );
}

function ResultPanel({
  results,
  active,
  activeIndex,
  onSelect,
  running,
  runError,
  maxRows,
  canCopy,
  canExport,
  sessionId,
}: {
  results: SqlResult[];
  active?: SqlResult;
  activeIndex: number;
  onSelect: (index: number) => void;
  running: boolean;
  runError: string;
  maxRows: number;
  canCopy: boolean;
  canExport: boolean;
  sessionId: string;
}) {
  const copy = (result: SqlResult) => {
    navigator.clipboard.writeText(toTsv(result)).then(
      () => Setting.showMessage("success", i18next.t("database:Copied")),
      (e) => Setting.showMessage("error", String(e)),
    );
  };

  const exportCsv = async(result: SqlResult) => {
    const res = await fetch(`${Setting.ServerUrl}/api/export-database-result?${qs.stringify({id: sessionId, result: result.id})}`, {credentials: "include"});
    if ((res.headers.get("Content-Type") ?? "").includes("json")) {
      const body = await res.json();
      Setting.showMessage("error", body.msg);
      return;
    }
    const blob = await res.blob();
    const name = /filename\*=UTF-8''([^;]+)/.exec(res.headers.get("Content-Disposition") ?? "")?.[1];
    const link = document.createElement("a");
    link.href = URL.createObjectURL(blob);
    link.download = name ? decodeURIComponent(name) : "result.csv";
    link.click();
    URL.revokeObjectURL(link.href);
  };

  if (results.length === 0) {
    return (
      <div className="flex min-h-0 flex-1 items-center justify-center p-4 text-sm text-muted-foreground">
        {running ? <Loader2 className="h-5 w-5 animate-spin" /> : runError ? <span className="text-destructive">{runError}</span> : i18next.t("database:The results show up here")}
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {results.length > 1 ? (
        <div className="flex shrink-0 gap-1 overflow-x-auto border-b px-2 pt-1">
          {results.map((result, i) => (
            <button
              key={i}
              type="button"
              title={result.statement}
              className={cn(
                "max-w-[220px] shrink-0 truncate rounded-t border border-b-0 px-2 py-1 font-mono text-xs",
                i === activeIndex ? "bg-background" : "bg-muted text-muted-foreground",
                result.error && "text-destructive",
              )}
              onClick={() => onSelect(i)}
            >
              {i + 1}. {result.statement}
            </button>
          ))}
          {running ? <Loader2 className="m-1 h-4 w-4 animate-spin text-muted-foreground" /> : null}
        </div>
      ) : null}
      {active ? (
        <>
          <div className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1 border-b px-3 py-1.5 text-xs">
            <ResultSummary result={active} maxRows={maxRows} />
            {active.columns ? (
              <span className="ml-auto flex gap-1">
                {canCopy ? (
                  <Button size="sm" variant="ghost" className="h-7" onClick={() => copy(active)}>
                    <Copy />
                    {i18next.t("database:Copy")}
                  </Button>
                ) : null}
                {canExport ? (
                  <Button size="sm" variant="ghost" className="h-7" onClick={() => exportCsv(active)}>
                    <Download />
                    {i18next.t("database:Export CSV")}
                  </Button>
                ) : null}
              </span>
            ) : null}
          </div>
          {active.columns ? (
            <div
              className={cn("min-h-0 flex-1 overflow-auto", !canCopy && "select-none")}
              onCopy={canCopy ? undefined : (e) => e.preventDefault()}
              onCut={canCopy ? undefined : (e) => e.preventDefault()}
            >
              <table className="border-separate border-spacing-0 font-mono text-xs">
                <thead className="sticky top-0 z-10 bg-muted">
                  <tr>
                    <th className="sticky left-0 border-b border-r bg-muted px-2 py-1 text-right font-normal text-muted-foreground">#</th>
                    {active.columns.map((column, i) => (
                      <th key={i} className="whitespace-nowrap border-b border-r px-2 py-1 text-left font-semibold">
                        {column.name}
                        {column.type ? <span className="ml-1.5 font-normal text-muted-foreground">{column.type.toLowerCase()}</span> : null}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {(active.rows ?? []).map((row, i) => (
                    <tr key={i} className="hover:bg-accent/50">
                      <td className="sticky left-0 border-b border-r bg-background px-2 py-0.5 text-right text-muted-foreground">{i + 1}</td>
                      {row.map((value, j) => (
                        <td key={j} className="max-w-[360px] truncate whitespace-nowrap border-b border-r px-2 py-0.5" title={value ?? "NULL"}>
                          {value === null ? <span className="italic text-muted-foreground">NULL</span> : value}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <div className="min-h-0 flex-1 overflow-auto p-3">
              <pre className="whitespace-pre-wrap break-all font-mono text-xs text-muted-foreground">{active.statement}</pre>
            </div>
          )}
        </>
      ) : null}
    </div>
  );
}

function ResultSummary({result, maxRows}: {result: SqlResult; maxRows: number}) {
  const duration = <span className="tabular-nums text-muted-foreground">{result.duration} ms</span>;
  if (result.action === "Block") {
    return (
      <span className="flex items-center gap-1.5 text-destructive">
        <ShieldAlert className="h-3.5 w-3.5" />
        {i18next.t("database:Blocked by the command rule")}: {result.rule}
      </span>
    );
  }

  const alert = result.action === "Alert" ? (
    <span className="flex items-center gap-1 text-amber-600 dark:text-amber-400">
      <TriangleAlert className="h-3.5 w-3.5" />
      {i18next.t("database:Matched an alert rule, recorded")}
    </span>
  ) : null;

  if (result.error) {
    return (
      <>
        <span className="break-all text-destructive">{result.error}</span>
        {duration}
        {alert}
      </>
    );
  }

  return (
    <>
      <span>
        {result.columns
          ? i18next.t("database:{{n}} row(s)", {n: result.rowCount})
          : i18next.t("database:{{n}} row(s) affected", {n: result.rowCount})}
      </span>
      {result.truncated ? (
        <span className="text-amber-600 dark:text-amber-400">
          {i18next.t("database:Only the first {{count}} rows are shown", {count: result.rows?.length ?? maxRows})}
        </span>
      ) : null}
      {duration}
      {alert}
    </>
  );
}
