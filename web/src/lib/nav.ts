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
import {CalendarClock, Cable, ShieldQuestion, UserCog, ClipboardCheck, Cloud, FileCode, History, House, KeyRound, KeySquare, LayoutDashboard, ListChecks, LogIn, MonitorPlay, Network, PackageCheck, ScrollText, Server, Settings, ShieldAlert, ShieldCheck, SquareTerminal, UserCheck, Users, Webhook, type LucideIcon} from "lucide-react";
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

export interface NavGroup {
  key: string;
  label: string;
  icon: LucideIcon;
  items: NavItem[];
}

/** With this many entries or fewer the menu is one flat list, as it is for ordinary users. */
const MaxItemsForFlatMenu = 7;

/** The console navigation in groups, the same shape as the Casdoor console. */
export function getNavGroups(account: Account | null | undefined): NavGroup[] {
  if (!account) {
    return [];
  }

  const isAdmin = Setting.isAdminUser(account);
  const groups: NavGroup[] = [];

  groups.push({
    key: "/home",
    label: i18next.t("general:Home"),
    icon: House,
    items: [
      {key: "/", label: i18next.t("general:Overview"), icon: LayoutDashboard},
      {
        key: "/workbench",
        label: i18next.t("general:Workbench"),
        icon: MonitorPlay,
        description: i18next.t("general:Remote desktop management all in one screen"),
        newTab: true,
      },
    ],
  });

  const assetItems: NavItem[] = [
    {
      key: "/assets",
      label: i18next.t("general:Assets"),
      icon: Server,
      description: i18next.t("general:Machines can be connected via RDP, VNC or SSH"),
    },
  ];
  if (isAdmin) {
    assetItems.push(
      {
        key: "/network-gateways",
        label: i18next.t("general:Network gateways"),
        icon: Network,
        description: i18next.t("general:Reach assets in other networks through SSH jump hosts or agents"),
      },
      {
        key: "/cloud-accounts",
        label: i18next.t("general:Cloud accounts"),
        icon: Cloud,
        description: i18next.t("general:Keep the servers of Alibaba Cloud, AWS, Tencent Cloud, Azure and GCP accounts as assets"),
      },
      {
        key: "/patches",
        label: i18next.t("general:Patches"),
        icon: PackageCheck,
        description: i18next.t("general:Scan and install system updates on SSH assets"),
      },
    );
  }
  groups.push({key: "/asset-management", label: i18next.t("general:Asset management"), icon: Server, items: assetItems});

  const accountItems: NavItem[] = [];
  if (isAdmin && Setting.isLocalAuth()) {
    accountItems.push({
      key: "/users",
      label: i18next.t("general:Users"),
      icon: Users,
      description: i18next.t("general:Accounts that sign in to Casvisor"),
    });
  }
  accountItems.push({
    key: "/ssh-keys",
    label: i18next.t("general:SSH keys"),
    icon: KeySquare,
    description: i18next.t("general:Connect to SSH assets with ssh, scp, sftp, Xshell or VS Code"),
  });
  if (isAdmin) {
    accountItems.push(
      {
        key: "/account-templates",
        label: i18next.t("general:Account templates"),
        icon: UserCog,
        description: i18next.t("general:Give many assets the same system account at once, and keep them in line"),
      },
      {
        key: "/account-risks",
        label: i18next.t("general:Account risks"),
        icon: ShieldQuestion,
        description: i18next.t("general:Weak, old and reused passwords, and accounts on the assets that Casvisor does not manage"),
      },
    );
  }
  groups.push({key: "/account-management", label: i18next.t("general:Account management"), icon: Users, items: accountItems});

  const accessItems: NavItem[] = [];
  if (isAdmin) {
    accessItems.push({
      key: "/permissions",
      label: i18next.t("general:Permissions"),
      icon: KeyRound,
      description: i18next.t("general:Who can connect to which assets"),
    });
  }
  accessItems.push({
    key: "/access-requests",
    label: i18next.t("general:Access requests"),
    icon: ClipboardCheck,
    description: isAdmin ? i18next.t("general:Approve or reject the access users ask for") : i18next.t("general:Ask for access to assets for a while"),
  });
  if (isAdmin) {
    accessItems.push(
      {
        key: "/login-rules",
        label: i18next.t("general:Login rules"),
        icon: LogIn,
        description: i18next.t("general:Allow, reject, review or ask for two-step verification when users sign in, by user, IP and time"),
      },
      {
        key: "/login-reviews",
        label: i18next.t("general:Login reviews"),
        icon: ShieldCheck,
        description: i18next.t("general:Approve or reject the sign-ins that login rules hold for review"),
      },
      {
        key: "/connect-rules",
        label: i18next.t("general:Connect rules"),
        icon: Cable,
        description: i18next.t("general:Allow or reject ways to connect, such as the browser, SSH clients, SFTP, port forwarding and Remote Desktop Connection"),
      },
      {
        key: "/command-rules",
        label: i18next.t("general:Command rules"),
        icon: ShieldAlert,
        description: i18next.t("general:Block, hold for review or flag dangerous commands"),
      },
      {
        key: "/command-reviews",
        label: i18next.t("general:Command reviews"),
        icon: UserCheck,
        description: i18next.t("general:Approve or reject the commands that command rules hold for review"),
      },
    );
  }
  groups.push({key: "/access-control", label: i18next.t("general:Access control"), icon: KeyRound, items: accessItems});

  if (!isAdmin) {
    return groups;
  }

  groups.push(
    {
      key: "/job-center",
      label: i18next.t("general:Job center"),
      icon: ListChecks,
      items: [
        {
          key: "/jobs",
          label: i18next.t("general:Jobs"),
          icon: ListChecks,
          description: i18next.t("general:Run commands, scripts and file transfers on many SSH assets at once"),
        },
        {
          key: "/job-schedules",
          label: i18next.t("general:Job schedules"),
          icon: CalendarClock,
          description: i18next.t("general:Run jobs at set times, such as backups every night or checks every hour"),
        },
      ],
    },
    {
      key: "/auditing",
      label: i18next.t("general:Auditing"),
      icon: History,
      items: [
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
          description: i18next.t("general:Commands run in SSH sessions and SQL run in database sessions"),
        },
        {
          key: "/records",
          label: i18next.t("general:Records"),
          icon: ScrollText,
          description: i18next.t("general:Audit logs"),
        },
      ],
    },
    {
      key: "/admin",
      label: i18next.t("general:Admin"),
      icon: Settings,
      items: [
        {
          key: "/webhooks",
          label: i18next.t("general:Webhooks"),
          icon: Webhook,
          description: i18next.t("general:Notify chat groups or your own systems of blocked commands, access requests, and sign-ins and commands waiting for review"),
        },
        {key: "/swagger", label: i18next.t("general:Swagger"), icon: FileCode, href: "/swagger/"},
      ],
    },
  );

  return groups;
}

/** Every entry of the menu in order, for the shortcuts on the home page. */
export function getNavItems(account: Account | null | undefined): NavItem[] {
  return getNavGroups(account).flatMap((group) => group.items);
}

/** A menu with only a handful of entries drops the group headers and shows one flat list. */
export function shouldFlattenNav(groups: NavGroup[]): boolean {
  return groups.reduce((count, group) => count + group.items.length, 0) <= MaxItemsForFlatMenu;
}
