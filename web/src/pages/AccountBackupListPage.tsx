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
import {Link} from "react-router-dom";
import {ArchiveRestore} from "lucide-react";
import * as AccountBackupBackend from "@/backend/AccountBackupBackend";
import type {AccountBackup} from "@/backend/types";
import {RestoreBackupDialog} from "@/components/account/RestoreBackupDialog";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {boolColumn} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {Badge} from "@/components/ui/badge";
import {Button} from "@/components/ui/button";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";

export function BackupRunBadge({status}: {status: string}) {
  if (!status) {
    return null;
  }
  return <Badge variant={status === "Succeeded" ? "success" : "destructive"}>{i18next.t(`accountBackup:${status}`)}</Badge>;
}

export async function sendBackupNow(backup: AccountBackup, onDone?: () => void) {
  const res = await AccountBackupBackend.runAccountBackup(backup.owner, backup.name);
  if (res.status === "ok") {
    Setting.showMessage("success", i18next.t("accountBackup:The backup was sent"));
  } else {
    Setting.showMessage("error", res.msg);
  }
  onDone?.();
}

export async function downloadBackup(backup: AccountBackup) {
  const error = await AccountBackupBackend.downloadAccountBackup(backup.owner, backup.name);
  if (error) {
    Setting.showMessage("error", error);
  }
}

export default function AccountBackupListPage() {
  const {account} = useAccount();
  const owner = Setting.getRequestOrganization(account);
  const [restoring, setRestoring] = React.useState(false);
  const [nonce, setNonce] = React.useState(0);

  const fetchBackups = React.useCallback(
    (query: TableQuery) =>
      AccountBackupBackend.getAccountBackups(owner, {
        page: query.page,
        pageSize: query.pageSize,
        field: query.searchedColumn,
        value: query.searchText,
        sortField: query.sortField,
        sortOrder: query.sortOrder,
      }),
    [owner],
  );

  const newBackup = (): AccountBackup => ({
    owner: account!.owner,
    name: `backup_${Setting.getRandomName()}`,
    createdTime: dayjs().format(),
    updatedTime: dayjs().format(),
    displayName: "",
    description: "",
    includeAssets: true,
    password: "",
    webhooks: [],
    cron: "0 2 * * 0",
    timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone ?? "",
    isEnabled: true,
    nextRunTime: "",
    lastRunTime: "",
    lastRunStatus: "",
    lastRunMessage: "",
    lastAccountCount: 0,
  });

  const columns: ColumnDef<AccountBackup>[] = [
    {
      dataIndex: "displayName",
      title: i18next.t("general:Name"),
      width: 200,
      searchable: true,
      render: (value, backup) => (
        <Link to={`/account-backups/${Setting.getIdPath(backup.owner, backup.name)}`} className="font-medium underline-offset-4 hover:underline">
          {value || backup.name}
        </Link>
      ),
    },
    {
      dataIndex: "includeAssets",
      title: i18next.t("accountBackup:Contents"),
      width: 170,
      render: (value: boolean) => (value ? i18next.t("accountBackup:Accounts and assets") : i18next.t("accountBackup:Accounts only")),
    },
    {
      dataIndex: "webhooks",
      title: i18next.t("accountBackup:Send with"),
      width: 180,
      render: (value: string[] | null) => (
        <div className="flex flex-wrap gap-1">
          {(value ?? []).map((name) => <Badge key={name} variant="secondary">{name}</Badge>)}
        </div>
      ),
    },
    {
      dataIndex: "cron",
      title: i18next.t("job:Schedule"),
      width: 170,
      render: (value, backup) => (value ? (
        <div className="text-xs">
          <code className="font-mono">{value}</code>
          {backup.nextRunTime ? <div className="text-muted-foreground">{i18next.t("job:Next run")}: {dayjs(backup.nextRunTime).format("MM-DD HH:mm")}</div> : null}
        </div>
      ) : <span className="text-xs text-muted-foreground">{i18next.t("accountBackup:By hand only")}</span>),
    },
    {
      dataIndex: "lastRunTime",
      title: i18next.t("job:Last run"),
      width: 260,
      render: (value: string, backup) => (value ? (
        <div className="text-xs">
          <div className="flex items-center gap-1.5">
            <BackupRunBadge status={backup.lastRunStatus} />
            <span className="tabular-nums text-muted-foreground">{dayjs(value).format("YYYY-MM-DD HH:mm")}</span>
            {backup.lastRunStatus === "Succeeded" ? <span className="text-muted-foreground">{i18next.t("accountBackup:{{count}} accounts", {count: backup.lastAccountCount})}</span> : null}
          </div>
          {backup.lastRunMessage ? <div className="mt-0.5 line-clamp-2 break-all text-muted-foreground" title={backup.lastRunMessage}>{backup.lastRunMessage}</div> : null}
        </div>
      ) : <span className="text-xs text-muted-foreground">{i18next.t("job:Not run yet")}</span>),
    },
    boolColumn<AccountBackup>({dataIndex: "isEnabled", title: i18next.t("permission:Is enabled"), width: 110}),
  ];

  return (
    <>
      <CrudListPage<AccountBackup>
        key={nonce}
        title={i18next.t("general:Account backups")}
        description={i18next.t("general:Zip files of the passwords and private keys, encrypted, to download or to email on a schedule")}
        columns={columns}
        fetch={fetchBackups}
        deps={[owner]}
        newRecord={newBackup}
        remove={AccountBackupBackend.deleteAccountBackup}
        editUrl={(backup) => `/account-backups/${Setting.getIdPath(backup.owner, backup.name)}`}
        actionColumnWidth={240}
        rowActions={(backup) => [
          {key: "download", label: i18next.t("accountBackup:Download"), onSelect: () => downloadBackup(backup)},
          ...((backup.webhooks ?? []).length > 0 ? [{key: "send", label: i18next.t("accountBackup:Send now"), onSelect: () => sendBackupNow(backup, () => setNonce((n) => n + 1))}] : []),
        ]}
        toolbar={
          <Button variant="outline" onClick={() => setRestoring(true)}>
            <ArchiveRestore />
            {i18next.t("accountBackup:Restore a backup")}
          </Button>
        }
      />
      <RestoreBackupDialog owner={owner} open={restoring} onOpenChange={setRestoring} />
    </>
  );
}
