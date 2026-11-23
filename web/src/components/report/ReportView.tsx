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

import * as React from "react";
import i18next from "i18next";
import dayjs from "dayjs";
import type {ReportData, ReportEvent, ReportItem, ReportReviews} from "@/backend/types";
import {Table, TableBody, TableCell, TableHead, TableHeader, TableRow} from "@/components/ui/table";
import {cn} from "@/lib/utils";

export function formatDuration(seconds = 0) {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  return `${h}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
}

/** The first and the last day of the report, as the end it is given is exclusive. */
export function getReportRangeText(data: ReportData) {
  const start = dayjs(data.startTime);
  const end = dayjs(data.endTime).subtract(1, "second");
  return `${start.format("YYYY-MM-DD")} ~ ${end.format("YYYY-MM-DD")}`;
}

function getActionLabel(action: string) {
  const key = `report:${action}`;
  const label = i18next.t(key);
  return label === key ? action : label;
}

function Stat({label, value, tone}: {label: string; value: React.ReactNode; tone?: "danger" | "warning"}) {
  return (
    <div className="break-inside-avoid rounded-lg border bg-card px-3 py-2">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className={cn("mt-0.5 text-xl font-semibold tabular-nums", tone === "danger" && "text-destructive", tone === "warning" && "text-amber-600 dark:text-amber-500")}>{value}</div>
    </div>
  );
}

function Stats({children}: {children: React.ReactNode}) {
  return <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4 print:grid-cols-4">{children}</div>;
}

interface ItemColumn {
  title: string;
  value: (item: ReportItem) => React.ReactNode;
}

function ItemTable({title, nameTitle, items, columns}: {title: string; nameTitle: string; items: ReportItem[] | null; columns: ItemColumn[]}) {
  const rows = items ?? [];
  return (
    <div className="min-w-0 break-inside-avoid">
      <h3 className="mb-1 text-sm font-medium">{title}</h3>
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="h-8">{nameTitle}</TableHead>
              {columns.map((column) => <TableHead key={column.title} className="h-8 text-right">{column.title}</TableHead>)}
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.length === 0 ? (
              <TableRow>
                <TableCell colSpan={columns.length + 1} className="py-3 text-center text-xs text-muted-foreground">{i18next.t("report:None")}</TableCell>
              </TableRow>
            ) : rows.map((item) => (
              <TableRow key={item.name}>
                <TableCell className="max-w-[16rem] truncate py-1.5 font-mono text-xs" title={item.name}>{item.name || "-"}</TableCell>
                {columns.map((column) => <TableCell key={column.title} className="py-1.5 text-right tabular-nums">{column.value(item)}</TableCell>)}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}

function EventTable({title, events, withDuration = false}: {title: string; events: ReportEvent[] | null; withDuration?: boolean}) {
  const rows = events ?? [];
  if (rows.length === 0) {
    return null;
  }
  return (
    <div className="min-w-0">
      <h3 className="mb-1 text-sm font-medium">{title}</h3>
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="h-8 w-40">{i18next.t("report:Time")}</TableHead>
              <TableHead className="h-8">{i18next.t("report:User")}</TableHead>
              <TableHead className="h-8">{i18next.t("report:Client IP")}</TableHead>
              <TableHead className="h-8">{i18next.t("report:Asset")}</TableHead>
              <TableHead className="h-8">{i18next.t("report:Action")}</TableHead>
              <TableHead className="h-8">{i18next.t("report:Detail")}</TableHead>
              {withDuration ? <TableHead className="h-8 text-right">{i18next.t("report:Duration")}</TableHead> : null}
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((event, index) => (
              <TableRow key={index} className="break-inside-avoid">
                <TableCell className="whitespace-nowrap py-1.5 text-xs tabular-nums">{dayjs(event.time).format("YYYY-MM-DD HH:mm:ss")}</TableCell>
                <TableCell className="py-1.5 font-mono text-xs">{event.user}</TableCell>
                <TableCell className="py-1.5 font-mono text-xs">{event.clientIp}</TableCell>
                <TableCell className="py-1.5 font-mono text-xs">{event.asset}</TableCell>
                <TableCell className="whitespace-nowrap py-1.5 text-xs">{getActionLabel(event.action)}</TableCell>
                <TableCell className="max-w-[28rem] break-all py-1.5 font-mono text-xs">{event.detail}</TableCell>
                {withDuration ? <TableCell className="py-1.5 text-right text-xs tabular-nums">{formatDuration(event.seconds)}</TableCell> : null}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      {rows.length >= 200 ? <p className="mt-1 text-xs text-muted-foreground">{i18next.t("report:Only the first 200 rows are listed")}</p> : null}
    </div>
  );
}

function Section({title, children}: {title: string; children: React.ReactNode}) {
  return (
    <section className="space-y-3 rounded-xl border p-4 print:rounded-none print:border-0 print:border-t print:px-0">
      <h2 className="text-lg font-semibold">{title}</h2>
      {children}
    </section>
  );
}

function Grid({children}: {children: React.ReactNode}) {
  return <div className="grid gap-4 md:grid-cols-2 print:grid-cols-2">{children}</div>;
}

function ReviewStats({title, reviews}: {title: string; reviews: ReportReviews}) {
  return (
    <div className="space-y-1.5">
      <h3 className="text-sm font-medium">{title}</h3>
      <Stats>
        <Stat label={i18next.t("report:Total")} value={reviews.total} />
        <Stat label={i18next.t("report:Approved")} value={reviews.approved} />
        <Stat label={i18next.t("report:Rejected")} value={reviews.rejected} tone={reviews.rejected > 0 ? "warning" : undefined} />
        <Stat label={i18next.t("report:Pending")} value={reviews.pending} />
      </Stats>
    </div>
  );
}

/** The sections of an audit report, the same on the page and in its print view. */
export function ReportView({data}: {data: ReportData}) {
  const countColumn: ItemColumn = {title: i18next.t("report:Count"), value: (item) => item.count};
  const durationColumn: ItemColumn = {title: i18next.t("report:Duration"), value: (item) => formatDuration(item.seconds)};
  const loginColumns: ItemColumn[] = [
    {title: i18next.t("report:Succeeded"), value: (item) => item.count},
    {title: i18next.t("report:Failed"), value: (item) => item.failed ?? 0},
  ];
  const fileColumns: ItemColumn[] = [
    {title: i18next.t("report:Uploads"), value: (item) => item.uploads ?? 0},
    {title: i18next.t("report:Downloads"), value: (item) => item.downloads ?? 0},
  ];
  const {login, session, command, file, approval, account, asset} = data;

  return (
    <div className="space-y-4">
      {login ? (
        <Section title={i18next.t("report:Logins")}>
          <Stats>
            <Stat label={i18next.t("report:Successful sign-ins")} value={login.succeeded} />
            <Stat label={i18next.t("report:Failed sign-ins")} value={login.failed} tone={login.failed > 0 ? "danger" : undefined} />
            <Stat label={i18next.t("report:Off-hours sign-ins")} value={login.offHours} tone={login.offHours > 0 ? "warning" : undefined} />
            <Stat label={i18next.t("report:Rejected by login rules")} value={login.rejected} tone={login.rejected > 0 ? "warning" : undefined} />
            <Stat label={i18next.t("report:Held for review")} value={login.reviewed} />
            <Stat label={i18next.t("report:Asked for two-step verification")} value={login.mfaRequired} />
          </Stats>
          <Grid>
            <ItemTable title={i18next.t("report:By user")} nameTitle={i18next.t("report:User")} items={login.users} columns={loginColumns} />
            <ItemTable title={i18next.t("report:By client IP")} nameTitle={i18next.t("report:Client IP")} items={login.clientIps} columns={loginColumns} />
          </Grid>
          <EventTable title={i18next.t("report:Failed, off-hours and rejected sign-ins")} events={login.events} />
        </Section>
      ) : null}

      {session ? (
        <Section title={i18next.t("report:Sessions")}>
          <Stats>
            <Stat label={i18next.t("report:Total sessions")} value={session.total} />
            <Stat label={i18next.t("report:Total duration")} value={formatDuration(session.seconds)} />
            <Stat label={i18next.t("report:Users")} value={session.users} />
            <Stat label={i18next.t("report:Assets")} value={session.assets} />
          </Stats>
          <Grid>
            <ItemTable title={i18next.t("report:By user")} nameTitle={i18next.t("report:User")} items={session.byUser} columns={[countColumn, durationColumn]} />
            <ItemTable title={i18next.t("report:By asset")} nameTitle={i18next.t("report:Asset")} items={session.byAsset} columns={[countColumn, durationColumn]} />
            <ItemTable title={i18next.t("report:By protocol")} nameTitle={i18next.t("report:Protocol")} items={session.byProtocol} columns={[countColumn, durationColumn]} />
          </Grid>
          <EventTable title={i18next.t("report:Longest sessions")} events={session.longest} withDuration />
        </Section>
      ) : null}

      {command ? (
        <Section title={i18next.t("report:Commands")}>
          <Stats>
            <Stat label={i18next.t("report:Total commands")} value={command.total} />
            <Stat label={i18next.t("report:Blocked")} value={command.blocked} tone={command.blocked > 0 ? "danger" : undefined} />
            <Stat label={i18next.t("report:Alerted")} value={command.alerted} tone={command.alerted > 0 ? "warning" : undefined} />
            <Stat label={i18next.t("report:Held for review")} value={command.reviewed} />
          </Stats>
          <Grid>
            <ItemTable title={i18next.t("report:Rules hit most")} nameTitle={i18next.t("report:Rule")} items={command.rules} columns={[{title: i18next.t("report:Hits"), value: (item) => item.count}]} />
            <ItemTable title={i18next.t("report:By user")} nameTitle={i18next.t("report:User")} items={command.byUser} columns={[countColumn, {title: i18next.t("report:Blocked"), value: (item) => item.blocked ?? 0}]} />
          </Grid>
          <EventTable title={i18next.t("report:Blocked, flagged and reviewed commands")} events={command.events} />
        </Section>
      ) : null}

      {file ? (
        <Section title={i18next.t("report:File transfers")}>
          <Stats>
            <Stat label={i18next.t("report:Uploads")} value={file.uploads} />
            <Stat label={i18next.t("report:Downloads")} value={file.downloads} />
          </Stats>
          <Grid>
            <ItemTable title={i18next.t("report:By user")} nameTitle={i18next.t("report:User")} items={file.byUser} columns={fileColumns} />
            <ItemTable title={i18next.t("report:By asset")} nameTitle={i18next.t("report:Asset")} items={file.byAsset} columns={fileColumns} />
          </Grid>
          <EventTable title={i18next.t("report:Transfers")} events={file.events} />
        </Section>
      ) : null}

      {approval ? (
        <Section title={i18next.t("report:Access requests and reviews")}>
          <ReviewStats title={i18next.t("report:Access requests")} reviews={approval.accessRequests} />
          <ReviewStats title={i18next.t("report:Login reviews")} reviews={approval.loginReviews} />
          <ReviewStats title={i18next.t("report:Command reviews")} reviews={approval.commandReviews} />
          <Grid>
            <ItemTable title={i18next.t("report:Requesters")} nameTitle={i18next.t("report:User")} items={approval.requesters} columns={[countColumn]} />
            <ItemTable title={i18next.t("report:Reviewers")} nameTitle={i18next.t("report:User")} items={approval.reviewers} columns={[countColumn]} />
          </Grid>
          <EventTable title={i18next.t("report:Access requests")} events={approval.events} />
        </Section>
      ) : null}

      {account ? (
        <Section title={i18next.t("report:Accounts")}>
          <Stats>
            <Stat label={i18next.t("report:Password changes")} value={account.rotated} />
            <Stat label={i18next.t("report:Failed password changes")} value={account.rotateFailed} tone={account.rotateFailed > 0 ? "danger" : undefined} />
            <Stat label={i18next.t("report:Password pushes")} value={account.pushed} />
            <Stat label={i18next.t("report:Failed pushes")} value={account.pushFailed} tone={account.pushFailed > 0 ? "danger" : undefined} />
            <Stat label={i18next.t("report:New risky accounts")} value={account.newRisks} tone={account.newRisks > 0 ? "warning" : undefined} />
            <Stat label={i18next.t("report:Resolved risky accounts")} value={account.resolvedRisks} />
            <Stat label={i18next.t("report:Open risky accounts now")} value={account.openRisks} />
            <Stat label={i18next.t("report:Ignored risky accounts now")} value={account.ignoredRisks} />
          </Stats>
          <Grid>
            <ItemTable title={i18next.t("report:New risks by kind")} nameTitle={i18next.t("report:Kind")} items={account.riskKinds} columns={[countColumn]} />
          </Grid>
          <EventTable title={i18next.t("report:Password changes and pushes")} events={account.events} />
        </Section>
      ) : null}

      {asset ? (
        <Section title={i18next.t("report:Assets and permissions")}>
          <Stats>
            <Stat label={i18next.t("report:Assets now")} value={asset.total} />
            <Stat label={i18next.t("report:Added assets")} value={asset.added} />
            <Stat label={i18next.t("report:Deleted assets")} value={asset.deleted} />
            <Stat label={i18next.t("report:Permissions now")} value={asset.permissions} />
            <Stat label={i18next.t("report:Added permissions")} value={asset.permissionsAdded} />
            <Stat label={i18next.t("report:Updated permissions")} value={asset.permissionsUpdated} />
            <Stat label={i18next.t("report:Removed permissions")} value={asset.permissionsDeleted} />
          </Stats>
          <Grid>
            <ItemTable title={i18next.t("report:Assets by protocol")} nameTitle={i18next.t("report:Protocol")} items={asset.byProtocol} columns={[countColumn]} />
          </Grid>
          <EventTable title={i18next.t("report:Asset changes")} events={asset.events} />
          <EventTable title={i18next.t("report:Permission changes")} events={asset.permissionEvents} />
        </Section>
      ) : null}
    </div>
  );
}
