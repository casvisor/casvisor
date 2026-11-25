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
import {useNavigate, useParams} from "react-router-dom";
import * as WebhookBackend from "@/backend/WebhookBackend";
import type {Webhook} from "@/backend/types";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import {Switch} from "@/components/ui/switch";
import {Loading} from "@/components/common/Loading";
import {MultiSelect} from "@/components/common/MultiSelect";
import {SelectField} from "@/components/common/SelectField";
import {UnauthorizedPage} from "@/components/common/UnauthorizedPage";
import {EditPageShell} from "@/components/crud/EditPageShell";
import {FormRow} from "@/components/crud/FormRow";
import {useEditRecord} from "@/hooks/use-edit-record";
import {getModeTitleKey, submitEdit} from "@/lib/crud";
import * as Setting from "@/lib/setting";
import NotFoundPage from "@/pages/NotFoundPage";

const urlPlaceholders: Record<string, string> = {
  JSON: "https://example.com/casvisor-webhook",
  DingTalk: "https://oapi.dingtalk.com/robot/send?access_token=...",
  Feishu: "https://open.feishu.cn/open-apis/bot/v2/hook/...",
  WeCom: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=...",
  Slack: "https://hooks.slack.com/services/...",
};

const secretTips: Record<string, string> = {
  JSON: "webhook:Secret - Tooltip",
  DingTalk: "webhook:Secret DingTalk - Tooltip",
  Feishu: "webhook:Secret Feishu - Tooltip",
};

