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
import {RefreshCw} from "lucide-react";
import * as SessionBackend from "@/backend/SessionBackend";
import type {DatabaseToken} from "@/backend/types";
import {ConfirmButton} from "@/components/common/ConfirmButton";
import {PageHeader} from "@/components/crud/PageHeader";
import {Button} from "@/components/ui/button";
import {Card, CardContent, CardDescription, CardHeader, CardTitle} from "@/components/ui/card";
import {Table, TableBody, TableCell, TableHead, TableHeader, TableRow} from "@/components/ui/table";
import {useAccount} from "@/hooks/use-account";
import {useGateways} from "@/lib/gateway";
import * as Setting from "@/lib/setting";

/** The database tokens that database clients sign in to the database proxy with, which can be revoked here. */
export default function DatabaseTokenListPage() {
  const {account} = useAccount();
  const gateway = useGateways();
  const isAdmin = Setting.isAdminUser(account);
  const [tokens, setTokens] = React.useState<DatabaseToken[]>([]);
  const [loading, setLoading] = React.useState(true);

  const refresh = React.useCallback(() => {
    setLoading(true);
    SessionBackend.getDatabaseTokens().then((res) => {
      setLoading(false);
      if (res.status === "ok") {
        setTokens(res.data ?? []);
      } else {
        Setting.showMessage("error", res.msg);
      }
    });
  }, []);

  React.useEffect(() => {
    refresh();
  }, [refresh]);

  const revoke = (token: DatabaseToken) => SessionBackend.revokeDatabaseToken(token.id).then((res) => {
    if (res.status === "ok") {
      refresh();
    } else {
      Setting.showMessage("error", res.msg);
    }
  });

  const proxy = gateway?.dbProxy;
  const host = proxy?.host || window.location.hostname;
  const isEnabled = !!(proxy?.mysqlPort || proxy?.postgresqlPort);
  const columnCount = isAdmin ? 8 : 7;

  return (
    <div className="space-y-4">
      <PageHeader
        title={i18next.t("general:Database tokens")}
        description={i18next.t("general:Connect to MySQL and PostgreSQL assets with DBeaver, Navicat, DataGrip, mysql or psql")}
        actions={(
          <Button variant="outline" onClick={refresh}>
            <RefreshCw />
            {i18next.t("general:Refresh")}
          </Button>
        )}
      />

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{i18next.t("sshKey:How to connect")}</CardTitle>
          <CardDescription>
            {isEnabled
              ? i18next.t("database:Get a token from the local client menu of a MySQL or PostgreSQL asset, then sign in with it as both the user and the password")
              : i18next.t("database:The database proxy is not enabled, set dbProxyMysqlPort or dbProxyPostgresqlPort in conf/app.conf to enable it")}
          </CardDescription>
        </CardHeader>
        {isEnabled ? (
          <CardContent className="space-y-1 text-sm">
            {proxy?.mysqlPort ? <p>MySQL: <span className="font-mono">{host}:{proxy.mysqlPort}</span></p> : null}
            {proxy?.postgresqlPort ? <p>PostgreSQL: <span className="font-mono">{host}:{proxy.postgresqlPort}</span></p> : null}
            <p className="pt-1 text-muted-foreground">
              {i18next.t("database:Each connection is a session, its statements are checked against the command rules and recorded; revoking a token disconnects its sessions")}
            </p>
          </CardContent>
        ) : null}
      </Card>

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{i18next.t("database:Token")}</TableHead>
              {isAdmin ? <TableHead>{i18next.t("general:User")}</TableHead> : null}
              <TableHead>{i18next.t("general:Asset")}</TableHead>
              <TableHead>{i18next.t("general:Created time")}</TableHead>
              <TableHead>{i18next.t("database:Expire time")}</TableHead>
              <TableHead>{i18next.t("sshKey:Last used time")}</TableHead>
              <TableHead className="w-24">{i18next.t("general:Sessions")}</TableHead>
              <TableHead className="w-24" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {tokens.length === 0 ? (
              <TableRow>
                <TableCell colSpan={columnCount} className="h-20 text-center text-muted-foreground">
                  {loading ? i18next.t("general:Loading") : i18next.t("database:No database tokens")}
                </TableCell>
              </TableRow>
            ) : tokens.map((token) => (
              <TableRow key={token.id}>
                <TableCell className="font-mono text-xs">{token.hint}</TableCell>
                {isAdmin ? <TableCell>{token.user}</TableCell> : null}
                <TableCell>
                  {token.asset}
                  {token.account ? <span className="text-muted-foreground"> ({token.account})</span> : null}
                </TableCell>
                <TableCell>{Setting.getFormattedDate(token.createdTime)}</TableCell>
                <TableCell>{Setting.getFormattedDate(token.expireTime)}</TableCell>
                <TableCell>
                  {token.lastUsedTime ? `${Setting.getFormattedDate(token.lastUsedTime)} ${token.lastUsedIp}` : "-"}
                </TableCell>
                <TableCell className="tabular-nums">{token.sessions}</TableCell>
                <TableCell className="text-right">
                  <ConfirmButton size="sm" variant="destructive" onConfirm={() => revoke(token)}>
                    {i18next.t("database:Revoke")}
                  </ConfirmButton>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>
    </div>
  );
}
