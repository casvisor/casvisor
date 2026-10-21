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

export function getAssets(owner: string, params: ListParams = {}) {
  return get("/api/get-assets", listQuery(owner, params));
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

export function deleteAsset(asset: any) {
  return post("/api/delete-asset", {}, asset);
}
