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
import * as NetworkGatewayBackend from "@/backend/NetworkGatewayBackend";
import type {Asset, NetworkGateway} from "@/backend/types";
import {Input} from "@/components/ui/input";
import {Textarea} from "@/components/ui/textarea";
import {Loading} from "@/components/common/Loading";
import {PasswordInput} from "@/components/common/PasswordInput";
import {SelectField} from "@/components/common/SelectField";
import {UnauthorizedPage} from "@/components/common/UnauthorizedPage";
import {EditPageShell} from "@/components/crud/EditPageShell";
import {FormRow} from "@/components/crud/FormRow";
import {useEditRecord} from "@/hooks/use-edit-record";
import {getDefaultPort, OperatingSystems, Protocols} from "@/lib/asset";
import {getModeTitleKey, submitEdit} from "@/lib/crud";
import * as Setting from "@/lib/setting";
import NotFoundPage from "@/pages/NotFoundPage";

const directConnection = "__direct__";

export default function AssetEditPage() {
  const {owner = "", name = ""} = useParams();
  const navigate = useNavigate();
  const [saving, setSaving] = React.useState(false);

  const {record: asset, updateField, updateFields, loading, notFound, denied, mode} = useEditRecord<Asset>({
    fetch: () => AssetBackend.getAsset(owner, name),
    deps: [owner, name],
  });

  const [gateways, setGateways] = React.useState<NetworkGateway[]>([]);
  React.useEffect(() => {
    NetworkGatewayBackend.getNetworkGateways(owner).then((res) => {
      if (res.status === "ok") {
        setGateways(res.data ?? []);
      }
    });
  }, [owner]);

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
      {asset.protocol === "SSH" ? (
        <FormRow labelKey="asset:Private key">
          <Textarea
            value={asset.privateKey}
            rows={4}
            className="font-mono text-xs"
            placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
            onChange={(e) => updateField("privateKey", e.target.value)}
          />
        </FormRow>
      ) : null}
      {asset.protocol === "SSH" || asset.protocol === "RDP" ? (
        <FormRow labelKey="asset:Fingerprint">
          <div className="flex w-full flex-col gap-1">
            <Input
              value={asset.fingerprint}
              className="font-mono text-xs"
              onChange={(e) => updateField("fingerprint", e.target.value)}
            />
            <span className="text-xs text-muted-foreground">
              {i18next.t("asset:Recorded on the first connection, a different host key or certificate is rejected afterwards. Clear it if the server was reinstalled or its certificate renewed")}
            </span>
          </div>
        </FormRow>
      ) : null}
      <FormRow label={i18next.t("asset:Network gateway")} tooltip={i18next.t("asset:Network gateway - Tooltip")}>
        <SelectField
          value={asset.networkGateway || directConnection}
          options={[
            {id: directConnection, name: i18next.t("asset:Direct connection")},
            ...gateways.map((gateway) => ({id: gateway.name, name: gateway.displayName || gateway.name})),
          ]}
          onChange={(value) => updateField("networkGateway", value === directConnection ? "" : value)}
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
    </EditPageShell>
  );
}
