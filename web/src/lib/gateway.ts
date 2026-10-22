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
import * as SshKeyBackend from "@/backend/SshKeyBackend";
import type {Asset, SshGateway} from "@/backend/types";
import * as Setting from "@/lib/setting";

let gatewayPromise: Promise<SshGateway | null> | null = null;

function fetchSshGateway() {
  if (!gatewayPromise) {
    gatewayPromise = SshKeyBackend.getSshGateway()
      .then((res) => (res?.status === "ok" ? res.data as SshGateway : null))
      .catch(() => null);
  }
  return gatewayPromise;
}

/** The SSH gateway's address, null while loading or when the gateway is off. */
export function useSshGateway() {
  const [gateway, setGateway] = React.useState<SshGateway | null>(null);
  React.useEffect(() => {
    let active = true;
    fetchSshGateway().then((res) => {
      if (active && res?.enabled) {
        setGateway(res);
      }
    });
    return () => {
      active = false;
    };
  }, []);
  return gateway;
}

export function getGatewayHost(gateway: SshGateway) {
  return gateway.host || window.location.hostname;
}

/** "ssh -p 2222 web1@casvisor.example.com", with the organization when the asset is in another one. */
export function getSshCommand(gateway: SshGateway, asset: Asset, account: {owner: string} | null | undefined) {
  const target = account && asset.owner === account.owner ? asset.name : `${asset.owner}/${asset.name}`;
  const port = gateway.port && gateway.port !== "22" ? `-p ${gateway.port} ` : "";
  return `ssh ${port}${target}@${getGatewayHost(gateway)}`;
}

export function copyText(text: string) {
  navigator.clipboard.writeText(text).then(
    () => Setting.showMessage("success", i18next.t("general:Copied to clipboard")),
    () => Setting.showMessage("error", i18next.t("general:Failed to copy")),
  );
}
