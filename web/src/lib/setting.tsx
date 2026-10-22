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

import i18next from "i18next";
import dayjs from "dayjs";
import {toast} from "sonner";
import Sdk from "casdoor-js-sdk";
import * as Conf from "@/Conf";

// The frontend is served by the backend in production, so relative URLs are enough.
// In development the Vite dev-server proxies /api (and its websockets) to the Go backend.
export const ServerUrl: string = "";

export const StaticBaseUrl = Conf.StaticBaseUrl;

// ---- fetch -------------------------------------------------------------------

/**
 * Reads a fetch Response body and returns parsed JSON, or a normalized {status: "error", msg}
 * when HTTP failed or the body is not JSON (e.g. HTML error pages).
 */
export async function handleFetchResponse(res: Response): Promise<any> {
  const text = await res.text();
  let data: any = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      const preview = text.replace(/\s+/g, " ").trim().slice(0, 160);
      return {
        status: "error",
        msg: `HTTP ${res.status} ${res.statusText || ""}`.trim() + (preview ? `: ${preview}` : ""),
      };
    }
  }
  if (!res.ok && (!data || data.status !== "error")) {
    const msg = (data && (data.msg || data.message)) || `HTTP ${res.status} ${res.statusText || ""}`.trim();
    return {status: "error", msg};
  }
  return data;
}

export function isResponseDenied(data: any) {
  return data?.msg === "Unauthorized operation" || data?.msg === "未授权的操作";
}

/** The websocket URL of a backend path, on the origin that served the page. */
export function getWebSocketUrl(path: string) {
  if (ServerUrl) {
    return ServerUrl.replace(/^http/, "ws") + path;
  }
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${protocol}//${window.location.host}${path}`;
}

// ---- messages and links ------------------------------------------------------

export function showMessage(type: "" | "success" | "error" | "info" | "warning", text: string) {
  if (type === "") {
    return;
  } else if (type === "success") {
    toast.success(text);
  } else if (type === "error") {
    toast.error(text);
  } else if (type === "warning") {
    toast.warning(text);
  } else {
    toast.info(text);
  }
}

function isScriptUrl(link: string) {
  // eslint-disable-next-line no-script-url
  return typeof link === "string" && link.trim().toLowerCase().startsWith("javascript:");
}

export function goToLink(link: string) {
  if (isScriptUrl(link)) {
    return;
  }
  window.location.href = link;
}

export function openLink(link: string) {
  if (isScriptUrl(link)) {
    return;
  }
  window.open(link, "_blank", "noopener,noreferrer");
}

export function isMobile() {
  if (typeof window === "undefined") {
    return false;
  }
  return window.matchMedia("(max-width: 767px)").matches;
}

// ---- language ----------------------------------------------------------------

export const Countries = [
  {label: "English", key: "en", country: "US", alt: "English"},
  {label: "中文", key: "zh", country: "CN", alt: "中文"},
  {label: "Español", key: "es", country: "ES", alt: "Español"},
  {label: "Français", key: "fr", country: "FR", alt: "Français"},
  {label: "Deutsch", key: "de", country: "DE", alt: "Deutsch"},
  {label: "Indonesia", key: "id", country: "ID", alt: "Indonesia"},
  {label: "日本語", key: "ja", country: "JP", alt: "日本語"},
  {label: "한국어", key: "ko", country: "KR", alt: "한국어"},
  {label: "Русский", key: "ru", country: "RU", alt: "Русский"},
];

export function getLanguage() {
  const language = i18next.language;
  return (language !== undefined && language !== null && language !== "" && language !== "null") ? language : Conf.DefaultLanguage;
}

export function setLanguage(language: string) {
  localStorage.setItem("language", language);
  i18next.changeLanguage(language);
}

// ---- small helpers -----------------------------------------------------------

export function deepCopy<T>(obj: T): T {
  if (obj === null) {
    return null as T;
  }
  return Object.assign({}, obj);
}

export function myParseInt(i: any) {
  const res = parseInt(i);
  return isNaN(res) ? 0 : res;
}

export function getShortText(s: string | undefined | null, maxLength = 35) {
  if (!s) {
    return "";
  }
  if (s.length > maxLength) {
    return `${s.slice(0, maxLength)}...`;
  }
  return s;
}

export function getShortName(s: string) {
  return (s ?? "").split("/").slice(-1)[0];
}

export function getFormattedDate(date: string | undefined | null) {
  if (!date) {
    return null;
  }
  const parsed = dayjs(date);
  return parsed.isValid() ? parsed.format("YYYY-MM-DD HH:mm:ss") : date;
}

