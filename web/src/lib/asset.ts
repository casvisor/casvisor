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

export const Protocols = ["RDP", "VNC", "SSH", "Telnet", "MySQL", "PostgreSQL", "Kubernetes", "Web", "Desktop"];

/** The API server of a Kubernetes cluster, whose password is a bearer token. */
export const KubernetesProtocol = "Kubernetes";

/** The protocols whose sessions are a terminal, with commands read off it. */
export function isTerminalProtocol(protocol: string) {
  return protocol === "SSH" || protocol === "Telnet" || protocol === KubernetesProtocol;
}

/** A website, opened through Casvisor's reverse proxy on a subdomain of its own. */
export const WebProtocol = "Web";

/** The protocols whose sessions are a list of statements or requests instead of a recording. */
export function isRequestLogProtocol(protocol: string) {
  return isDatabaseProtocol(protocol) || protocol === WebProtocol;
}

/** A computer running Casvisor Desktop, reached through its device instead of a host and port. */
export const DesktopProtocol = "Desktop";

export const DatabaseProtocols = ["MySQL", "PostgreSQL"];

export function isDatabaseProtocol(protocol: string) {
  return DatabaseProtocols.includes(protocol);
}

export const OperatingSystems = ["Windows", "Linux"];

export function getDefaultPort(protocol: string) {
  const ports: Record<string, number> = {RDP: 3389, VNC: 5900, SSH: 22, Telnet: 23, MySQL: 3306, PostgreSQL: 5432, Kubernetes: 6443, Web: 80};
  return ports[protocol] ?? 0;
}

/** The actions a permission can grant, in the order the antd page listed them. */
export const PermissionActions = ["Connect", "Copy", "Paste", "Upload", "Download", "Forward"];
