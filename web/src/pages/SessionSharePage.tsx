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
import {useParams} from "react-router-dom";
import * as SessionShareBackend from "@/backend/SessionShareBackend";
import {SessionStreamView} from "@/components/access/SessionStreamView";
import {Loading} from "@/components/common/Loading";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";

interface ShareInfo {
  owner: string;
  name: string;
  asset: string;
  creator: string;
  mode: string;
  expireTime: string;
}

/** Joins a session someone shared: the code they handed over first, then the session, to
 *  watch it or, when the share allows it, to type in it too. */
export default function SessionSharePage() {
  const {owner = "", name = ""} = useParams();
  const shareId = `${owner}/${name}`;
  const [info, setInfo] = React.useState<ShareInfo | null>(null);
  const [error, setError] = React.useState("");
  const [code, setCode] = React.useState("");
  const [joined, setJoined] = React.useState(false);
  const [refusal, setRefusal] = React.useState("");
  const onRefused = React.useCallback((message: string) => {
    setJoined(false);
    setRefusal(message);
  }, []);

  React.useEffect(() => {
    document.title = i18next.t("session:Shared session");
    SessionShareBackend.getSessionShare(shareId).then((res) => {
      if (res.status === "ok") {
        setInfo(res.data);
      } else {
        setError(res.msg);
      }
    });
  }, [shareId]);

  const getPath = React.useCallback(
    (control: boolean) => `/api/join-session-share?id=${encodeURIComponent(shareId)}&code=${encodeURIComponent(code)}${control ? "&control=true" : ""}`,
    [shareId, code],
  );

  if (joined && info) {
    return (
      <SessionStreamView
        title={i18next.t("session:Shared session")}
        subtitle={`${info.asset} | ${info.creator}`}
        getPath={getPath}
        canControl={info.mode === "Control"}
        onRefused={onRefused}
      />
    );
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-muted/40 p-4">
      <div className="w-full max-w-sm rounded-lg border bg-background p-6 shadow-sm">
        <h1 className="text-lg font-semibold">{i18next.t("session:Shared session")}</h1>
        {error ? (
          <p className="mt-3 text-sm text-destructive">{error}</p>
        ) : !info ? (
          <Loading className="h-24" />
        ) : (
          <form
            className="mt-4 grid gap-3 text-sm"
            onSubmit={(e) => {
              e.preventDefault();
              setRefusal("");
              setJoined(true);
            }}
          >
            <p className="text-muted-foreground">
              {i18next.t(info.mode === "Control" ? "session:{{user}} shares the session on {{asset}} with you to watch and type in it" : "session:{{user}} shares the session on {{asset}} with you to watch it", {user: info.creator, asset: info.asset, interpolation: {escapeValue: false}})}
            </p>
            <p className="text-xs text-muted-foreground">{i18next.t("session:Until {{time}}", {time: dayjs(info.expireTime).format("YYYY-MM-DD HH:mm")})}</p>
            <Input
              autoFocus
              inputMode="numeric"
              autoComplete="one-time-code"
              placeholder={i18next.t("session:The code they gave you")}
              value={code}
              className="text-center font-mono text-lg tracking-widest placeholder:font-sans placeholder:text-sm placeholder:tracking-normal"
              onChange={(e) => setCode(e.target.value.replace(/\D/g, "").slice(0, 6))}
            />
            {refusal ? <p className="text-sm text-destructive">{refusal}</p> : null}
            <Button type="submit" disabled={code.length !== 6}>{i18next.t("session:Join")}</Button>
          </form>
        )}
      </div>
    </div>
  );
}
