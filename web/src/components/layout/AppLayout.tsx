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
import {Outlet} from "react-router-dom";
import {SidebarInset, SidebarProvider} from "@/components/ui/sidebar";
import {Loading} from "@/components/common/Loading";
import {Header} from "@/components/layout/Header";
import {AppSidebar} from "@/components/layout/Sidebar";
import {Logo} from "@/components/common/Logo";

/**
 * `SidebarProvider` persists the rail through its own `sidebar_state` cookie, so
 * the first render has to start from that cookie or the rail flashes open before
 * settling shut.
 */
function readSidebarCookie(): boolean {
  const match = document.cookie.split(";").map((part) => part.trim()).find((part) => part.startsWith("sidebar_state="));
  return match === undefined ? true : match.slice("sidebar_state=".length) === "true";
}

function PoweredBy() {
  return (
    <span className="inline-flex items-center gap-1.5">
      Powered by
      <a target="_blank" href="https://github.com/casvisor/casvisor" rel="noreferrer">
        <Logo className="h-5 text-sm" />
      </a>
    </span>
  );
}

export function AppLayout() {
  return (
    <div className="flex h-screen flex-col overflow-hidden bg-background">
      <SidebarProvider defaultOpen={readSidebarCookie()} className="min-h-0 flex-1">
        <AppSidebar />
        <SidebarInset className="min-h-0 overflow-hidden">
          <Header />
          <div className="min-h-0 flex-1 overflow-y-auto scrollbar-thin">
            <div className="mx-auto w-full max-w-[1600px] p-4 md:p-6">
              <React.Suspense fallback={<Loading />}>
                <Outlet />
              </React.Suspense>
            </div>
          </div>
        </SidebarInset>
      </SidebarProvider>

      <footer id="footer" className="shrink-0 border-t bg-background py-3 text-center text-xs text-muted-foreground">
        <PoweredBy />
      </footer>
    </div>
  );
}
