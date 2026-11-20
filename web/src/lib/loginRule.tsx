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
import type {LoginRule} from "@/backend/types";
import {Badge} from "@/components/ui/badge";

const actionVariants: Record<string, "success" | "destructive" | "warning" | "info"> = {
  Allow: "success",
  Reject: "destructive",
  Review: "warning",
  Mfa: "info",
};

export function LoginActionBadge({action}: {action: string}) {
  return <Badge variant={actionVariants[action] ?? "secondary"}>{i18next.t(`loginRule:${action}`)}</Badge>;
}

const statusVariants: Record<string, "success" | "destructive" | "warning" | "info" | "secondary"> = {
  Pending: "warning",
  Approved: "info",
  Used: "success",
  Rejected: "destructive",
  Canceled: "secondary",
  Expired: "secondary",
};

export const LoginReviewStatuses = ["Pending", "Approved", "Used", "Rejected", "Canceled", "Expired"];

export function LoginReviewStatusBadge({status}: {status: string}) {
  return <Badge variant={statusVariants[status] ?? "secondary"}>{i18next.t(`loginRule:${status}`)}</Badge>;
}

/** The names of the days, Sunday first, the way the rules number them. */
export function getWeekdayNames() {
  return [0, 1, 2, 3, 4, 5, 6].map((day) => i18next.t(`loginRule:Weekday ${day}`));
}

/** "Mon, Tue 09:00-18:00 (Asia/Shanghai)", or "Any time". */
export function describeLoginTime(rule: LoginRule) {
  const parts: string[] = [];
  const names = getWeekdayNames();
  if (rule.weekdays && rule.weekdays.length > 0 && rule.weekdays.length < 7) {
    parts.push([...rule.weekdays].sort().map((day) => names[day]).join(", "));
  }
  if (rule.startTime && rule.endTime) {
    parts.push(`${rule.startTime}-${rule.endTime}`);
  }
  if (parts.length === 0) {
    return i18next.t("loginRule:Any time");
  }
  return parts.join(" ") + (rule.timeZone ? ` (${rule.timeZone})` : "");
}
