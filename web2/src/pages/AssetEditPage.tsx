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
import {useNavigate, useParams} from "react-router-dom";
import * as AssetBackend from "@/backend/AssetBackend";
import type {Asset, RemoteApp} from "@/backend/types";
import {Input} from "@/components/ui/input";
import {Switch} from "@/components/ui/switch";
import {Loading} from "@/components/common/Loading";
import {PasswordInput} from "@/components/common/PasswordInput";
import {SelectField} from "@/components/common/SelectField";
import {UnauthorizedPage} from "@/components/common/UnauthorizedPage";
import {EditableTable} from "@/components/crud/EditableTable";
import {EditPageShell} from "@/components/crud/EditPageShell";
import {FormRow} from "@/components/crud/FormRow";
import {useEditRecord} from "@/hooks/use-edit-record";
import {getDefaultPort, OperatingSystems, Protocols} from "@/lib/asset";
import {getModeTitleKey, submitEdit} from "@/lib/crud";
import * as Setting from "@/lib/setting";
import NotFoundPage from "@/pages/NotFoundPage";

function RemoteAppTable({rows, onChange}: {rows: RemoteApp[]; onChange: (rows: RemoteApp[]) => void}) {
  return (
    <EditableTable<RemoteApp>
      rows={rows}
      onChange={onChange}
      newRow={() => ({no: rows.length, remoteAppName: "||cmd", remoteAppDir: "C:\\Windows\\System32\\cmd.exe", remoteAppArgs: ""})}
      columns={[
        {
          key: "no",
          title: "No.",
          width: 80,
          render: (row, _index, update) => (
            <Input value={row.no} onChange={(e) => update({no: Setting.myParseInt(e.target.value)})} />
          ),
        },
        {
          key: "remoteAppName",
          title: i18next.t("asset:Remote App"),
          width: 160,
          render: (row, _index, update) => (
            <Input value={row.remoteAppName} onChange={(e) => update({remoteAppName: e.target.value})} />
          ),
        },
        {
          key: "remoteAppDir",
          title: i18next.t("asset:Remote App Dir"),
          render: (row, _index, update) => (
            <Input value={row.remoteAppDir} onChange={(e) => update({remoteAppDir: e.target.value})} />
          ),
        },
        {
          key: "remoteAppArgs",
          title: i18next.t("asset:Remote App Args"),
          width: 200,
          render: (row, _index, update) => (
            <Input value={row.remoteAppArgs} onChange={(e) => update({remoteAppArgs: e.target.value})} />
          ),
        },
      ]}
    />
  );
}

export default function AssetEditPage() {
  const {owner = "", name = ""} = useParams();
  const navigate = useNavigate();
  const [saving, setSaving] = React.useState(false);

  const {record: asset, updateField, updateFields, loading, notFound, denied, mode} = useEditRecord<Asset>({
    fetch: () => AssetBackend.getAsset(owner, name),
    transform: (asset) => ({...asset, remoteApps: asset.remoteApps ?? []}),
    deps: [owner, name],
  });

  if (loading) {
    return <Loading />;
  }
  if (denied) {
    return <UnauthorizedPage />;
  }
  if (notFound || !asset) {
    return <NotFoundPage />;
  }

  const save = (exitAfterSave: boolean) => {
    setSaving(true);
    return submitEdit({
      mode,
      record: asset,
      add: AssetBackend.addAsset,
      update: (record) => AssetBackend.updateAsset(owner, name, record),
      onSaved: (record) => {
        if (exitAfterSave) {
          navigate("/assets");
        } else if (record.owner !== owner || record.name !== name) {
          navigate(`/assets/${record.owner}/${encodeURIComponent(record.name)}`, {replace: true});
        }
      },
      onFailed: () => updateField("name", name),
    }).finally(() => setSaving(false));
  };

  const setEnableRemoteApp = (checked: boolean) => {
    if (checked && (asset.remoteApps ?? []).length === 0) {
      Setting.showMessage("error", i18next.t("asset:Cannot enable Remote App when Remote Apps are empty. Please add at least one Remote App in below table first, then enable again"));
      return;
    }
    updateField("enableRemoteApp", checked);
  };

  return (
    <EditPageShell
      title={i18next.t(getModeTitleKey("asset:Edit Asset", mode))}
      mode={mode}
      backTo="/assets"
      onSave={save}
      saving={saving}
      remove={() => AssetBackend.deleteAsset(asset)}
      grid
    >
      <FormRow labelKey="general:Organization">
        <Input value={asset.owner} onChange={(e) => updateField("owner", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Name">
        <Input value={asset.name} onChange={(e) => updateField("name", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Display name">
        <Input value={asset.displayName} onChange={(e) => updateField("displayName", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Description">
        <Input value={asset.description} onChange={(e) => updateField("description", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Protocol">
        <SelectField
          value={asset.protocol}
          options={Protocols.map((protocol) => ({id: protocol, name: protocol}))}
          onChange={(protocol) => updateFields({protocol, port: getDefaultPort(protocol)})}
        />
      </FormRow>
      <FormRow labelKey="asset:Host">
        <Input value={asset.host} onChange={(e) => updateField("host", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Port">
        <Input
          type="number"
          value={asset.port}
          onChange={(e) => updateField("port", Setting.myParseInt(e.target.value))}
        />
      </FormRow>
      <FormRow labelKey="general:Username">
        <Input value={asset.username} autoComplete="off" onChange={(e) => updateField("username", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Password">
        <PasswordInput
          value={asset.password}
          autoComplete="new-password"
          onChange={(e) => updateField("password", e.target.value)}
        />
      </FormRow>
      <FormRow labelKey="asset:OS">
        <SelectField
          value={asset.os}
          options={OperatingSystems.map((os) => ({id: os, name: os}))}
          onChange={(os) => updateField("os", os)}
        />
      </FormRow>
      <FormRow labelKey="general:Tag">
        <Input value={asset.tag} onChange={(e) => updateField("tag", e.target.value)} />
      </FormRow>
      {asset.protocol === "RDP" ? (
        <FormRow labelKey="general:Enable Remote App">
          <Switch checked={asset.enableRemoteApp} onCheckedChange={setEnableRemoteApp} />
        </FormRow>
      ) : null}
      {asset.protocol === "RDP" ? (
        <FormRow labelKey="general:Remote Apps" block>
          <RemoteAppTable rows={asset.remoteApps ?? []} onChange={(rows) => updateField("remoteApps", rows)} />
        </FormRow>
      ) : null}
    </EditPageShell>
  );
}
