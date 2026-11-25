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
import {Plus, Trash2} from "lucide-react";
import {useLocation, useNavigate, useParams} from "react-router-dom";
import * as PlatformBackend from "@/backend/PlatformBackend";
import type {Platform, PlatformProtocol} from "@/backend/types";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import {Switch} from "@/components/ui/switch";
import {Textarea} from "@/components/ui/textarea";
import {Loading} from "@/components/common/Loading";
import {SelectField} from "@/components/common/SelectField";
import {UnauthorizedPage} from "@/components/common/UnauthorizedPage";
import {EditPageShell} from "@/components/crud/EditPageShell";
import {FormRow} from "@/components/crud/FormRow";
import {useEditRecord} from "@/hooks/use-edit-record";
import {getDefaultPort, Protocols} from "@/lib/asset";
import {getModeTitleKey, submitEdit} from "@/lib/crud";
import NotFoundPage from "@/pages/NotFoundPage";
import {copyPlatform, getMethodName} from "@/pages/PlatformListPage";
import * as Setting from "@/lib/setting";

function ProtocolsEditor({value, onChange}: {value: PlatformProtocol[]; onChange: (value: PlatformProtocol[]) => void}) {
  const update = (index: number, patch: Partial<PlatformProtocol>) => onChange(value.map((protocol, i) => (i === index ? {...protocol, ...patch} : protocol)));
  const unused = Protocols.filter((name) => !value.some((protocol) => protocol.name === name));
  return (
    <div className="w-full space-y-2">
      {value.map((protocol, index) => (
        <div key={index} className="flex items-center gap-2">
          <SelectField
            className="w-44"
            value={protocol.name}
            options={Protocols.filter((name) => name === protocol.name || unused.includes(name)).map((name) => ({id: name, name}))}
            onChange={(name) => update(index, {name, port: getDefaultPort(name)})}
          />
          <Input type="number" className="w-28" value={protocol.port} onChange={(e) => update(index, {port: Number(e.target.value) || 0})} />
          <Button type="button" size="iconSm" variant="ghost" aria-label={i18next.t("general:Delete")} onClick={() => onChange(value.filter((_, i) => i !== index))}>
            <Trash2 />
          </Button>
        </div>
      ))}
      {unused.length > 0 ? (
        <Button type="button" size="sm" variant="outline" onClick={() => onChange([...value, {name: unused[0], port: getDefaultPort(unused[0])}])}>
          <Plus />
          {i18next.t("general:Add")}
        </Button>
      ) : null}
    </div>
  );
}

