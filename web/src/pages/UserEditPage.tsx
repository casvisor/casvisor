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
import * as LocalUserBackend from "@/backend/LocalUserBackend";
import * as PermissionBackend from "@/backend/PermissionBackend";
import type {LocalUser, Principal} from "@/backend/types";
import {Loading} from "@/components/common/Loading";
import {MultiSelect} from "@/components/common/MultiSelect";
import {PasswordInput} from "@/components/common/PasswordInput";
import {UnauthorizedPage} from "@/components/common/UnauthorizedPage";
import {EditPageShell} from "@/components/crud/EditPageShell";
import {FormRow} from "@/components/crud/FormRow";
import {Input} from "@/components/ui/input";
import {Switch} from "@/components/ui/switch";
import {useEditRecord} from "@/hooks/use-edit-record";
import {getModeTitleKey, submitEdit} from "@/lib/crud";
import NotFoundPage from "@/pages/NotFoundPage";

export default function UserEditPage() {
  const {owner = "", name = ""} = useParams();
  const navigate = useNavigate();
  const [saving, setSaving] = React.useState(false);
  const [groups, setGroups] = React.useState<Principal[]>([]);

  const {record: user, updateField, loading, notFound, denied, mode} = useEditRecord<LocalUser>({
    fetch: () => LocalUserBackend.getLocalUser(owner, name),
    deps: [owner, name],
  });

  React.useEffect(() => {
    PermissionBackend.getGroups().then((res) => res.status === "ok" && setGroups(res.data ?? []));
  }, []);

  if (loading) {
    return <Loading />;
  }
  if (denied) {
    return <UnauthorizedPage />;
  }
  if (notFound || !user) {
    return <NotFoundPage />;
  }

  const save = (exitAfterSave: boolean) => {
    setSaving(true);
    return submitEdit({
      mode,
      record: user,
      add: LocalUserBackend.addLocalUser,
      update: (record) => LocalUserBackend.updateLocalUser(owner, name, record),
      onSaved: (record) => {
        updateField("password", "");
        if (exitAfterSave) {
          navigate("/users");
        } else if (record.name !== name) {
          navigate(`/users/${record.owner}/${encodeURIComponent(record.name)}`, {replace: true});
        }
      },
    }).finally(() => setSaving(false));
  };

  return (
    <EditPageShell
      title={i18next.t(getModeTitleKey("user:Edit User", mode))}
      mode={mode}
      backTo="/users"
      onSave={save}
      saving={saving}
      remove={() => LocalUserBackend.deleteLocalUser(user)}
      grid
    >
      <FormRow labelKey="general:Organization">
        <Input value={user.owner} disabled />
      </FormRow>
      <FormRow labelKey="general:Name">
        <Input value={user.name} disabled={mode !== "add"} onChange={(e) => updateField("name", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Display name">
        <Input value={user.displayName} onChange={(e) => updateField("displayName", e.target.value)} />
      </FormRow>
      <FormRow labelKey="user:Email">
        <Input value={user.email} onChange={(e) => updateField("email", e.target.value)} />
      </FormRow>
      <FormRow label={i18next.t("general:Password")} tooltip={i18next.t("user:Password - Tooltip")}>
        <PasswordInput
          value={user.password ?? ""}
          autoComplete="new-password"
          placeholder={mode === "add" ? "" : i18next.t("user:Leave empty to keep the password")}
          onChange={(e) => updateField("password", e.target.value)}
        />
      </FormRow>
      <FormRow labelKey="permission:Groups">
        <MultiSelect
          creatable
          value={user.groups ?? []}
          options={groups.map((group) => ({value: group.name, label: group.name}))}
          onChange={(value) => updateField("groups", value)}
        />
      </FormRow>
      <FormRow labelKey="user:Is admin">
        <Switch checked={user.isAdmin} onCheckedChange={(checked) => updateField("isAdmin", checked)} />
      </FormRow>
      <FormRow labelKey="user:Is forbidden">
        <Switch checked={user.isForbidden} onCheckedChange={(checked) => updateField("isForbidden", checked)} />
      </FormRow>
    </EditPageShell>
  );
}
