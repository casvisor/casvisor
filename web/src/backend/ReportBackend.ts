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

import i18next from "i18next";
import {get, listQuery, post, type ListParams} from "@/backend/request";
import {downloadFile, XlsxContentType} from "@/lib/download";

export const ReportPeriods = ["this-week", "last-week", "this-month", "last-month", "today", "yesterday", "last-7-days", "last-30-days"];

export const ReportSections = ["login", "session", "command", "file", "approval", "account", "asset"] as const;

/** What a report covers: a period, or "custom" with the first and the last day, and the working hours. */
export interface ReportQuery {
  owner: string;
  period: string;
  startDate?: string;
  endDate?: string;
  timeZone?: string;
  weekdays?: number[];
  workStartTime?: string;
  workEndTime?: string;
}

export function toSearchParams(query: ReportQuery): URLSearchParams {
  const search = new URLSearchParams({owner: query.owner, period: query.period});
  if (query.period === "custom") {
    search.set("startDate", query.startDate ?? "");
    search.set("endDate", query.endDate ?? "");
  }
  for (const key of ["timeZone", "workStartTime", "workEndTime"] as const) {
    if (query[key]) {
      search.set(key, query[key]!);
    }
  }
  if (query.weekdays && query.weekdays.length > 0) {
    search.set("weekdays", query.weekdays.join(","));
  }
  return search;
}

export function getReportData(query: ReportQuery) {
  return get("/api/get-report-data", Object.fromEntries(toSearchParams(query)));
}

function getLanguage() {
  return i18next.language?.startsWith("zh") ? "zh" : "en";
}

/** Downloads the report as an Excel workbook; resolves to an error message, or "" when it was saved. */
export function downloadReport(query: ReportQuery) {
  const search = toSearchParams(query);
  search.set("lang", getLanguage());
  return downloadFile("/api/download-report", search, XlsxContentType, "casvisor-report.xlsx");
}

export function downloadSavedReport(owner: string, name: string) {
  return downloadFile("/api/download-report", new URLSearchParams({id: `${owner}/${name}`}), XlsxContentType, "casvisor-report.xlsx");
}

export function getReports(owner: string, params: ListParams = {}) {
  return get("/api/get-reports", listQuery(owner, params));
}

export function getReport(owner: string, name: string) {
  return get("/api/get-report", {id: `${owner}/${name}`});
}

export function updateReport(owner: string, name: string, report: any) {
  return post("/api/update-report", {id: `${owner}/${name}`}, report);
}

export function addReport(report: any) {
  return post("/api/add-report", {}, report);
}

export function deleteReport(report: any) {
  return post("/api/delete-report", {}, report);
}

/** Makes the report now and emails it with its webhooks. */
export function runReport(owner: string, name: string) {
  return post("/api/run-report", {id: `${owner}/${name}`});
}
