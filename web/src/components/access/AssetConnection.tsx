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
import * as AssetBackend from "@/backend/AssetBackend";
import {Loading} from "@/components/common/Loading";
import type {ConnectionProps} from "@/components/access/util";
import * as Setting from "@/lib/setting";

// each client library is only downloaded for the protocol that needs it
const SshView = React.lazy(() => import("@/components/access/SshView"));
const VncView = React.lazy(() => import("@/components/access/VncView"));
const RdpView = React.lazy(() => import("@/components/access/RdpView"));
const GuacdView = React.lazy(() => import("@/components/access/GuacdView"));

/** Looks the asset up and opens it with the client its protocol needs. */
export function AssetConnection(props: ConnectionProps) {
  const [protocol, setProtocol] = React.useState("");

  React.useEffect(() => {
    const [owner, name] = props.assetId.split("/");
    AssetBackend.getAsset(owner, name).then((res) => {
      if (res.status === "ok" && res.data) {
        setProtocol(res.data.protocol);
      } else {
        Setting.showMessage("error", `${i18next.t("asset:Failed to get the asset")}: ${res.msg || props.assetId}`);
      }
    });
  }, [props.assetId]);

  const fallback = <Loading className="h-full bg-[#1b1b1b]" />;
  if (protocol === "") {
    return fallback;
  }

  return (
    <React.Suspense fallback={fallback}>
      {protocol === "SSH" ? <SshView {...props} /> : protocol === "VNC" ? <VncView {...props} /> : protocol === "RDP" ? <RdpView {...props} /> : <GuacdView {...props} />}
    </React.Suspense>
  );
}
