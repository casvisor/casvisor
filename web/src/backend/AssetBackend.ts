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

import {get, listQuery, post, type ListParams} from "@/backend/request";
import * as Setting from "@/lib/setting";

/** `node` keeps the assets in that node or below it, "@none" the assets in no node. */
export function getAssets(owner: string, params: ListParams = {}, node = "") {
  return get("/api/get-assets", {...listQuery(owner, params), node});
}

/** xlsx is an Excel workbook in base64, read instead of csv when it is set. */
export function importAssets(owner: string, csv: string, update: boolean, dryRun: boolean, xlsx = "") {
  return post("/api/import-assets", {}, {owner, csv, xlsx, update, dryRun});
}

/** "add" puts the assets in the nodes as well, "remove" takes them out, "set" leaves them in exactly the nodes. */
export function moveAssets(owner: string, assets: string[], nodes: string[], mode: "add" | "remove" | "set") {
  return post("/api/move-assets", {}, {owner, assets, nodes, mode});
}

export function getAsset(owner: string, name: string) {
  return get("/api/get-asset", {id: `${owner}/${name}`});
}

export function updateAsset(owner: string, name: string, asset: any) {
  return post("/api/update-asset", {id: `${owner}/${name}`}, asset);
}

export function addAsset(asset: any) {
  return post("/api/add-asset", {}, asset);
}

/** Connects to the asset as it is in the form; id is the saved asset, whose masked secrets are used. */
export function testAsset(id: string, asset: any) {
  return post("/api/test-asset", id ? {id} : {}, asset);
}

export function deleteAsset(asset: any) {
  return post("/api/delete-asset", {}, asset);
}

/** Lists the namespaces and the pods of a namespace of a Kubernetes asset, with the token typed for an asset that keeps none. */
export function getKubernetesPods(assetId: string, account = "", namespace = "", password = "") {
  const query: Record<string, string> = {assetId};
  if (account) {
    query.account = account;
  }
  if (namespace) {
    query.namespace = namespace;
  }
  return post("/api/get-kubernetes-pods", query, password ? {password} : undefined);
}

/** The namespace, pod and container a log or a file transfer is for. */
export interface KubernetesContainer {
  assetId: string;
  account?: string;
  namespace: string;
  pod: string;
  container: string;
}

function getKubernetesQuery(target: KubernetesContainer, extra: Record<string, string> = {}) {
  const query: Record<string, string> = {assetId: target.assetId, namespace: target.namespace, pod: target.pod, container: target.container, ...extra};
  if (target.account) {
    query.account = target.account;
  }
  return new URLSearchParams(query).toString();
}

/** Streams the log of a container; the response is plain text, or JSON with the error. */
export function fetchKubernetesLogs(target: KubernetesContainer, options: {tailLines: number; follow: boolean; timestamps: boolean; previous: boolean}, password: string, signal: AbortSignal) {
  const extra: Record<string, string> = {tailLines: String(options.tailLines)};
  for (const name of ["follow", "timestamps", "previous"] as const) {
    if (options[name]) {
      extra[name] = "true";
    }
  }
  return fetch(`${Setting.ServerUrl}/api/get-kubernetes-logs?${getKubernetesQuery(target, extra)}`, {
    method: "POST",
    credentials: "include",
    body: password ? JSON.stringify({password}) : undefined,
    signal,
  });
}

/** Downloads a file from a container, or a directory as a tar file; the response is the file, or JSON with the error. */
export function fetchKubernetesFile(target: KubernetesContainer, path: string, password: string) {
  return fetch(`${Setting.ServerUrl}/api/download-kubernetes-file?${getKubernetesQuery(target, {path})}`, {
    method: "POST",
    credentials: "include",
    body: password ? JSON.stringify({password}) : undefined,
  });
}

export function uploadKubernetesFiles(target: KubernetesContainer, dir: string, files: File[], password: string) {
  const form = new FormData();
  for (const file of files) {
    form.append("files", file, file.name);
  }
  if (password) {
    form.append("password", password);
  }
  return fetch(`${Setting.ServerUrl}/api/upload-kubernetes-file?${getKubernetesQuery(target, {path: dir})}`, {
    method: "POST",
    credentials: "include",
    body: form,
  }).then(Setting.handleFetchResponse);
}

/** Issues a kubeconfig that lets kubectl reach the asset through Casvisor. */
export function addKubernetesToken(assetId: string, account = "") {
  return post("/api/add-kubernetes-token", account ? {assetId, account} : {assetId});
}

export function getKubernetesTokens(assetId: string) {
  return get("/api/get-kubernetes-tokens", {assetId});
}

export function revokeKubernetesToken(id: string) {
  return post("/api/revoke-kubernetes-token", {id});
}
