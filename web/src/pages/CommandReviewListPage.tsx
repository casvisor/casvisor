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
import {Link} from "react-router-dom";
import * as CommandBackend from "@/backend/CommandBackend";
import type {CommandReview} from "@/backend/types";
import {SelectField} from "@/components/common/SelectField";
import {Badge} from "@/components/ui/badge";
import {Button} from "@/components/ui/button";
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle} from "@/components/ui/dialog";
import {Textarea} from "@/components/ui/textarea";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {dateColumn, textColumn} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {useAccount} from "@/hooks/use-account";
import * as Setting from "@/lib/setting";

const allStatuses = "@all";

const statusVariants: Record<string, "success" | "destructive" | "warning" | "info" | "secondary"> = {
  Pending: "warning",
  Approved: "info",
  Used: "success",
  Rejected: "destructive",
  Canceled: "secondary",
  Expired: "secondary",
};

export function CommandReviewStatusBadge({status}: {status: string}) {
  return <Badge variant={statusVariants[status] ?? "secondary"}>{i18next.t(`command:${status}`)}</Badge>;
}

function ReviewDialog({review, approve, onClose}: {review: CommandReview; approve: boolean; onClose: (changed: boolean) => void}) {
  const [comment, setComment] = React.useState("");
  const [saving, setSaving] = React.useState(false);

  const submit = async() => {
    setSaving(true);
    try {
      const res = await CommandBackend.reviewCommand(`${review.owner}/${review.name}`, approve, comment);
      if (res.status === "ok") {
        Setting.showMessage("success", approve ? i18next.t("command:The command has been approved") : i18next.t("command:The command has been rejected"));
        onClose(true);
      } else {
        Setting.showMessage("error", res.msg);
      }
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose(false)}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{approve ? i18next.t("command:Approve the command") : i18next.t("command:Reject the command")}</DialogTitle>
          <DialogDescription>
            {[Setting.getShortName(review.user), review.asset, review.clientIp].filter(Boolean).join(", ")}
          </DialogDescription>
        </DialogHeader>
        <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-all rounded-md border bg-muted p-3 font-mono text-xs">{review.text}</pre>
        {approve ? <p className="text-sm text-muted-foreground">{i18next.t("command:The command runs as soon as it is approved")}</p> : null}
        <Textarea value={comment} rows={3} placeholder={i18next.t("command:Comment, shown to the user")} onChange={(e) => setComment(e.target.value)} />
        <DialogFooter>
          <Button variant="ghost" onClick={() => onClose(false)}>{i18next.t("general:Cancel")}</Button>
          <Button variant={approve ? "default" : "destructive"} loading={saving} onClick={submit}>
            {approve ? i18next.t("command:Approve") : i18next.t("command:Reject")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export default function CommandReviewListPage() {
  const {account} = useAccount();
  const owner = Setting.getRequestOrganization(account);
  const [status, setStatus] = React.useState("Pending");
  const [reviewing, setReviewing] = React.useState<{review: CommandReview; approve: boolean; refresh: () => void} | null>(null);

  const fetchReviews = React.useCallback(
    (query: TableQuery) =>
      CommandBackend.getCommandReviews(owner, status === allStatuses ? "" : status, {
        page: query.page,
        pageSize: query.pageSize,
        field: query.searchedColumn,
        value: query.searchText,
        sortField: query.sortField,
        sortOrder: query.sortOrder,
      }),
    [owner, status],
  );

  const columns: ColumnDef<CommandReview>[] = [
    dateColumn<CommandReview>("createdTime"),
    {
      dataIndex: "text",
      title: i18next.t("command:Command"),
      width: 320,
      searchable: true,
      render: (value) => <code className="line-clamp-3 break-all font-mono text-xs" title={value}>{value}</code>,
    },
    {
      dataIndex: "status",
      title: i18next.t("general:Status"),
      width: 110,
      render: (value) => <CommandReviewStatusBadge status={value} />,
    },
    {
      dataIndex: "user",
      title: i18next.t("general:User"),
      width: 130,
      searchable: true,
      render: (value, review) => (
        <div className="text-xs">
          <div>{Setting.getShortName(value)}</div>
          {review.operator && review.operator !== value ? <div className="text-muted-foreground">{i18next.t("command:Typed by {{name}}", {name: Setting.getShortName(review.operator)})}</div> : null}
        </div>
      ),
    },
    {
      dataIndex: "asset",
      title: i18next.t("general:Asset"),
      width: 160,
      searchable: true,
      render: (value) => <Link to={`/assets/${Setting.getIdPath(value)}`} className="underline-offset-4 hover:underline">{value}</Link>,
    },
    {
      dataIndex: "rule",
      title: i18next.t("command:Rule"),
      width: 150,
      render: (value) => (value ? <Link to={`/command-rules/${Setting.getIdPath(value)}`} className="underline-offset-4 hover:underline">{Setting.getShortName(value)}</Link> : null),
    },
    {
      dataIndex: "session",
      title: i18next.t("general:Session"),
      width: 110,
      render: (value, review) => {
        const label = Setting.getShortName(value).slice(0, 8);
        // a command still waiting can be looked at in the session it waits in
        return review.status === "Pending" && value?.includes("/") ? (
          <Link to={`/sessions/${Setting.getIdPath(value)}/monitor`} target="_blank" rel="noreferrer" title={i18next.t("command:Watch the session")} className="font-mono text-xs text-primary underline-offset-4 hover:underline">{label}</Link>
        ) : (
          <Link to={`/commands?session=${encodeURIComponent(value)}`} className="font-mono text-xs underline-offset-4 hover:underline">{label}</Link>
        );
      },
    },
    textColumn<CommandReview>({dataIndex: "clientIp", title: i18next.t("general:Client IP"), width: 130, searchable: true}),
    {
      dataIndex: "reviewer",
      title: i18next.t("command:Reviewer"),
      width: 120,
      render: (value) => (value ? Setting.getShortName(value) : null),
    },
    textColumn<CommandReview>({dataIndex: "comment", title: i18next.t("command:Comment"), width: 180}),
  ];

  return (
    <>
      <CrudListPage<CommandReview>
        title={i18next.t("general:Command reviews")}
        description={i18next.t("general:Approve or reject the commands that command rules hold for review")}
        columns={columns}
        fetch={fetchReviews}
        deps={[owner, status]}
        toolbar={(
          <SelectField
            className="w-36"
            value={status}
            onChange={setStatus}
            options={[{id: allStatuses, name: i18next.t("command:All")}, ...CommandBackend.CommandReviewStatuses.map((s) => ({id: s, name: i18next.t(`command:${s}`)}))]}
          />
        )}
        actionColumnWidth={180}
        rowActions={(review, _index, {refresh}) => review.status === "Pending" ? [
          {key: "approve", label: i18next.t("command:Approve"), onSelect: () => setReviewing({review, approve: true, refresh})},
          {key: "reject", label: i18next.t("command:Reject"), destructive: true, onSelect: () => setReviewing({review, approve: false, refresh})},
        ] : []}
        tableId="command-reviews"
      />
      {reviewing ? (
        <ReviewDialog
          review={reviewing.review}
          approve={reviewing.approve}
          onClose={(changed) => {
            setReviewing(null);
            if (changed) {
              reviewing.refresh();
            }
          }}
        />
      ) : null}
    </>
  );
}
