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
import {Lock, User} from "lucide-react";
import type {Asset} from "@/backend/types";
import {Button} from "@/components/ui/button";
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle} from "@/components/ui/dialog";
import {Input} from "@/components/ui/input";
import {PasswordInput} from "@/components/common/PasswordInput";
import {getAssetCredentialRequirements} from "@/lib/asset";
import * as Setting from "@/lib/setting";

type ConnectCredentialsDialogProps = {
  asset: Asset;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  initialUsername?: string;
  notice?: string;
  onSubmit: (username: string, password: string) => Promise<boolean>;
};

/** Asks for the credentials the protocol needs and the asset does not keep. */
export function ConnectCredentialsDialog({asset, open, onOpenChange, initialUsername, notice, onSubmit}: ConnectCredentialsDialogProps) {
  const [username, setUsername] = React.useState(initialUsername || asset.username || "");
  const [password, setPassword] = React.useState("");
  const [submitting, setSubmitting] = React.useState(false);
  const submittingRef = React.useRef(false);
  const noticeId = React.useId();
  const {needsUsername, needsPassword} = getAssetCredentialRequirements(asset);
  const canSubmit = (!needsUsername || username !== "") && (!needsPassword || password !== "");

  React.useEffect(() => {
    if (open) {
      setUsername(initialUsername || asset.username || "");
    }
    setPassword("");
  }, [open, initialUsername, asset.username]);

  const changeOpen = (next: boolean) => {
    if (!next) {
      if (submittingRef.current) {
        return;
      }
      setPassword("");
    }
    onOpenChange(next);
  };

  const submit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!open || !canSubmit || submittingRef.current) {
      return;
    }
    submittingRef.current = true;
    setSubmitting(true);
    try {
      if (await onSubmit(needsUsername ? username : "", needsPassword ? password : "")) {
        setPassword("");
        onOpenChange(false);
      }
    } catch (err) {
      Setting.showMessage("error", err instanceof Error ? err.message : i18next.t("asset:Failed to connect"));
    } finally {
      submittingRef.current = false;
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={changeOpen}>
      <DialogContent className="max-w-sm" aria-describedby={notice ? noticeId : undefined} hideClose={submitting}>
        <DialogHeader>
          <DialogTitle>{i18next.t("general:Connect")}: {asset.displayName || asset.name}</DialogTitle>
        </DialogHeader>
        {notice ? (
          <DialogDescription id={noticeId} className="rounded-md border bg-muted p-3 text-foreground">
            {notice}
          </DialogDescription>
        ) : null}
        <form className="space-y-3" onSubmit={submit}>
          {asset.protocol !== "VNC" && (
            <div className="relative">
              <User className="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                className="pl-8"
                placeholder={i18next.t("general:Username")}
                aria-label={i18next.t("general:Username")}
                value={username}
                disabled={!needsUsername || submitting}
                required={needsUsername}
                autoFocus={needsUsername}
                autoComplete="username"
                onChange={(e) => setUsername(e.target.value)}
              />
            </div>
          )}
          {needsPassword && (
            <div className="relative">
              <Lock className="absolute left-2.5 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <PasswordInput
                className="[&_input]:pl-8"
                placeholder={i18next.t("general:Password")}
                aria-label={i18next.t("general:Password")}
                value={password}
                disabled={submitting}
                required
                autoFocus={!needsUsername}
                autoComplete="current-password"
                onChange={(e) => setPassword(e.target.value)}
              />
            </div>
          )}
          <DialogFooter>
            <Button type="submit" className="w-full" disabled={!canSubmit || submitting}>
              {i18next.t("general:Connect")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