function PlatformEditor() {
  const {owner = "", name = ""} = useParams();
  const navigate = useNavigate();
  const [saving, setSaving] = React.useState(false);

  const {record: platform, updateField, updateFields, loading, notFound, denied, mode} = useEditRecord<Platform>({
    fetch: () => PlatformBackend.getPlatform(owner, name),
    deps: [owner, name],
  });

  if (loading) {
    return <Loading />;
  }
  if (denied) {
    return <UnauthorizedPage />;
  }
  if (notFound || !platform) {
    return <NotFoundPage />;
  }

  const readOnly = mode === "view" || Boolean(platform.isBuiltIn);
  const abilities = PlatformBackend.MethodAbilities[platform.method] ?? PlatformBackend.MethodAbilities[""];
  const isCli = platform.method === "CLI";

  const save = (exitAfterSave: boolean) => {
    setSaving(true);
    return submitEdit({
      mode,
      record: {...platform, passwordLength: Number(platform.passwordLength) || 0},
      add: PlatformBackend.addPlatform,
      update: (record) => PlatformBackend.updatePlatform(owner, name, record),
      onSaved: (record) => {
        if (exitAfterSave) {
          navigate("/platforms");
        } else if (mode === "add" || record.name !== name) {
          navigate(`/platforms/${Setting.getIdPath(record.owner, record.name)}`, {replace: true});
        }
      },
      onFailed: () => updateField("name", name),
    }).finally(() => setSaving(false));
  };

  const copy = () => {
    const record = copyPlatform(platform);
    navigate(`/platforms/${Setting.getIdPath(record.owner, record.name)}`, {state: {mode: "add", record}});
  };

  const changeMethod = (method: string) => {
    const next = PlatformBackend.MethodAbilities[method] ?? PlatformBackend.MethodAbilities[""];
    updateFields({method, canChangePassword: next.change, canPush: next.push, canGather: next.gather});
  };

  return (
    <EditPageShell
      title={i18next.t(getModeTitleKey("platform:Edit Platform", readOnly ? "view" : mode))}
      description={platform.isBuiltIn ? i18next.t("platform:A built-in platform can't be changed, copy it to make one of your own") : i18next.t("general:Ports, terminal charsets, elevation and password changes of each kind of system")}
      mode={readOnly ? "view" : mode}
      backTo="/platforms"
      onSave={save}
      saving={saving}
      remove={readOnly ? undefined : () => PlatformBackend.deletePlatform(platform)}
      extraActions={mode !== "add" ? <Button variant="outline" onClick={copy}>{i18next.t("platform:Copy")}</Button> : null}
      grid
    >
      <fieldset disabled={readOnly} className="contents">
        <FormRow labelKey="general:Organization">
          <Input value={platform.owner} disabled />
        </FormRow>
        <FormRow labelKey="general:Name">
          <Input value={platform.name} onChange={(e) => updateField("name", e.target.value)} />
        </FormRow>
        <FormRow labelKey="general:Display name">
          <Input value={platform.displayName} onChange={(e) => updateField("displayName", e.target.value)} />
        </FormRow>
        <FormRow labelKey="general:Description">
          <Input value={platform.description} onChange={(e) => updateField("description", e.target.value)} />
        </FormRow>
        <FormRow labelKey="platform:Category">
          <SelectField
            value={platform.category}
            disabled={readOnly}
            options={PlatformBackend.PlatformCategories.map((category) => ({id: category, name: i18next.t(`platform:${category}`)}))}
            onChange={(category) => updateField("category", category)}
          />
        </FormRow>
        <FormRow label={i18next.t("platform:Protocols")} tooltip={i18next.t("platform:Protocols - Tooltip")}>
          <ProtocolsEditor value={platform.protocols ?? []} onChange={(protocols) => updateField("protocols", protocols)} />
        </FormRow>
        <FormRow label={i18next.t("platform:Charset")} tooltip={i18next.t("platform:Charset - Tooltip")}>
          <SelectField
            value={platform.charset || "UTF-8"}
            disabled={readOnly}
            options={PlatformBackend.PlatformCharsets.map((charset) => ({id: charset, name: charset}))}
            onChange={(charset) => updateField("charset", charset)}
          />
        </FormRow>
        <FormRow label={i18next.t("platform:Elevation")} tooltip={i18next.t("platform:Elevation - Tooltip")}>
          <div className="flex w-full items-center gap-2">
            <SelectField
              className="w-40"
              value={platform.elevation || "none"}
              disabled={readOnly}
              options={PlatformBackend.Elevations.map((elevation) => ({id: elevation || "none", name: elevation || i18next.t("platform:None")}))}
              onChange={(elevation) => updateField("elevation", elevation === "none" ? "" : elevation)}
            />
            {platform.elevation === "su" || platform.elevation === "enable" ? (
              <Input
                className="font-mono"
                value={platform.elevationCommand}
                placeholder={platform.elevation === "su" ? "su - root" : "enable"}
                onChange={(e) => updateField("elevationCommand", e.target.value)}
              />
            ) : null}
          </div>
        </FormRow>
        <FormRow label={i18next.t("platform:Method")} tooltip={i18next.t("platform:Method - Tooltip")}>
          <SelectField
            value={platform.method || "none"}
            disabled={readOnly}
            options={PlatformBackend.PlatformMethods.map((method) => ({id: method || "none", name: getMethodName(method)}))}
            onChange={(method) => changeMethod(method === "none" ? "" : method)}
          />
        </FormRow>
        {isCli ? (
          <>
            <FormRow label={i18next.t("platform:Change commands")} tooltip={i18next.t("platform:Change commands - Tooltip")}>
              <Textarea className="font-mono text-xs" rows={7} value={platform.changeCommands} onChange={(e) => updateField("changeCommands", e.target.value)} />
            </FormRow>
            <FormRow label={i18next.t("platform:Push commands")} tooltip={i18next.t("platform:Push commands - Tooltip")}>
              <Textarea className="font-mono text-xs" rows={7} value={platform.pushCommands} onChange={(e) => updateField("pushCommands", e.target.value)} />
            </FormRow>
            <FormRow label={i18next.t("platform:Prompt pattern")} tooltip={i18next.t("platform:Prompt pattern - Tooltip")}>
              <Input className="font-mono text-xs" value={platform.promptPattern} onChange={(e) => updateField("promptPattern", e.target.value)} />
            </FormRow>
            <FormRow label={i18next.t("platform:Error pattern")} tooltip={i18next.t("platform:Error pattern - Tooltip")}>
              <Input className="font-mono text-xs" value={platform.errorPattern} onChange={(e) => updateField("errorPattern", e.target.value)} />
            </FormRow>
          </>
        ) : null}
        <FormRow label={i18next.t("platform:Password length")} tooltip={i18next.t("platform:Password rules - Tooltip")}>
          <Input type="number" min={8} max={128} className="w-28" value={platform.passwordLength} onChange={(e) => updateField("passwordLength", Number(e.target.value))} />
        </FormRow>
        <FormRow label={i18next.t("platform:Characters")}>
          <div className="flex h-9 flex-wrap items-center gap-4 text-sm">
            {([["passwordUppercase", "A-Z"], ["passwordLowercase", "a-z"], ["passwordDigits", "0-9"]] as const).map(([field, label]) => (
              <label key={field} className="flex items-center gap-2">
                <Switch checked={platform[field]} onCheckedChange={(checked) => updateField(field, checked)} />
                <span className="font-mono">{label}</span>
              </label>
            ))}
          </div>
        </FormRow>
        <FormRow label={i18next.t("platform:Symbols")} tooltip={i18next.t("platform:Symbols - Tooltip")}>
          <Input className="font-mono" value={platform.passwordSymbols} onChange={(e) => updateField("passwordSymbols", e.target.value)} />
        </FormRow>
        <FormRow label={i18next.t("platform:Excluded characters")} tooltip={i18next.t("platform:Excluded characters - Tooltip")}>
          <Input className="font-mono" value={platform.passwordExclude} placeholder="0O1lI" onChange={(e) => updateField("passwordExclude", e.target.value)} />
        </FormRow>
        <FormRow label={i18next.t("platform:Change passwords")} tooltip={i18next.t("platform:Change passwords - Tooltip")}>
          <Switch checked={platform.canChangePassword} disabled={readOnly || !abilities.change} onCheckedChange={(checked) => updateField("canChangePassword", checked)} />
        </FormRow>
        <FormRow label={i18next.t("platform:Push accounts")} tooltip={i18next.t("platform:Push accounts - Tooltip")}>
          <Switch checked={platform.canPush} disabled={readOnly || !abilities.push} onCheckedChange={(checked) => updateField("canPush", checked)} />
        </FormRow>
        <FormRow label={i18next.t("platform:Gather accounts")} tooltip={i18next.t("platform:Gather accounts - Tooltip")}>
          <Switch checked={platform.canGather} disabled={readOnly || !abilities.gather} onCheckedChange={(checked) => updateField("canGather", checked)} />
        </FormRow>
      </fieldset>
    </EditPageShell>
  );
}

/** Remounts the editor on every navigation, so that a copy opened from this page starts afresh. */
export default function PlatformEditPage() {
  const location = useLocation();
  return <PlatformEditor key={location.key} />;
}
