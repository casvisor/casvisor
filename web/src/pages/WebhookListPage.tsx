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
import * as WebhookBackend from "@/backend/WebhookBackend";
import type {Webhook} from "@/backend/types";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {boolColumn, linkColumn, textColumn} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {Badge} from "@/components/ui/badge";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";

export default function WebhookListPage() {
  const {account} = useAccount();
  const owner = Setting.getRequestOrganization(account);

  const fetchWebhooks = React.useCallback(
    (query: TableQuery) =>
      WebhookBackend.getWebhooks(owner, {
        page: query.page,
        pageSize: query.pageSize,
        field: query.searchedColumn,
        value: query.searchText,
        sortField: query.sortField,
        sortOrder: query.sortOrder,
      }),
    [owner],
  );

  const newWebhook = (): Webhook => ({
    owner: account!.owner,
    name: `webhook_${Setting.getRandomName()}`,
    createdTime: dayjs().format(),
    updatedTime: dayjs().format(),
    displayName: `New Webhook - ${Setting.getRandomName()}`,
    description: "",
    type: "JSON",
    url: "https://example.com/casvisor-webhook",
    secret: "",
    events: [...WebhookBackend.WebhookEvents],
    isEnabled: false,
  });

  const columns: ColumnDef<Webhook>[] = [
    linkColumn<Webhook>({dataIndex: "name", width: 170, to: (webhook) => `/webhooks/${webhook.owner}/${webhook.name}`}),
    textColumn<Webhook>({dataIndex: "displayName", title: i18next.t("general:Display name"), width: 180, searchable: true}),
    {
      dataIndex: "type",
      title: i18next.t("general:Type"),
      width: 110,
      render: (value) => i18next.t(`webhook:${value}`),
    },
    textColumn<Webhook>({dataIndex: "url", title: i18next.t("webhook:URL"), width: 260, searchable: true}),
    {
      dataIndex: "events",
      title: i18next.t("webhook:Events"),
      render: (value: string[] | null) => (
        <div className="flex flex-wrap gap-1">
          {(value ?? []).map((event) => <Badge key={event} variant="secondary">{i18next.t(`webhook:${event}`)}</Badge>)}
        </div>
      ),
    },
    boolColumn<Webhook>({dataIndex: "isEnabled", title: i18next.t("permission:Is enabled"), width: 120}),
  ];

  return (
    <CrudListPage<Webhook>
      title={i18next.t("general:Webhooks")}
      description={i18next.t("general:Notify chat groups or your own systems of blocked commands, access requests, and sign-ins and commands waiting for review")}
      columns={columns}
      fetch={fetchWebhooks}
      deps={[owner]}
      newRecord={newWebhook}
      add={WebhookBackend.addWebhook}
      remove={WebhookBackend.deleteWebhook}
      editUrl={(webhook) => `/webhooks/${webhook.owner}/${webhook.name}`}
    />
  );
}
