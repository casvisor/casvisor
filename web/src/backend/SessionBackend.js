// Copyright 2024 The casbin Authors. All Rights Reserved.
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

import * as Setting from "../Setting";
import {Connected} from "../SessionListPage";

export function getSessions(owner, page = "", pageSize = "", field = "", value = "", sortField = "", sortOrder = "", status = Connected) {
  return fetch(`${Setting.ServerUrl}/api/get-sessions?owner=${owner}&p=${page}&pageSize=${pageSize}&field=${field}&value=${value}&sortField=${sortField}&sortOrder=${sortOrder}&status=${status}`, {
    method: "GET",
    credentials: "include",
  }).then(res => res.json());
}

export function getSession(owner, name) {
  return fetch(`${Setting.ServerUrl}/api/get-session?id=${owner}/${encodeURIComponent(name)}`, {
    method: "GET",
    credentials: "include",
  }).then(res => res.json());
}

export function updateSession(owner, name, session) {
  const newSession = Setting.deepCopy(session);
  return fetch(`${Setting.ServerUrl}/api/update-session?id=${owner}/${encodeURIComponent(name)}`, {
    method: "POST",
    credentials: "include",
    body: JSON.stringify(newSession),
  }).then(res => res.json());
}

export function addAssetTunnel(assetId, mode = "guacd") {
  const formData = new FormData();
  formData.append("assetId", assetId);
  formData.append("mode", mode);

  return fetch(`${Setting.ServerUrl}/api/add-asset-tunnel`, {
    method: "POST",
    credentials: "include",
    body: formData,
  }).then(res => res.json());
}

export function deleteSession(session) {
  const newSession = Setting.deepCopy(session);
  return fetch(`${Setting.ServerUrl}/api/delete-session`, {
    method: "POST",
    credentials: "include",
    body: JSON.stringify(newSession),
  }).then(res => res.json());
}

export function connect(sessionId) {
  const formData = new FormData();
  formData.append("id", sessionId);

  return fetch(`${Setting.ServerUrl}/api/start-session`, {
    method: "POST",
    credentials: "include",
    body: formData,
  }).then(res => res.json());
}

export function disconnect(sessionId) {
  const formData = new FormData();
  formData.append("id", sessionId);

  return fetch(`${Setting.ServerUrl}/api/stop-session`, {
    method: "POST",
    credentials: "include",
    body: formData,
  }).then(res => res.json());
}
