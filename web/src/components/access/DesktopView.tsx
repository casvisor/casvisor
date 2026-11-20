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
import {closeView, holdPageLock, reloadIfUpdated, toggleFullscreen, type ConnectionProps} from "@/components/access/util";
import {codeToScancode, keysymToScancode} from "@/components/access/rdpKeymap";
import {
  DesktopMessage,
  type DesktopDisplay,
  DesktopScreen,
  MouseAction,
  encodeJson,
  encodeKey,
  encodeMouse,
  encodeText,
  isWebCodecsSupported,
  messagePayload,
  parseJson,
} from "@/components/access/desktopScreen";
import * as Setting from "@/lib/setting";

const NotReachable = "The remote computer is offline or can't be reached.";

function scancodeOf(key: {code: number; extended: boolean}) {
  return key.extended ? 0xe000 | key.code : key.code;
}

/** A computer running Casvisor Desktop: its screen is H.264 decoded with
 *  WebCodecs, the mouse, keyboard and clipboard go back to it. */
export default function DesktopView(props: ConnectionProps) {
  const {assetId, inTab, addClient} = props;
  const assetName = Setting.getShortName(assetId);

  const canvasRef = React.useRef<HTMLCanvasElement>(null);
  const wsRef = React.useRef<WebSocket | null>(null);
  const activeRef = React.useRef(false);
  const operationsRef = React.useRef<string[]>([]);
  const lastClipboardRef = React.useRef("");
  const pressedRef = React.useRef(new Set<number>());
  const [clipboardText, setClipboardText] = React.useState("");
  const [clipboardOpen, setClipboardOpen] = React.useState(false);
  const [failure, setFailure] = React.useState<string | null>(null);
  const [overlay, setOverlay] = React.useState<string | null>(null);
  const [displays, setDisplays] = React.useState<{displays: DesktopDisplay[]; current: number} | null>(null);
  const status = useConnectionToast(assetId);

  const sendRaw = (msg: Uint8Array) => {
    const ws = wsRef.current;
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(msg);
    }
  };

  const hasOperation = (operation: string) => operationsRef.current.includes(operation);

  const sendClipboard = (text: string) => {
    if (!text || text === lastClipboardRef.current || !hasOperation("paste")) {
      return;
    }
    lastClipboardRef.current = text;
    sendRaw(encodeText(DesktopMessage.Clipboard, text));
  };

  const disconnect = () => {
    const ws = wsRef.current;
    wsRef.current = null;
    ws?.close();
  };

  const connect = () => {
    if (!isWebCodecsSupported()) {
      setFailure(i18next.t("asset:This browser can't decode the remote screen, use a recent Chrome, Edge, Firefox or Safari"));
      return;
    }
    status.loading(i18next.t("asset:Connecting..."));

    SessionBackend.addAssetTunnel(assetId, props.account).then((res) => {
      if (!activeRef.current) {
        return;
      }
      if (res.status !== "ok") {
        status.dismiss();
        Setting.showMessage("error", `${i18next.t("asset:Failed to connect")}: ${res.msg}`);
        return;
      }

      const session = res.data;
      const sessionId = `${session.owner}/${session.name}`;
      operationsRef.current = session.operations ?? [];
      lastClipboardRef.current = "";
      const canvas = canvasRef.current!;
      const screen = new DesktopScreen(canvas);
      screen.onNeedKeyFrame = () => sendRaw(encodeJson(DesktopMessage.Control, {type: "keyframe"}));

      const ws = new WebSocket(Setting.getWebSocketUrl(`/api/get-asset-desktop?${qs.stringify({sessionId})}`), ["binary"]);
      ws.binaryType = "arraybuffer";
      wsRef.current = ws;
      let connected = false;
      let failureReason = "";

      ws.onmessage = (event) => {
        if (typeof event.data === "string") {
          return;
        }
        const msg = new Uint8Array(event.data as ArrayBuffer);
        if ((msg[0] === DesktopMessage.VideoConfig || msg[0] === DesktopMessage.Displays) && !connected) {
          connected = true;
          status.success(i18next.t("asset:Connection successful"));
          SessionBackend.connect(sessionId);
          canvas.focus();
        }
        if (screen.apply(msg)) {
          return;
        }
        if (msg[0] === DesktopMessage.Displays) {
          setDisplays(parseJson(msg));
        } else if (msg[0] === DesktopMessage.Clipboard) {
          const text = new TextDecoder().decode(messagePayload(msg));
          lastClipboardRef.current = text;
          setClipboardText(text);
          navigator.clipboard?.writeText(text).catch(() => undefined);
        } else if (msg[0] === DesktopMessage.Notice) {
          const notice = parseJson(msg) ?? {};
          if (notice.type === "waiting") {
            status.loading(i18next.t("asset:Waiting for the person at the remote computer to allow the session..."));
          } else if (notice.type === "secure-desktop") {
            setOverlay(i18next.t("asset:The remote computer is showing the lock screen or a UAC prompt, which can't be shown here"));
          } else if (notice.type === "desktop") {
            setOverlay(null);
          } else if (notice.message) {
            failureReason = notice.message;
          }
        }
      };

      ws.onclose = () => {
        if (wsRef.current !== ws && wsRef.current !== null) {
          return;
        }
        const closedByUs = wsRef.current === null;
        wsRef.current = null;
        screen.dispose();
        setDisplays(null);
        setOverlay(null);
        pressedRef.current.clear();
        status.dismiss();
        if (closedByUs) {
          return;
        }
        if (failureReason) {
          setFailure(failureReason);
        } else if (!connected) {
          setFailure(i18next.t(`asset:${NotReachable}`));
        } else {
          status.error(i18next.t("asset:Connection closed"));
        }
      };
      ws.onerror = () => {
        if (!connected && !failureReason) {
          failureReason = i18next.t(`asset:${NotReachable}`);
        }
      };
    }).catch(() => {
      status.dismiss();
      Setting.showMessage("error", i18next.t("asset:Failed to connect"));
    });
  };

  React.useEffect(() => {
    if (!inTab) {
      document.title = assetName;
    }
    addClient?.({disconnect});
    activeRef.current = true;
    const releaseLock = holdPageLock();
    connect();
    return () => {
      activeRef.current = false;
      releaseLock();
      disconnect();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [assetId]);

  React.useEffect(() => {
    const onFocus = () => {
      if (navigator.clipboard && wsRef.current && hasOperation("paste")) {
        navigator.clipboard.readText().then(sendClipboard).catch(() => undefined);
      }
    };
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const toRemote = (e: React.MouseEvent | React.WheelEvent) => {
    const canvas = canvasRef.current!;
    const rect = canvas.getBoundingClientRect();
    return {
      x: ((e.clientX - rect.left) / rect.width) * canvas.width,
      y: ((e.clientY - rect.top) / rect.height) * canvas.height,
    };
  };

  const onMouseMove = (e: React.MouseEvent) => {
    const {x, y} = toRemote(e);
    sendRaw(encodeMouse(MouseAction.Move, 0, x, y));
  };

  const onMouseButton = (e: React.MouseEvent, down: boolean) => {
    e.preventDefault();
    if (down) {
      canvasRef.current?.focus();
    }
    const {x, y} = toRemote(e);
    sendRaw(encodeMouse(down ? MouseAction.Down : MouseAction.Up, e.button, x, y));
  };

  const onWheel = (e: React.WheelEvent) => {
    const {x, y} = toRemote(e);
    const dy = e.deltaY === 0 ? 0 : e.deltaY < 0 ? 120 : -120;
    const dx = e.deltaX === 0 ? 0 : e.deltaX > 0 ? 120 : -120;
    sendRaw(encodeMouse(MouseAction.Wheel, 0, x, y, dx, dy));
  };

  const onKey = (e: React.KeyboardEvent, down: boolean) => {
    const mapped = codeToScancode(e.code);
    if (!mapped) {
      return;
    }
    e.preventDefault();
    if (down && e.code === "KeyV" && e.ctrlKey && hasOperation("paste") && navigator.clipboard) {
      navigator.clipboard.readText().then(sendClipboard).catch(() => undefined).finally(() => {
        sendRaw(encodeKey(scancodeOf(mapped), true));
      });
      pressedRef.current.add(scancodeOf(mapped));
      return;
    }
    const scancode = scancodeOf(mapped);
    if (down) {
      pressedRef.current.add(scancode);
    } else {
      pressedRef.current.delete(scancode);
    }
    sendRaw(encodeKey(scancode, down));
  };

  const releaseKeys = () => {
    for (const scancode of pressedRef.current) {
      sendRaw(encodeKey(scancode, false));
    }
    pressedRef.current.clear();
  };

  const sendCombinationKey = (keys: number[]) => {
    // Windows ignores an injected Ctrl+Alt+Del, the device's service sends it instead.
    if (keys.join(",") === [0xffe3, 0xffe9, 0xffff].join(",")) {
      sendRaw(encodeJson(DesktopMessage.Control, {type: "ctrl-alt-del"}));
      canvasRef.current?.focus();
      return;
    }
    const mapped = keys.map(keysymToScancode).filter(Boolean) as {code: number; extended: boolean}[];
    for (const k of mapped) {
      sendRaw(encodeKey(scancodeOf(k), true));
    }
    for (const k of [...mapped].reverse()) {
      sendRaw(encodeKey(scancodeOf(k), false));
    }
    canvasRef.current?.focus();
  };

  return (
    <div className="flex h-full w-full">
      <div className="relative h-full min-w-0 flex-1 overflow-hidden bg-[#1b1b1b]">
        <canvas
          ref={canvasRef}
          tabIndex={0}
          width={1}
          height={1}
          className="absolute inset-0 m-auto max-h-full max-w-full outline-none"
          onMouseMove={onMouseMove}
          onMouseDown={(e) => onMouseButton(e, true)}
          onMouseUp={(e) => onMouseButton(e, false)}
          onContextMenu={(e) => e.preventDefault()}
          onWheel={onWheel}
          onKeyDown={(e) => onKey(e, true)}
          onKeyUp={(e) => onKey(e, false)}
          onBlur={releaseKeys}
        />
        {overlay ? (
          <div className="absolute inset-x-0 top-16 z-10 mx-auto w-fit max-w-[80%] rounded-md bg-black/70 px-4 py-2 text-sm text-white">
            {overlay}
          </div>
        ) : null}
        {displays && displays.displays.length > 1 ? (
          <div className="absolute bottom-4 left-4 z-10 flex gap-1.5 opacity-60 transition-opacity hover:opacity-100">
            {displays.displays.map((display) => (
              <button
                key={display.index}
                className={`h-8 rounded-md px-3 text-xs shadow-md ${display.index === displays.current ? "bg-primary text-primary-foreground" : "bg-secondary text-secondary-foreground"}`}
                onClick={() => {
                  sendRaw(encodeJson(DesktopMessage.Control, {type: "display", index: display.index}));
                  canvasRef.current?.focus();
                }}
              >
                {`${i18next.t("asset:Display")} ${display.index + 1}`}
              </button>
            ))}
          </div>
        ) : null}
        <ConnectionToolbar
          onFullscreen={() => {
            toggleFullscreen();
            canvasRef.current?.focus();
          }}
          onClipboard={() => setClipboardOpen(true)}
          onHotKey={sendCombinationKey}
        />
      </div>
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
          reloadIfUpdated(props).then((reloaded) => !reloaded && connect());
        }}
        onClose={() => closeView(props)}
      />
    </div>
  );
}
