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

import i18next from "i18next";
import {Cable} from "lucide-react";
import {Button} from "@/components/ui/button";
import {Tooltip, TooltipContent, TooltipTrigger} from "@/components/ui/tooltip";
import type {Asset, SshGateway} from "@/backend/types";
import {copyText, getTunnelCommand, getTunnelLocalPort} from "@/lib/gateway";
import type {Account} from "@/hooks/use-account";

/** Copies the command that forwards a local port to the asset, for mstsc, VNC viewers and other clients of its protocol. */
export function TunnelCommandButton({asset, gateway, account}: {asset: Asset; gateway: SshGateway; account: Account}) {
  const command = getTunnelCommand(gateway, asset, account);
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button size="icon" variant="outline" className="h-8 w-8" aria-label={i18next.t("asset:Copy port forwarding command")} onClick={() => copyText(command)}>
          <Cable />
        </Button>
      </TooltipTrigger>
      <TooltipContent className="max-w-md">
        <div>{i18next.t("asset:Copy port forwarding command")}: {command}</div>
        <div>{i18next.t("asset:Keep it running, then connect your client to")} localhost:{getTunnelLocalPort(asset)}</div>
      </TooltipContent>
    </Tooltip>
  );
}
