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

import {get, listQuery, post, type ListParams} from "@/backend/request";

export function getNetworkGateways(owner: string, params: ListParams = {}) {
  return get("/api/get-network-gateways", listQuery(owner, params));
}

export function getNetworkGateway(owner: string, name: string) {
  return get("/api/get-network-gateway", {id: `${owner}/${name}`});
}

export function updateNetworkGateway(owner: string, name: string, gateway: any) {
  return post("/api/update-network-gateway", {id: `${owner}/${name}`}, gateway);
}

export function addNetworkGateway(gateway: any) {
  return post("/api/add-network-gateway", {}, gateway);
}

export function deleteNetworkGateway(gateway: any) {
  return post("/api/delete-network-gateway", {}, gateway);
}

export function testNetworkGateway(owner: string, name: string) {
  return post("/api/test-network-gateway", {id: `${owner}/${name}`}, {});
}
