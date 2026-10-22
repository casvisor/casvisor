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
import {Navigate} from "react-router-dom";
import {Lock, User} from "lucide-react";
import * as AccountBackend from "@/backend/AccountBackend";
import {LanguageSelect} from "@/components/common/LanguageSelect";
import {Loading} from "@/components/common/Loading";
import {Logo} from "@/components/common/Logo";
import {PasswordInput} from "@/components/common/PasswordInput";
import {ThemeToggle} from "@/components/common/ThemeToggle";
import {Button} from "@/components/ui/button";
import {Card, CardContent} from "@/components/ui/card";
import {Input} from "@/components/ui/input";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";

/** Signs in with an account Casvisor keeps itself, when it runs without Casdoor. */
export default function LoginPage() {
  const {account, loading, reload} = useAccount();
  const [username, setUsername] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [error, setError] = React.useState("");
  const [submitting, setSubmitting] = React.useState(false);

  if (!Setting.isLocalAuth()) {
    window.location.replace(Setting.getSigninUrl());
    return <Loading className="min-h-screen" />;
  }
  if (loading) {
    return <Loading className="min-h-screen" />;
  }
  if (account) {
    const from = sessionStorage.getItem("from") || "/";
    sessionStorage.removeItem("from");
    return <Navigate to={from} replace />;
  }

  const submit = () => {
    setSubmitting(true);
    setError("");
    AccountBackend.signinLocal(username.trim(), password).then((res) => {
      setSubmitting(false);
      if (res.status === "ok") {
        Setting.showMessage("success", i18next.t("general:Successfully logged in"));
        reload();
      } else {
        setError(res.msg);
      }
    });
  };

  return (
    <div className="flex min-h-screen flex-col bg-muted/40">
      <div className="flex justify-end gap-1 p-3">
        <LanguageSelect />
        <ThemeToggle />
      </div>
      <div className="flex flex-1 items-start justify-center px-4 pt-[12vh]">
        <Card className="w-full max-w-sm">
          <CardContent className="p-6">
            <div className="mb-6 flex flex-col items-center gap-2">
              <Logo className="h-9" />
              <p className="text-sm text-muted-foreground">{i18next.t("login:Sign in to continue")}</p>
            </div>
            <form
              className="space-y-3"
              onSubmit={(e) => {
                e.preventDefault();
                submit();
              }}
            >
              <div className="relative">
                <User className="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  className="pl-8"
                  placeholder={i18next.t("general:Username")}
                  value={username}
                  autoFocus
                  autoComplete="username"
                  onChange={(e) => setUsername(e.target.value)}
                />
              </div>
              <div className="relative">
                <Lock className="absolute left-2.5 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                <PasswordInput
                  className="[&_input]:pl-8"
                  placeholder={i18next.t("general:Password")}
                  value={password}
                  autoComplete="current-password"
                  onChange={(e) => setPassword(e.target.value)}
                />
              </div>
              {error ? <p className="text-sm text-destructive">{error}</p> : null}
              <Button type="submit" className="w-full" disabled={!username.trim() || !password} loading={submitting}>
                {i18next.t("account:Sign In")}
              </Button>
            </form>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
