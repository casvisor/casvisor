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

import {get, post} from "@/backend/request";

export function getAssetAccounts(owner: string, asset: string) {
  return get("/api/get-asset-accounts", {asset: `${owner}/${asset}`});
}

export function updateAssetAccount(owner: string, name: string, account: any) {
  return post("/api/update-asset-account", {id: `${owner}/${name}`}, account);
}

export function addAssetAccount(account: any) {
  return post("/api/add-asset-account", {}, account);
}

export function deleteAssetAccount(account: any) {
  return post("/api/delete-asset-account", {}, account);
}

export function rotateAssetAccount(owner: string, name: string) {
  return post("/api/rotate-asset-account", {id: `${owner}/${name}`}, {});
}

export function pushAssetAccount(owner: string, name: string) {
  return post("/api/push-asset-account", {id: `${owner}/${name}`}, {});
}
