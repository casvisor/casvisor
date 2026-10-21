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

import * as React from "react";
import {Link, useLocation} from "react-router-dom";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
  useSidebar,
} from "@/components/ui/sidebar";
import {getNavItems, type NavItem} from "@/lib/nav";
import * as Setting from "@/lib/setting";
import {useAccount} from "@/hooks/use-account";
import {Logo} from "@/components/common/Logo";

/**
 * An external entry opens in a new tab; everything else is a router link. It is
 * rendered under an `asChild` slot, so it forwards its ref and the class names
 * and handlers the menu button merges onto it.
 */
const NavLink = React.forwardRef<HTMLAnchorElement, {item: NavItem} & React.ComponentPropsWithoutRef<"a">>(
  ({item, ...props}, ref) => {
    const Icon = item.icon;
    const content = (
      <>
        <Icon />
        <span>{item.label}</span>
      </>
    );

    if (item.href) {
      return (
        <a ref={ref} href={item.href} target="_blank" rel="noreferrer" {...props}>
          {content}
        </a>
      );
    }
    return (
      <Link ref={ref} to={item.key} target={item.newTab ? "_blank" : undefined} {...props}>
        {content}
      </Link>
    );
  },
);
NavLink.displayName = "NavLink";

export function AppSidebar() {
  const {account} = useAccount();
  const location = useLocation();
  const {state, isMobile, setOpenMobile} = useSidebar();
  const collapsed = state === "collapsed" && !isMobile;
  // recomputed every render on purpose: the labels come from i18next
  const items = getNavItems(account);

  const isActive = (key: string) =>
    key === "/" ? location.pathname === "/" : location.pathname === key || location.pathname.startsWith(key + "/");

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader className="h-14 justify-center border-b">
        <Link to="/" className="flex items-center overflow-hidden px-1">
          {collapsed ? (
            <img src={Setting.getFaviconUrl()} alt="Casvisor" className="h-6 w-6 rounded object-contain" />
          ) : (
            <Logo className="max-w-[160px]" />
          )}
        </Link>
      </SidebarHeader>

      <SidebarContent className="scrollbar-thin">
        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu>
              {items.map((item) => (
                <SidebarMenuItem key={item.key}>
                  <SidebarMenuButton asChild tooltip={item.label} isActive={!item.href && !item.newTab && isActive(item.key)}>
                    <NavLink item={item} onClick={() => setOpenMobile(false)} />
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarRail />
    </Sidebar>
  );
}
