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
import * as NetworkGatewayBackend from "@/backend/NetworkGatewayBackend";
import type {NetworkGateway} from "@/backend/types";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import {Switch} from "@/components/ui/switch";
import {Textarea} from "@/components/ui/textarea";
import {Loading} from "@/components/common/Loading";
import {PasswordInput} from "@/components/common/PasswordInput";
import {UnauthorizedPage} from "@/components/common/UnauthorizedPage";
import {EditPageShell} from "@/components/crud/EditPageShell";
import {FormRow} from "@/components/crud/FormRow";
import {useEditRecord} from "@/hooks/use-edit-record";
import {getModeTitleKey, submitEdit} from "@/lib/crud";
import * as Setting from "@/lib/setting";
import NotFoundPage from "@/pages/NotFoundPage";

export default function NetworkGatewayEditPage() {
  const {owner = "", name = ""} = useParams();
  const navigate = useNavigate();
  const [saving, setSaving] = React.useState(false);
  const [testing, setTesting] = React.useState(false);

  const {record: gateway, updateField, loading, notFound, denied, mode} = useEditRecord<NetworkGateway>({
    fetch: () => NetworkGatewayBackend.getNetworkGateway(owner, name),
    deps: [owner, name],
  });

  if (loading) {
    return <Loading />;
  }
  if (denied) {
    return <UnauthorizedPage />;
  }
  if (notFound || !gateway) {
    return <NotFoundPage />;
  }

  const save = (exitAfterSave: boolean) => {
    setSaving(true);
    return submitEdit({
      mode,
      record: gateway,
      add: NetworkGatewayBackend.addNetworkGateway,
      update: (record) => NetworkGatewayBackend.updateNetworkGateway(owner, name, record),
      onSaved: (record) => {
        if (exitAfterSave) {
          navigate("/network-gateways");
        } else if (record.name !== name) {
          navigate(`/network-gateways/${record.owner}/${encodeURIComponent(record.name)}`, {replace: true});
        }
      },
      onFailed: () => updateField("name", name),
    }).finally(() => setSaving(false));
  };

  // The test connects with the saved settings, so the form is saved first.
  const test = async() => {
    setTesting(true);
    try {
      const saved = await save(false);
      if (!saved) {
        return;
      }
      const res = await NetworkGatewayBackend.testNetworkGateway(gateway.owner, gateway.name);
      if (res.status === "ok") {
        updateField("fingerprint", res.data);
        Setting.showMessage("success", `${i18next.t("networkGateway:Connected")}: ${res.data2}`);
      } else {
        Setting.showMessage("error", `${i18next.t("networkGateway:Failed to connect")}: ${res.msg}`);
      }
    } catch (error: any) {
      Setting.showMessage("error", `${i18next.t("general:Failed to connect to server")}: ${error}`);
    } finally {
      setTesting(false);
    }
  };

  return (
    <EditPageShell
      title={i18next.t(getModeTitleKey("networkGateway:Edit Network Gateway", mode))}
      description={i18next.t("networkGateway:Casvisor connects to the gateway over SSH and reaches the assets that use it from there, the gateway needs AllowTcpForwarding")}
      mode={mode}
      backTo="/network-gateways"
      onSave={save}
      saving={saving}
      remove={() => NetworkGatewayBackend.deleteNetworkGateway(gateway)}
      extraActions={
        <Button variant="outline" disabled={testing || saving} onClick={test}>
          {i18next.t("networkGateway:Test connection")}
        </Button>
      }
      grid
    >
      <FormRow labelKey="general:Organization">
        <Input value={gateway.owner} disabled />
      </FormRow>
      <FormRow labelKey="general:Name">
        <Input value={gateway.name} onChange={(e) => updateField("name", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Display name">
        <Input value={gateway.displayName} onChange={(e) => updateField("displayName", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Description">
        <Input value={gateway.description} onChange={(e) => updateField("description", e.target.value)} />
      </FormRow>
      <FormRow labelKey="asset:Host">
        <Input value={gateway.host} onChange={(e) => updateField("host", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Port">
        <Input
          type="number"
          value={gateway.port}
          onChange={(e) => updateField("port", Setting.myParseInt(e.target.value))}
        />
      </FormRow>
      <FormRow labelKey="general:Username">
        <Input value={gateway.username} autoComplete="off" onChange={(e) => updateField("username", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Password">
        <PasswordInput
          value={gateway.password}
          autoComplete="new-password"
          onChange={(e) => updateField("password", e.target.value)}
        />
      </FormRow>
      <FormRow labelKey="asset:Private key">
        <Textarea
          value={gateway.privateKey}
          rows={4}
          className="font-mono text-xs"
          placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
          onChange={(e) => updateField("privateKey", e.target.value)}
        />
      </FormRow>
      <FormRow labelKey="asset:Fingerprint">
        <div className="flex w-full flex-col gap-1">
          <Input
            value={gateway.fingerprint}
            className="font-mono text-xs"
            onChange={(e) => updateField("fingerprint", e.target.value)}
          />
          <span className="text-xs text-muted-foreground">
            {i18next.t("networkGateway:Recorded on the first connection, a different host key is rejected afterwards. Clear it if the gateway was reinstalled")}
          </span>
        </div>
      </FormRow>
      <FormRow labelKey="permission:Is enabled">
        <Switch checked={gateway.isEnabled} onCheckedChange={(checked) => updateField("isEnabled", checked)} />
      </FormRow>
    </EditPageShell>
  );
}
