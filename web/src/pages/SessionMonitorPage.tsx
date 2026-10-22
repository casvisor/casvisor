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
import {Eye} from "lucide-react";
import {Terminal} from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import * as SessionBackend from "@/backend/SessionBackend";
import {Button} from "@/components/ui/button";
import {createScreen, type ReplayScreen} from "@/components/replay/screens";
import * as Setting from "@/lib/setting";

/** Watches a connected session live, read only: a terminal at the size the
 *  user's has, or a VNC or RDP screen drawn from the session's recording. */
export default function SessionMonitorPage() {
  const {owner = "", name = ""} = useParams();
  const sessionId = `${owner}/${name}`;
  const containerRef = React.useRef<HTMLDivElement>(null);
  const [status, setStatus] = React.useState<"connecting" | "watching" | "ended">("connecting");
  const [message, setMessage] = React.useState("");
  const [asset, setAsset] = React.useState("");
  const [isScreen, setIsScreen] = React.useState(false);

  React.useEffect(() => {
    SessionBackend.getSession(owner, name).then((res) => {
      if (res.status === "ok" && res.data) {
        setAsset(`${res.data.asset} | ${res.data.creator}`);
      }
    });
  }, [owner, name]);

  React.useEffect(() => {
    document.title = `${i18next.t("session:Monitor")} | ${name}`;
    const container = containerRef.current!;
    let terminal: Terminal | null = null;
    let screen: ReplayScreen | null = null;
    const getTerminal = () => {
      if (!terminal) {
        terminal = new Terminal({
          disableStdin: true,
          cursorBlink: false,
          fontSize: 14,
          fontFamily: "Menlo, Consolas, \"DejaVu Sans Mono\", monospace",
          theme: {background: "#1b1b1b"},
        });
        terminal.open(container);
      }
      return terminal;
    };

    const ws = new WebSocket(Setting.getWebSocketUrl(`/api/get-session-monitor?id=${encodeURIComponent(sessionId)}`));
    ws.binaryType = "arraybuffer";
    ws.onmessage = (event) => {
      if (typeof event.data !== "string") {
        const data = new Uint8Array(event.data);
        if (screen) {
          // a frame of the recording: its kind, then its data
          void screen.apply(data[0], data.subarray(1));
        } else {
          getTerminal().write(data);
        }
        return;
      }
      const msg = JSON.parse(event.data);
      if (msg.type === "header") {
        setIsScreen(true);
        screen = createScreen(container, msg.header);
      } else if (msg.type === "resize" && msg.cols > 0 && msg.rows > 0) {
        getTerminal().resize(msg.cols, msg.rows);
      } else if (msg.type === "connected") {
        setStatus("watching");
      } else if (msg.type === "error") {
        setMessage(msg.message);
      }
    };
    ws.onclose = (event) => {
      setStatus("ended");
      if (event.reason) {
        setMessage((current) => current || event.reason);
      }
    };

    return () => {
      ws.close();
      terminal?.dispose();
      screen?.dispose();
    };
  }, [sessionId, name]);

  return (
    <div className="flex h-screen w-screen flex-col bg-[#1b1b1b] text-neutral-200">
      <div className="flex items-center gap-3 border-b border-neutral-700 px-4 py-2 text-sm">
        <Eye className="h-4 w-4" />
        <span className="font-medium">{i18next.t("session:Monitor")}</span>
        <span className="truncate text-neutral-400">{asset || sessionId}</span>
        <span className="ml-auto flex items-center gap-2">
          <span className={status === "watching" ? "h-2 w-2 rounded-full bg-red-500" : "h-2 w-2 rounded-full bg-neutral-500"} />
          {status === "watching" ? i18next.t("session:Live") : status === "ended" ? i18next.t("session:Ended") : i18next.t("asset:Connecting...")}
        </span>
        {status === "ended" ? (
          <Button size="sm" variant="secondary" onClick={() => window.location.reload()}>{i18next.t("asset:Reconnect")}</Button>
        ) : null}
      </div>
      {message ? <div className="bg-neutral-800 px-4 py-2 text-sm text-amber-300">{message}</div> : null}
      <div className={isScreen ? "min-h-0 flex-1" : "min-h-0 flex-1 overflow-auto p-2"}>
        <div ref={containerRef} className={isScreen ? "h-full w-full" : undefined} />
      </div>
    </div>
  );
}
