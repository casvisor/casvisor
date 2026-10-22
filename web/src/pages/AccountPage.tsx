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
import * as AccountBackend from "@/backend/AccountBackend";
import {PasswordInput} from "@/components/common/PasswordInput";
import {PageHeader} from "@/components/crud/PageHeader";
import {Button} from "@/components/ui/button";
import {Card, CardContent, CardHeader, CardTitle} from "@/components/ui/card";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";

function Field({label, children}: {label: string; children: React.ReactNode}) {
  return (
    <div className="grid gap-1.5 sm:grid-cols-[140px_1fr] sm:items-center sm:gap-4">
      <span className="text-sm text-muted-foreground">{label}</span>
      <div className="min-w-0">{children}</div>
    </div>
  );
}

/** The signed-in user's own account; with local accounts, also where the password is changed. */
export default function AccountPage() {
  const {account} = useAccount();
  const [oldPassword, setOldPassword] = React.useState("");
  const [newPassword, setNewPassword] = React.useState("");
  const [confirmPassword, setConfirmPassword] = React.useState("");
  const [saving, setSaving] = React.useState(false);

  if (!account) {
    return null;
  }

  const mismatch = confirmPassword !== "" && confirmPassword !== newPassword;
  const groups = (account.groups ?? []).map((group: string) => Setting.getShortName(group)).join(", ");

  const submit = () => {
    setSaving(true);
    AccountBackend.changePassword(oldPassword, newPassword).then((res) => {
      setSaving(false);
      if (res.status === "ok") {
        Setting.showMessage("success", i18next.t("account:Password changed"));
        setOldPassword("");
        setNewPassword("");
        setConfirmPassword("");
      } else {
        Setting.showMessage("error", res.msg);
      }
    });
  };

  return (
    <div className="max-w-2xl space-y-4">
      <PageHeader title={i18next.t("account:My Account")} />
      <Card>
        <CardContent className="space-y-3 p-6">
          <Field label={i18next.t("general:Organization")}>{account.owner}</Field>
          <Field label={i18next.t("general:Username")}>{account.name}</Field>
          <Field label={i18next.t("general:Display name")}>{account.displayName || "-"}</Field>
          <Field label={i18next.t("permission:Groups")}>{groups || "-"}</Field>
          <Field label={i18next.t("user:Is admin")}>{account.isAdmin ? i18next.t("general:Yes") : i18next.t("general:No")}</Field>
        </CardContent>
      </Card>

      {Setting.isLocalAuth() ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{i18next.t("account:Change password")}</CardTitle>
          </CardHeader>
          <CardContent>
            <form
              className="space-y-3"
              onSubmit={(e) => {
                e.preventDefault();
                submit();
              }}
            >
              <Field label={i18next.t("account:Old password")}>
                <PasswordInput value={oldPassword} autoComplete="current-password" onChange={(e) => setOldPassword(e.target.value)} />
              </Field>
              <Field label={i18next.t("account:New password")}>
                <PasswordInput value={newPassword} autoComplete="new-password" onChange={(e) => setNewPassword(e.target.value)} />
              </Field>
              <Field label={i18next.t("account:Confirm password")}>
                <PasswordInput value={confirmPassword} autoComplete="new-password" onChange={(e) => setConfirmPassword(e.target.value)} />
              </Field>
              {mismatch ? <p className="text-sm text-destructive">{i18next.t("account:The two passwords do not match")}</p> : null}
              <div className="flex justify-end">
                <Button type="submit" disabled={!oldPassword || newPassword.length < 8 || newPassword !== confirmPassword} loading={saving}>
                  {i18next.t("account:Change password")}
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">{i18next.t("account:At least 8 characters")}</p>
            </form>
          </CardContent>
        </Card>
      ) : (
        <Button variant="outline" onClick={() => Setting.openLink(Setting.getMyProfileUrl(account))}>
          {i18next.t("account:Manage the account in Casdoor")}
        </Button>
      )}
    </div>
  );
}
