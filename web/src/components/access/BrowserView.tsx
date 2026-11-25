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
import {ArrowLeft, ArrowRight, Download, KeyRound, Loader2, RotateCw, X} from "lucide-react";
import * as AssetBackend from "@/backend/AssetBackend";
import * as SessionBackend from "@/backend/SessionBackend";
import {ClipboardDialog, ConnectionToolbar, FailureDialog, useConnectionToast} from "@/components/access/ConnectionChrome";
import {blobToBase64, formatBytes} from "@/components/access/rdpClipboard";
import {RdpScreen} from "@/components/access/rdpScreen";
import {closeView, holdPageLock, reloadIfUpdated, toggleFullscreen, type ConnectionProps} from "@/components/access/util";
import {Button} from "@/components/ui/button";
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle} from "@/components/ui/dialog";
import {DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger} from "@/components/ui/dropdown-menu";
import {Input} from "@/components/ui/input";
import * as Setting from "@/lib/setting";

const NotReachable = "Remote service exception, please check if the target device can be accessed normally.";

interface Location {
  url: string;
  title: string;
  tabs: number;
  canGoBack: boolean;
  canGoForward: boolean;
}

interface BrowserDownload {
  id: string;
  name: string;
  size: number;
}

interface PageDialog {
  type: string;
  message: string;
  defaultPrompt: string;
}

const mouseButtons = ["left", "middle", "right"];

function getModifiers(e: {altKey: boolean; ctrlKey: boolean; metaKey: boolean; shiftKey: boolean}) {
  return (e.altKey ? 1 : 0) | (e.ctrlKey ? 2 : 0) | (e.metaKey ? 4 : 0) | (e.shiftKey ? 8 : 0);
}

function getPressedButton(buttons: number) {
  return buttons & 1 ? "left" : buttons & 2 ? "right" : buttons & 4 ? "middle" : "none";
}

/** The text a key types, as the browser on the server expects it. */
function getKeyText(e: React.KeyboardEvent) {
  if (e.ctrlKey || e.metaKey || (e.altKey && !e.getModifierState("AltGraph"))) {
    return "";
  }
  if (e.key === "Enter") {
    return "\r";
  }
  return e.key.length === 1 ? e.key : "";
}

/**
 * A website opened in a browser that Casvisor runs. Its screen is drawn on a canvas, and the mouse,
 * the keyboard and the clipboard go back to it. Casvisor signs in to the website for the user,
 * and the address bar only opens pages of the website.
 */
