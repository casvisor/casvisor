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
  database: string;
  privateKey: string;
  fingerprint: string;
  networkGateway: string;
  /** the names of the nodes of the asset tree the asset is in */
  nodes?: string[] | null;
  /** the cloud account the asset is synced from, and its server there */
  cloudAccount?: string;
  cloudInstance?: string;
  /** the accounts the signed-in user may connect as, the asset's own account first */
  accounts?: AccountInfo[];
}

/** Posts the events of its organization it subscribes to, as JSON or as a chat bot message. */
export interface Webhook {
  owner: string;
  name: string;
  createdTime: string;
  updatedTime: string;
  displayName: string;
  description: string;
  type: string;
  url: string;
  secret: string;
  events: string[] | null;
  isEnabled: boolean;
}

/** Decides what happens when the users it covers sign in; the first rule by priority that matches wins. */
export interface LoginRule {
  owner: string;
  name: string;
  createdTime: string;
  updatedTime: string;
  displayName: string;
  description: string;
  priority: number;
  users: string[] | null;
  groups: string[] | null;
  clientIps: string[] | null;
  /** 0 is Sunday */
  weekdays: number[] | null;
  /** "HH:mm", both empty for the whole day */
  startTime: string;
  endTime: string;
  timeZone: string;
  methods: string[] | null;
  action: string;
  reviewers: string[] | null;
  isEnabled: boolean;
}

export interface LoginReview {
  owner: string;
  name: string;
  createdTime: string;
  updatedTime: string;
  user: string;
  clientIp: string;
  clientIpDesc: string;
  userAgent: string;
  method: string;
  rule: string;
  status: string;
  expireTime: string;
  reviewer: string;
  reviewedTime: string;
  comment: string;
}

/** A folder of the asset tree. */
export interface AssetNode {
  owner: string;
  name: string;
  createdTime: string;
  updatedTime: string;
  displayName: string;
  description: string;
  parent: string;
  /** the assets in the node or below it */
  assetCount: number;
}

export interface AssetImportResult {
  row: number;
  name: string;
  action: "create" | "update" | "skip" | "error";
  message: string;
  changes: string[] | null;
}

/** An account of a cloud provider whose servers Casvisor keeps as assets. */
export interface CloudAccount {
  owner: string;
  name: string;
  createdTime: string;
  updatedTime: string;
  displayName: string;
  description: string;
  provider: string;
  accessKey: string;
  accessSecret: string;
  tenantId: string;
  subscriptionId: string;
  projectId: string;
  regions: string[] | null;
  addressType: string;
  linuxUsername: string;
  windowsUsername: string;
  nodePath: string;
  tag: string;
  networkGateway: string;
  syncInterval: number;
  deleteMissing: boolean;
  isEnabled: boolean;
  lastSyncTime: string;
  lastSyncStatus: string;
  lastSyncMessage: string;
}

export interface CloudSyncResult {
  row: number;
  name: string;
  action: "create" | "update" | "skip" | "error" | "missing" | "delete";
  message: string;
  changes: string[] | null;
}

export interface CloudSyncSummary {
  instances: number;
  created: number;
  updated: number;
  skipped: number;
  failed: number;
  missing: number;
  deleted: number;
  createdNodes: number;
}

export interface AssetImportSummary {
  created: number;
  updated: number;
  skipped: number;
  failed: number;
  createdNodes: number;
}

export interface AccountInfo {
  username: string;
  displayName: string;
  isDefault: boolean;
}

export interface AccessRequest {
  owner: string;
  name: string;
  createdTime: string;
  updatedTime: string;
  user: string;
  assets: string[] | null;
  accounts: string[] | null;
  actions: string[] | null;
  startTime: string;
  endTime: string;
  reason: string;
  status: string;
  reviewer: string;
  reviewedTime: string;
  reviewComment: string;
  permission: string;
}

export interface RequestableAsset {
  name: string;
  displayName: string;
  protocol: string;
  tag: string;
  accounts: string[];
}

export interface AssetAccount {
  owner: string;
  name: string;
  createdTime: string;
  updatedTime: string;
  asset: string;
  username: string;
  displayName: string;
  description: string;
  password: string;
  privateKey: string;
  isEnabled: boolean;
  autoRotate: boolean;
  rotateIntervalDays: number;
  lastRotatedTime: string;
  rotateStatus: string;
  rotateMessage: string;
}

