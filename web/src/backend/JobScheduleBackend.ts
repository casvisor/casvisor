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

/** Cron expressions to start from, with the label key of each. */
export const CronPresets: {cron: string; label: string}[] = [
  {cron: "0 * * * *", label: "job:Every hour"},
  {cron: "0 3 * * *", label: "job:Every day at 03:00"},
  {cron: "0 9 * * 1-5", label: "job:Weekdays at 09:00"},
  {cron: "0 2 * * 0", label: "job:Every Sunday at 02:00"},
  {cron: "0 4 1 * *", label: "job:The 1st of every month at 04:00"},
  {cron: "*/15 * * * *", label: "job:Every 15 minutes"},
];

export function getJobSchedules(owner: string, params: ListParams = {}) {
  return get("/api/get-job-schedules", listQuery(owner, params));
}

export function getJobSchedule(owner: string, name: string) {
  return get("/api/get-job-schedule", {id: `${owner}/${name}`});
}

export function updateJobSchedule(owner: string, name: string, schedule: any) {
  return post("/api/update-job-schedule", {id: `${owner}/${name}`}, schedule);
}

export function addJobSchedule(schedule: any) {
  return post("/api/add-job-schedule", {}, schedule);
}

export function deleteJobSchedule(schedule: any) {
  return post("/api/delete-job-schedule", {}, schedule);
}

export function runJobSchedule(owner: string, name: string) {
  return post("/api/run-job-schedule", {id: `${owner}/${name}`});
}

export function getJobScheduleRunTimes(cron: string, timeZone: string) {
  return get("/api/get-job-schedule-run-times", {cron, timeZone});
}
