// Copyright 2023 The casbin Authors. All Rights Reserved.
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
import {FileCode, History, House, KeyRound, KeySquare, MonitorPlay, PackageCheck, ScrollText, Server, ShieldAlert, SquareTerminal, type LucideIcon} from "lucide-react";
import * as Setting from "@/lib/setting";
import type {Account} from "@/hooks/use-account";

export interface NavItem {
  key: string;
  label: string;
  icon: LucideIcon;
  description?: string;
  /** rendered as an external link instead of a router link */
  href?: string;
  /** opens in a new tab, as the workbench takes the whole window */
  newTab?: boolean;
}

/** The console navigation, mirroring the antd header menu. */
export function getNavItems(account: Account | null | undefined): NavItem[] {
  if (!account) {
    return [];
  }

  const isAdmin = Setting.isAdminUser(account);
  const items: NavItem[] = [
    {key: "/", label: i18next.t("general:Home"), icon: House},
    {
      key: "/assets",
      label: i18next.t("general:Assets"),
      icon: Server,
      description: i18next.t("general:Machines can be connected via RDP, VNC or SSH"),
    },
    {
      key: "/ssh-keys",
      label: i18next.t("general:SSH keys"),
      icon: KeySquare,
      description: i18next.t("general:Connect to SSH assets with ssh, scp, sftp, Xshell or VS Code"),
    },
  ];

  if (isAdmin) {
    items.push(
      {
        key: "/permissions",
        label: i18next.t("general:Permissions"),
        icon: KeyRound,
        description: i18next.t("general:Who can connect to which assets"),
      },
      {
        key: "/patches",
        label: i18next.t("general:Patches"),
        icon: PackageCheck,
        description: i18next.t("general:Scan and install system updates on SSH assets"),
      },
      {
        key: "/sessions",
        label: i18next.t("general:Sessions"),
        icon: History,
        description: i18next.t("general:Active or history connection sessions"),
      },
      {
        key: "/commands",
        label: i18next.t("general:Commands"),
        icon: SquareTerminal,
        description: i18next.t("general:Commands run in SSH sessions"),
      },
      {
        key: "/command-rules",
        label: i18next.t("general:Command rules"),
        icon: ShieldAlert,
        description: i18next.t("general:Block or flag dangerous commands in SSH sessions"),
      },
      {
        key: "/records",
        label: i18next.t("general:Records"),
        icon: ScrollText,
        description: i18next.t("general:Audit logs"),
      },
    );
  }

  items.push({
    key: "/workbench",
    label: i18next.t("general:Workbench"),
    icon: MonitorPlay,
    description: i18next.t("general:Remote desktop management all in one screen"),
    newTab: true,
  });

  if (isAdmin) {
    items.push({key: "/swagger", label: i18next.t("general:Swagger"), icon: FileCode, href: "/swagger/"});
  }

  return items;
}
