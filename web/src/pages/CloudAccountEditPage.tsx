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
import * as CloudAccountBackend from "@/backend/CloudAccountBackend";
import * as NetworkGatewayBackend from "@/backend/NetworkGatewayBackend";
import type {CloudAccount} from "@/backend/types";
import {CloudSyncDialog} from "@/components/asset/CloudSyncDialog";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import {Switch} from "@/components/ui/switch";
import {Textarea} from "@/components/ui/textarea";
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

/** The labels of the access key and the secret, which every provider names its own way. */
const keyLabels: Record<string, [string, string]> = {
  Aliyun: ["AccessKey ID", "AccessKey Secret"],
  AWS: ["Access key ID", "Secret access key"],
  Tencent: ["SecretId", "SecretKey"],
  Azure: ["Client ID", "Client secret"],
  GCP: ["", ""],
  Huawei: ["Access Key ID (AK)", "Secret Access Key (SK)"],
};

export default function CloudAccountEditPage() {
  const {owner = "", name = ""} = useParams();
  const navigate = useNavigate();
  const [saving, setSaving] = React.useState(false);
  const [loadingRegions, setLoadingRegions] = React.useState(false);
  const [regionOptions, setRegionOptions] = React.useState<string[]>([]);
  const [gateways, setGateways] = React.useState<{name: string; displayName: string}[]>([]);
  const [syncing, setSyncing] = React.useState(false);

  const {record: cloudAccount, updateField, loading, notFound, denied, mode} = useEditRecord<CloudAccount>({
    fetch: () => CloudAccountBackend.getCloudAccount(owner, name),
    deps: [owner, name],
  });

  React.useEffect(() => {
    NetworkGatewayBackend.getNetworkGateways(owner).then((res) => res.status === "ok" && setGateways(res.data ?? []));
  }, [owner]);

  if (loading) {
    return <Loading />;
  }
  if (denied) {
    return <UnauthorizedPage />;
  }
  if (notFound || !cloudAccount) {
    return <NotFoundPage />;
  }

  const save = (exitAfterSave: boolean) => {
    setSaving(true);
    return submitEdit({
      mode,
      record: cloudAccount,
      add: CloudAccountBackend.addCloudAccount,
      update: (record) => CloudAccountBackend.updateCloudAccount(owner, name, record),
      onSaved: (record) => {
        if (exitAfterSave) {
          navigate("/cloud-accounts");
        } else if (record.name !== name) {
          navigate(`/cloud-accounts/${record.owner}/${encodeURIComponent(record.name)}`, {replace: true});
        }
      },
      onFailed: () => updateField("name", name),
    }).finally(() => setSaving(false));
  };

  // The regions are listed with the account as it is in the form, without saving it.
  const loadRegions = async() => {
    setLoadingRegions(true);
    try {
      const res = await CloudAccountBackend.getCloudRegions(mode === "add" ? "" : `${owner}/${name}`, cloudAccount);
      if (res.status === "ok") {
        setRegionOptions(res.data ?? []);
        Setting.showMessage("success", i18next.t("cloudAccount:{{n}} regions found, the credentials work", {n: (res.data ?? []).length}));
      } else {
        Setting.showMessage("error", `${i18next.t("cloudAccount:Failed to list the regions")}: ${res.msg}`);
      }
    } catch (error: any) {
      Setting.showMessage("error", `${i18next.t("general:Failed to connect to server")}: ${error}`);
    } finally {
      setLoadingRegions(false);
    }
  };

  const provider = cloudAccount.provider;
  const [keyLabel, secretLabel] = keyLabels[provider] ?? ["", ""];
  const regions = cloudAccount.regions ?? [];
  const regionChoices = [...new Set([...regionOptions, ...regions])];

  return (
    <>
      <EditPageShell
        title={i18next.t(getModeTitleKey("cloudAccount:Edit Cloud Account", mode))}
        description={i18next.t("general:Keep the servers of Alibaba Cloud, AWS, Tencent Cloud, Azure, GCP and Huawei Cloud accounts as assets")}
        mode={mode}
        backTo="/cloud-accounts"
        onSave={save}
        saving={saving}
        remove={() => CloudAccountBackend.deleteCloudAccount(cloudAccount)}
        extraActions={mode === "edit" ? (
          <Button variant="outline" disabled={saving} onClick={() => setSyncing(true)}>
            {i18next.t("cloudAccount:Sync")}
          </Button>
        ) : null}
        grid
      >
        <FormRow labelKey="general:Organization">
          <Input value={cloudAccount.owner} disabled />
        </FormRow>
        <FormRow labelKey="general:Name">
          <Input value={cloudAccount.name} onChange={(e) => updateField("name", e.target.value)} />
        </FormRow>
        <FormRow labelKey="general:Display name">
          <Input value={cloudAccount.displayName} onChange={(e) => updateField("displayName", e.target.value)} />
        </FormRow>
        <FormRow labelKey="general:Description">
          <Input value={cloudAccount.description} onChange={(e) => updateField("description", e.target.value)} />
        </FormRow>
        <FormRow label={i18next.t("cloudAccount:Provider")}>
          <SelectField
            value={provider}
            options={CloudAccountBackend.CloudProviders.map((item) => ({id: item, name: i18next.t(`cloudAccount:${item}`)}))}
            onChange={(value) => updateField("provider", value)}
          />
        </FormRow>
        {provider === "Azure" ? (
          <>
            <FormRow label={i18next.t("cloudAccount:Tenant ID")} tooltip={i18next.t("cloudAccount:Azure - Tooltip")}>
              <Input className="font-mono" value={cloudAccount.tenantId} onChange={(e) => updateField("tenantId", e.target.value.trim())} />
            </FormRow>
            <FormRow label={i18next.t("cloudAccount:Subscription ID")}>
              <Input className="font-mono" value={cloudAccount.subscriptionId} onChange={(e) => updateField("subscriptionId", e.target.value.trim())} />
            </FormRow>
          </>
        ) : null}
        {provider === "GCP" ? (
          <>
            <FormRow label={i18next.t("cloudAccount:Service account key")} tooltip={i18next.t("cloudAccount:GCP - Tooltip")}>
              <Textarea
                className="font-mono text-xs"
                rows={4}
                value={cloudAccount.accessSecret === "***" ? "" : cloudAccount.accessSecret}
                placeholder={cloudAccount.accessSecret === "***" ? i18next.t("cloudAccount:Saved, paste a new key to replace it") : "{\"type\": \"service_account\", ...}"}
                onChange={(e) => updateField("accessSecret", e.target.value || (mode === "add" ? "" : "***"))}
              />
            </FormRow>
            <FormRow label={i18next.t("cloudAccount:Project ID")}>
              <Input className="font-mono" value={cloudAccount.projectId} placeholder={i18next.t("cloudAccount:The project of the key when empty")} onChange={(e) => updateField("projectId", e.target.value.trim())} />
            </FormRow>
          </>
        ) : (
          <>
            <FormRow label={keyLabel} tooltip={i18next.t(`cloudAccount:${provider} - Tooltip`)}>
              <Input className="font-mono" value={cloudAccount.accessKey} onChange={(e) => updateField("accessKey", e.target.value.trim())} />
            </FormRow>
            <FormRow label={secretLabel}>
              <Input type="password" autoComplete="new-password" value={cloudAccount.accessSecret} onChange={(e) => updateField("accessSecret", e.target.value.trim())} />
            </FormRow>
          </>
        )}
        <FormRow label={i18next.t("cloudAccount:Regions")} tooltip={i18next.t("cloudAccount:Regions - Tooltip")}>
          <div className="flex w-full items-start gap-2">
            <div className="min-w-0 flex-1">
              <MultiSelect
                creatable
                value={regions}
                placeholder={i18next.t("cloudAccount:All regions")}
                options={regionChoices.map((region) => ({value: region, label: region}))}
                onChange={(value) => updateField("regions", value)}
              />
            </div>
            <Button variant="outline" loading={loadingRegions} onClick={loadRegions}>{i18next.t("cloudAccount:Load regions")}</Button>
          </div>
        </FormRow>
        <FormRow label={i18next.t("cloudAccount:Address")} tooltip={i18next.t("cloudAccount:Address - Tooltip")}>
          <SelectField
            value={cloudAccount.addressType}
            options={CloudAccountBackend.CloudAddressTypes.map((item) => ({id: item, name: i18next.t(`cloudAccount:${item} IP first`)}))}
            onChange={(value) => updateField("addressType", value)}
          />
        </FormRow>
        <FormRow label={i18next.t("cloudAccount:Linux username")} tooltip={i18next.t("cloudAccount:Usernames - Tooltip")}>
          <Input value={cloudAccount.linuxUsername} onChange={(e) => updateField("linuxUsername", e.target.value.trim())} />
        </FormRow>
        <FormRow label={i18next.t("cloudAccount:Windows username")}>
          <Input value={cloudAccount.windowsUsername} onChange={(e) => updateField("windowsUsername", e.target.value.trim())} />
        </FormRow>
        <FormRow label={i18next.t("cloudAccount:Asset tree node")} tooltip={i18next.t("cloudAccount:Asset tree node - Tooltip")}>
          <Input value={cloudAccount.nodePath} placeholder={i18next.t("cloudAccount:For example Cloud/Alibaba Cloud")} onChange={(e) => updateField("nodePath", e.target.value)} />
        </FormRow>
        <FormRow labelKey="general:Tag">
          <Input value={cloudAccount.tag} onChange={(e) => updateField("tag", e.target.value.trim())} />
        </FormRow>
        <FormRow label={i18next.t("asset:Network gateway")}>
          <SelectField
            value={cloudAccount.networkGateway || "@none"}
            options={[{id: "@none", name: i18next.t("cloudAccount:Direct")}, ...gateways.map((gateway) => ({id: gateway.name, name: gateway.displayName || gateway.name}))]}
            onChange={(value) => updateField("networkGateway", value === "@none" ? "" : value)}
          />
        </FormRow>
        <FormRow label={i18next.t("cloudAccount:Sync interval")} tooltip={i18next.t("cloudAccount:Sync interval - Tooltip")}>
          <Input type="number" min={0} value={cloudAccount.syncInterval} onChange={(e) => updateField("syncInterval", Number(e.target.value) || 0)} />
        </FormRow>
        <FormRow label={i18next.t("cloudAccount:Delete missing assets")} tooltip={i18next.t("cloudAccount:Delete missing assets - Tooltip")}>
          <Switch checked={cloudAccount.deleteMissing} onCheckedChange={(checked) => updateField("deleteMissing", checked)} />
        </FormRow>
        <FormRow labelKey="permission:Is enabled">
          <Switch checked={cloudAccount.isEnabled} onCheckedChange={(checked) => updateField("isEnabled", checked)} />
        </FormRow>
      </EditPageShell>
      {syncing ? <CloudSyncDialog account={cloudAccount} onClose={() => setSyncing(false)} /> : null}
    </>
  );
}
