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
import {ShieldCheck} from "lucide-react";
import * as AccountBackend from "@/backend/AccountBackend";
import {PasswordInput} from "@/components/common/PasswordInput";
import {Badge} from "@/components/ui/badge";
import {Button} from "@/components/ui/button";
import {Card, CardContent, CardHeader, CardTitle} from "@/components/ui/card";
import {Input} from "@/components/ui/input";
import * as Setting from "@/lib/setting";

interface MfaSetup {
  secret: string;
  url: string;
  qrCode: string;
}

/** Turns two-factor sign-in with an authenticator app on and off for a local account. */
export function MfaCard() {
  const [enabled, setEnabled] = React.useState<boolean | null>(null);
  const [setup, setSetup] = React.useState<MfaSetup | null>(null);
  const [code, setCode] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [busy, setBusy] = React.useState(false);

  const load = React.useCallback(() => {
    AccountBackend.getAccount().then((res) => {
      if (res.status === "ok") {
        setEnabled(Boolean(res.data2?.mfaEnabled));
      }
    });
  }, []);

  React.useEffect(load, [load]);

  const start = async() => {
    setBusy(true);
    try {
      const res = await AccountBackend.getMfaSetup();
      if (res.status === "ok") {
        setSetup(res.data);
        setCode("");
      } else {
        Setting.showMessage("error", res.msg);
      }
    } finally {
      setBusy(false);
    }
  };

  const enable = async() => {
    setBusy(true);
    try {
      const res = await AccountBackend.enableMfa(code);
      if (res.status === "ok") {
        Setting.showMessage("success", i18next.t("account:Two-factor sign-in is on"));
        setSetup(null);
        load();
      } else {
        Setting.showMessage("error", res.msg);
      }
    } finally {
      setBusy(false);
    }
  };

  const disable = async() => {
    setBusy(true);
    try {
      const res = await AccountBackend.disableMfa(password);
      if (res.status === "ok") {
        Setting.showMessage("success", i18next.t("account:Two-factor sign-in is off"));
        setPassword("");
        load();
      } else {
        Setting.showMessage("error", res.msg);
      }
    } finally {
      setBusy(false);
    }
  };

  if (enabled === null) {
    return null;
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <ShieldCheck className="h-4 w-4" />
          {i18next.t("account:Two-factor sign-in")}
          {enabled ? <Badge variant="success">{i18next.t("account:On")}</Badge> : <Badge variant="secondary">{i18next.t("account:Off")}</Badge>}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <p className="text-sm text-muted-foreground">
          {i18next.t("account:Ask for a code from an authenticator app such as Google Authenticator, Microsoft Authenticator or 1Password after the password")}
        </p>
        {enabled ? (
          <form
            className="flex flex-wrap items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              disable();
            }}
          >
            <PasswordInput
              className="w-64"
              placeholder={i18next.t("general:Password")}
              value={password}
              autoComplete="current-password"
              onChange={(e) => setPassword(e.target.value)}
            />
            <Button type="submit" variant="outline" disabled={!password} loading={busy}>{i18next.t("account:Turn off")}</Button>
          </form>
        ) : setup ? (
          <form
            className="space-y-3"
            onSubmit={(e) => {
              e.preventDefault();
              enable();
            }}
          >
            <div className="flex flex-wrap items-start gap-4">
              <img src={setup.qrCode} alt="QR code" className="h-40 w-40 rounded border bg-white p-1" />
              <div className="min-w-0 flex-1 space-y-2 text-sm">
                <p>{i18next.t("account:Scan the QR code with the authenticator app, or enter this key")}</p>
                <code className="block break-all rounded bg-muted px-2 py-1 font-mono text-xs">{setup.secret}</code>
                <p>{i18next.t("account:Then enter the 6-digit code it shows")}</p>
                <div className="flex gap-2">
                  <Input
                    className="w-32 font-mono tracking-widest"
                    inputMode="numeric"
                    autoComplete="one-time-code"
                    maxLength={6}
                    value={code}
                    onChange={(e) => setCode(e.target.value.replace(/\D/g, ""))}
                  />
                  <Button type="submit" disabled={code.length !== 6} loading={busy}>{i18next.t("account:Turn on")}</Button>
                  <Button type="button" variant="ghost" onClick={() => setSetup(null)}>{i18next.t("general:Cancel")}</Button>
                </div>
              </div>
            </div>
          </form>
        ) : (
          <Button variant="outline" loading={busy} onClick={start}>{i18next.t("account:Set up")}</Button>
        )}
      </CardContent>
    </Card>
  );
}
