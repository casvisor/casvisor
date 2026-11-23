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
import {ChevronDown} from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  SidebarRail,
  useSidebar,
} from "@/components/ui/sidebar";
import {getNavGroups, shouldFlattenNav, type NavGroup, type NavItem} from "@/lib/nav";
import * as Setting from "@/lib/setting";
import {useAccount} from "@/hooks/use-account";
import {Logo} from "@/components/common/Logo";
import {cn} from "@/lib/utils";

const OPEN_GROUPS_KEY = "casvisor.openNavGroups";

function readOpenGroups(): string[] | null {
  try {
    const raw = localStorage.getItem(OPEN_GROUPS_KEY);
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
}

function writeOpenGroups(keys: string[]) {
  try {
    localStorage.setItem(OPEN_GROUPS_KEY, JSON.stringify(keys));
  } catch {
    // a blocked store just means the groups reopen on the active one next visit
  }
}

/**
 * An external entry opens in a new tab; everything else is a router link. It is
 * rendered under an `asChild` slot, so it forwards its ref and the class names
 * and handlers the menu button merges onto it.
 */
const NavLink = React.forwardRef<HTMLAnchorElement, {item: NavItem; showIcon?: boolean} & React.ComponentPropsWithoutRef<"a">>(
  ({item, showIcon, ...props}, ref) => {
    const Icon = item.icon;
    const content = (
      <>
        {showIcon ? <Icon /> : null}
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

/** A group shown as its icon while the rail is collapsed; the menu keeps every page reachable. */
function CollapsedGroup({
  group,
  active,
  isItemActive,
}: {
  group: NavGroup;
  active: boolean;
  isItemActive: (item: NavItem) => boolean;
}) {
  const Icon = group.icon;
  return (
    <SidebarMenuItem>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <SidebarMenuButton tooltip={group.label} isActive={active}>
            <Icon />
            <span>{group.label}</span>
          </SidebarMenuButton>
        </DropdownMenuTrigger>
        <DropdownMenuContent side="right" align="start" className="w-52">
          <DropdownMenuLabel>{group.label}</DropdownMenuLabel>
          <DropdownMenuSeparator />
          {group.items.map((item) => (
            <DropdownMenuItem key={item.key} asChild className={cn(isItemActive(item) && "font-medium")}>
              <NavLink item={item} />
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </SidebarMenuItem>
  );
}

function ExpandedGroup({
  group,
  open,
  onOpenChange,
  active,
  isItemActive,
  onNavigate,
}: {
  group: NavGroup;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  active: boolean;
  isItemActive: (item: NavItem) => boolean;
  onNavigate: () => void;
}) {
  const Icon = group.icon;
  // a shut group still has to show that the page you are on is inside it
  const hasActiveChild = !open && group.items.some(isItemActive);

  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        tooltip={group.label}
        isActive={hasActiveChild}
        aria-expanded={open}
        onClick={() => onOpenChange(!open)}
        className={cn(active && "font-medium")}
      >
        <Icon />
        <span>{group.label}</span>
        <ChevronDown className={cn("ml-auto opacity-50 transition-transform", open && "rotate-180")} />
      </SidebarMenuButton>
      {open ? (
        <SidebarMenuSub>
          {group.items.map((item) => (
            <SidebarMenuSubItem key={item.key}>
              <SidebarMenuSubButton asChild isActive={isItemActive(item)}>
                <NavLink item={item} onClick={onNavigate} />
              </SidebarMenuSubButton>
            </SidebarMenuSubItem>
          ))}
        </SidebarMenuSub>
      ) : null}
    </SidebarMenuItem>
  );
}

export function AppSidebar() {
  const {account} = useAccount();
  const location = useLocation();
  const {state, isMobile, setOpenMobile} = useSidebar();
  const collapsed = state === "collapsed" && !isMobile;
  // recomputed every render on purpose: the labels come from i18next
  const groups = getNavGroups(account);
  // an ordinary user has only a handful of pages, which read better as one flat list
  const flatten = shouldFlattenNav(groups);

  const isItemActive = (item: NavItem) => {
    if (item.href || item.newTab) {
      return false;
    }
    return item.key === "/" ? location.pathname === "/" : location.pathname === item.key || location.pathname.startsWith(item.key + "/");
  };

  const activeGroupKey = groups.find((group) => group.items.some(isItemActive))?.key;

  const [openGroups, setOpenGroups] = React.useState<string[]>(() => readOpenGroups() ?? []);

  // opening a page also opens its group, so the menu always shows where you are
  React.useEffect(() => {
    if (activeGroupKey && !openGroups.includes(activeGroupKey)) {
      setOpenGroups((previous) => {
        const next = [...previous, activeGroupKey];
        writeOpenGroups(next);
        return next;
      });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeGroupKey]);

  const setGroupOpen = (key: string, open: boolean) => {
    setOpenGroups((previous) => {
      const next = open ? [...new Set([...previous, key])] : previous.filter((k) => k !== key);
      writeOpenGroups(next);
      return next;
    });
  };

  const onNavigate = () => setOpenMobile(false);

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
              {flatten
                ? groups.flatMap((group) =>
                  group.items.map((item) => (
                    <SidebarMenuItem key={item.key}>
                      <SidebarMenuButton asChild tooltip={item.label} isActive={isItemActive(item)}>
                        <NavLink item={item} showIcon onClick={onNavigate} />
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  )),
                )
                : groups.map((group) =>
                  collapsed ? (
                    <CollapsedGroup key={group.key} group={group} active={activeGroupKey === group.key} isItemActive={isItemActive} />
                  ) : (
                    <ExpandedGroup
                      key={group.key}
                      group={group}
                      open={openGroups.includes(group.key)}
                      onOpenChange={(open) => setGroupOpen(group.key, open)}
                      active={activeGroupKey === group.key}
                      isItemActive={isItemActive}
                      onNavigate={onNavigate}
                    />
                  ),
                )}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarRail />
    </Sidebar>
  );
}