export interface NetworkGateway {
  owner: string;
  name: string;
  createdTime: string;
  updatedTime: string;
  displayName: string;
  description: string;
  host: string;
  port: number;
  username: string;
  password: string;
  privateKey: string;
  fingerprint: string;
  isEnabled: boolean;
  type: string;
  allowedNetworks: string[];
  agentKey?: string;
  agentVersion?: string;
  agentIp?: string;
  lastOnlineTime?: string;
  isOnline?: boolean;
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
  nodes?: string[] | null;
  accounts: string[] | null;
  actions: string[] | null;
  clientIps: string[] | null;
  startTime: string;
  endTime: string;
  weekdays?: number[] | null;
  dailyStartTime?: string;
  dailyEndTime?: string;
  timeZone?: string;
  watermark?: boolean;
  isEnabled: boolean;
}

/** A share of a connected session, with the code to join it. */
export interface SessionShare {
  owner: string;
  name: string;
  createdTime: string;
  session: string;
  asset: string;
  creator: string;
  users: string[] | null;
  mode: string;
  code: string;
  expireTime: string;
  isRevoked: boolean;
  failedAttempts: number;
  joinCount: number;
}

export interface Session {
  owner: string;
  name: string;
  createdTime: string;
  startTime: string;
  endTime: string;
  protocol: string;
  asset: string;
  account?: string;
  /** the namespace, pod and container of a Kubernetes session */
  target?: string;
  creator: string;
  clientIp: string;
  userAgent: string;
  clientIpDesc: string;
  userAgentDesc: string;
  status: string;
  mode: string;
  operations: string[] | null;
  watermark?: string;
  recordings: string[] | null;
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
  /** whether mstsc can open RDP assets through the Remote Desktop Gateway */
  rdGateway?: boolean;
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
  /** the administrators who may approve what a Review rule holds, all of them when empty */
  reviewers?: string[] | null;
  isEnabled: boolean;
}

/** A command that a Review rule holds until an administrator approves it. */
export interface CommandReview {
  owner: string;
  name: string;
  createdTime: string;
  updatedTime: string;
  session: string;
  asset: string;
  user: string;
  operator: string;
  clientIp: string;
  protocol: string;
  text: string;
  rule: string;
  status: string;
  expireTime: string;
  reviewer: string;
  reviewedTime: string;
  comment: string;
}

export interface Command {
  owner: string;
  name: string;
  createdTime: string;
  session: string;
  asset: string;
  user: string;
  operator?: string;
  clientIp: string;
  text: string;
  action: string;
  rule: string;
  /** the review of a command a Review rule held, and who decided it */
  review?: string;
  reviewer?: string;
  protocol: string;
  rowCount: number;
  /** the HTTP status of a request in a web session */
  statusCode?: number;
  duration: number;
  error: string;
}

export interface SqlResult {
  id: string;
  statement: string;
  columns: {name: string; type: string}[] | null;
  rows: (string | null)[][] | null;
  rowCount: number;
  truncated: boolean;
  duration: number;
  error: string;
  action: string;
  rule: string;
  /** The default database after the statement, when the statement changed it. */
  database?: string;
}

export interface DatabaseTable {
  schema: string;
  name: string;
  type: string;
}

export interface DatabaseColumn {
  name: string;
  type: string;
  key: string;
  nullable: boolean;
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
  mfaEnabled?: boolean;
}

export interface JobResult {
  owner: string;
  job: string;
  asset: string;
  displayName: string;
  host: string;
  session: string;
  status: string;
  exitCode: number;
  output: string;
  error: string;
  rule: string;
  startTime: string;
  endTime: string;
}

export interface Job {
  owner: string;
  name: string;
  createdTime: string;
  displayName: string;
  type: string;
  assets: string[] | null;
  assetTags: string[] | null;
  content: string;
  scriptType: string;
  fileKey: string;
  fileName: string;
  fileSize: number;
  filePath: string;
  timeout: number;
  concurrency: number;
  user: string;
  clientIp: string;
  status: string;
  startTime: string;
  endTime: string;
  message: string;
  total: number;
  succeeded: number;
  failed: number;
  /** the job schedule that started the job, if any */
  schedule?: string;
  results?: JobResult[] | null;
}

/** Starts a job on a cron schedule, as the administrator who last saved it. */
export interface JobSchedule {
  owner: string;
  name: string;
  createdTime: string;
  updatedTime: string;
  displayName: string;
  description: string;
  type: string;
  assets: string[] | null;
  assetTags: string[] | null;
  content: string;
  scriptType: string;
  fileKey: string;
  fileName: string;
  fileSize: number;
  filePath: string;
  timeout: number;
  concurrency: number;
  cron: string;
  timeZone: string;
  isEnabled: boolean;
  user: string;
  userGroups: string[] | null;
  nextRunTime: string;
  lastRunTime: string;
  lastJob: string;
  lastRunStatus: string;
  lastRunMessage: string;
}

export interface KubernetesPod {
  name: string;
  phase: string;
  node: string;
  containers: string[];
  createdTime: string;
}

export interface KubernetesPods {
  namespaces: string[];
  namespace: string;
  pods: KubernetesPod[];
}
