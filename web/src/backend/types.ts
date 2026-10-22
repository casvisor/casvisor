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
  privateKey: string;
  fingerprint: string;
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

export interface SshKey {
  owner: string;
  name: string;
  createdTime: string;
  displayName: string;
  user: string;
  publicKey: string;
  fingerprint: string;
  lastUsedTime: string;
}

export interface SshGateway {
  enabled: boolean;
  host: string;
  port: string;
  fingerprint: string;
}

export interface SessionFile {
  name: string;
  size: number;
  mode: string;
  isDir: boolean;
  isLink: boolean;
  modifiedTime: string;
}

export interface CommandRule {
  owner: string;
  name: string;
  createdTime: string;
  updatedTime: string;
  displayName: string;
  description: string;
  pattern: string;
  action: string;
  users: string[] | null;
  groups: string[] | null;
  assets: string[] | null;
  assetTags: string[] | null;
  isEnabled: boolean;
}

export interface Command {
  owner: string;
  name: string;
  createdTime: string;
  session: string;
  asset: string;
  user: string;
  clientIp: string;
  text: string;
  action: string;
  rule: string;
}

export interface PatchUpdate {
  id: string;
  title: string;
  kb: string;
  category: string;
  severity: string;
  isSecurity: boolean;
  currentVersion: string;
  newVersion: string;
  rebootRequired: boolean;
}

export interface PatchStatus {
  owner: string;
  name: string;
  updatedTime: string;
  platform: string;
  os: string;
  manager: string;
  updates: PatchUpdate[] | null;
  updateCount: number;
  securityCount: number;
  rebootRequired: boolean;
  scannedTime: string;
  message: string;
  job: string;
  jobType: string;
  displayName: string;
  host: string;
  tag: string;
}

export interface PatchJob {
  owner: string;
  name: string;
  createdTime: string;
  endTime: string;
  asset: string;
  type: string;
  updates: string[] | null;
  user: string;
  status: string;
  message: string;
  output: string;
}

export interface LocalUser {
  owner: string;
  name: string;
  createdTime: string;
  updatedTime: string;
  displayName: string;
  email: string;
  groups: string[] | null;
  isAdmin: boolean;
  isForbidden: boolean;
  password: string;
  lastSigninTime: string;
}
