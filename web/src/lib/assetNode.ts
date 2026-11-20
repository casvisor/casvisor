// Copyright 2026 The casbin Authors. All Rights Reserved.
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

import type {AssetNode} from "@/backend/types";
import type {MultiSelectOption} from "@/components/common/MultiSelect";

/** Filters the assets that are in no node. */
export const NoNode = "@none";

export function getNodeTitle(node: AssetNode) {
  return node.displayName || node.name;
}

/** The display names from the root to the node, such as "Beijing / Production". */
export function getNodePath(nodes: AssetNode[], name: string, separator = " / ") {
  const byName = new Map(nodes.map((node) => [node.name, node]));
  const titles: string[] = [];
  const seen = new Set<string>();
  let current = byName.get(name);
  while (current && !seen.has(current.name)) {
    seen.add(current.name);
    titles.unshift(getNodeTitle(current));
    current = current.parent ? byName.get(current.parent) : undefined;
  }
  return titles.length > 0 ? titles.join(separator) : name;
}

export function getChildNodes(nodes: AssetNode[], parent: string) {
  return nodes.filter((node) => (node.parent ?? "") === parent && node.name !== parent);
}

/** The node and every node below it. */
export function getDescendantNames(nodes: AssetNode[], name: string) {
  const res = new Set([name]);
  let changed = true;
  while (changed) {
    changed = false;
    for (const node of nodes) {
      if (node.parent && res.has(node.parent) && !res.has(node.name)) {
        res.add(node.name);
        changed = true;
      }
    }
  }
  return res;
}

/** The nodes in tree order, each labeled with its path, for pickers. */
export function getNodeOptions(nodes: AssetNode[]): MultiSelectOption[] {
  const res: MultiSelectOption[] = [];
  const visit = (parent: string) => {
    for (const node of getChildNodes(nodes, parent)) {
      const path = getNodePath(nodes, node.name);
      res.push({value: node.name, label: path, keywords: `${path} ${node.name}`});
      visit(node.name);
    }
  };
  visit("");
  return res;
}

export const AssetCsvColumns = ["name", "displayName", "description", "protocol", "host", "port", "username", "password", "privateKey", "database", "os", "tag", "nodes", "networkGateway"];

export const AssetCsvTemplate = [
  AssetCsvColumns.join(","),
  "web-01,Web 01,,SSH,192.168.1.10,22,root,ChangeMe,,,Linux,prod,Beijing/Production,",
  "win-01,Windows 01,,RDP,192.168.1.20,3389,Administrator,ChangeMe,,,Windows,,Beijing/Office,",
  "db-01,Orders DB,,MySQL,192.168.1.30,3306,app,ChangeMe,,orders,Linux,prod,Beijing/Production;Databases,",
].join("\n") + "\n";

const bom = String.fromCharCode(0xfeff);

/** Reads a CSV file saved as UTF-8 or, the way Excel saves it on a Chinese Windows, as GB 18030. */
export async function readCsvFile(file: File) {
  const buffer = await file.arrayBuffer();
  let text: string;
  try {
    text = new TextDecoder("utf-8", {fatal: true}).decode(buffer);
  } catch {
    text = new TextDecoder("gb18030").decode(buffer);
  }
  return text.startsWith(bom) ? text.slice(1) : text;
}

/** Saves the text as a file, with a BOM so that Excel reads it as UTF-8. */
export function downloadText(fileName: string, text: string) {
  const blob = new Blob([bom, text], {type: "text/csv;charset=utf-8"});
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = fileName;
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 10000);
}
