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
import {ChevronDown, LogOut, Settings} from "lucide-react";
import {Avatar, AvatarFallback, AvatarImage} from "@/components/ui/avatar";
import {SidebarTrigger} from "@/components/ui/sidebar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {BreadcrumbBar} from "@/components/layout/BreadcrumbBar";
import {LanguageSelect} from "@/components/common/LanguageSelect";
import {ThemeToggle} from "@/components/common/ThemeToggle";
import {useAccount} from "@/hooks/use-account";
import {useLogout} from "@/hooks/use-logout";
import {useHoverMenu} from "@/hooks/use-hover-menu";
import * as Setting from "@/lib/setting";

export function Header() {
  const {account} = useAccount();
  const logout = useLogout();
  const accountMenu = useHoverMenu();

  if (!account) {
    return null;
  }

  return (
    <header className="sticky top-0 z-30 flex h-14 shrink-0 items-center gap-2 border-b bg-background/95 px-3 backdrop-blur supports-[backdrop-filter]:bg-background/80">
      {/* toggles the rail on a desktop and opens the sheet on a phone */}
      <SidebarTrigger className="-ml-1" />
      <BreadcrumbBar />

      <div className="ml-auto flex items-center gap-1.5">
        <LanguageSelect />
        <ThemeToggle />

        <DropdownMenu {...accountMenu.root}>
          <DropdownMenuTrigger asChild {...accountMenu.trigger}>
            <button type="button" className="ml-1 flex items-center gap-2 rounded-md p-1 hover:bg-accent">
              <Avatar className="h-8 w-8">
                {account.avatar ? <AvatarImage src={account.avatar} alt={account.name} /> : null}
                <AvatarFallback style={{backgroundColor: Setting.getAvatarColor(account.name), color: "#fff"}}>
                  {(account.name || "?").charAt(0).toUpperCase()}
                </AvatarFallback>
              </Avatar>
              <span className="hidden max-w-[160px] truncate text-sm md:inline">
                {account.displayName || account.name}
              </span>
              <ChevronDown className="h-3.5 w-3.5 opacity-60" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56" {...accountMenu.content}>
            <DropdownMenuLabel className="truncate font-normal">
              <div className="text-sm font-medium">{account.displayName || account.name}</div>
              <div className="truncate text-xs text-muted-foreground">
                {account.owner}/{account.name}
              </div>
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => Setting.openLink(Setting.getMyProfileUrl(account))}>
              <Settings />
              {i18next.t("account:My Account")}
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={logout}>
              <LogOut />
              {i18next.t("account:Sign Out")}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </header>
  );
}
