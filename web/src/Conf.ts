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

// The backend rewrites these quoted values in the served bundle with the
// casdoorEndpoint, clientId, casdoorOrganization and casdoorApplication of
// conf/app.conf (routers/filter.go), so keep them as plain string literals.
export const AuthConfig = {
  serverUrl: "https://door.casdoor.com",
  clientId: "b108dacba027db36ec26",
  appName: "app-casvisor",
  organizationName: "casbin",
  redirectPath: "/callback",
};

export const ForceLanguage = "";
export const DefaultLanguage = "en";
export const IsDemoMode = false;

export const StaticBaseUrl = "https://cdn.casbin.org";
