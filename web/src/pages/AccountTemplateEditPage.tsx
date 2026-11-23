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
import * as AccountTemplateBackend from "@/backend/AccountTemplateBackend";
import * as AssetBackend from "@/backend/AssetBackend";
import type {AccountTemplate, Asset} from "@/backend/types";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import {Switch} from "@/components/ui/switch";
import {Textarea} from "@/components/ui/textarea";
import {Loading} from "@/components/common/Loading";
import {MultiSelect} from "@/components/common/MultiSelect";
import {UnauthorizedPage} from "@/components/common/UnauthorizedPage";
import {EditPageShell} from "@/components/crud/EditPageShell";
import {FormRow} from "@/components/crud/FormRow";
import {useEditRecord} from "@/hooks/use-edit-record";
import {getModeTitleKey, submitEdit} from "@/lib/crud";
import NotFoundPage from "@/pages/NotFoundPage";
import {ApplyTemplateDialog} from "@/pages/AccountTemplateListPage";

export default function AccountTemplateEditPage() {
  const {owner = "", name = ""} = useParams();
  const navigate = useNavigate();
  const [saving, setSaving] = React.useState(false);
  const [assets, setAssets] = React.useState<Asset[]>([]);
  const [applying, setApplying] = React.useState(false);

  const {record: template, updateField, loading, notFound, denied, mode} = useEditRecord<AccountTemplate>({
    fetch: () => AccountTemplateBackend.getAccountTemplate(owner, name),
    deps: [owner, name],
  });

  React.useEffect(() => {
    AssetBackend.getAssets(owner).then((res) => res.status === "ok" && setAssets(res.data ?? []));
  }, [owner]);

  if (loading) {
    return <Loading />;
  }
  if (denied) {
    return <UnauthorizedPage />;
  }
  if (notFound || !template) {
    return <NotFoundPage />;
  }

  const save = (exitAfterSave: boolean) => {
    setSaving(true);
    return submitEdit({
      mode,
      record: {...template, rotateIntervalDays: Number(template.rotateIntervalDays) || 0},
      add: AccountTemplateBackend.addAccountTemplate,
      update: (record) => AccountTemplateBackend.updateAccountTemplate(owner, name, record),
      onSaved: (record) => {
        if (exitAfterSave) {
          navigate("/account-templates");
        } else if (record.name !== name) {
          navigate(`/account-templates/${record.owner}/${encodeURIComponent(record.name)}`, {replace: true});
        }
      },
      onFailed: () => updateField("name", name),
    }).finally(() => setSaving(false));
  };

  const assetOptions = [{value: "*", label: i18next.t("permission:All")}, ...assets.map((asset) => ({value: asset.name, label: `${asset.displayName || asset.name} (${asset.protocol})`}))];
  const tagOptions = [...new Set(assets.map((asset) => asset.tag).filter(Boolean))].map((tag) => ({value: tag, label: tag}));

  return (
    <>
      <EditPageShell
        title={i18next.t(getModeTitleKey("accountTemplate:Edit Account Template", mode))}
        description={i18next.t("general:Give many assets the same system account at once, and keep them in line")}
        mode={mode}
        backTo="/account-templates"
        onSave={save}
        saving={saving}
        remove={() => AccountTemplateBackend.deleteAccountTemplate(template)}
        extraActions={mode === "edit" ? (
          <Button variant="outline" disabled={saving} onClick={() => setApplying(true)}>{i18next.t("accountTemplate:Apply")}</Button>
        ) : null}
        grid
      >
        <FormRow labelKey="general:Organization">
          <Input value={template.owner} disabled />
        </FormRow>
        <FormRow labelKey="general:Name">
          <Input value={template.name} onChange={(e) => updateField("name", e.target.value)} />
        </FormRow>
        <FormRow labelKey="general:Display name">
          <Input value={template.displayName} onChange={(e) => updateField("displayName", e.target.value)} />
        </FormRow>
        <FormRow labelKey="general:Description">
          <Input value={template.description} onChange={(e) => updateField("description", e.target.value)} />
        </FormRow>
        <FormRow labelKey="general:Username">
          <Input className="font-mono" value={template.username} autoComplete="off" onChange={(e) => updateField("username", e.target.value.trim())} />
        </FormRow>
        <FormRow label={i18next.t("accountTemplate:A random password for each asset")} tooltip={i18next.t("accountTemplate:Random password - Tooltip")}>
          <Switch checked={template.randomPassword} onCheckedChange={(checked) => updateField("randomPassword", checked)} />
        </FormRow>
        {!template.randomPassword ? (
          <>
            <FormRow labelKey="general:Password">
              <Input type="password" autoComplete="new-password" value={template.password} onChange={(e) => updateField("password", e.target.value)} />
            </FormRow>
            <FormRow labelKey="asset:Private key">
              <Textarea
                className="font-mono text-xs"
                rows={3}
                value={template.privateKey === "***" ? "" : template.privateKey}
                placeholder={template.privateKey === "***" ? i18next.t("accountTemplate:Saved, paste a new key to replace it") : "-----BEGIN OPENSSH PRIVATE KEY-----"}
                onChange={(e) => updateField("privateKey", e.target.value || (template.privateKey === "***" ? "***" : ""))}
              />
            </FormRow>
          </>
        ) : null}
        <FormRow label={i18next.t("accountTemplate:Push")} tooltip={i18next.t("accountTemplate:Push - Tooltip")}>
          <Switch checked={template.push} onCheckedChange={(checked) => updateField("push", checked)} />
        </FormRow>
        <FormRow label={i18next.t("accountTemplate:Rotate")} tooltip={i18next.t("accountTemplate:Rotate - Tooltip")}>
          <div className="flex items-center gap-3">
            <Switch checked={template.autoRotate} onCheckedChange={(checked) => updateField("autoRotate", checked)} />
            {template.autoRotate ? (
              <>
                <Input type="number" min={1} className="w-24" value={template.rotateIntervalDays} onChange={(e) => updateField("rotateIntervalDays", Number(e.target.value))} />
                <span className="text-sm text-muted-foreground">{i18next.t("accountTemplate:days")}</span>
              </>
            ) : null}
          </div>
        </FormRow>
        <FormRow label={i18next.t("general:Assets")} tooltip={i18next.t("accountTemplate:Assets - Tooltip")}>
          <MultiSelect value={template.assets ?? []} options={assetOptions} onChange={(value) => updateField("assets", value)} />
        </FormRow>
        <FormRow labelKey="permission:Asset tags">
          <MultiSelect creatable value={template.assetTags ?? []} options={tagOptions} onChange={(value) => updateField("assetTags", value)} />
        </FormRow>
      </EditPageShell>
      {applying ? <ApplyTemplateDialog template={template} onClose={() => setApplying(false)} /> : null}
    </>
  );
}
