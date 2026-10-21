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

export interface RemoteApp {
  no: number;
  remoteAppName: string;
  remoteAppDir: string;
  remoteAppArgs: string;
}

export interface Asset {
  owner: string;
  name: string;
  createdTime: string;
  updatedTime: string;
  displayName: string;
  description: string;
  tag: string;
  os: string;
  protocol: string;
  host: string;
  port: number;
  username: string;
  password: string;
  enableRemoteApp: boolean;
  remoteApps: RemoteApp[] | null;
}

export interface Permission {
  owner: string;
  name: string;
  createdTime: string;
  updatedTime: string;
  displayName: string;
  description: string;
  users: string[] | null;
  groups: string[] | null;
  assets: string[] | null;
  assetTags: string[] | null;
  actions: string[] | null;
  clientIps: string[] | null;
  startTime: string;
  endTime: string;
  isEnabled: boolean;
}

export interface Session {
  owner: string;
  name: string;
  createdTime: string;
  startTime: string;
  endTime: string;
  protocol: string;
  connectionId: string;
  asset: string;
  creator: string;
  clientIp: string;
  userAgent: string;
  clientIpDesc: string;
  userAgentDesc: string;
  status: string;
  mode: string;
  operations: string[] | null;
  [key: string]: any;
}

export interface Record {
  id: number;
  owner: string;
  name: string;
  createdTime: string;
  organization: string;
  clientIp: string;
  userAgent: string;
  user: string;
  method: string;
  requestUri: string;
  action: string;
  language: string;
  object: string;
  response: string;
}

/** A Casdoor user or group offered as a permission subject. */
export interface Principal {
  name: string;
  displayName: string;
}
