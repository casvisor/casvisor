// Copyright 2023 The casbin Authors. All Rights Reserved.
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
import {useNavigate, useParams} from "react-router-dom";
import * as AssetBackend from "@/backend/AssetBackend";
import * as PermissionBackend from "@/backend/PermissionBackend";
import type {Asset, Permission, Principal} from "@/backend/types";
import {Input} from "@/components/ui/input";
import {Switch} from "@/components/ui/switch";
import {Loading} from "@/components/common/Loading";
import {MultiSelect, type MultiSelectOption} from "@/components/common/MultiSelect";
import {TagsInput} from "@/components/common/TagsInput";
import {UnauthorizedPage} from "@/components/common/UnauthorizedPage";
import {EditPageShell} from "@/components/crud/EditPageShell";
import {FormRow} from "@/components/crud/FormRow";
import {useEditRecord} from "@/hooks/use-edit-record";
import {PermissionActions} from "@/lib/asset";
import {getModeTitleKey, submitEdit} from "@/lib/crud";
import NotFoundPage from "@/pages/NotFoundPage";

/** "*" grants every user, group or asset, the way the antd page offered it first. */
function getPrincipalOptions(principals: Principal[]): MultiSelectOption[] {
  return [{name: "*", displayName: i18next.t("permission:All")}, ...principals].map((item) => ({
    value: item.name,
    label: item.displayName && item.displayName !== item.name ? `${item.displayName} (${item.name})` : item.name,
    keywords: item.displayName,
  }));
}

function getActionLabel(action: string) {
  const key = `permission:${action}`;
  const label = i18next.t(key);
  return label === key || label === action ? action : label;
}

/** A datetime-local control over the RFC 3339 string the backend stores, "" meaning no limit. */
function TimeInput({value, onChange}: {value: string; onChange: (value: string) => void}) {
  const parsed = value ? dayjs(value) : null;
  return (
    <Input
      type="datetime-local"
      step={1}
      value={parsed && parsed.isValid() ? parsed.format("YYYY-MM-DDTHH:mm:ss") : ""}
      onChange={(e) => onChange(e.target.value ? dayjs(e.target.value).format() : "")}
    />
  );
}

export default function PermissionEditPage() {
  const {owner = "", name = ""} = useParams();
  const navigate = useNavigate();
  const [saving, setSaving] = React.useState(false);
  const [users, setUsers] = React.useState<Principal[]>([]);
  const [groups, setGroups] = React.useState<Principal[]>([]);
  const [assets, setAssets] = React.useState<Asset[]>([]);

  const {record: permission, updateField, loading, notFound, denied, mode} = useEditRecord<Permission>({
    fetch: () => PermissionBackend.getPermission(owner, name),
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
  if (notFound || !permission) {
    return <NotFoundPage />;
  }

  const save = (exitAfterSave: boolean) => {
    setSaving(true);
    return submitEdit({
      mode,
      record: permission,
      add: PermissionBackend.addPermission,
      update: (record) => PermissionBackend.updatePermission(owner, name, record),
      onSaved: (record) => {
        if (exitAfterSave) {
          navigate("/permissions");
        } else if (record.name !== name) {
          navigate(`/permissions/${record.owner}/${encodeURIComponent(record.name)}`, {replace: true});
        }
      },
      onFailed: () => updateField("name", name),
    }).finally(() => setSaving(false));
  };

  return (
    <EditPageShell
      title={i18next.t(getModeTitleKey("permission:Edit Permission", mode))}
      mode={mode}
      backTo="/permissions"
      onSave={save}
      saving={saving}
      remove={() => PermissionBackend.deletePermission(permission)}
      grid
    >
      <FormRow labelKey="general:Organization">
        <Input value={permission.owner} disabled />
      </FormRow>
      <FormRow labelKey="general:Name">
        <Input value={permission.name} onChange={(e) => updateField("name", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Display name">
        <Input value={permission.displayName} onChange={(e) => updateField("displayName", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Description">
        <Input value={permission.description} onChange={(e) => updateField("description", e.target.value)} />
      </FormRow>
      <FormRow labelKey="permission:Users">
        <MultiSelect
          creatable
          value={permission.users ?? []}
          options={getPrincipalOptions(users)}
          onChange={(value) => updateField("users", value)}
        />
      </FormRow>
      <FormRow labelKey="permission:Groups">
        <MultiSelect
          creatable
          value={permission.groups ?? []}
          options={getPrincipalOptions(groups)}
          onChange={(value) => updateField("groups", value)}
        />
      </FormRow>
      <FormRow label={i18next.t("general:Assets")} tooltip={i18next.t("permission:Assets - Tooltip")}>
        <MultiSelect
          value={permission.assets ?? []}
          options={getPrincipalOptions(assets)}
          onChange={(value) => updateField("assets", value)}
        />
      </FormRow>
      <FormRow labelKey="permission:Asset tags">
        <MultiSelect
          creatable
          value={permission.assetTags ?? []}
          options={tagOptions}
          onChange={(value) => updateField("assetTags", value)}
        />
      </FormRow>
      <FormRow labelKey="permission:Actions">
        <MultiSelect
          value={permission.actions ?? []}
          options={PermissionActions.map((action) => ({value: action, label: getActionLabel(action)}))}
          onChange={(value) => updateField("actions", value)}
        />
      </FormRow>
      <FormRow labelKey="permission:Client IPs">
        <TagsInput
          value={permission.clientIps ?? []}
          placeholder="192.168.1.0/24"
          onChange={(value) => updateField("clientIps", value)}
        />
      </FormRow>
      <FormRow labelKey="permission:Start time">
        <TimeInput value={permission.startTime} onChange={(value) => updateField("startTime", value)} />
      </FormRow>
      <FormRow labelKey="permission:End time">
        <TimeInput value={permission.endTime} onChange={(value) => updateField("endTime", value)} />
      </FormRow>
      <FormRow labelKey="permission:Is enabled">
        <Switch checked={permission.isEnabled} onCheckedChange={(checked) => updateField("isEnabled", checked)} />
      </FormRow>
    </EditPageShell>
  );
}
