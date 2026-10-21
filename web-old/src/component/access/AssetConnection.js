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

import React, {useEffect, useState} from "react";
import GuacdPage from "./GuacdPage";
import SshPage from "./SshPage";
import VncPage from "./VncPage";
import * as Setting from "../../Setting";
import * as AssetBackend from "../../backend/AssetBackend";

const AssetConnection = (props) => {
  const [protocol, setProtocol] = useState("");

  useEffect(() => {
    const [owner, name] = props.assetId.split("/");
    AssetBackend.getAsset(owner, name).then((res) => {
      if (res.status === "ok" && res.data) {
        setProtocol(res.data.protocol);
      } else {
        Setting.showMessage("error", `Failed to get the asset: ${res.msg || props.assetId}`);
      }
    });
  }, [props.assetId]);

  if (protocol === "SSH") {
    return <SshPage {...props} />;
  } else if (protocol === "VNC") {
    return <VncPage {...props} />;
  } else if (protocol !== "") {
    return <GuacdPage {...props} />;
  }

  return <div style={{height: props.activeKey ? "calc(100vh - 40px)" : "100vh", backgroundColor: "#1b1b1b"}} />;
};

export default AssetConnection;
