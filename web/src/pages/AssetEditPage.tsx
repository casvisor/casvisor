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
import {Link, useNavigate, useParams} from "react-router-dom";
import * as AssetBackend from "@/backend/AssetBackend";
import * as AssetNodeBackend from "@/backend/AssetNodeBackend";
import * as NetworkGatewayBackend from "@/backend/NetworkGatewayBackend";
import * as SessionBackend from "@/backend/SessionBackend";
import type {Asset, AssetNode, NetworkGateway, WebLogin} from "@/backend/types";
import {MultiSelect} from "@/components/common/MultiSelect";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import {Textarea} from "@/components/ui/textarea";
import {Loading} from "@/components/common/Loading";
import {PasswordInput} from "@/components/common/PasswordInput";
import {SelectField} from "@/components/common/SelectField";
import {UnauthorizedPage} from "@/components/common/UnauthorizedPage";
import {AssetAccountsPanel} from "@/components/access/AssetAccountsPanel";
import {EditPageShell} from "@/components/crud/EditPageShell";
import {FormRow} from "@/components/crud/FormRow";
import {useEditRecord} from "@/hooks/use-edit-record";
import {DesktopProtocol, getDefaultPort, isDatabaseProtocol, KubernetesProtocol, OperatingSystems, Protocols, WebModeBrowser, WebProtocol} from "@/lib/asset";
import {getNodeOptions} from "@/lib/assetNode";
import {getModeTitleKey, submitEdit} from "@/lib/crud";
import * as Setting from "@/lib/setting";
import NotFoundPage from "@/pages/NotFoundPage";

const directConnection = "__direct__";
const webModeProxy = "Proxy";
const emptyWebLogin: WebLogin = {loginUrl: "", usernameSelector: "", passwordSelector: "", nextSelector: "", submitSelector: ""};

const webLoginFields: {key: keyof WebLogin; label: string; tooltip: string; placeholder: string}[] = [
  {key: "loginUrl", label: "web:Sign-in page", tooltip: "web:Sign-in page - Tooltip", placeholder: "/login"},
  {key: "usernameSelector", label: "web:Username field", tooltip: "web:Username field - Tooltip", placeholder: "#username"},
  {key: "nextSelector", label: "web:Next button", tooltip: "web:Next button - Tooltip", placeholder: "#next"},
  {key: "passwordSelector", label: "web:Password field", tooltip: "web:Password field - Tooltip", placeholder: "input[type=password]"},
  {key: "submitSelector", label: "web:Sign-in button", tooltip: "web:Sign-in button - Tooltip", placeholder: "button[type=submit]"},
];

