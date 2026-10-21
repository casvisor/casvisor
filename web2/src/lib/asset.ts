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

export const Protocols = ["RDP", "VNC", "SSH", "Telnet"];

export const OperatingSystems = ["Windows", "Linux"];

export function getDefaultPort(protocol: string) {
  const ports: Record<string, number> = {RDP: 3389, VNC: 5900, SSH: 22, Telnet: 23};
  return ports[protocol] ?? 0;
}

/** The actions a permission can grant, in the order the antd page listed them. */
export const PermissionActions = ["Connect", "Copy", "Paste", "Upload", "Download"];
