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
import {Terminal} from "lucide-react";
import {Button} from "@/components/ui/button";
import {Tooltip, TooltipContent, TooltipTrigger} from "@/components/ui/tooltip";
import type {Asset, SshGateway} from "@/backend/types";
import {copyText, getSshCommand} from "@/lib/gateway";
import type {Account} from "@/hooks/use-account";

/** Copies the command that reaches an SSH asset through the gateway with a local SSH client. */
export function SshCommandButton({asset, gateway, account}: {asset: Asset; gateway: SshGateway; account: Account | null | undefined}) {
  const command = getSshCommand(gateway, asset, account);
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button size="icon" variant="outline" className="h-8 w-8" aria-label={i18next.t("asset:Copy SSH command")} onClick={() => copyText(command)}>
          <Terminal />
        </Button>
      </TooltipTrigger>
      <TooltipContent>{i18next.t("asset:Copy SSH command")}: {command}</TooltipContent>
    </Tooltip>
  );
}
