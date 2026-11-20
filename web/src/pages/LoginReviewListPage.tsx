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
import * as LoginRuleBackend from "@/backend/LoginRuleBackend";
import type {LoginReview} from "@/backend/types";
import {SelectField} from "@/components/common/SelectField";
import {Button} from "@/components/ui/button";
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle} from "@/components/ui/dialog";
import {Textarea} from "@/components/ui/textarea";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {dateColumn, textColumn} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {useAccount} from "@/hooks/use-account";
import {LoginReviewStatusBadge, LoginReviewStatuses} from "@/lib/loginRule";
import * as Setting from "@/lib/setting";

const allStatuses = "@all";

function ReviewDialog({review, approve, onClose}: {review: LoginReview; approve: boolean; onClose: (changed: boolean) => void}) {
  const [comment, setComment] = React.useState("");
  const [saving, setSaving] = React.useState(false);

  const submit = async() => {
    setSaving(true);
    try {
      const res = await LoginRuleBackend.reviewLogin(`${review.owner}/${review.name}`, approve, comment);
      if (res.status === "ok") {
        Setting.showMessage("success", approve ? i18next.t("loginRule:The sign-in has been approved") : i18next.t("loginRule:The sign-in has been rejected"));
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
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{approve ? i18next.t("loginRule:Approve the sign-in") : i18next.t("loginRule:Reject the sign-in")}: {review.user}</DialogTitle>
          <DialogDescription>
            {[review.method, review.clientIp + (review.clientIpDesc ? ` (${review.clientIpDesc})` : ""), review.rule].join(", ")}
          </DialogDescription>
        </DialogHeader>
        <Textarea value={comment} rows={3} placeholder={i18next.t("loginRule:Comment, shown to the user")} onChange={(e) => setComment(e.target.value)} />
        <DialogFooter>
          <Button variant="ghost" onClick={() => onClose(false)}>{i18next.t("general:Cancel")}</Button>
          <Button variant={approve ? "default" : "destructive"} loading={saving} onClick={submit}>
            {approve ? i18next.t("loginRule:Approve") : i18next.t("loginRule:Reject")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export default function LoginReviewListPage() {
  const {account} = useAccount();
  const owner = Setting.getRequestOrganization(account);
  const [status, setStatus] = React.useState("Pending");
  const [reviewing, setReviewing] = React.useState<{review: LoginReview; approve: boolean; refresh: () => void} | null>(null);

  const fetchReviews = React.useCallback(
    (query: TableQuery) =>
      LoginRuleBackend.getLoginReviews(owner, status === allStatuses ? "" : status, {
        page: query.page,
        pageSize: query.pageSize,
        field: query.searchedColumn,
        value: query.searchText,
        sortField: query.sortField,
        sortOrder: query.sortOrder,
      }),
    [owner, status],
  );

  const columns: ColumnDef<LoginReview>[] = [
    dateColumn<LoginReview>("createdTime"),
    textColumn<LoginReview>({dataIndex: "user", title: i18next.t("general:User"), width: 130, searchable: true}),
    {
      dataIndex: "clientIp",
      title: i18next.t("permission:Client IPs"),
      width: 170,
      render: (value, review) => (
        <div className="text-xs">
          <div className="font-mono">{value}</div>
          {review.clientIpDesc ? <div className="text-muted-foreground">{review.clientIpDesc}</div> : null}
        </div>
      ),
    },
    textColumn<LoginReview>({dataIndex: "method", title: i18next.t("loginRule:Sign-in methods"), width: 90}),
    textColumn<LoginReview>({dataIndex: "rule", title: i18next.t("general:Login rules"), width: 150, searchable: true}),
    {
      dataIndex: "status",
      title: i18next.t("general:Status"),
      width: 110,
      render: (value) => <LoginReviewStatusBadge status={value} />,
    },
    textColumn<LoginReview>({dataIndex: "reviewer", title: i18next.t("loginRule:Reviewer"), width: 140}),
    textColumn<LoginReview>({dataIndex: "comment", title: i18next.t("loginRule:Comment"), width: 180}),
    textColumn<LoginReview>({dataIndex: "userAgent", title: i18next.t("general:User agent"), width: 200}),
  ];

  return (
    <>
      <CrudListPage<LoginReview>
        title={i18next.t("general:Login reviews")}
        description={i18next.t("general:Approve or reject the sign-ins that login rules hold for review")}
        columns={columns}
        fetch={fetchReviews}
        deps={[owner, status]}
        toolbar={(
          <SelectField
            className="w-36"
            value={status}
            onChange={setStatus}
            options={[{id: allStatuses, name: i18next.t("loginRule:All")}, ...LoginReviewStatuses.map((s) => ({id: s, name: i18next.t(`loginRule:${s}`)}))]}
          />
        )}
        actionColumnWidth={180}
        rowActions={(review, _index, {refresh}) => review.status === "Pending" ? [
          {key: "approve", label: i18next.t("loginRule:Approve"), onSelect: () => setReviewing({review, approve: true, refresh})},
          {key: "reject", label: i18next.t("loginRule:Reject"), destructive: true, onSelect: () => setReviewing({review, approve: false, refresh})},
        ] : []}
        tableId="login-reviews"
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
