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
import {toast} from "sonner";
import * as SessionBackend from "@/backend/SessionBackend";
import {ClipboardDialog, ConnectionToolbar, FailureDialog, useConnectionToast} from "@/components/access/ConnectionChrome";
import {FilePanel} from "@/components/access/FilePanel";
import {closeView, holdPageLock, reloadIfUpdated, toggleFullscreen, type ConnectionProps} from "@/components/access/util";
import * as Setting from "@/lib/setting";
import {keysymToScancode, codeToScancode} from "@/components/access/rdpKeymap";
import {RdpScreen, setCanvasCursor} from "@/components/access/rdpScreen";
import {
  ClipboardDownload,
  type ClipboardFileInfo,
  FrameClipboardFile,
  type LocalClipboard,
  base64ToBlob,
  blobToBase64,
  formatBytes,
  readDataTransfer,
  sendFileData,
  toPng,
} from "@/components/access/rdpClipboard";

const NotReachable = "Remote service exception, please check if the target device can be accessed normally.";

/** An RDP screen drawn on a canvas. The gateway's native gordp client speaks
 *  the graphics pipeline and decodes its codecs; this view keeps the surfaces
 *  and caches and draws each server frame at once. Mouse and keyboard are
 *  relayed back. */
export default function RdpView(props: ConnectionProps) {
  const {assetId, inTab, addClient, username, password} = props;
  const assetName = Setting.getShortName(assetId);

  const canvasRef = React.useRef<HTMLCanvasElement>(null);
  const wsRef = React.useRef<WebSocket | null>(null);
  const activeRef = React.useRef(false);
  const operationsRef = React.useRef<string[]>([]);
  const lastClipboardRef = React.useRef("");
  const [clipboardText, setClipboardText] = React.useState("");
  const [clipboardOpen, setClipboardOpen] = React.useState(false);
  const [clipboardImage, setClipboardImage] = React.useState<string | null>(null);
  const [clipboardFiles, setClipboardFiles] = React.useState<ClipboardFileInfo[]>([]);
  // What this browser last put on the remote clipboard, so pasting it again
  // does not send it again.
  const lastOfferRef = React.useRef("");
  // Resolved when the remote host took what is being pasted.
  const offerReadyRef = React.useRef<((ok: boolean) => void) | null>(null);
  const ctrlHeldRef = React.useRef(false);
  const pasteTimerRef = React.useRef<ReturnType<typeof setTimeout> | null>(null);
  const downloadsRef = React.useRef(new Map<number, ClipboardDownload>());
  const downloadIdRef = React.useRef(0);
  const clipboardToast = React.useRef(`clipboard-${Math.random().toString(36).slice(2)}`).current;
  const [failure, setFailure] = React.useState<string | null>(null);
  const [session, setSession] = React.useState<{id: string; operations: string[]} | null>(null);
  const [showFiles, setShowFiles] = React.useState(false);
  const status = useConnectionToast(assetId);
  // Reconnection attempts in a row that did not get the screen back.
  const reconnectsRef = React.useRef(0);

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
    lastOfferRef.current = "";
    send({t: "clipboard", text});
  };

  // Puts what was pasted or dropped on the remote clipboard: text, an image
  // and files, whose bytes follow. Resolves once the remote host has it.
  const offerClipboard = async (data: LocalClipboard): Promise<boolean> => {
    const ws = wsRef.current;
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      return false;
    }
    if (!hasOperation("paste")) {
      Setting.showMessage("warning", i18next.t("asset:Can not paste"));
      return false;
    }
    let files = data.files;
    if (files.length > 0 && !hasOperation("upload")) {
      Setting.showMessage("warning", i18next.t("asset:Can not paste files"));
      files = [];
    }
    if (!data.text && !data.image && files.length === 0) {
      return false;
    }
    const key = JSON.stringify([data.text, data.image?.size, files.map((f) => [f.info.name, f.info.size, f.file?.lastModified])]);
    if (key === lastOfferRef.current) {
      return true;
    }

    const msg: any = {t: "clipboard", text: data.text};
    if (data.image) {
      msg.png = await blobToBase64(await toPng(data.image));
    }
    if (files.length > 0) {
      msg.files = files.map((f) => f.info);
    }
    offerReadyRef.current?.(false);
    const ready = new Promise<boolean>((resolve) => {
      offerReadyRef.current = resolve;
    });
    ws.send(JSON.stringify(msg));
    const total = files.reduce((n, f) => n + (f.file ? f.info.size : 0), 0);
    if (total > 0) {
      const uploading = i18next.t("asset:Copying files to the remote clipboard");
      toast.loading(`${uploading} 0%`, {id: clipboardToast, duration: Infinity});
      try {
        await sendFileData(ws, files, (sent) => {
          toast.loading(`${uploading} ${Math.floor((sent * 100) / total)}%`, {id: clipboardToast, duration: Infinity});
        });
      } catch {
        toast.dismiss(clipboardToast);
        return false;
      }
    }
    const timer = setTimeout(() => offerReadyRef.current?.(false), 10000);
    const ok = await ready;
    clearTimeout(timer);
    if (total > 0) {
      toast.dismiss(clipboardToast);
    }
    if (ok) {
      lastOfferRef.current = key;
      lastClipboardRef.current = data.text;
    }
    return ok;
  };

  // Ctrl+V on the remote desktop, after the browser's paste reached it.
  const sendPasteKeys = () => {
    const ctrl = codeToScancode("ControlLeft")!;
    const v = codeToScancode("KeyV")!;
    const pressCtrl = !ctrlHeldRef.current;
    if (pressCtrl) {
      send({t: "k", code: ctrl.code, down: true, extended: ctrl.extended});
    }
    send({t: "k", code: v.code, down: true, extended: v.extended});
    send({t: "k", code: v.code, down: false, extended: v.extended});
    if (pressCtrl) {
      send({t: "k", code: ctrl.code, down: false, extended: ctrl.extended});
    }
  };

  const downloadClipboardFiles = () => {
    const id = ++downloadIdRef.current;
    send({t: "clipfile", id});
    toast.loading(i18next.t("asset:Downloading..."), {id: clipboardToast, duration: Infinity});
  };

  // Puts what was copied on the remote desktop on this machine's clipboard.
  const writeLocalClipboard = (text: string, png?: string) => {
    const items: Record<string, Blob> = {};
    if (text) {
      items["text/plain"] = new Blob([text], {type: "text/plain"});
    }
    if (png) {
      items["image/png"] = base64ToBlob(png, "image/png");
    }
    if (Object.keys(items).length === 0) {
      return;
    }
    const writeText = () => {
      if (text) {
        navigator.clipboard?.writeText(text).catch(() => undefined);
      }
    };
    if (navigator.clipboard?.write && typeof ClipboardItem !== "undefined") {
      navigator.clipboard.write([new ClipboardItem(items)]).catch(writeText);
    } else {
      writeText();
    }
  };

  // Tries again after the connection dropped, waiting longer each time, and
  // gives up when the screen did not come back after a few tries.
  const reconnect = () => {
    if (reconnectsRef.current >= 5) {
      return false;
    }
    const delay = Math.min(1000 * 2 ** reconnectsRef.current, 10000);
    reconnectsRef.current++;
    status.loading(i18next.t("asset:Reconnecting..."));
    setTimeout(() => {
      if (activeRef.current && !wsRef.current) {
        connect(true);
      }
    }, delay);
    return true;
  };

  const disconnect = () => {
    const ws = wsRef.current;
    if (ws) {
      wsRef.current = null;
      ws.close();
    }
  };

  // The remote desktop is requested at the physical pixel size of the view and
  // told the display scaling, so it is drawn 1:1 and sharp on high-DPI screens.
  const pixelRatio = () => window.devicePixelRatio || 1;

  // Map a mouse event to remote desktop pixel coordinates.
  const toRemote = (e: React.MouseEvent | WheelEvent) => {
    const canvas = canvasRef.current!;
    const rect = canvas.getBoundingClientRect();
    const x = ((e.clientX - rect.left) / rect.width) * canvas.width;
    const y = ((e.clientY - rect.top) / rect.height) * canvas.height;
    return {x: Math.max(0, Math.round(x)), y: Math.max(0, Math.round(y))};
  };

  const connect = (reconnecting = false) => {
    status.loading(i18next.t(reconnecting ? "asset:Reconnecting..." : "asset:Connecting..."));

    SessionBackend.addAssetTunnel(assetId).then((res) => {
      if (!activeRef.current) {
        return;
      }
      if (res.status !== "ok") {
        if (reconnecting && reconnect()) {
          return;
        }
        status.dismiss();
        Setting.showMessage("error", `${i18next.t("asset:Failed to connect")}: ${res.msg}`);
        return;
      }

      const session = res.data;
      operationsRef.current = session.operations ?? [];
      lastClipboardRef.current = "";
      const canvas = canvasRef.current!;
      const ratio = pixelRatio();
      const params: Record<string, any> = {
        sessionId: `${session.owner}/${session.name}`,
        width: Math.round((canvas.parentElement?.clientWidth || 1024) * ratio),
        height: Math.round((canvas.parentElement?.clientHeight || 768) * ratio),
        scale: Math.round(ratio * 100),
        gfx: 1,
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
      const screen = new RdpScreen(canvas);

      ws.onmessage = (event) => {
        if (typeof event.data === "string") {
          const msg = JSON.parse(event.data);
          if (msg.t === "init") {
            screen.init(msg.width, msg.height);
            connected = true;
            reconnectsRef.current = 0;
            status.success(i18next.t("asset:Connection successful"));
            SessionBackend.connect(`${session.owner}/${session.name}`);
            setSession({id: `${session.owner}/${session.name}`, operations: session.operations ?? []});
            canvas.focus();
            // the connection usually drops because the server was upgraded
            if (reconnecting) {
              reloadIfUpdated(props);
            }
          } else if (msg.t === "size") {
            screen.setSize(msg.width, msg.height);
          } else if (msg.t === "clipboard") {
            const text = msg.text ?? "";
            lastClipboardRef.current = text;
            lastOfferRef.current = "";
            setClipboardText(text);
            setClipboardImage((old) => {
              if (old) {
                URL.revokeObjectURL(old);
              }
              return msg.png ? URL.createObjectURL(base64ToBlob(msg.png, "image/png")) : null;
            });
            setClipboardFiles(msg.files ?? []);
            writeLocalClipboard(text, msg.png);
            if (msg.files?.length) {
              toast(`${i18next.t("asset:Files copied on the remote desktop")}: ${msg.files.length}`, {
                id: clipboardToast,
                duration: 10000,
                action: {label: i18next.t("asset:Download"), onClick: downloadClipboardFiles},
              });
            }
          } else if (msg.t === "clipboard-ready") {
            offerReadyRef.current?.(true);
            offerReadyRef.current = null;
          } else if (msg.t === "clipboard-error") {
            offerReadyRef.current?.(false);
            offerReadyRef.current = null;
            toast.error(msg.message, {id: clipboardToast});
          } else if (msg.t === "clipfile-start") {
            downloadsRef.current.set(msg.id, new ClipboardDownload(msg.name, msg.size));
          } else if (msg.t === "clipfile-end") {
            const download = downloadsRef.current.get(msg.id);
            downloadsRef.current.delete(msg.id);
            if (msg.message) {
              toast.error(msg.message, {id: clipboardToast});
            } else if (download) {
              download.save();
              toast.success(`${i18next.t("asset:Downloaded")}: ${download.name}`, {id: clipboardToast, duration: 3000});
            }
          } else if (msg.t === "cursor") {
            setCanvasCursor(canvas, msg);
          } else if (msg.t === "error") {
            failureReason = msg.message || NotReachable;
          }
          return;
        }
        const data = event.data as ArrayBuffer;
        if (new Uint8Array(data, 0, 1)[0] === FrameClipboardFile) {
          const download = downloadsRef.current.get(new DataView(data).getUint32(1, true));
          if (download) {
            download.add(data);
            const progress = download.size > 0 ? ` ${Math.floor((download.received * 100) / download.size)}%` : ` ${formatBytes(download.received)}`;
            toast.loading(`${i18next.t("asset:Downloading...")}${progress}`, {id: clipboardToast, duration: Infinity});
          }
          return;
        }
        screen.frame(data);
      };

      ws.onclose = (event) => {
        if (wsRef.current !== ws) {
          return;
        }
        wsRef.current = null;
        downloadsRef.current.clear();
        offerReadyRef.current?.(false);
        offerReadyRef.current = null;
        setSession(null);
        setShowFiles(false);
        status.dismiss();
        // The remote session goes on, so a connection that dropped (1006) or
        // that the gateway ended on an error (1011) picks it up again.
        const dropped = connected ? event.code === 1006 || event.code === 1011 : reconnecting;
        if (dropped && reconnect()) {
          return;
        }
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
    }).catch(() => {
      if (reconnecting && activeRef.current && reconnect()) {
        return;
      }
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
        const ratio = pixelRatio();
        send({t: "resize", width: Math.round(width * ratio), height: Math.round(height * ratio)});
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
    if (e.code === "ControlLeft" || e.code === "ControlRight") {
      ctrlHeldRef.current = down;
    }
    // Ctrl+V lets the browser paste first, so what was copied on this machine
    // (an image or files too) is on the remote clipboard before the remote
    // desktop pastes. The V is sent once it is there.
    if (e.code === "KeyV" && e.ctrlKey && !e.altKey && !e.metaKey && hasOperation("paste")) {
      if (down && !e.repeat) {
        if (pasteTimerRef.current) {
          clearTimeout(pasteTimerRef.current);
        }
        // in case the browser fires no paste event
        pasteTimerRef.current = setTimeout(() => {
          pasteTimerRef.current = null;
          sendPasteKeys();
        }, 500);
      } else {
        e.preventDefault();
      }
      return;
    }
    e.preventDefault();
    send({t: "k", code: mapped.code, down, extended: mapped.extended});
  };

  const onPaste = (e: React.ClipboardEvent) => {
    e.preventDefault();
    const fromKey = pasteTimerRef.current !== null;
    if (pasteTimerRef.current) {
      clearTimeout(pasteTimerRef.current);
      pasteTimerRef.current = null;
    }
    readDataTransfer(e.clipboardData)
      .then(offerClipboard)
      .catch(() => false)
      .then(() => {
        if (fromKey) {
          sendPasteKeys();
        }
      });
  };

  const onDrop = (e: React.DragEvent) => {
    e.preventDefault();
    canvasRef.current?.focus();
    readDataTransfer(e.dataTransfer)
      .then(offerClipboard)
      .then((ok) => {
        if (ok) {
          toast.success(i18next.t("asset:Copied to the remote clipboard, press Ctrl+V on the remote desktop to paste"), {id: clipboardToast, duration: 5000});
        }
      })
      .catch(() => undefined);
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
          onPaste={onPaste}
          onDragOver={(e) => {
            e.preventDefault();
            e.dataTransfer.dropEffect = "copy";
          }}
          onDrop={onDrop}
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
        image={clipboardImage}
        files={clipboardFiles}
        onDownload={() => {
          setClipboardOpen(false);
          downloadClipboardFiles();
          canvasRef.current?.focus();
        }}
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