export default function WebhookEditPage() {
  const {owner = "", name = ""} = useParams();
  const navigate = useNavigate();
  const [saving, setSaving] = React.useState(false);
  const [testing, setTesting] = React.useState(false);

  const {record: webhook, updateField, loading, notFound, denied, mode} = useEditRecord<Webhook>({
    fetch: () => WebhookBackend.getWebhook(owner, name),
    deps: [owner, name],
  });

  if (loading) {
    return <Loading />;
  }
  if (denied) {
    return <UnauthorizedPage />;
  }
  if (notFound || !webhook) {
    return <NotFoundPage />;
  }

  const save = (exitAfterSave: boolean) => {
    setSaving(true);
    return submitEdit({
      mode,
      record: webhook,
      add: WebhookBackend.addWebhook,
      update: (record) => WebhookBackend.updateWebhook(owner, name, record),
      onSaved: (record) => {
        if (exitAfterSave) {
          navigate("/webhooks");
        } else if (record.name !== name) {
          navigate(`/webhooks/${Setting.getIdPath(record.owner, record.name)}`, {replace: true});
        }
      },
      onFailed: () => updateField("name", name),
    }).finally(() => setSaving(false));
  };

  // The test sends what is in the form without saving it.
  const test = async() => {
    setTesting(true);
    try {
      const res = await WebhookBackend.testWebhook(mode === "add" ? "" : `${owner}/${name}`, webhook);
      if (res.status === "ok") {
        Setting.showMessage("success", i18next.t("webhook:Test message sent"));
      } else {
        Setting.showMessage("error", `${i18next.t("webhook:Failed to send")}: ${res.msg}`);
      }
    } catch (error: any) {
      Setting.showMessage("error", `${i18next.t("general:Failed to connect to server")}: ${error}`);
    } finally {
      setTesting(false);
    }
  };

  const secretTip = secretTips[webhook.type];

  return (
    <EditPageShell
      title={i18next.t(getModeTitleKey("webhook:Edit Webhook", mode))}
      description={i18next.t("general:Notify chat groups, mailboxes or your own systems of blocked commands, reviews, jobs and account risks")}
      mode={mode}
      backTo="/webhooks"
      onSave={save}
      saving={saving}
      remove={() => WebhookBackend.deleteWebhook(webhook)}
      extraActions={mode !== "view" ? (
        <Button variant="outline" disabled={testing || saving} onClick={test}>
          {i18next.t("webhook:Send test message")}
        </Button>
      ) : null}
      grid
    >
      <FormRow labelKey="general:Organization">
        <Input value={webhook.owner} disabled />
      </FormRow>
      <FormRow labelKey="general:Name">
        <Input value={webhook.name} onChange={(e) => updateField("name", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Display name">
        <Input value={webhook.displayName} onChange={(e) => updateField("displayName", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Description">
        <Input value={webhook.description} onChange={(e) => updateField("description", e.target.value)} />
      </FormRow>
      <FormRow label={i18next.t("general:Type")} tooltip={i18next.t("webhook:Type - Tooltip")}>
        <SelectField
          value={webhook.type}
          options={WebhookBackend.WebhookTypes.map((type) => ({id: type, name: i18next.t(`webhook:${type}`)}))}
          onChange={(value) => updateField("type", value)}
        />
      </FormRow>
      {webhook.type === "Email" ? (
        <>
          <FormRow label={i18next.t("webhook:SMTP server")} tooltip={i18next.t("webhook:SMTP server - Tooltip")}>
            <div className="flex w-full gap-2">
              <Input value={webhook.smtpHost ?? ""} placeholder="smtp.example.com" onChange={(e) => updateField("smtpHost", e.target.value.trim())} />
              <Input
                className="w-24 shrink-0"
                type="number"
                value={webhook.smtpPort || ""}
                placeholder="465"
                onChange={(e) => updateField("smtpPort", Number(e.target.value) || 0)}
              />
            </div>
          </FormRow>
          <FormRow label={i18next.t("webhook:SMTP username")} tooltip={i18next.t("webhook:SMTP username - Tooltip")}>
            <Input value={webhook.smtpUsername ?? ""} placeholder="casvisor@example.com" onChange={(e) => updateField("smtpUsername", e.target.value.trim())} />
          </FormRow>
          <FormRow label={i18next.t("webhook:SMTP password")} tooltip={i18next.t("webhook:SMTP password - Tooltip")}>
            <Input type="password" autoComplete="new-password" value={webhook.secret} onChange={(e) => updateField("secret", e.target.value.trim())} />
          </FormRow>
          <FormRow label={i18next.t("webhook:Sender")} tooltip={i18next.t("webhook:Sender - Tooltip")}>
            <Input value={webhook.smtpFrom ?? ""} placeholder="Casvisor <casvisor@example.com>" onChange={(e) => updateField("smtpFrom", e.target.value)} />
          </FormRow>
          <FormRow label={i18next.t("webhook:Recipients")} tooltip={i18next.t("webhook:Recipients - Tooltip")}>
            <Input
              value={(webhook.recipients ?? []).join(", ")}
              placeholder="ops@example.com, security@example.com"
              onChange={(e) => updateField("recipients", e.target.value.split(/[,;\s]+/).filter((address, index, all) => address !== "" || index === all.length - 1))}
            />
          </FormRow>
        </>
      ) : (
        <FormRow label={i18next.t("webhook:URL")} tooltip={i18next.t("webhook:URL - Tooltip")}>
          <Input value={webhook.url} placeholder={urlPlaceholders[webhook.type]} onChange={(e) => updateField("url", e.target.value.trim())} />
        </FormRow>
      )}
      {secretTip ? (
        <FormRow label={i18next.t("webhook:Secret")} tooltip={i18next.t(secretTip)}>
          <Input type="password" autoComplete="new-password" value={webhook.secret} onChange={(e) => updateField("secret", e.target.value.trim())} />
        </FormRow>
      ) : null}
      <FormRow label={i18next.t("webhook:Events")} tooltip={i18next.t("webhook:Events - Tooltip")}>
        <MultiSelect
          value={webhook.events ?? []}
          options={WebhookBackend.WebhookEvents.map((event) => ({value: event, label: i18next.t(`webhook:${event}`)}))}
          onChange={(value) => updateField("events", value)}
        />
      </FormRow>
      <FormRow labelKey="permission:Is enabled">
        <Switch checked={webhook.isEnabled} onCheckedChange={(checked) => updateField("isEnabled", checked)} />
      </FormRow>
    </EditPageShell>
  );
}
