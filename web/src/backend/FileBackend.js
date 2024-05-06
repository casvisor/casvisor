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

export function activateFile(key, filename) {
  return fetch(`${Setting.ServerUrl}/api/activate-file?key=${key}&filename=${filename}`, {
    method: "POST",
    credentials: "include",
    headers: {
      "Accept-Language": Setting.getAcceptLanguage(),
    },
  }).then(res => res.json());
}

export function lsFiles(id, key, mode = "") {
  return fetch(`${Setting.ServerUrl}/api/ls-files?id=${id}&key=${key}&mode=${mode}`, {
    method: "POST",
    credentials: "include",
    headers: {
      "Accept-Language": Setting.getAcceptLanguage(),
    },
  }).then(res => res.json());
}

export function mkdirFile(id, key) {
  return fetch(`${Setting.ServerUrl}/api/mkdir-file?id=${id}&key=${key}`, {
    method: "POST",
    credentials: "include",
    headers: {
      "Accept-Language": Setting.getAcceptLanguage(),
    },
  }).then(res => res.json());
}

export function uploadFile(id, key, file) {
  const formData = new FormData();
  formData.append("file", file, file.name);

  return fetch(`${Setting.ServerUrl}/api/upload-file?id=${id}&key=${key}`, {
    method: "POST",
    credentials: "include",
    body: formData,
    headers: {
      "Accept-Language": Setting.getAcceptLanguage(),
    },
  }).then(res => res.json());
}

export function downloadFile(id, key) {
  return fetch(`${Setting.ServerUrl}/api/download-file?id=${id}&key=${key}`, {
    method: "GET",
    credentials: "include",
    headers: {
      "Accept-Language": Setting.getAcceptLanguage(),
    },
  }).then(res => res.blob());
}

export function deleteFile(id, key) {
  return fetch(`${Setting.ServerUrl}/api/delete-file?id=${id}&key=${key}`, {
    method: "POST",
    credentials: "include",
    headers: {
      "Accept-Language": Setting.getAcceptLanguage(),
    },
  }).then(res => res.json());
}