export default function AssetEditPage() {
  const {owner = "", name = ""} = useParams();
  const navigate = useNavigate();
  const [saving, setSaving] = React.useState(false);
  const [testing, setTesting] = React.useState(false);

  const {record: asset, updateField, updateFields, loading, notFound, denied, mode} = useEditRecord<Asset>({
    fetch: () => AssetBackend.getAsset(owner, name),
    deps: [owner, name],
  });

  const [gateways, setGateways] = React.useState<NetworkGateway[]>([]);
  const [nodes, setNodes] = React.useState<AssetNode[]>([]);
  const [hasRemoteBrowser, setHasRemoteBrowser] = React.useState(false);
  React.useEffect(() => {
    SessionBackend.getRemoteBrowser().then((res) => setHasRemoteBrowser(res.status === "ok" && res.data === true));
  }, []);
  React.useEffect(() => {
    NetworkGatewayBackend.getNetworkGateways(owner).then((res) => {
      if (res.status === "ok") {
        setGateways(res.data ?? []);
      }
    });
    AssetNodeBackend.getAssetNodes(owner).then((res) => {
      if (res.status === "ok") {
        setNodes(res.data ?? []);
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

  const isKubernetes = asset.protocol === KubernetesProtocol;
  const isWeb = asset.protocol === WebProtocol;
  const isBrowser = isWeb && asset.webMode === WebModeBrowser;
  const webLogin = asset.webLogin ?? emptyWebLogin;

  // The test connects with what is in the form without saving it; a host key seen for the first time goes into the form.
  const test = async() => {
    setTesting(true);
    try {
      const res = await AssetBackend.testAsset(mode === "add" ? "" : `${owner}/${name}`, asset);
      if (res.status === "ok") {
        if (res.data.fingerprint) {
          updateField("fingerprint", res.data.fingerprint);
        }
        const version = res.data.version ? `, ${res.data.version}` : "";
        Setting.showMessage("success", `${i18next.t("asset:Connection successful")}: ${res.data.latency} ms${version}`);
      } else {
        Setting.showMessage("error", `${i18next.t("asset:Failed to connect")}: ${res.msg}`);
      }
    } catch (error: any) {
      Setting.showMessage("error", `${i18next.t("general:Failed to connect to server")}: ${error}`);
    } finally {
      setTesting(false);
    }
  };

  return (
    <EditPageShell
      title={i18next.t(getModeTitleKey("asset:Edit Asset", mode))}
      mode={mode}
      backTo="/assets"
      onSave={save}
      saving={saving}
      remove={() => AssetBackend.deleteAsset(asset)}
      extraActions={mode !== "view" ? (
        <Button variant="outline" disabled={testing || saving} onClick={test}>
          {i18next.t("asset:Test connection")}
        </Button>
      ) : null}
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
      {asset.cloudAccount ? (
        <FormRow label={i18next.t("cloudAccount:Synced from")} tooltip={i18next.t("cloudAccount:Synced from - Tooltip")}>
          <div className="flex h-9 items-center gap-2 text-sm">
            <Link to={`/cloud-accounts/${asset.owner}/${asset.cloudAccount}`} className="text-primary underline-offset-4 hover:underline">{asset.cloudAccount}</Link>
            <span className="font-mono text-xs text-muted-foreground">{asset.cloudInstance}</span>
          </div>
        </FormRow>
      ) : null}
      <FormRow labelKey="general:Protocol">
        <SelectField
          value={asset.protocol}
          options={Protocols.map((protocol) => ({id: protocol, name: protocol}))}
          onChange={(protocol) => updateFields({protocol, port: getDefaultPort(protocol)})}
        />
      </FormRow>
      {asset.protocol !== DesktopProtocol ? (
        <>
          <FormRow
            labelKey={isKubernetes ? "asset:API server" : isWeb ? "web:Address" : "asset:Host"}
            tooltip={isWeb ? i18next.t("web:Address - Tooltip") : undefined}
          >
            <Input
              value={asset.host}
              placeholder={isKubernetes ? "https://192.168.1.10:6443" : isWeb ? "http://192.168.1.10:3000/" : undefined}
              onChange={(e) => updateField("host", e.target.value)}
            />
          </FormRow>
          <FormRow labelKey="general:Port">
            <Input
              type="number"
              value={asset.port}
              onChange={(e) => updateField("port", Setting.myParseInt(e.target.value))}
            />
          </FormRow>
          <FormRow
            labelKey="general:Username"
            tooltip={isKubernetes ? i18next.t("asset:Username of a Kubernetes cluster - Tooltip") : isBrowser ? i18next.t("web:Sign-in account - Tooltip") : isWeb ? i18next.t("web:Basic authentication - Tooltip") : undefined}
          >
            <Input value={asset.username} autoComplete="off" onChange={(e) => updateField("username", e.target.value)} />
          </FormRow>
          <FormRow labelKey={isKubernetes ? "asset:Token" : "general:Password"}>
            <PasswordInput
              value={asset.password}
              autoComplete="new-password"
              onChange={(e) => updateField("password", e.target.value)}
            />
          </FormRow>
        </>
      ) : null}
      {isWeb && (hasRemoteBrowser || isBrowser) ? (
        <FormRow label={i18next.t("web:Open in")} tooltip={i18next.t("web:Open in - Tooltip")}>
          <SelectField
            value={asset.webMode || webModeProxy}
            options={[
              {id: webModeProxy, name: i18next.t("web:Reverse proxy")},
              {id: WebModeBrowser, name: i18next.t("web:Casvisor's browser")},
            ]}
            onChange={(value) => updateField("webMode", value === WebModeBrowser ? WebModeBrowser : "")}
          />
        </FormRow>
      ) : null}
      {isBrowser ? webLoginFields.map((field) => (
        <FormRow key={field.key} label={i18next.t(field.label)} tooltip={i18next.t(field.tooltip)}>
          <Input
            value={webLogin[field.key]}
            placeholder={field.placeholder}
            className={field.key === "loginUrl" ? undefined : "font-mono text-xs"}
            onChange={(e) => updateField("webLogin", {...webLogin, [field.key]: e.target.value})}
          />
        </FormRow>
      )) : null}
      {asset.protocol === "Oracle" ? (
        <FormRow label={i18next.t("asset:Service name")} tooltip={i18next.t("asset:Service name - Tooltip")}>
          <Input value={asset.database ?? ""} placeholder="ORCLPDB1" onChange={(e) => updateField("database", e.target.value)} />
        </FormRow>
      ) : asset.protocol === "Redis" ? (
        <FormRow label={i18next.t("asset:Database number")} tooltip={i18next.t("asset:Database number - Tooltip")}>
          <Input value={asset.database ?? ""} placeholder="0" onChange={(e) => updateField("database", e.target.value)} />
        </FormRow>
      ) : isDatabaseProtocol(asset.protocol) ? (
        <FormRow
          label={i18next.t("asset:Database")}
          tooltip={asset.protocol === "MongoDB" ? i18next.t("asset:Database of MongoDB - Tooltip") : i18next.t("asset:Database - Tooltip")}
        >
          <Input value={asset.database ?? ""} onChange={(e) => updateField("database", e.target.value)} />
        </FormRow>
      ) : null}
      {isKubernetes ? (
        <FormRow labelKey="asset:Namespace">
          <Input value={asset.database ?? ""} placeholder="default" onChange={(e) => updateField("database", e.target.value)} />
        </FormRow>
      ) : null}
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
      {asset.protocol === "SSH" || asset.protocol === "RDP" || (isDatabaseProtocol(asset.protocol) && asset.protocol !== "Oracle") || isKubernetes || isWeb ? (
        <FormRow labelKey="asset:Fingerprint">
          <div className="flex w-full flex-col gap-1">
            <Input
              value={asset.fingerprint}
              className="font-mono text-xs"
              onChange={(e) => updateField("fingerprint", e.target.value)}
            />
            <span className="text-xs text-muted-foreground">
              {isWeb
                ? i18next.t("web:A certificate the system trusts is always accepted. Any other one, like a self-signed certificate, is recorded by its public key on the first connection, and a different one is rejected afterwards")
                : isKubernetes
                  ? i18next.t("asset:The public key of the API server is recorded on the first connection, a different one is rejected afterwards. Clear it if the cluster was reinstalled")
                  : i18next.t("asset:Recorded on the first connection, a different host key or certificate is rejected afterwards. Clear it if the server was reinstalled or its certificate renewed")}
            </span>
          </div>
        </FormRow>
      ) : null}
      {asset.protocol === DesktopProtocol ? (
        <FormRow label={i18next.t("asset:Casvisor Desktop device")} tooltip={i18next.t("asset:Casvisor Desktop device - Tooltip")}>
          <SelectField
            value={asset.networkGateway}
            options={gateways
              .filter((gateway) => gateway.type === DesktopProtocol)
              .map((gateway) => ({id: gateway.name, name: gateway.displayName || gateway.name}))}
            onChange={(value) => updateField("networkGateway", value)}
          />
        </FormRow>
      ) : (
        <FormRow label={i18next.t("asset:Network gateway")} tooltip={i18next.t("asset:Network gateway - Tooltip")}>
          <SelectField
            value={asset.networkGateway || directConnection}
            options={[
              {id: directConnection, name: i18next.t("asset:Direct connection")},
              ...gateways
                .filter((gateway) => gateway.type !== DesktopProtocol)
                .map((gateway) => ({
                  id: gateway.name,
                  name: gateway.domain
                    ? `${gateway.displayName || gateway.name} (${i18next.t("networkGateway:Network domain")}: ${gateway.domain})`
                    : gateway.displayName || gateway.name,
                })),
            ]}
            onChange={(value) => updateField("networkGateway", value === directConnection ? "" : value)}
          />
        </FormRow>
      )}
      <FormRow labelKey="asset:OS">
        <SelectField
          value={asset.os}
          options={OperatingSystems.map((os) => ({id: os, name: os}))}
          onChange={(os) => updateField("os", os)}
        />
      </FormRow>
      <FormRow label={i18next.t("assetNode:Nodes")} tooltip={i18next.t("assetNode:Nodes - Tooltip")}>
        <MultiSelect value={asset.nodes ?? []} options={getNodeOptions(nodes)} onChange={(value) => updateField("nodes", value)} />
      </FormRow>
      <FormRow labelKey="general:Tag">
        <Input value={asset.tag} onChange={(e) => updateField("tag", e.target.value)} />
      </FormRow>
      {mode !== "add" ? (
        <FormRow labelKey="account:System accounts" block>
          <AssetAccountsPanel owner={owner} asset={name} protocol={asset.protocol} ownUsername={asset.username} />
        </FormRow>
      ) : null}
    </EditPageShell>
  );
}
