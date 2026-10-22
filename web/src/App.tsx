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
import {Navigate, Route, Routes, useLocation} from "react-router-dom";
import {Loading} from "@/components/common/Loading";
import {AppLayout} from "@/components/layout/AppLayout";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";

const pageModules = import.meta.glob("./pages/*.tsx");
const lazyCache = new Map<string, React.LazyExoticComponent<React.ComponentType<any>>>();

// every page is its own chunk, so the console does not download xterm and noVNC
function page(name: string) {
  let component = lazyCache.get(name);
  if (!component) {
    component = React.lazy(pageModules[`./pages/${name}.tsx`] as () => Promise<{default: React.ComponentType<any>}>);
    lazyCache.set(name, component);
  }
  const Component = component;
  return <Component />;
}

const AuthCallback = React.lazy(() => import("@/pages/auth/AuthCallback"));

/** Sends anonymous visitors to Casdoor and back. */
function RequireAuth({children}: {children: React.ReactNode}) {
  const {account, loading} = useAccount();
  const location = useLocation();

  if (loading || account === undefined) {
    return <Loading className="min-h-screen" />;
  }
  if (account === null) {
    sessionStorage.setItem("from", location.pathname + location.search);
    window.location.replace(Setting.getSigninUrl());
    return <Loading className="min-h-screen" />;
  }
  return <>{children}</>;
}

/** /signin only bounces to Casdoor, or home for a visitor who is signed in already. */
function SigninRedirect() {
  const {account, loading} = useAccount();
  React.useEffect(() => {
    if (!loading && account === null) {
      window.location.replace(Setting.getSigninUrl());
    }
  }, [account, loading]);

  if (account) {
    return <Navigate to="/" replace />;
  }
  return <Loading className="min-h-screen" />;
}

export default function App() {
  return (
    <React.Suspense fallback={<Loading className="min-h-screen" />}>
      <Routes>
        <Route path="/callback" element={<AuthCallback />} />
        <Route path="/signin" element={<SigninRedirect />} />
        <Route path="/login" element={page("LoginPage")} />

        {/* the remote desktops take the whole window, without the console around them */}
        <Route path="/access/:owner/:name" element={<RequireAuth>{page("AccessPage")}</RequireAuth>} />
        <Route path="/workbench" element={<RequireAuth>{page("WorkbenchPage")}</RequireAuth>} />
        <Route path="/sessions/:owner/:name/monitor" element={<RequireAuth>{page("SessionMonitorPage")}</RequireAuth>} />
        <Route path="/sessions/:owner/:name/replay" element={<RequireAuth>{page("SessionReplayPage")}</RequireAuth>} />

        <Route element={<RequireAuth><AppLayout /></RequireAuth>}>
          <Route path="/" element={page("HomePage")} />
          <Route path="/assets" element={page("AssetListPage")} />
          <Route path="/assets/:owner/:name" element={page("AssetEditPage")} />
          <Route path="/permissions" element={page("PermissionListPage")} />
          <Route path="/permissions/:owner/:name" element={page("PermissionEditPage")} />
          <Route path="/sessions" element={page("SessionListPage")} />
          <Route path="/commands" element={page("CommandListPage")} />
          <Route path="/patches" element={page("PatchListPage")} />
          <Route path="/patches/:owner/:name" element={page("PatchDetailPage")} />
          <Route path="/network-gateways" element={page("NetworkGatewayListPage")} />
          <Route path="/network-gateways/:owner/:name" element={page("NetworkGatewayEditPage")} />
          <Route path="/command-rules" element={page("CommandRuleListPage")} />
          <Route path="/command-rules/:owner/:name" element={page("CommandRuleEditPage")} />
          <Route path="/records" element={page("RecordListPage")} />
          <Route path="/records/:owner/:name" element={page("RecordEditPage")} />
          <Route path="/ssh-keys" element={page("SshKeyListPage")} />
          <Route path="/account" element={page("AccountPage")} />
          <Route path="/users" element={page("UserListPage")} />
          <Route path="/users/:owner/:name" element={page("UserEditPage")} />
          <Route path="*" element={page("NotFoundPage")} />
        </Route>
      </Routes>
    </React.Suspense>
  );
}
