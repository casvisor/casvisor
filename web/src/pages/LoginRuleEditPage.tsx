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
import * as LoginRuleBackend from "@/backend/LoginRuleBackend";
import * as PermissionBackend from "@/backend/PermissionBackend";
import type {LoginRule, Principal} from "@/backend/types";
import {Input} from "@/components/ui/input";
import {Switch} from "@/components/ui/switch";
import {Loading} from "@/components/common/Loading";
import {MultiSelect, type MultiSelectOption} from "@/components/common/MultiSelect";
import {SelectField} from "@/components/common/SelectField";
import {TagsInput} from "@/components/common/TagsInput";
import {UnauthorizedPage} from "@/components/common/UnauthorizedPage";
import {EditPageShell} from "@/components/crud/EditPageShell";
import {FormRow} from "@/components/crud/FormRow";
import {useEditRecord} from "@/hooks/use-edit-record";
import {getModeTitleKey, submitEdit} from "@/lib/crud";
import * as Setting from "@/lib/setting";
import {getWeekdayNames} from "@/lib/loginRule";
import NotFoundPage from "@/pages/NotFoundPage";

function getPrincipalOptions(principals: Principal[], withAll = true): MultiSelectOption[] {
  return [...(withAll ? [{name: "*", displayName: i18next.t("permission:All")}] : []), ...principals].map((item) => ({
    value: item.name,
    label: item.displayName && item.displayName !== item.name ? `${item.displayName} (${item.name})` : item.name,
    keywords: item.displayName,
  }));
}

const actionTips: Record<string, string> = {
  Allow: "loginRule:Allow - Tooltip",
  Reject: "loginRule:Reject - Tooltip",
  Review: "loginRule:Review - Tooltip",
  Mfa: "loginRule:Mfa - Tooltip",
};

export default function LoginRuleEditPage() {
  const {owner = "", name = ""} = useParams();
  const navigate = useNavigate();
  const [saving, setSaving] = React.useState(false);
  const [users, setUsers] = React.useState<Principal[]>([]);
  const [groups, setGroups] = React.useState<Principal[]>([]);

  const {record: rule, updateField, loading, notFound, denied, mode} = useEditRecord<LoginRule>({
    fetch: () => LoginRuleBackend.getLoginRule(owner, name),
    deps: [owner, name],
  });

  React.useEffect(() => {
    PermissionBackend.getUsers().then((res) => res.status === "ok" && setUsers(res.data ?? []));
    PermissionBackend.getGroups().then((res) => res.status === "ok" && setGroups(res.data ?? []));
  }, [owner]);

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
      add: LoginRuleBackend.addLoginRule,
      update: (record) => LoginRuleBackend.updateLoginRule(owner, name, record),
      onSaved: (record) => {
        if (exitAfterSave) {
          navigate("/login-rules");
        } else if (record.name !== name) {
          navigate(`/login-rules/${Setting.getIdPath(record.owner, record.name)}`, {replace: true});
        }
      },
      onFailed: () => updateField("name", name),
    }).finally(() => setSaving(false));
  };

  const weekdayNames = getWeekdayNames();
  const weekdayOrder = [1, 2, 3, 4, 5, 6, 0];

  return (
    <EditPageShell
      title={i18next.t(getModeTitleKey("loginRule:Edit Login Rule", mode))}
      mode={mode}
      backTo="/login-rules"
      onSave={save}
      saving={saving}
      remove={() => LoginRuleBackend.deleteLoginRule(rule)}
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
      <FormRow label={i18next.t("loginRule:Priority")} tooltip={i18next.t("loginRule:Priority - Tooltip")}>
        <Input type="number" value={rule.priority} onChange={(e) => updateField("priority", e.target.value === "" ? 0 : Number(e.target.value))} />
      </FormRow>
      <FormRow label={i18next.t("command:Action")} tooltip={i18next.t(actionTips[rule.action] ?? "loginRule:Allow - Tooltip")}>
        <SelectField
          value={rule.action}
          options={LoginRuleBackend.LoginActions.map((action) => ({id: action, name: i18next.t(`loginRule:${action}`)}))}
          onChange={(value) => updateField("action", value)}
        />
      </FormRow>
      <FormRow label={i18next.t("permission:Users")} tooltip={i18next.t("loginRule:Users - Tooltip")}>
        <MultiSelect creatable value={rule.users ?? []} options={getPrincipalOptions(users)} onChange={(value) => updateField("users", value)} />
      </FormRow>
      <FormRow labelKey="permission:Groups">
        <MultiSelect creatable value={rule.groups ?? []} options={getPrincipalOptions(groups)} onChange={(value) => updateField("groups", value)} />
      </FormRow>
      <FormRow label={i18next.t("permission:Client IPs")} tooltip={i18next.t("loginRule:Client IPs - Tooltip")}>
        <TagsInput value={rule.clientIps ?? []} placeholder="192.168.1.0/24" onChange={(value) => updateField("clientIps", value)} />
      </FormRow>
      <FormRow label={i18next.t("loginRule:Sign-in methods")} tooltip={i18next.t("loginRule:Sign-in methods - Tooltip")}>
        <MultiSelect
          value={rule.methods ?? []}
          options={LoginRuleBackend.LoginMethods.map((method) => ({value: method, label: i18next.t(`loginRule:${method}`)}))}
          placeholder={i18next.t("loginRule:All methods")}
          onChange={(value) => updateField("methods", value)}
        />
      </FormRow>
      <FormRow label={i18next.t("loginRule:Weekdays")} tooltip={i18next.t("loginRule:Weekdays - Tooltip")}>
        <MultiSelect
          value={(rule.weekdays ?? []).map(String)}
          options={weekdayOrder.map((day) => ({value: String(day), label: weekdayNames[day]}))}
          placeholder={i18next.t("loginRule:Every day")}
          onChange={(value) => updateField("weekdays", value.map(Number).sort())}
        />
      </FormRow>
      <FormRow label={i18next.t("loginRule:Time of day")} tooltip={i18next.t("loginRule:Time of day - Tooltip")}>
        <div className="flex w-full items-center gap-2">
          <Input type="time" value={rule.startTime} onChange={(e) => updateField("startTime", e.target.value)} />
          <span className="text-muted-foreground">-</span>
          <Input type="time" value={rule.endTime} onChange={(e) => updateField("endTime", e.target.value)} />
        </div>
      </FormRow>
      <FormRow label={i18next.t("loginRule:Time zone")} tooltip={i18next.t("loginRule:Time zone - Tooltip")}>
        <Input value={rule.timeZone} placeholder="Asia/Shanghai" onChange={(e) => updateField("timeZone", e.target.value.trim())} />
      </FormRow>
      {rule.action === "Review" ? (
        <FormRow label={i18next.t("loginRule:Reviewers")} tooltip={i18next.t("loginRule:Reviewers - Tooltip")}>
          <MultiSelect creatable value={rule.reviewers ?? []} options={getPrincipalOptions(users, false)} onChange={(value) => updateField("reviewers", value)} />
        </FormRow>
      ) : null}
      {rule.action === "Mfa" && !Setting.isLocalAuth() ? (
        <FormRow label="">
          <p className="text-sm text-muted-foreground">{i18next.t("loginRule:With Casdoor sign-in, two-step verification is set in Casdoor, and this rule lets the sign-in go on")}</p>
        </FormRow>
      ) : null}
      <FormRow labelKey="permission:Is enabled">
        <Switch checked={rule.isEnabled} onCheckedChange={(checked) => updateField("isEnabled", checked)} />
      </FormRow>
    </EditPageShell>
  );
}
