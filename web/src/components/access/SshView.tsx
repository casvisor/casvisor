// Copyright 2024 The casbin Authors. All Rights Reserved.
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
import qs from "qs";
import {Terminal} from "@xterm/xterm";
import {FitAddon} from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import * as SessionBackend from "@/backend/SessionBackend";
import {ConnectionToolbar, FailureDialog, useConnectionToast} from "@/components/access/ConnectionChrome";
import {FilePanel} from "@/components/access/FilePanel";
import {closeView, debounce, toggleFullscreen, type ConnectionProps} from "@/components/access/util";
import * as Setting from "@/lib/setting";

const NotReachable = "Remote service exception, please check if the target device can be accessed normally.";

/** An SSH or Telnet shell in xterm.js, talking to the backend's native tunnel. */
export default function SshView(props: ConnectionProps & {protocol: string}) {
  const {assetId, inTab, addClient, username, password, protocol} = props;
  const assetName = Setting.getShortName(assetId);

  const containerRef = React.useRef<HTMLDivElement>(null);
  const terminalRef = React.useRef<Terminal | null>(null);
  const fitAddonRef = React.useRef<FitAddon | null>(null);
  const wsRef = React.useRef<WebSocket | null>(null);
  const [failure, setFailure] = React.useState<string | null>(null);
  const [session, setSession] = React.useState<{id: string; operations: string[]} | null>(null);
  const [showFiles, setShowFiles] = React.useState(false);
  const status = useConnectionToast(assetId);

  const send = (msg: any) => {
    const ws = wsRef.current;
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(msg));
    }
  };

  const connect = () => {
    const terminal = terminalRef.current;
    if (!terminal) {
      return;
    }
    status.loading(i18next.t("asset:Connecting..."));
    setSession(null);

    SessionBackend.addAssetTunnel(assetId).then((res) => {
      if (terminalRef.current !== terminal) {
        return;
      }
      if (res.status !== "ok") {
        status.dismiss();
        Setting.showMessage("error", `${i18next.t("asset:Failed to connect")}: ${res.msg}`);
        return;
      }

      const session = res.data;
      fitAddonRef.current?.fit();

      const params: Record<string, any> = {
        sessionId: `${session.owner}/${session.name}`,
        cols: terminal.cols,
        rows: terminal.rows,
      };
      if (username || password) {
        params.username = username;
        params.password = password;
      }

      let errorMessage = "";
      let connected = false;
      const ws = new WebSocket(Setting.getWebSocketUrl(`/api/get-asset-${protocol === "Telnet" ? "telnet" : "ssh"}?${qs.stringify(params)}`));
      ws.binaryType = "arraybuffer";
      wsRef.current = ws;

      ws.onmessage = (event) => {
        if (typeof event.data !== "string") {
          terminal.write(new Uint8Array(event.data));
          return;
        }

        const msg = JSON.parse(event.data);
        if (msg.type === "connected") {
          connected = true;
          setSession({id: `${session.owner}/${session.name}`, operations: session.operations ?? []});
          status.success(i18next.t("asset:Connection successful"));
          terminal.focus();
        } else if (msg.type === "error") {
          errorMessage = msg.message;
        }
      };

      ws.onclose = () => {
        if (wsRef.current !== ws) {
          return;
        }
        status.dismiss();
        setSession(null);
        setShowFiles(false);
        if (errorMessage) {
          setFailure(errorMessage);
        } else if (!connected) {
          setFailure(NotReachable);
        } else {
          terminal.write("\r\n\x1b[33mConnection closed\x1b[0m\r\n");
          status.error(i18next.t("asset:Connection closed"));
        }
      };
    });
  };

  React.useEffect(() => {
    if (!inTab) {
      document.title = assetName;
    }

    const terminal = new Terminal({
      cursorBlink: true,
      fontSize: 14,
      fontFamily: "Menlo, Consolas, \"DejaVu Sans Mono\", monospace",
      theme: {background: "#1b1b1b"},
    });
    const fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.open(containerRef.current!);
    fitAddon.fit();
    terminal.focus();

    terminal.onData((data) => send({type: "data", data}));
    terminal.onResize(({cols, rows}) => send({type: "resize", cols, rows}));

    terminalRef.current = terminal;
    fitAddonRef.current = fitAddon;

    const resizeObserver = new ResizeObserver(debounce(() => fitAddon.fit(), 100));
    resizeObserver.observe(containerRef.current!);

    addClient?.({disconnect: () => wsRef.current?.close()});
    connect();

    return () => {
      resizeObserver.disconnect();
      const ws = wsRef.current;
      wsRef.current = null;
      ws?.close();
      terminalRef.current = null;
      terminal.dispose();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [assetId]);

  const reconnect = () => {
    setFailure(null);
    terminalRef.current?.reset();
    connect();
  };

  const canTransfer = session !== null && protocol === "SSH" && session.operations.includes("fileSystem");

  return (
    <div className="flex h-full w-full">
      <div className="relative h-full min-w-0 flex-1 bg-[#1b1b1b] p-2">
        <div ref={containerRef} className="h-full w-full" />
        <ConnectionToolbar
          onFullscreen={() => {
            toggleFullscreen();
            terminalRef.current?.focus();
          }}
          onFiles={canTransfer ? () => setShowFiles(!showFiles) : undefined}
        />
      </div>
      {canTransfer && showFiles ? (
        <FilePanel sessionId={session.id} operations={session.operations} onClose={() => setShowFiles(false)} />
      ) : null}
      <FailureDialog assetName={assetName} message={failure} onReconnect={reconnect} onClose={() => closeView(props)} />
    </div>
  );
}
