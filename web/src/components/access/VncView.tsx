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
import RFB from "@novnc/novnc/core/rfb.js";
import * as SessionBackend from "@/backend/SessionBackend";
import {ClipboardDialog, ConnectionToolbar, FailureDialog, useConnectionToast} from "@/components/access/ConnectionChrome";
import {closeView, holdPageLock, toggleFullscreen, type ConnectionProps} from "@/components/access/util";
import * as Setting from "@/lib/setting";

const NotReachable = "Remote service exception, please check if the target device can be accessed normally.";

/** A VNC screen in noVNC, talking to the backend's native VNC tunnel. */
export default function VncView(props: ConnectionProps) {
  const {assetId, inTab, addClient, username, password} = props;
  const assetName = Setting.getShortName(assetId);

  const screenRef = React.useRef<HTMLDivElement>(null);
  const rfbRef = React.useRef<any>(null);
  const operationsRef = React.useRef<string[]>([]);
  const activeRef = React.useRef(false);
  const [clipboardText, setClipboardText] = React.useState("");
  const [clipboardOpen, setClipboardOpen] = React.useState(false);
  const [failure, setFailure] = React.useState<string | null>(null);
  const status = useConnectionToast(assetId);

  const hasOperation = (operation: string) => operationsRef.current.includes(operation);

  const disconnect = () => {
    const rfb = rfbRef.current;
    if (rfb) {
      rfbRef.current = null;
      rfb.disconnect();
    }
  };

  const focus = () => rfbRef.current?.focus();

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

      const params: Record<string, any> = {sessionId: `${session.owner}/${session.name}`};
      if (username || password) {
        params.username = username;
        params.password = password;
      }

      const screen = screenRef.current!;
      while (screen.firstChild) {
        screen.removeChild(screen.firstChild);
      }

      const rfb = new RFB(screen, Setting.getWebSocketUrl(`/api/get-asset-vnc?${qs.stringify(params)}`), {wsProtocols: ["binary"]});
      rfb.scaleViewport = true;
      rfb.resizeSession = true;
      rfb.background = "#1b1b1b";
      rfbRef.current = rfb;

      let failureReason = "";
      let connected = false;

      rfb.addEventListener("connect", () => {
        connected = true;
        status.success(i18next.t("asset:Connection successful"));
        rfb.focus();
      });

      rfb.addEventListener("securityfailure", (event: any) => {
        failureReason = event.detail.reason || "Authentication failed";
      });

      rfb.addEventListener("disconnect", () => {
        if (rfbRef.current !== rfb) {
          return;
        }
        rfbRef.current = null;
        status.dismiss();

        if (failureReason) {
          setFailure(failureReason);
        } else if (!connected) {
          setFailure(NotReachable);
        } else {
          status.error(i18next.t("asset:Connection closed"));
        }
      });

      rfb.addEventListener("clipboard", (event: any) => {
        if (!hasOperation("copy")) {
          return;
        }
        const text = event.detail.text;
        setClipboardText(text);
        navigator.clipboard?.writeText(text).catch(() => undefined);
      });
    });
  };

  const sendClipboard = (text: string) => {
    if (!rfbRef.current) {
      return;
    }
    if (!hasOperation("paste")) {
      Setting.showMessage("warning", i18next.t("asset:Can not paste"));
      return;
    }
    rfbRef.current.clipboardPasteFrom(text);
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
      if (navigator.clipboard && rfbRef.current && hasOperation("paste")) {
        navigator.clipboard.readText().then((text) => {
          if (text) {
            rfbRef.current?.clipboardPasteFrom(text);
          }
        }).catch(() => undefined);
      }
    };
    window.addEventListener("focus", handleWindowFocus);
    return () => window.removeEventListener("focus", handleWindowFocus);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const sendCombinationKey = (keys: number[]) => {
    const rfb = rfbRef.current;
    if (!rfb) {
      return;
    }
    for (const key of keys) {
      rfb.sendKey(key, null, true);
    }
    for (const key of [...keys].reverse()) {
      rfb.sendKey(key, null, false);
    }
    Setting.showMessage("success", i18next.t("asset:Combination keys sent"));
    focus();
  };

  return (
    <div className="relative h-full w-full bg-[#1b1b1b]">
      <div ref={screenRef} className="h-full w-full" />
      <ConnectionToolbar
        onFullscreen={() => {
          toggleFullscreen();
          focus();
        }}
        onClipboard={() => setClipboardOpen(true)}
        onHotKey={sendCombinationKey}
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
          connect();
        }}
        onClose={() => closeView(props)}
      />
    </div>
  );
}
