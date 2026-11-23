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
import dayjs from "dayjs";
import {Copy} from "lucide-react";
import * as PermissionBackend from "@/backend/PermissionBackend";
import * as SessionShareBackend from "@/backend/SessionShareBackend";
import type {Principal, Session, SessionShare} from "@/backend/types";
import {MultiSelect} from "@/components/common/MultiSelect";
import {SelectField} from "@/components/common/SelectField";
import {Button} from "@/components/ui/button";
import {useAccount} from "@/hooks/use-account";
import {Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle} from "@/components/ui/dialog";
import * as Setting from "@/lib/setting";

/** The protocols whose sessions others may type in too; the screens of VNC, RDP and
 *  Desktop sessions can only be watched. */
const controllableProtocols = ["SSH", "Telnet", "Kubernetes"];

const durations = [10, 30, 60, 120, 480];

export const SessionShareContext = React.createContext<{session: Session; protocol: string} | null>(null);

export function getShareUrl(share: SessionShare) {
  return `${window.location.origin}/share/${share.owner}/${share.name}`;
}

function copy(text: string) {
  navigator.clipboard.writeText(text).then(
    () => Setting.showMessage("success", i18next.t("session:Copied")),
    (e) => Setting.showMessage("error", String(e)),
  );
}

/** Shares the session with other users of the organization: a link and a code to hand them,
 *  and the shares still open, which can be revoked. */
export function ShareSessionDialog({session, protocol, onClose}: {session: Session; protocol: string; onClose: () => void}) {
  const sessionId = `${session.owner}/${session.name}`;
  const {account} = useAccount();
  const isAdmin = Setting.isAdminUser(account);
  const canControl = controllableProtocols.includes(protocol);
  const [users, setUsers] = React.useState<string[]>([]);
  const [userOptions, setUserOptions] = React.useState<Principal[]>([]);
  const [mode, setMode] = React.useState(canControl ? "Control" : "View");
  const [minutes, setMinutes] = React.useState(60);
  const [busy, setBusy] = React.useState(false);
  const [shares, setShares] = React.useState<SessionShare[]>([]);

  const refresh = React.useCallback(() => {
    SessionShareBackend.getSessionShares(sessionId).then((res) => res.status === "ok" && setShares(res.data ?? []));
  }, [sessionId]);

  React.useEffect(() => {
    refresh();
    // only administrators may list users, the others type the names
    if (isAdmin) {
      PermissionBackend.getUsers().then((res) => res.status === "ok" && setUserOptions(res.data ?? []));
    }
  }, [refresh, isAdmin]);

  const create = async() => {
    setBusy(true);
    try {
      const res = await SessionShareBackend.addSessionShare({session: sessionId, users, mode, expireMinutes: minutes});
      if (res.status === "ok") {
        refresh();
      } else {
        Setting.showMessage("error", res.msg);
      }
    } finally {
      setBusy(false);
    }
  };

  const revoke = async(share: SessionShare) => {
    const res = await SessionShareBackend.revokeSessionShare(`${share.owner}/${share.name}`);
    if (res.status === "ok") {
      refresh();
    } else {
      Setting.showMessage("error", res.msg);
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{i18next.t("session:Share this session")}</DialogTitle>
          <DialogDescription>{i18next.t("session:Share - Description")}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-3 text-sm">
          <label className="grid gap-1">
            <span className="text-muted-foreground">{i18next.t("session:Who may join")}</span>
            <MultiSelect
              creatable
              value={users}
              placeholder={i18next.t("session:Anyone in the organization")}
              options={userOptions.map((user) => ({value: user.name, label: user.displayName && user.displayName !== user.name ? `${user.displayName} (${user.name})` : user.name}))}
              onChange={setUsers}
            />
          </label>
          <div className="grid grid-cols-2 gap-3">
            <label className="grid gap-1">
              <span className="text-muted-foreground">{i18next.t("session:They may")}</span>
              <SelectField
                value={mode}
                options={(canControl ? ["Control", "View"] : ["View"]).map((item) => ({id: item, name: i18next.t(`session:Share ${item}`)}))}
                onChange={setMode}
              />
            </label>
            <label className="grid gap-1">
              <span className="text-muted-foreground">{i18next.t("session:For")}</span>
              <SelectField
                value={String(minutes)}
                options={durations.map((item) => ({id: String(item), name: item < 60 ? i18next.t("session:{{n}} minutes", {n: item}) : item === 60 ? i18next.t("session:1 hour") : i18next.t("session:{{n}} hours", {n: item / 60})}))}
                onChange={(value) => setMinutes(Number(value))}
              />
            </label>
          </div>
          <Button loading={busy} onClick={create}>{i18next.t("session:Create a share link")}</Button>
        </div>
        {shares.length > 0 ? (
          <div className="grid gap-2">
            {shares.map((share) => (
              <div key={share.name} className="grid gap-1.5 rounded-md border p-3 text-sm">
                <div className="flex items-center gap-2">
                  <code className="min-w-0 flex-1 truncate font-mono text-xs" title={getShareUrl(share)}>{getShareUrl(share)}</code>
                  <Button size="icon" variant="ghost" className="h-7 w-7" aria-label={i18next.t("session:Copy the link")} onClick={() => copy(getShareUrl(share))}><Copy /></Button>
                </div>
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                  <span>
                    {i18next.t("session:Code")}: <span className="font-mono text-base font-semibold tracking-widest text-foreground">{share.code}</span>
                  </span>
                  <Button size="sm" variant="outline" className="h-6 px-2 text-xs" onClick={() => copy(`${getShareUrl(share)}\n${i18next.t("session:Code")}: ${share.code}`)}>
                    {i18next.t("session:Copy the link and code")}
                  </Button>
                  <span>{i18next.t(`session:Share ${share.mode}`)}</span>
                  <span>{share.users && share.users.length > 0 ? share.users.join(", ") : i18next.t("session:Anyone in the organization")}</span>
                  <span>{i18next.t("session:Until {{time}}", {time: dayjs(share.expireTime).format("HH:mm")})}</span>
                  <span>{i18next.t("session:Joined {{n}} times", {n: share.joinCount})}</span>
                  <Button size="sm" variant="ghost" className="ml-auto h-6 px-2 text-xs text-destructive" onClick={() => revoke(share)}>{i18next.t("session:Revoke")}</Button>
                </div>
              </div>
            ))}
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