export default function BrowserView(props: ConnectionProps) {
  const {assetId, inTab, addClient} = props;
  const assetName = Setting.getShortName(assetId);

  const containerRef = React.useRef<HTMLDivElement>(null);
  const canvasRef = React.useRef<HTMLCanvasElement>(null);
  const inputRef = React.useRef<HTMLTextAreaElement>(null);
  const fileRef = React.useRef<HTMLInputElement>(null);
  const wsRef = React.useRef<WebSocket | null>(null);
  const activeRef = React.useRef(false);
  const sessionIdRef = React.useRef("");
  const operationsRef = React.useRef<string[]>([]);
  const lastMoveRef = React.useRef(0);

  const [location, setLocation] = React.useState<Location>({url: "", title: "", tabs: 1, canGoBack: false, canGoForward: false});
  const [address, setAddress] = React.useState("");
  const [editing, setEditing] = React.useState(false);
  const [signingIn, setSigningIn] = React.useState(false);
  const [hasLogin, setHasLogin] = React.useState(false);
  const [downloads, setDownloads] = React.useState<BrowserDownload[]>([]);
  const [chooser, setChooser] = React.useState<{multiple: boolean} | null>(null);
  const [pageDialog, setPageDialog] = React.useState<PageDialog | null>(null);
  const [promptText, setPromptText] = React.useState("");
  const [clipboardText, setClipboardText] = React.useState("");
  const [clipboardOpen, setClipboardOpen] = React.useState(false);
  const [failure, setFailure] = React.useState<string | null>(null);
  const status = useConnectionToast(assetId);

  const hasOperation = (operation: string) => operationsRef.current.includes(operation);

  const send = (msg: Record<string, any>) => {
    const ws = wsRef.current;
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(msg));
    }
  };

  const focus = () => inputRef.current?.focus({preventScroll: true});

  const disconnect = () => {
    const ws = wsRef.current;
    if (ws) {
      wsRef.current = null;
      ws.close();
    }
  };

  const getScreenSize = () => {
    const container = containerRef.current;
    return {width: Math.round(container?.clientWidth || 1280), height: Math.round(container?.clientHeight || 800)};
  };

  const onMessage = (msg: any, screen: RdpScreen) => {
    switch (msg.t) {
    case "size":
      screen.setSize(msg.width, msg.height);
      break;
    case "location":
      setLocation(msg);
      if (!inTab) {
        document.title = msg.title ? `${msg.title} | ${assetName}` : assetName;
      }
      break;
    case "cursor":
      if (canvasRef.current && /^[a-z-]+$/.test(msg.cursor ?? "")) {
        canvasRef.current.style.cursor = msg.cursor;
      }
      break;
    case "clipboard":
      setClipboardText(msg.text ?? "");
      navigator.clipboard?.writeText(msg.text ?? "").catch(() => undefined);
      break;
    case "signin":
      setSigningIn(Boolean(msg.active));
      break;
    case "notice":
      toast.warning(msg.message);
      break;
    case "download":
      setDownloads((list) => [{id: msg.id, name: msg.name, size: msg.size}, ...list]);
      toast.success(`${i18next.t("web:File downloaded")}: ${msg.name}`, {
        duration: 15000,
        action: {label: i18next.t("asset:Download"), onClick: () => saveDownload(msg.id)},
      });
      break;
    case "filechooser":
      setChooser({multiple: Boolean(msg.multiple)});
      break;
    case "dialog":
      setPromptText(msg.defaultPrompt ?? "");
      setPageDialog({type: msg.type, message: msg.message ?? "", defaultPrompt: msg.defaultPrompt ?? ""});
      break;
    case "dialog-closed":
      setPageDialog(null);
      break;
    }
  };

  const connect = () => {
    status.loading(i18next.t("asset:Connecting..."));
    setDownloads([]);

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
      props.onSession?.(session);
      operationsRef.current = session.operations ?? [];
      sessionIdRef.current = `${session.owner}/${session.name}`;

      const canvas = canvasRef.current!;
      const screen = new RdpScreen(canvas);
      const size = getScreenSize();
      screen.init(size.width, size.height);
      const params = {sessionId: sessionIdRef.current, ...size};
      const ws = new WebSocket(Setting.getWebSocketUrl(`/api/get-asset-browser?${qs.stringify(params)}`), ["binary"]);
      ws.binaryType = "arraybuffer";
      wsRef.current = ws;
      let connected = false;
      let failureReason = "";

      ws.onmessage = (event) => {
        if (typeof event.data !== "string") {
          void screen.frame(event.data);
          return;
        }
        const msg = JSON.parse(event.data);
        if (msg.t === "error") {
          failureReason = msg.message;
          return;
        }
        if (!connected && (msg.t === "size" || msg.t === "location")) {
          connected = true;
          status.success(i18next.t("asset:Connection successful"));
          focus();
        }
        onMessage(msg, screen);
      };

      ws.onclose = (event) => {
        if (wsRef.current !== ws) {
          return;
        }
        wsRef.current = null;
        status.dismiss();
        setSigningIn(false);
        if (!connected) {
          setFailure(failureReason || event.reason || NotReachable);
        } else {
          status.error(event.reason || i18next.t("asset:Connection closed"));
        }
      };
    });
  };

  React.useEffect(() => {
    if (!inTab) {
      document.title = assetName;
    }
    const [owner, name] = Setting.splitId(assetId);
    AssetBackend.getAsset(owner, name).then((res) => {
      if (res.status === "ok" && res.data) {
        const login = res.data.webLogin;
        setHasLogin(Boolean(login && (login.usernameSelector || login.passwordSelector)));
      }
    });

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
    // the browser follows the size of the view once resizing settles
    const container = containerRef.current;
    if (!container) {
      return;
    }
    let timer: ReturnType<typeof setTimeout> | undefined;
    const observer = new ResizeObserver(() => {
      clearTimeout(timer);
      timer = setTimeout(() => send({type: "resize", ...getScreenSize()}), 300);
    });
    observer.observe(container);
    return () => {
      clearTimeout(timer);
      observer.disconnect();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  React.useEffect(() => {
    if (!editing) {
      setAddress(location.url);
    }
  }, [location.url, editing]);

  const saveDownload = (id: string) => {
    const link = document.createElement("a");
    link.href = SessionBackend.getBrowserDownloadUrl(sessionIdRef.current, id);
    link.rel = "noopener";
    document.body.appendChild(link);
    link.click();
    link.remove();
  };

  /** Where a mouse event is on the browser's screen, which the canvas shows scaled to fit. */
  const toScreen = (e: {clientX: number; clientY: number}) => {
    const canvas = canvasRef.current!;
    const rect = canvas.getBoundingClientRect();
    const scale = Math.min(rect.width / canvas.width, rect.height / canvas.height) || 1;
    const left = rect.left + (rect.width - canvas.width * scale) / 2;
    const top = rect.top + (rect.height - canvas.height * scale) / 2;
    return {
      x: Math.max(0, Math.min(canvas.width, (e.clientX - left) / scale)),
      y: Math.max(0, Math.min(canvas.height, (e.clientY - top) / scale)),
    };
  };

  const onMouse = (e: React.MouseEvent, event: string) => {
    if (event === "move") {
      const now = performance.now();
      if (now - lastMoveRef.current < 16) {
        return;
      }
      lastMoveRef.current = now;
    } else {
      e.preventDefault();
      focus();
    }
    const {x, y} = toScreen(e);
    send({
      type: "mouse",
      event,
      x,
      y,
      button: event === "move" ? getPressedButton(e.buttons) : mouseButtons[e.button] ?? "none",
      buttons: e.buttons,
      clickCount: event === "move" ? 0 : Math.max(1, e.detail),
      modifiers: getModifiers(e),
    });
  };

  React.useEffect(() => {
    // a wheel listener of React is passive, and the page around the screen must not scroll
    const canvas = canvasRef.current;
    if (!canvas) {
      return;
    }
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const unit = e.deltaMode === 1 ? 40 : e.deltaMode === 2 ? 800 : 1;
      const {x, y} = toScreen(e);
      send({type: "mouse", event: "wheel", x, y, deltaX: e.deltaX * unit, deltaY: e.deltaY * unit, modifiers: getModifiers(e)});
    };
    canvas.addEventListener("wheel", onWheel, {passive: false});
    return () => canvas.removeEventListener("wheel", onWheel);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const onKey = (e: React.KeyboardEvent, down: boolean) => {
    if (e.nativeEvent.isComposing || e.key === "Process" || e.keyCode === 229) {
      return;
    }
    // Ctrl+V pastes what was copied on this machine, through the paste event
    if ((e.ctrlKey || e.metaKey) && !e.altKey && e.code === "KeyV") {
      return;
    }
    e.preventDefault();
    send({
      type: "key",
      event: down ? "down" : "up",
      key: e.key,
      code: e.code,
      keyCode: e.keyCode,
      location: e.location,
      repeat: e.repeat,
      text: down ? getKeyText(e) : "",
      modifiers: getModifiers(e),
    });
  };

  const onPaste = (e: React.ClipboardEvent) => {
    e.preventDefault();
    const text = e.clipboardData.getData("text/plain");
    if (!text) {
      return;
    }
    if (!hasOperation("paste")) {
      Setting.showMessage("warning", i18next.t("asset:Can not paste"));
      return;
    }
    send({type: "paste", text});
  };

  const uploadFiles = async(files: FileList | null) => {
    setChooser(null);
    if (!files || files.length === 0) {
      return;
    }
    const list = await Promise.all(Array.from(files).map(async(file) => ({name: file.name, data: await blobToBase64(file)})));
    send({type: "files", files: list});
    focus();
  };

  const answerDialog = (accept: boolean) => {
    send({type: "dialog", accept, text: promptText});
    setPageDialog(null);
    focus();
  };

  return (
    <div className="flex h-full w-full flex-col bg-[#1b1b1b] text-neutral-200">
      <div className="flex h-11 shrink-0 items-center gap-1 border-b border-neutral-700 px-2">
        <Button variant="ghost" size="icon" className="h-8 w-8" disabled={!location.canGoBack} aria-label={i18next.t("web:Go back")} onClick={() => send({type: "back"})}>
          <ArrowLeft />
        </Button>
        <Button variant="ghost" size="icon" className="h-8 w-8" disabled={!location.canGoForward} aria-label={i18next.t("web:Go forward")} onClick={() => send({type: "forward"})}>
          <ArrowRight />
        </Button>
        <Button variant="ghost" size="icon" className="h-8 w-8" aria-label={i18next.t("web:Reload")} onClick={() => send({type: "reload"})}>
          <RotateCw />
        </Button>
        <form
          className="min-w-0 flex-1"
          onSubmit={(e) => {
            e.preventDefault();
            send({type: "navigate", url: address});
            setEditing(false);
            focus();
          }}
        >
          <Input
            value={address}
            spellCheck={false}
            className="h-8 border-neutral-700 bg-neutral-900 font-mono text-xs text-neutral-200"
            aria-label={i18next.t("web:Address")}
            onFocus={(e) => {
              setEditing(true);
              e.target.select();
            }}
            onBlur={() => setEditing(false)}
            onChange={(e) => setAddress(e.target.value)}
          />
        </form>
        {location.tabs > 1 ? (
          <Button variant="ghost" size="sm" className="h-8 gap-1 px-2" onClick={() => send({type: "close-tab"})}>
            <X />
            {i18next.t("web:Close this tab")} ({location.tabs})
          </Button>
        ) : null}
        {hasLogin ? (
          <Button variant="ghost" size="icon" className="h-8 w-8" aria-label={i18next.t("web:Sign in again")} title={i18next.t("web:Sign in again")} onClick={() => send({type: "signin"})}>
            <KeyRound />
          </Button>
        ) : null}
        {downloads.length > 0 ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon" className="h-8 w-8" aria-label={i18next.t("web:Downloads")}>
                <Download />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="max-w-80">
              {downloads.map((d) => (
                <DropdownMenuItem key={d.id} onSelect={() => saveDownload(d.id)}>
                  <span className="truncate">{d.name}</span>
                  <span className="ml-auto shrink-0 pl-3 text-xs text-muted-foreground">{formatBytes(d.size)}</span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        ) : null}
        <ConnectionToolbar
          inline
          onFullscreen={() => {
            toggleFullscreen();
            focus();
          }}
          onClipboard={() => setClipboardOpen(true)}
        />
      </div>
      <div ref={containerRef} className="relative min-h-0 flex-1 overflow-hidden">
        <canvas
          ref={canvasRef}
          className="h-full w-full object-contain outline-none"
          onMouseMove={(e) => onMouse(e, "move")}
          onMouseDown={(e) => onMouse(e, "down")}
          onMouseUp={(e) => onMouse(e, "up")}
          onContextMenu={(e) => e.preventDefault()}
        />
        <textarea
          ref={inputRef}
          aria-label={assetName}
          className="pointer-events-none absolute left-0 top-0 h-px w-px resize-none opacity-0"
          autoComplete="off"
          autoCorrect="off"
          autoCapitalize="off"
          spellCheck={false}
          onKeyDown={(e) => onKey(e, true)}
          onKeyUp={(e) => onKey(e, false)}
          onPaste={onPaste}
          onCompositionEnd={(e) => {
            if (e.data) {
              send({type: "text", text: e.data});
            }
            e.currentTarget.value = "";
          }}
          onInput={(e) => {
            // text that comes without key presses, like dictation or an on-screen keyboard
            const input = e.nativeEvent as InputEvent;
            if (!input.isComposing) {
              if (input.inputType === "insertText" && input.data) {
                send({type: "text", text: input.data});
              }
              e.currentTarget.value = "";
            }
          }}
        />
        {signingIn ? (
          <div className="absolute inset-0 flex items-center justify-center gap-2 bg-[#1b1b1b] text-sm text-neutral-300">
            <Loader2 className="h-4 w-4 animate-spin" />
            {i18next.t("web:Signing in to the website...")}
          </div>
        ) : null}
      </div>
      <input ref={fileRef} type="file" className="hidden" multiple={chooser?.multiple} onChange={(e) => {
        void uploadFiles(e.target.files);
        e.target.value = "";
      }} />
      <Dialog open={chooser !== null} onOpenChange={(open) => !open && setChooser(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{i18next.t("web:Upload files")}</DialogTitle>
            <DialogDescription>{i18next.t("web:The website asks for files. Choose them on this computer to upload them through Casvisor")}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setChooser(null)}>{i18next.t("general:Cancel")}</Button>
            <Button onClick={() => fileRef.current?.click()}>{i18next.t("web:Choose files")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog open={pageDialog !== null} onOpenChange={(open) => !open && answerDialog(false)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{location.title || assetName}</DialogTitle>
            <DialogDescription className="whitespace-pre-wrap break-words">{pageDialog?.message}</DialogDescription>
          </DialogHeader>
          {pageDialog?.type === "prompt" ? (
            <Input value={promptText} onChange={(e) => setPromptText(e.target.value)} />
          ) : null}
          <DialogFooter>
            {pageDialog?.type !== "alert" ? (
              <Button variant="outline" onClick={() => answerDialog(false)}>{i18next.t("general:Cancel")}</Button>
            ) : null}
            <Button onClick={() => answerDialog(true)}>{i18next.t("general:OK")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <ClipboardDialog
        open={clipboardOpen}
        text={clipboardText}
        onOk={(text) => {
          if (!hasOperation("paste")) {
            Setting.showMessage("warning", i18next.t("asset:Can not paste"));
          } else if (text) {
            send({type: "paste", text});
            setClipboardText(text);
          }
          setClipboardOpen(false);
          focus();
        }}
        onCancel={() => {
          setClipboardOpen(false);
          focus();
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
