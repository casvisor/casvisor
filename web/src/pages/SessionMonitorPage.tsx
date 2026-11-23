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
import {useParams} from "react-router-dom";
import * as SessionBackend from "@/backend/SessionBackend";
import {SessionStreamView} from "@/components/access/SessionStreamView";

/** Watches a connected session live; an administrator can take part in a terminal
 *  session and type in it, and the user is told who joined. */
export default function SessionMonitorPage() {
  const {owner = "", name = ""} = useParams();
  const sessionId = `${owner}/${name}`;
  const [asset, setAsset] = React.useState("");

  React.useEffect(() => {
    document.title = `${i18next.t("session:Monitor")} | ${name}`;
    SessionBackend.getSession(owner, name).then((res) => {
      if (res.status === "ok" && res.data) {
        setAsset(`${res.data.asset} | ${res.data.creator}`);
      }
    });
  }, [owner, name]);

  const getPath = React.useCallback(
    (control: boolean) => `/api/get-session-monitor?id=${encodeURIComponent(sessionId)}${control ? "&control=true" : ""}`,
    [sessionId],
  );

  return <SessionStreamView title={i18next.t("session:Monitor")} subtitle={asset || sessionId} getPath={getPath} canControl />;
}
