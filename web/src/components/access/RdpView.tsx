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
import qs from "qs";
import * as SessionBackend from "@/backend/SessionBackend";
import {ClipboardDialog, ConnectionToolbar, FailureDialog, useConnectionToast} from "@/components/access/ConnectionChrome";
import {FilePanel} from "@/components/access/FilePanel";
import {closeView, toggleFullscreen, type ConnectionProps} from "@/components/access/util";
import * as Setting from "@/lib/setting";
import {keysymToScancode, codeToScancode} from "@/components/access/rdpKeymap";

const NotReachable = "Remote service exception, please check if the target device can be accessed normally.";

/** An RDP screen drawn on a canvas from PNG tiles, decoded on the gateway by the
 *  native gordp client. Mouse and keyboard are relayed back. */
export default function RdpView(props: ConnectionProps) {
  const {assetId, inTab, addClient, username, password} = props;
  const assetName = Setting.getShortName(assetId);

  const canvasRef = React.useRef<HTMLCanvasElement>(null);
  const wsRef = React.useRef<WebSocket | null>(null);
  const ctxRef = React.useRef<CanvasRenderingContext2D | null>(null);
  const activeRef = React.useRef(false);
  const operationsRef = React.useRef<string[]>([]);
  const lastClipboardRef = React.useRef("");
  const [clipboardText, setClipboardText] = React.useState("");
  const [clipboardOpen, setClipboardOpen] = React.useState(false);
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

  const hasOperation = (operation: string) => operationsRef.current.includes(operation);

  // Offer text to the remote clipboard, skipping what it already has.
  const sendClipboard = (text: string) => {
    if (!text || text === lastClipboardRef.current || !hasOperation("paste")) {
      return;
    }
    lastClipboardRef.current = text;
    send({t: "clipboard", text});
  };

  const disconnect = () => {
    const ws = wsRef.current;
    if (ws) {
      wsRef.current = null;
      ws.close();
    }
  };

  // Map a mouse event to remote desktop pixel coordinates.
  const toRemote = (e: React.MouseEvent | WheelEvent) => {
    const canvas = canvasRef.current!;
    const rect = canvas.getBoundingClientRect();
    const x = ((e.clientX - rect.left) / rect.width) * canvas.width;
    const y = ((e.clientY - rect.top) / rect.height) * canvas.height;
    return {x: Math.max(0, Math.round(x)), y: Math.max(0, Math.round(y))};
  };

  const connect = () => {
    status.loading(i18next.t("asset:Connecting..."));

    SessionBackend.addAssetTunnel(assetId).then((res) => {
      if (!activeRef.current) {
        return;
      }
      if (res.status !== "ok") {
        status.dismiss();
        Setting.showMessage("error", `${i18next.t("asset:Failed to connect")}: ${res.msg}`);
        return;
      }

      const session = res.data;
      operationsRef.current = session.operations ?? [];
      lastClipboardRef.current = "";
      const canvas = canvasRef.current!;
      const params: Record<string, any> = {
        sessionId: `${session.owner}/${session.name}`,
        width: Math.round(canvas.parentElement?.clientWidth || 1024),
        height: Math.round(canvas.parentElement?.clientHeight || 768),
      };
      if (username || password) {
        params.username = username;
        params.password = password;
      }

      const ws = new WebSocket(Setting.getWebSocketUrl(`/api/get-asset-rdp?${qs.stringify(params)}`), ["binary"]);
      ws.binaryType = "arraybuffer";
      wsRef.current = ws;
      let connected = false;
      let failureReason = "";

      ws.onmessage = (event) => {
        if (typeof event.data === "string") {
          const msg = JSON.parse(event.data);
          if (msg.t === "init") {
            canvas.width = msg.width;
            canvas.height = msg.height;
            ctxRef.current = canvas.getContext("2d");
            connected = true;
            status.success(i18next.t("asset:Connection successful"));
            SessionBackend.connect(`${session.owner}/${session.name}`);
            setSession({id: `${session.owner}/${session.name}`, operations: session.operations ?? []});
            canvas.focus();
          } else if (msg.t === "size") {
            if (canvas.width !== msg.width || canvas.height !== msg.height) {
              canvas.width = msg.width;
              canvas.height = msg.height;
            }
          } else if (msg.t === "clipboard") {
            lastClipboardRef.current = msg.text;
            setClipboardText(msg.text);
            navigator.clipboard?.writeText(msg.text).catch(() => undefined);
          } else if (msg.t === "error") {
            failureReason = msg.message || NotReachable;
          }
          return;
        }
        drawTile(event.data as ArrayBuffer);
      };

      ws.onclose = () => {
        if (wsRef.current !== ws) {
          return;
        }
        wsRef.current = null;
        setSession(null);
        setShowFiles(false);
        status.dismiss();
        if (failureReason) {
          setFailure(failureReason);
        } else if (!connected) {
          setFailure(NotReachable);
        } else {
          status.error(i18next.t("asset:Connection closed"));
        }
      };
      ws.onerror = () => {
        if (!connected && !failureReason) {
          failureReason = NotReachable;
        }
      };
    });
  };

  const drawTile = (data: ArrayBuffer) => {
    const view = new DataView(data);
    const x = view.getUint16(0, true);
    const y = view.getUint16(2, true);
    const blob = new Blob([data.slice(8)], {type: "image/png"});
    createImageBitmap(blob).then((bmp) => {
      ctxRef.current?.drawImage(bmp, x, y);
      bmp.close();
    }).catch(() => undefined);
  };

  React.useEffect(() => {
    if (!inTab) {
      document.title = assetName;
    }
    addClient?.({disconnect});
    activeRef.current = true;
    connect();
    return () => {
      activeRef.current = false;
      disconnect();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [assetId]);

  React.useEffect(() => {
    // what the user copied on this machine is there to paste on the remote one
    const handleWindowFocus = () => {
      if (navigator.clipboard && wsRef.current && hasOperation("paste")) {
        navigator.clipboard.readText().then(sendClipboard).catch(() => undefined);
      }
    };
    window.addEventListener("focus", handleWindowFocus);
    return () => window.removeEventListener("focus", handleWindowFocus);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  React.useEffect(() => {
    // follow the size of the view with the remote desktop once resizing settles
    const container = canvasRef.current?.parentElement;
    if (!container) {
      return;
    }
    let timer: ReturnType<typeof setTimeout> | undefined;
    let lastWidth = Math.round(container.clientWidth);
    let lastHeight = Math.round(container.clientHeight);
    const observer = new ResizeObserver(() => {
      clearTimeout(timer);
      timer = setTimeout(() => {
        const width = Math.round(container.clientWidth);
        const height = Math.round(container.clientHeight);
        if ((width === lastWidth && height === lastHeight) || width < 200 || height < 200) {
          return;
        }
        lastWidth = width;
        lastHeight = height;
        send({t: "resize", width, height});
      }, 500);
    });
    observer.observe(container);
    return () => {
      clearTimeout(timer);
      observer.disconnect();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const onMouseMove = (e: React.MouseEvent) => {
    const {x, y} = toRemote(e);
    send({t: "mm", x, y});
  };
  const onMouseButton = (e: React.MouseEvent, down: boolean) => {
    e.preventDefault();
    canvasRef.current?.focus();
    const {x, y} = toRemote(e);
    send({t: "mb", button: e.button, down, x, y});
  };
  const onWheel = (e: React.WheelEvent) => {
    const {x, y} = toRemote(e);
    send({t: "mw", x, y, delta: e.deltaY < 0 ? 120 : -120});
  };
  const onKey = (e: React.KeyboardEvent, down: boolean) => {
    const mapped = codeToScancode(e.code);
    if (!mapped) {
      return;
    }
    e.preventDefault();
    send({t: "k", code: mapped.code, down, extended: mapped.extended});
  };

  const sendCombinationKey = (keys: number[]) => {
    const mapped = keys.map(keysymToScancode).filter(Boolean) as {code: number; extended: boolean}[];
    for (const k of mapped) {
      send({t: "k", code: k.code, down: true, extended: k.extended});
    }
    for (const k of [...mapped].reverse()) {
      send({t: "k", code: k.code, down: false, extended: k.extended});
    }
    canvasRef.current?.focus();
  };

  const canTransfer = session !== null && session.operations.includes("fileSystem");

  return (
    <div className="flex h-full w-full">
      <div className="relative h-full min-w-0 flex-1 bg-[#1b1b1b]">
        <canvas
          ref={canvasRef}
          tabIndex={0}
          className="h-full w-full object-contain outline-none"
          onMouseMove={onMouseMove}
          onMouseDown={(e) => onMouseButton(e, true)}
          onMouseUp={(e) => onMouseButton(e, false)}
          onContextMenu={(e) => e.preventDefault()}
          onWheel={onWheel}
          onKeyDown={(e) => onKey(e, true)}
          onKeyUp={(e) => onKey(e, false)}
        />
        <ConnectionToolbar
          onFullscreen={() => {
            toggleFullscreen();
            canvasRef.current?.focus();
          }}
          onClipboard={() => setClipboardOpen(true)}
          onHotKey={sendCombinationKey}
          onFiles={canTransfer ? () => setShowFiles(!showFiles) : undefined}
        />
      </div>
      {canTransfer && showFiles ? (
        <FilePanel sessionId={session.id} operations={session.operations} onClose={() => setShowFiles(false)} />
      ) : null}
      <ClipboardDialog
        open={clipboardOpen}
        text={clipboardText}
        onOk={(text) => {
          if (!hasOperation("paste")) {
            Setting.showMessage("warning", i18next.t("asset:Can not paste"));
          } else {
            sendClipboard(text);
            setClipboardText(text);
          }
          setClipboardOpen(false);
          canvasRef.current?.focus();
        }}
        onCancel={() => {
          setClipboardOpen(false);
          canvasRef.current?.focus();
        }}
      />
      <FailureDialog
        assetName={assetName}
        message={failure}
        onReconnect={() => {
          setFailure(null);
          connect();
        }}
        onClose={() => closeView(props)}
      />
    </div>
  );
}
