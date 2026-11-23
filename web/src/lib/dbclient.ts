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

import type {Asset, SshGateway} from "@/backend/types";

/** Where a database client reaches an asset through the database proxy, with a database token as the user and the password. */
export interface DbConnection {
  dbType: "mysql" | "postgresql";
  host: string;
  port: string;
  token: string;
  database: string;
  name: string;
}

/** The port of the database proxy for the asset's protocol, "" when the proxy is off for it. */
export function getDbProxyPort(gateway: SshGateway | null | undefined, asset: Asset) {
  if (asset.protocol === "MySQL") {
    return gateway?.dbProxy?.mysqlPort ?? "";
  }
  if (asset.protocol === "PostgreSQL") {
    return gateway?.dbProxy?.postgresqlPort ?? "";
  }
  return "";
}

export function getDbConnection(gateway: SshGateway, asset: Asset, token: string): DbConnection {
  return {
    dbType: asset.protocol === "MySQL" ? "mysql" : "postgresql",
    host: gateway.dbProxy?.host || window.location.hostname,
    port: getDbProxyPort(gateway, asset),
    token,
    database: asset.database ?? "",
    name: asset.name,
  };
}

/** The link that has Casvisor's helper on the user's machine open the connection in a database client there. */
export function getHelperUrl(c: DbConnection) {
  const params = [
    ["type", "db"],
    ["dbtype", c.dbType],
    ["host", c.host],
    ["port", c.port],
    ["user", c.token],
    ["password", c.token],
    ["database", c.database],
    ["name", c.name],
  ];
  return `casvisor://launch?${params.map(([key, value]) => `${key}=${encodeURIComponent(value)}`).join("&")}`;
}

function quoteShell(value: string) {
  return /^[\w.:/@=-]*$/.test(value) ? value : `'${value.replace(/'/g, "'\\''")}'`;
}

/** "mysql -h ... -P 3306 -u <token> -p<token> db" or "psql postgresql://<token>:<token>@...:5432/db". */
export function getCliCommand(c: DbConnection) {
  if (c.dbType === "mysql") {
    const database = c.database ? ` ${quoteShell(c.database)}` : "";
    return `mysql -h ${quoteShell(c.host)} -P ${c.port} -u ${c.token} -p${c.token}${database}`;
  }
  return `psql "postgresql://${c.token}:${c.token}@${c.host}:${c.port}/${encodeURIComponent(c.database)}?sslmode=prefer"`;
}

export function getJdbcUrl(c: DbConnection) {
  const database = encodeURIComponent(c.database);
  if (c.dbType === "mysql") {
    return `jdbc:mysql://${c.host}:${c.port}/${database}?user=${c.token}&password=${c.token}&sslMode=PREFERRED`;
  }
  return `jdbc:postgresql://${c.host}:${c.port}/${database}?user=${c.token}&password=${c.token}&sslmode=prefer`;
}

/** The connection as DBeaver's command line takes it: dbeaver -con "driver=mysql|host=...". */
export function getDbeaverConnectionSpec(c: DbConnection) {
  const fields = [
    ["driver", c.dbType === "mysql" ? "mysql" : "postgresql"],
    ["name", `${c.name} (Casvisor)`],
    ["host", c.host],
    ["port", c.port],
    ["database", c.database],
    ["user", c.token],
    ["password", c.token],
    ["savePassword", "true"],
    ["connect", "true"],
  ];
  return fields.filter(([, value]) => value !== "").map(([key, value]) => `${key}=${value.replace(/[|"]/g, "")}`).join("|");
}

/** A data-sources.json of DBeaver with the connection, which DBeaver reads from its workspace. */
export function getDbeaverDataSources(c: DbConnection) {
  const id = `casvisor-${c.dbType}-${c.name}`;
  const url = c.dbType === "mysql" ? `jdbc:mysql://${c.host}:${c.port}/${c.database}` : `jdbc:postgresql://${c.host}:${c.port}/${c.database}`;
  return JSON.stringify({
    folders: {},
    connections: {
      [id]: {
        "provider": c.dbType === "mysql" ? "mysql" : "postgresql",
        "driver": c.dbType === "mysql" ? "mysql8" : "postgres-jdbc",
        "name": `${c.name} (Casvisor)`,
        "save-password": true,
        "configuration": {
          "host": c.host,
          "port": c.port,
          "database": c.database,
          "url": url,
          "user": c.token,
          "password": c.token,
          "type": "dev",
          "auth-model": "native",
        },
      },
    },
  }, null, 2);
}

export function downloadText(fileName: string, text: string, type = "application/json") {
  const link = document.createElement("a");
  link.href = URL.createObjectURL(new Blob([text], {type}));
  link.download = fileName;
  link.click();
  setTimeout(() => URL.revokeObjectURL(link.href), 10000);
}