/** "1h 2m 3s" since the given time, as the session list shows how long a connection lasts. */
export function getDurationSince(start: string | undefined | null, end?: string | null) {
  if (!start) {
    return "-";
  }
  const from = dayjs(start);
  const to = end ? dayjs(end) : dayjs();
  if (!from.isValid() || !to.isValid()) {
    return "-";
  }
  const seconds = Math.max(0, to.diff(from, "second"));
  return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m ${seconds % 60}s`;
}

export function getRandomName() {
  return Math.random().toString(36).slice(-6);
}

export function generateId() {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    return (c === "x" ? r : (r & 0x3) | 0x8).toString(16);
  });
}

export function getIdFromObject(obj: any) {
  if (obj === undefined || obj === null) {
    return "";
  }
  return `${obj.owner}/${obj.name}`;
}

function getHashInt(s: string) {
  let hash = 0;
  for (let i = 0; i < s.length; i++) {
    hash = (hash << 5) - hash + s.charCodeAt(i);
    hash |= 0;
  }
  return Math.abs(hash);
}

export function getAvatarColor(s: string) {
  const colorList = ["#f56a00", "#7265e6", "#ffbf00", "#00a2ae"];
  return colorList[getHashInt(s ?? "") % 4];
}

/** Pretty-prints a JSON string, leaving anything that is not JSON as it is. */
export function formatJsonString(s: string | undefined | null) {
  if (!s) {
    return "";
  }

  try {
    return JSON.stringify(JSON.parse(s), null, 2);
  } catch {
    return s;
  }
}

export function getLogo() {
  return `${StaticBaseUrl}/img/casvisor-logo_1200x256.png`;
}

export function getFaviconUrl() {
  return `${StaticBaseUrl}/img/casvisor.png`;
}

// ---- Casdoor sign-in ---------------------------------------------------------

export let CasdoorSdk: any = null;

export function initCasdoorSdk() {
  if (CasdoorSdk === null) {
    CasdoorSdk = new Sdk(Conf.AuthConfig as any);
  }
}

function getUrlWithLanguage(url: string) {
  return `${url}${url.includes("?") ? "&" : "?"}language=${getLanguage()}`;
}

/**
 * Without a Casdoor endpoint in conf/app.conf, users sign in to Casvisor itself
 * with the accounts it keeps (the backend writes "" into Conf.AuthConfig.serverUrl).
 */
export function isLocalAuth() {
  return Conf.AuthConfig.serverUrl === "";
}

export function getSigninUrl() {
  if (isLocalAuth()) {
    return "/login";
  }
  initCasdoorSdk();
  return getUrlWithLanguage(CasdoorSdk.getSigninUrl());
}

export function getSignupUrl() {
  if (isLocalAuth()) {
    return "/login";
  }
  initCasdoorSdk();
  return getUrlWithLanguage(CasdoorSdk.getSignupUrl());
}

export function getUserProfileUrl(userName: string, account: any) {
  if (isLocalAuth()) {
    return userName.includes("/") ? `/users/${userName}` : `/users/${account?.owner}/${userName}`;
  }
  initCasdoorSdk();
  return getUrlWithLanguage(CasdoorSdk.getUserProfileUrl(userName, account));
}

export function getMyProfileUrl(account: any) {
  if (isLocalAuth()) {
    return "/account";
  }
  initCasdoorSdk();
  return getUrlWithLanguage(CasdoorSdk.getMyProfileUrl(account));
}

/** A page of the Casdoor console, such as "/organizations/casbin", for the signed-in user. */
export function getCasdoorUrl(account: any, path: string) {
  if (isLocalAuth()) {
    return path.startsWith("/users/") ? path : "";
  }
  return getMyProfileUrl(account).replace("/account", path);
}

/** Exchanges the ?code=&state= of the Casdoor redirect for a Casvisor session. */
export function signin(): Promise<any> {
  initCasdoorSdk();
  return CasdoorSdk.signin(ServerUrl);
}

// ---- roles and organization --------------------------------------------------

export function isAdminUser(account: any) {
  return !!account && (account.owner === "built-in" || account.isAdmin === true);
}

export function getOrganization() {
  return localStorage.getItem("organization") ?? "All";
}

export function getRequestOrganization(account: any) {
  if (isAdminUser(account)) {
    return getOrganization() === "All" ? account.owner : getOrganization();
  }
  return account?.owner;
}
