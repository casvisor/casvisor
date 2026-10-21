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

import React, {useEffect, useRef, useState} from "react";
import {Affix, Button, Modal, message} from "antd";
import {CloseCircleOutlined, ExpandOutlined} from "@ant-design/icons";
import Draggable from "react-draggable";
import {Terminal} from "@xterm/xterm";
import {FitAddon} from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import qs from "qs";
import {debounce, getWebSocketUrl, toggleFullscreen} from "./Util";
import * as Setting from "../../Setting";
import * as SessionBackend from "../../backend/SessionBackend";

const SshPage = (props) => {
  const {assetId, activeKey, addClient, closePane, username, password} = props;

  const [isFullScreened, setIsFullScreened] = useState(false);
  const containerRef = useRef(null);
  const terminalRef = useRef(null);
  const fitAddonRef = useRef(null);
  const wsRef = useRef(null);

  useEffect(() => {
    if (!activeKey) {
      document.title = assetId.split("/")[1];
    }

    const terminal = new Terminal({
      cursorBlink: true,
      fontSize: 14,
      fontFamily: "Menlo, Consolas, \"DejaVu Sans Mono\", monospace",
      theme: {background: "#1b1b1b"},
    });
    const fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.open(containerRef.current);
    fitAddon.fit();
    terminal.focus();

    terminal.onData((data) => {
      send({type: "data", data: data});
    });
    terminal.onResize(({cols, rows}) => {
      send({type: "resize", cols: cols, rows: rows});
    });

    terminalRef.current = terminal;
    fitAddonRef.current = fitAddon;

    const fit = debounce(() => {
      fitAddon.fit();
    }, 100);
    const resizeObserver = new ResizeObserver(fit);
    resizeObserver.observe(containerRef.current);

    if (addClient) {
      addClient({
        disconnect: () => {
          if (wsRef.current) {
            wsRef.current.close();
          }
        },
      });
    }

    connect();

    return () => {
      resizeObserver.disconnect();
      if (wsRef.current) {
        wsRef.current.onclose = null;
        wsRef.current.close();
      }
      terminalRef.current = null;
      terminal.dispose();
    };
  }, [assetId]);

  const send = (msg) => {
    const ws = wsRef.current;
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(msg));
    }
  };

  const connect = () => {
    const key = "message";
    message.loading({content: "Connecting...", duration: 0, key: key});

    const terminal = terminalRef.current;
    SessionBackend.addAssetTunnel(assetId, "native").then((res) => {
      if (terminalRef.current !== terminal) {
        return;
      }
      if (res.status !== "ok") {
        message.destroy(key);
        Setting.showMessage("error", "Failed to connect: " + res.msg);
        return;
      }

      const session = res.data;
      fitAddonRef.current.fit();

      const params = {
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
      const ws = new WebSocket(getWebSocketUrl(`/api/get-asset-ssh?${qs.stringify(params)}`));
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
          message.success({content: "Connection successful", duration: 3, key: key});
          terminal.focus();
        } else if (msg.type === "error") {
          errorMessage = msg.message;
        }
      };

      ws.onclose = () => {
        message.destroy(key);
        if (errorMessage) {
          showMessage(errorMessage);
        } else if (!connected) {
          showMessage("Remote service exception, please check if the target device can be accessed normally.");
        } else {
          terminal.write("\r\n\x1b[33mConnection closed\x1b[0m\r\n");
          message.error({content: "Connection closed", duration: 3, key: key});
        }
      };
    });
  };

  const showMessage = (msg) => {
    Modal.confirm({
      title: `Failed to connect to: ${assetId.split("/")[1]}`,
      icon: <CloseCircleOutlined />,
      content: msg,
      centered: true,
      okText: "Reconnect",
      cancelText: "Close this page",
      cancelButtonProps: {"danger": true},
      onOk() {
        terminalRef.current.reset();
        connect();
      },
      onCancel() {
        if (activeKey) {
          closePane(activeKey);
        } else {
          window.close();
        }
      },
    });
  };

  const fullScreen = () => {
    if (props.toggleFullscreen) {
      props.toggleFullscreen();
    }
    setIsFullScreened(!isFullScreened);
    toggleFullscreen();
    terminalRef.current.focus();
  };

  return (
    <div>
      <div style={{
        width: "100%",
        height: activeKey ? "calc(100vh - 40px)" : "100vh",
        padding: 8,
        boxSizing: "border-box",
        backgroundColor: "#1b1b1b",
      }}>
        <div ref={containerRef} style={{width: "100%", height: "100%"}} />
      </div>
      <Draggable>
        <Affix style={{position: "absolute", top: 50, right: 50}}>
          <Button icon={<ExpandOutlined />} onClick={() => {
            fullScreen();
          }} />
        </Affix>
      </Draggable>
    </div>
  );
};

export default SshPage;
