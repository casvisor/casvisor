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
import {Affix, Button, Dropdown, Menu, Modal, message} from "antd";
import {CloseCircleOutlined, CopyOutlined, ExpandOutlined, WindowsOutlined} from "@ant-design/icons";
import Draggable from "react-draggable";
import RFB from "@novnc/novnc/lib/rfb";
import qs from "qs";
import {getWebSocketUrl, toggleFullscreen} from "./Util";
import GuacdClipboard from "./GuacdClipboard";
import * as Setting from "../../Setting";
import * as SessionBackend from "../../backend/SessionBackend";

const VncPage = (props) => {
  const {assetId, activeKey, addClient, closePane, username, password} = props;

  const [clipboardText, setClipboardText] = useState("");
  const [clipboardVisible, setClipboardVisible] = useState(false);
  const [isFullScreened, setIsFullScreened] = useState(false);
  const screenRef = useRef(null);
  const rfbRef = useRef(null);
  const sessionRef = useRef({});
  const activeRef = useRef(false);

  useEffect(() => {
    if (!activeKey) {
      document.title = assetId.split("/")[1];
    }

    if (addClient) {
      addClient({
        disconnect: () => {
          if (rfbRef.current) {
            rfbRef.current.disconnect();
          }
        },
      });
    }

    activeRef.current = true;
    connect();

    return () => {
      activeRef.current = false;
      disconnect();
    };
  }, [assetId]);

  useEffect(() => {
    window.addEventListener("focus", handleWindowFocus);
    return () => {
      window.removeEventListener("focus", handleWindowFocus);
    };
  }, []);

  const disconnect = () => {
    const rfb = rfbRef.current;
    if (rfb) {
      rfbRef.current = null;
      rfb.disconnect();
    }
  };

  const hasOperation = (operation) => {
    const operations = sessionRef.current.operations;
    return operations && operations.includes(operation);
  };

  const connect = () => {
    const key = "message";
    message.loading({content: "Connecting...", duration: 0, key: key});

    SessionBackend.addAssetTunnel(assetId, "native").then((res) => {
      if (!activeRef.current) {
        return;
      }
      if (res.status !== "ok") {
        message.destroy(key);
        Setting.showMessage("error", "Failed to connect: " + res.msg);
        return;
      }

      const session = res.data;
      sessionRef.current = session;

      const params = {sessionId: `${session.owner}/${session.name}`};
      if (username || password) {
        params.username = username;
        params.password = password;
      }

      const screen = screenRef.current;
      while (screen.firstChild) {
        screen.removeChild(screen.firstChild);
      }

      const rfb = new RFB(screen, getWebSocketUrl(`/api/get-asset-vnc?${qs.stringify(params)}`), {wsProtocols: ["binary"]});
      rfb.scaleViewport = true;
      rfb.resizeSession = true;
      rfb.background = "#1b1b1b";
      rfbRef.current = rfb;

      let failureReason = "";
      let connected = false;

      rfb.addEventListener("connect", () => {
        connected = true;
        message.success({content: "Connection successful", duration: 3, key: key});
        rfb.focus();
      });

      rfb.addEventListener("securityfailure", (event) => {
        failureReason = event.detail.reason || "Authentication failed";
      });

      rfb.addEventListener("disconnect", () => {
        message.destroy(key);
        if (rfbRef.current !== rfb) {
          return;
        }
        rfbRef.current = null;

        if (failureReason) {
          showMessage(failureReason);
        } else if (!connected) {
          showMessage("Remote service exception, please check if the target device can be accessed normally.");
        } else {
          message.error({content: "Connection closed", duration: 3, key: key});
        }
      });

      rfb.addEventListener("clipboard", (event) => {
        if (!hasOperation("copy")) {
          return;
        }
        const text = event.detail.text;
        setClipboardText(text);
        if (navigator.clipboard) {
          navigator.clipboard.writeText(text).catch(() => {});
        }
      });
    });
  };

  const sendClipboard = (text) => {
    if (!rfbRef.current) {
      return;
    }
    if (!hasOperation("paste")) {
      message.warn("Can not paste");
      return;
    }
    rfbRef.current.clipboardPasteFrom(text);
  };

  const handleWindowFocus = () => {
    if (navigator.clipboard && rfbRef.current && hasOperation("paste")) {
      navigator.clipboard.readText().then((text) => {
        if (text) {
          sendClipboard(text);
        }
      }).catch(() => {});
    }
  };

  const focus = () => {
    if (rfbRef.current) {
      rfbRef.current.focus();
    }
  };

  const sendCombinationKey = (keys) => {
    const rfb = rfbRef.current;
    if (!rfb) {
      return;
    }
    for (let i = 0; i < keys.length; i++) {
      rfb.sendKey(keys[i], null, true);
    }
    for (let j = keys.length - 1; j >= 0; j--) {
      rfb.sendKey(keys[j], null, false);
    }
    Setting.showMessage("success", "Combination Keys successfully sent");
    focus();
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
    focus();
  };

  const hotKeyMenu = (
    <Menu>
      <Menu.Item key={"ctrl+alt+delete"} onClick={() => sendCombinationKey([0xffe3, 0xffe9, 0xffff])}>Ctrl+Alt+Delete</Menu.Item>
      <Menu.Item key={"ctrl+alt+backspace"} onClick={() => sendCombinationKey([0xffe3, 0xffe9, 0xff08])}>Ctrl+Alt+Backspace</Menu.Item>
      <Menu.Item key={"windows+d"} onClick={() => sendCombinationKey([0xffeb, 0x64])}>Windows+D</Menu.Item>
      <Menu.Item key={"windows+e"} onClick={() => sendCombinationKey([0xffeb, 0x65])}>Windows+E</Menu.Item>
      <Menu.Item key={"windows+r"} onClick={() => sendCombinationKey([0xffeb, 0x72])}>Windows+R</Menu.Item>
      <Menu.Item key={"windows+x"} onClick={() => sendCombinationKey([0xffeb, 0x78])}>Windows+X</Menu.Item>
      <Menu.Item key={"windows"} onClick={() => sendCombinationKey([0xffeb])}>Windows</Menu.Item>
    </Menu>
  );

  return (
    <div>
      <div ref={screenRef} style={{
        width: "100%",
        height: activeKey ? "calc(100vh - 40px)" : "100vh",
        backgroundColor: "#1b1b1b",
      }} />
      <Draggable>
        <Affix style={{position: "absolute", top: 50, right: 50}}>
          <Button icon={<ExpandOutlined />} onClick={() => {
            fullScreen();
          }} />
        </Affix>
      </Draggable>
      <Draggable>
        <Affix style={{position: "absolute", top: 50, right: 100}}>
          <Button icon={<CopyOutlined />} onClick={() => {
            setClipboardVisible(true);
          }} />
        </Affix>
      </Draggable>
      <Draggable>
        <Affix style={{position: "absolute", top: 100, right: 100}}>
          <Dropdown overlay={hotKeyMenu} trigger={["click"]} placement="bottomLeft">
            <Button icon={<WindowsOutlined />} />
          </Dropdown>
        </Affix>
      </Draggable>
      <GuacdClipboard visible={clipboardVisible}
        clipboardText={clipboardText}
        handleOk={(text) => {
          sendClipboard(text);
          setClipboardText(text);
          setClipboardVisible(false);
          focus();
        }}
        handleCancel={() => {
          setClipboardVisible(false);
          focus();
        }}
      />
    </div>
  );
};

export default VncPage;
