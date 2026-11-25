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
import * as AssetBackend from "@/backend/AssetBackend";
import * as ConnectRuleBackend from "@/backend/ConnectRuleBackend";
import * as PermissionBackend from "@/backend/PermissionBackend";
import type {Asset, ConnectRule, Principal} from "@/backend/types";
import {Input} from "@/components/ui/input";
import {Switch} from "@/components/ui/switch";
import {Loading} from "@/components/common/Loading";
import {MultiSelect, type MultiSelectOption} from "@/components/common/MultiSelect";
import {SelectField} from "@/components/common/SelectField";
import {UnauthorizedPage} from "@/components/common/UnauthorizedPage";
import {EditPageShell} from "@/components/crud/EditPageShell";
import {FormRow} from "@/components/crud/FormRow";
import {useEditRecord} from "@/hooks/use-edit-record";
import {getModeTitleKey, submitEdit} from "@/lib/crud";
import NotFoundPage from "@/pages/NotFoundPage";
import * as Setting from "@/lib/setting";

function getPrincipalOptions(principals: Principal[]): MultiSelectOption[] {
  return [{name: "*", displayName: i18next.t("permission:All")}, ...principals].map((item) => ({
    value: item.name,
    label: item.displayName && item.displayName !== item.name ? `${item.displayName} (${item.name})` : item.name,
    keywords: item.displayName,
  }));
}

export default function ConnectRuleEditPage() {
  const {owner = "", name = ""} = useParams();
  const navigate = useNavigate();
  const [saving, setSaving] = React.useState(false);
  const [users, setUsers] = React.useState<Principal[]>([]);
  const [groups, setGroups] = React.useState<Principal[]>([]);
  const [assets, setAssets] = React.useState<Asset[]>([]);

  const {record: rule, updateField, loading, notFound, denied, mode} = useEditRecord<ConnectRule>({
    fetch: () => ConnectRuleBackend.getConnectRule(owner, name),
    deps: [owner, name],
  });

  React.useEffect(() => {
    PermissionBackend.getUsers().then((res) => res.status === "ok" && setUsers(res.data ?? []));
    PermissionBackend.getGroups().then((res) => res.status === "ok" && setGroups(res.data ?? []));
    AssetBackend.getAssets(owner).then((res) => res.status === "ok" && setAssets(res.data ?? []));
  }, [owner]);

  const tagOptions = React.useMemo(
    () => [...new Set(assets.map((asset) => asset.tag).filter(Boolean))].map((tag) => ({value: tag, label: tag})),
    [assets],
  );

  if (loading) {
    return <Loading />;
  }
  if (denied) {
    return <UnauthorizedPage />;
  }
  if (notFound || !rule) {
    return <NotFoundPage />;
  }

  const save = (exitAfterSave: boolean) => {
    setSaving(true);
    return submitEdit({
      mode,
      record: {...rule, priority: Number(rule.priority) || 0},
      add: ConnectRuleBackend.addConnectRule,
      update: (record) => ConnectRuleBackend.updateConnectRule(owner, name, record),
      onSaved: (record) => {
        if (exitAfterSave) {
          navigate("/connect-rules");
        } else if (record.name !== name) {
          navigate(`/connect-rules/${Setting.getIdPath(record.owner, record.name)}`, {replace: true});
        }
      },
      onFailed: () => updateField("name", name),
    }).finally(() => setSaving(false));
  };

  return (
    <EditPageShell
      title={i18next.t(getModeTitleKey("connectRule:Edit Connect Rule", mode))}
      description={i18next.t("general:Allow or reject ways to connect, such as the browser, SSH clients, SFTP, port forwarding and Remote Desktop Connection")}
      mode={mode}
      backTo="/connect-rules"
      onSave={save}
      saving={saving}
      remove={() => ConnectRuleBackend.deleteConnectRule(rule)}
      grid
    >
      <FormRow labelKey="general:Organization">
        <Input value={rule.owner} disabled />
      </FormRow>
      <FormRow labelKey="general:Name">
        <Input value={rule.name} onChange={(e) => updateField("name", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Display name">
        <Input value={rule.displayName} onChange={(e) => updateField("displayName", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Description">
        <Input value={rule.description} onChange={(e) => updateField("description", e.target.value)} />
      </FormRow>
      <FormRow label={i18next.t("loginRule:Priority")} tooltip={i18next.t("connectRule:Priority - Tooltip")}>
        <Input type="number" value={rule.priority} onChange={(e) => updateField("priority", Number(e.target.value))} />
      </FormRow>
      <FormRow labelKey="command:Action">
        <SelectField
          value={rule.action}
          options={ConnectRuleBackend.ConnectActions.map((action) => ({id: action, name: i18next.t(`connectRule:${action}`)}))}
          onChange={(value) => updateField("action", value)}
        />
      </FormRow>
      <FormRow label={i18next.t("connectRule:Methods")} tooltip={i18next.t("connectRule:Methods - Tooltip")}>
        <MultiSelect
          value={rule.methods ?? []}
          placeholder={i18next.t("connectRule:Every method")}
          options={ConnectRuleBackend.ConnectMethods.map((method) => ({value: method, label: i18next.t(`connectRule:${method}`)}))}
          onChange={(value) => updateField("methods", value)}
        />
      </FormRow>
      <FormRow label={i18next.t("connectRule:Accounts")} tooltip={i18next.t("connectRule:Accounts - Tooltip")}>
        <MultiSelect creatable value={rule.accounts ?? []} placeholder={i18next.t("connectRule:Every account")} options={[]} onChange={(value) => updateField("accounts", value)} />
      </FormRow>
      <FormRow labelKey="permission:Users">
        <MultiSelect creatable value={rule.users ?? []} options={getPrincipalOptions(users)} onChange={(value) => updateField("users", value)} />
      </FormRow>
      <FormRow labelKey="permission:Groups">
        <MultiSelect creatable value={rule.groups ?? []} options={getPrincipalOptions(groups)} onChange={(value) => updateField("groups", value)} />
      </FormRow>
      <FormRow label={i18next.t("general:Assets")} tooltip={i18next.t("permission:Assets - Tooltip")}>
        <MultiSelect value={rule.assets ?? []} options={getPrincipalOptions(assets)} onChange={(value) => updateField("assets", value)} />
      </FormRow>
      <FormRow labelKey="permission:Asset tags">
        <MultiSelect creatable value={rule.assetTags ?? []} options={tagOptions} onChange={(value) => updateField("assetTags", value)} />
      </FormRow>
      <FormRow labelKey="permission:Is enabled">
        <Switch checked={rule.isEnabled} onCheckedChange={(checked) => updateField("isEnabled", checked)} />
      </FormRow>
    </EditPageShell>
  );
}
