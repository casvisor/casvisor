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
import {Base64} from "js-base64";
import Guacamole from "guacamole-common-js";
import * as SessionBackend from "@/backend/SessionBackend";
import {ClipboardDialog, ConnectionToolbar, FailureDialog, useConnectionToast} from "@/components/access/ConnectionChrome";
import {closeView, debounce, getGuacdErrorMessage, toggleFullscreen, type ConnectionProps} from "@/components/access/util";
import * as Setting from "@/lib/setting";

const STATE_IDLE = 0;
const STATE_CONNECTING = 1;
const STATE_WAITING = 2;
const STATE_CONNECTED = 3;
const STATE_DISCONNECTED = 5;

/** An RDP or Telnet screen drawn by guacamole-common-js through guacd. */
export default function GuacdView(props: ConnectionProps) {
  const {assetId, inTab, addClient, username, password} = props;
  const assetName = Setting.getShortName(assetId);

  const containerRef = React.useRef<HTMLDivElement>(null);
  const displayRef = React.useRef<HTMLDivElement>(null);
  const clientRef = React.useRef<any>(null);
  const sinkRef = React.useRef<any>(null);
  const sessionRef = React.useRef<any>({});
  const activeRef = React.useRef(false);
  const [protocol, setProtocol] = React.useState("");
  const [clipboardText, setClipboardText] = React.useState("");
  const [clipboardOpen, setClipboardOpen] = React.useState(false);
  const [failure, setFailure] = React.useState<string | null>(null);
  const status = useConnectionToast(assetId);

  const hasOperation = (operation: string) => (sessionRef.current.operations ?? []).includes(operation);

  const focus = () => sinkRef.current?.focus();

  const fitDisplay = () => {
    const client = clientRef.current;
    const container = containerRef.current;
    if (!client || !container) {
      return;
    }
    const display = client.getDisplay();
    if (display.getWidth() && display.getHeight()) {
      display.scale(Math.min(container.clientWidth / display.getWidth(), container.clientHeight / display.getHeight()));
    }
  };

  const handleClipboardReceived = (stream: any, mimetype: string) => {
    if (!hasOperation("copy")) {
      return;
    }

    if (/^text\//.exec(mimetype)) {
      const reader = new Guacamole.StringReader(stream);
      let data = "";
      reader.ontext = (text: string) => {
        data += text;
      };
      reader.onend = () => {
        setClipboardText(data);
        navigator.clipboard?.writeText(data).catch(() => undefined);
      };
    } else {
      const reader = new Guacamole.BlobReader(stream, mimetype);
      reader.onend = () => setClipboardText(reader.getBlob());
    }
  };

  const sendClipboard = (text: string, quiet = false) => {
    const client = clientRef.current;
    if (!client) {
      return;
    }
    if (!hasOperation("paste")) {
      if (!quiet) {
        Setting.showMessage("warning", i18next.t("asset:Can not paste"));
      }
      return;
    }
    const stream = client.createClipboardStream("text/plain");
    const writer = new Guacamole.StringWriter(stream);
    writer.sendText(text);
    writer.sendEnd();
  };

  const onError = (error: {code: number; message: string}) => {
    status.dismiss();
    let message = getGuacdErrorMessage(error.code, error.message);
    if (!message) {
      try {
        message = error.message ? Base64.decode(error.message) : "Unknown error.";
      } catch {
        message = error.message || "Unknown error.";
      }
    }
    setFailure(message);
  };

  const onStateChange = (state: number, sessionId: string) => {
    switch (state) {
    case STATE_IDLE:
      status.loading(i18next.t("asset:Initializing..."));
      break;
    case STATE_CONNECTING:
      status.loading(i18next.t("asset:Connecting..."));
      break;
    case STATE_WAITING:
      status.loading(i18next.t("asset:Waiting for server response..."));
      break;
    case STATE_CONNECTED:
      status.success(i18next.t("asset:Connection successful"));
      // marks the session as connected on the backend
      SessionBackend.connect(sessionId);
      break;
    case STATE_DISCONNECTED:
      status.error(i18next.t("asset:Connection closed"));
      clientRef.current = null;
      sinkRef.current = null;
      break;
    default:
      break;
    }
  };

  const renderDisplay = (sessionId: string, sessionProtocol: string, width: number, height: number) => {
    const tunnel = new Guacamole.WebSocketTunnel(Setting.getWebSocketUrl("/api/get-asset-tunnel"));
    const client = new Guacamole.Client(tunnel);

    client.onclipboard = handleClipboardReceived;
    client.onstatechange = (state: number) => onStateChange(state, sessionId);
    client.onerror = onError;
    tunnel.onerror = onError;

    const params: Record<string, any> = {
      sessionId,
      protocol: sessionProtocol,
      width,
      height,
      dpi: sessionProtocol === "Telnet" ? 192 : 96,
    };
    if (username || password) {
      params.username = encodeURIComponent(username ?? "");
      params.password = encodeURIComponent(password ?? "");
    }
    client.connect(qs.stringify(params));

    const display = client.getDisplay();
    display.onresize = () => fitDisplay();

    const element = display.getElement();
    const container = displayRef.current!;
    while (container.firstChild) {
      container.removeChild(container.firstChild);
    }
    container.appendChild(element);

    const sink = new Guacamole.InputSink();
    container.appendChild(sink.getElement());
    sink.focus();

    const keyboard = new Guacamole.Keyboard(sink.getElement());
    // false cancels the browser's own handling, so Backspace, Tab and the like reach the remote side
    keyboard.onkeydown = (keysym: number) => {
      client.sendKeyEvent(1, keysym);
      return false;
    };
    keyboard.onkeyup = (keysym: number) => client.sendKeyEvent(0, keysym);

    const sinkFocus = debounce(() => sink.focus());

    const mouse = new Guacamole.Mouse(element);
    mouse.onmousedown = mouse.onmouseup = (mouseState: any) => {
      sinkFocus();
      client.sendMouseState(mouseState);
    };
    mouse.onmousemove = (mouseState: any) => {
      sinkFocus();
      display.showCursor(false);
      mouseState.x = mouseState.x / display.getScale();
      mouseState.y = mouseState.y / display.getScale();
      client.sendMouseState(mouseState);
    };

    const touch = new Guacamole.Mouse.Touchpad(element);
    touch.onmousedown = touch.onmousemove = touch.onmouseup = (state: any) => client.sendMouseState(state);

    clientRef.current = client;
    sinkRef.current = sink;
  };

  const connect = () => {
    const container = containerRef.current;
    if (!container) {
      return;
    }
    const width = container.clientWidth;
    const height = container.clientHeight;
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
      sessionRef.current = session;
      setProtocol(session.protocol);
      renderDisplay(`${session.owner}/${session.name}`, session.protocol, width, height);
    });
  };

  const disconnect = () => {
    const client = clientRef.current;
    clientRef.current = null;
    sinkRef.current = null;
    client?.disconnect();
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
    const onResize = debounce(() => {
      const client = clientRef.current;
      const container = containerRef.current;
      if (client && container) {
        client.sendSize(container.clientWidth, container.clientHeight);
        fitDisplay();
      }
    });
    const onFocus = () => {
      navigator.clipboard?.readText().then((text) => sendClipboard(text, true)).catch(() => undefined);
    };
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      if (clientRef.current) {
        e.preventDefault();
        e.returnValue = "";
      }
    };
    window.addEventListener("resize", onResize);
    window.addEventListener("focus", onFocus);
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => {
      window.removeEventListener("resize", onResize);
      window.removeEventListener("focus", onFocus);
      window.removeEventListener("beforeunload", onBeforeUnload);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const sendCombinationKey = (keys: number[]) => {
    const client = clientRef.current;
    if (!client) {
      return;
    }
    keys.forEach((key) => client.sendKeyEvent(1, key));
    keys.forEach((key) => client.sendKeyEvent(0, key));
    Setting.showMessage("success", i18next.t("asset:Combination keys sent"));
    focus();
  };

  return (
    <div ref={containerRef} className="relative flex h-full w-full overflow-hidden bg-[#1b1b1b]">
      <div ref={displayRef} />
      <ConnectionToolbar
        onFullscreen={() => {
          toggleFullscreen();
          focus();
        }}
        onClipboard={() => setClipboardOpen(true)}
        onHotKey={protocol === "RDP" || protocol === "VNC" ? sendCombinationKey : undefined}
      />
      <ClipboardDialog
        open={clipboardOpen}
        text={clipboardText}
        onOk={(text) => {
          sendClipboard(text);
          setClipboardText(text);
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
          disconnect();
          connect();
        }}
        onClose={() => closeView(props)}
      />
    </div>
  );
}
