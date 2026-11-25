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
import {Link, useSearchParams} from "react-router-dom";
import {Plus} from "lucide-react";
import * as AccessRequestBackend from "@/backend/AccessRequestBackend";
import type {AccessRequest, RequestableAsset} from "@/backend/types";
import {Badge} from "@/components/ui/badge";
import {Button} from "@/components/ui/button";
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle} from "@/components/ui/dialog";
import {Input} from "@/components/ui/input";
import {Textarea} from "@/components/ui/textarea";
import {MultiSelect} from "@/components/common/MultiSelect";
import {CrudListPage} from "@/components/crud/CrudListPage";
import {dateColumn, tagsColumn, textColumn} from "@/components/crud/columns";
import type {ColumnDef, TableQuery} from "@/components/crud/types";
import {useAccount} from "@/hooks/use-account";
import {PermissionActions} from "@/lib/asset";
import * as Setting from "@/lib/setting";

const statusVariants: Record<string, "info" | "success" | "destructive" | "secondary" | "warning"> = {
  Pending: "info",
  Approved: "success",
  Rejected: "destructive",
  Cancelled: "secondary",
  Revoked: "warning",
};

function StatusBadge({status}: {status: string}) {
  return <Badge variant={statusVariants[status] ?? "secondary"}>{i18next.t(`accessRequest:${status}`)}</Badge>;
}

/** A datetime-local control over an RFC 3339 string. */
function TimeInput({value, onChange}: {value: string; onChange: (value: string) => void}) {
  const parsed = value ? dayjs(value) : null;
  return (
    <Input
      type="datetime-local"
      value={parsed && parsed.isValid() ? parsed.format("YYYY-MM-DDTHH:mm") : ""}
      onChange={(e) => onChange(e.target.value ? dayjs(e.target.value).format() : "")}
    />
  );
}

function Field({label, children}: {label: React.ReactNode; children: React.ReactNode}) {
  return (
    <div className="space-y-1.5">
      <label className="text-sm font-medium">{label}</label>
      {children}
    </div>
  );
}

function getActionLabel(action: string) {
  return i18next.t(`permission:${action}`);
}

function NewRequestDialog({open, onOpenChange, onCreated}: {open: boolean; onOpenChange: (open: boolean) => void; onCreated: () => void}) {
  const [assets, setAssets] = React.useState<RequestableAsset[]>([]);
  const [selected, setSelected] = React.useState<string[]>([]);
  const [accounts, setAccounts] = React.useState<string[]>([]);
  const [actions, setActions] = React.useState<string[]>(["Connect"]);
  const [startTime, setStartTime] = React.useState("");
  const [endTime, setEndTime] = React.useState("");
  const [reason, setReason] = React.useState("");
  const [saving, setSaving] = React.useState(false);

  React.useEffect(() => {
    if (!open) {
      return;
    }
    setSelected([]);
    setAccounts([]);
    setActions(["Connect"]);
    setStartTime("");
    setEndTime(dayjs().add(1, "day").startOf("minute").format());
    setReason("");
    AccessRequestBackend.getRequestableAssets().then((res) => {
      if (res.status === "ok") {
        setAssets(res.data ?? []);
      } else {
        Setting.showMessage("error", res.msg);
      }
    });
  }, [open]);

  const accountOptions = React.useMemo(() => {
    const names = new Set<string>();
    for (const asset of assets) {
      if (selected.includes(asset.name)) {
        asset.accounts.forEach((name) => names.add(name));
      }
    }
    return [...names].sort().map((name) => ({value: name, label: name}));
  }, [assets, selected]);

  const submit = async() => {
    setSaving(true);
    try {
      const res = await AccessRequestBackend.addAccessRequest({assets: selected, accounts, actions, startTime, endTime, reason});
      if (res.status === "ok") {
        Setting.showMessage("success", i18next.t("accessRequest:The request has been sent to the administrators"));
        onOpenChange(false);
        onCreated();
      } else {
        Setting.showMessage("error", res.msg);
      }
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{i18next.t("accessRequest:Request access")}</DialogTitle>
          <DialogDescription>{i18next.t("accessRequest:An administrator reviews the request. Once approved, the assets show up in your asset list until the end time")}</DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <Field label={i18next.t("general:Assets")}>
            <MultiSelect
              value={selected}
              options={assets.map((asset) => ({
                value: asset.name,
                label: `${asset.displayName || asset.name} (${asset.protocol})`,
                keywords: `${asset.name} ${asset.displayName} ${asset.tag}`,
              }))}
              onChange={setSelected}
            />
          </Field>
          <Field label={i18next.t("permission:Accounts")}>
            <MultiSelect
              value={accounts}
              options={accountOptions}
              placeholder={i18next.t("permission:The asset's own account")}
              onChange={setAccounts}
            />
          </Field>
          <Field label={i18next.t("permission:Actions")}>
            <MultiSelect
              value={actions}
              options={PermissionActions.map((action) => ({value: action, label: getActionLabel(action), disabled: action === "Connect"}))}
              onChange={(value) => setActions(value.includes("Connect") ? value : ["Connect", ...value])}
            />
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label={i18next.t("permission:Start time")}>
              <TimeInput value={startTime} onChange={setStartTime} />
            </Field>
            <Field label={i18next.t("permission:End time")}>
              <TimeInput value={endTime} onChange={setEndTime} />
            </Field>
          </div>
          <Field label={i18next.t("accessRequest:Reason")}>
            <Textarea value={reason} rows={3} onChange={(e) => setReason(e.target.value)} />
          </Field>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>{i18next.t("general:Cancel")}</Button>
          <Button loading={saving} disabled={selected.length === 0 || !endTime || reason.trim() === ""} onClick={submit}>
            {i18next.t("accessRequest:Send request")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

type ReviewKind = "approve" | "reject" | "revoke";

function ReviewDialog({request, kind, onClose}: {request: AccessRequest | null; kind: ReviewKind; onClose: (changed: boolean) => void}) {
  const [comment, setComment] = React.useState("");
  const [actions, setActions] = React.useState<string[]>([]);
  const [endTime, setEndTime] = React.useState("");
  const [saving, setSaving] = React.useState(false);

  React.useEffect(() => {
    if (request) {
      setComment("");
      setActions(request.actions ?? []);
      setEndTime(request.endTime);
    }
  }, [request]);

  if (!request) {
    return null;
  }

  const titles: Record<ReviewKind, string> = {
    approve: i18next.t("accessRequest:Approve"),
    reject: i18next.t("accessRequest:Reject"),
    revoke: i18next.t("accessRequest:Revoke"),
  };

  const submit = async() => {
    setSaving(true);
    try {
      const review = kind === "approve" ? {comment, actions, endTime} : {comment};
      const call = kind === "approve" ? AccessRequestBackend.approveAccessRequest : kind === "reject" ? AccessRequestBackend.rejectAccessRequest : AccessRequestBackend.revokeAccessRequest;
      const res = await call(request.owner, request.name, review);
      if (res.status === "ok") {
        Setting.showMessage("success", i18next.t("general:Successfully saved"));
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
          <DialogTitle>{titles[kind]}: {request.user}</DialogTitle>
          <DialogDescription className="whitespace-pre-wrap">{request.reason}</DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <div className="text-sm">
            <span className="text-muted-foreground">{i18next.t("general:Assets")}: </span>
            {(request.assets ?? []).join(", ")}
            {request.accounts?.length ? <span className="text-muted-foreground"> ({request.accounts.join(", ")})</span> : null}
          </div>
          {kind === "approve" ? (
            <>
              <Field label={i18next.t("permission:Actions")}>
                <MultiSelect
                  value={actions}
                  options={(request.actions ?? []).map((action) => ({value: action, label: getActionLabel(action), disabled: action === "Connect"}))}
                  onChange={(value) => setActions(value.includes("Connect") ? value : ["Connect", ...value])}
                />
              </Field>
              <Field label={i18next.t("permission:End time")}>
                <TimeInput value={endTime} onChange={setEndTime} />
              </Field>
            </>
          ) : kind === "revoke" ? (
            <p className="text-sm text-muted-foreground">{i18next.t("accessRequest:The permission of this request is deleted and its connected sessions are disconnected")}</p>
          ) : null}
          <Field label={i18next.t("accessRequest:Comment")}>
            <Textarea value={comment} rows={2} onChange={(e) => setComment(e.target.value)} />
          </Field>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onClose(false)}>{i18next.t("general:Cancel")}</Button>
          <Button variant={kind === "approve" ? "default" : "destructive"} loading={saving} onClick={submit}>{titles[kind]}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Users ask for access to assets here, and administrators approve or reject the requests. */
export default function AccessRequestListPage() {
  const {account} = useAccount();
  const owner = Setting.getRequestOrganization(account);
  const isAdmin = Setting.isAdminUser(account);
  const [searchParams, setSearchParams] = useSearchParams();
  const [creating, setCreating] = React.useState(searchParams.get("new") === "1");
  const [review, setReview] = React.useState<{request: AccessRequest; kind: ReviewKind; refresh: () => void} | null>(null);

  const fetchRequests = React.useCallback(
    (query: TableQuery) =>
      AccessRequestBackend.getAccessRequests(owner, {
        page: query.page,
        pageSize: query.pageSize,
        field: query.searchedColumn,
        value: query.searchText,
        sortField: query.sortField,
        sortOrder: query.sortOrder,
      }),
    [owner],
  );

  const columns: ColumnDef<AccessRequest>[] = [
    dateColumn<AccessRequest>("createdTime", i18next.t("general:Created time")),
    textColumn<AccessRequest>({dataIndex: "user", title: i18next.t("general:User"), width: 130, searchable: true}),
    {
      dataIndex: "status",
      title: i18next.t("general:Status"),
      width: 110,
      sortable: true,
      render: (value) => <StatusBadge status={value} />,
    },
    tagsColumn<AccessRequest>({dataIndex: "assets", title: i18next.t("general:Assets")}),
    tagsColumn<AccessRequest>({dataIndex: "accounts", title: i18next.t("permission:Accounts")}),
    tagsColumn<AccessRequest>({dataIndex: "actions", title: i18next.t("permission:Actions")}),
    {
      dataIndex: "endTime",
      title: i18next.t("permission:End time"),
      width: 165,
      render: (value) => <span className="whitespace-nowrap tabular-nums text-muted-foreground">{Setting.getFormattedDate(value)}</span>,
    },
    {
      dataIndex: "reason",
      title: i18next.t("accessRequest:Reason"),
      width: 220,
      searchable: true,
      render: (value) => <span className="line-clamp-2 text-xs">{value}</span>,
    },
    {
      dataIndex: "reviewer",
      title: i18next.t("accessRequest:Reviewer"),
      width: 170,
      render: (value, request) => (
        <div className="text-xs">
          <div>{value}</div>
          {request.reviewComment ? <div className="text-muted-foreground">{request.reviewComment}</div> : null}
          {request.permission ? (
            <Link to={`/permissions/${Setting.getIdPath(request.owner, request.permission)}`} className="text-muted-foreground underline-offset-4 hover:underline">
              {request.permission}
            </Link>
          ) : null}
        </div>
      ),
    },
  ];

  const closeNew = (open: boolean) => {
    setCreating(open);
    if (!open && searchParams.has("new")) {
      searchParams.delete("new");
      setSearchParams(searchParams, {replace: true});
    }
  };

  return (
    <CrudListPage<AccessRequest>
      title={i18next.t("general:Access requests")}
      description={isAdmin ? i18next.t("general:Approve or reject the access users ask for") : i18next.t("general:Ask for access to assets for a while")}
      columns={columns}
      fetch={fetchRequests}
      deps={[owner]}
      remove={isAdmin ? AccessRequestBackend.deleteAccessRequest : undefined}
      actionColumnWidth={220}
      rowActions={(request, _index, {refresh}) => [
        isAdmin && request.status === "Pending" ? {key: "approve", label: i18next.t("accessRequest:Approve"), onSelect: () => setReview({request, kind: "approve", refresh})} : null,
        isAdmin && request.status === "Pending" ? {key: "reject", label: i18next.t("accessRequest:Reject"), destructive: true, onSelect: () => setReview({request, kind: "reject", refresh})} : null,
        isAdmin && request.status === "Approved" ? {key: "revoke", label: i18next.t("accessRequest:Revoke"), destructive: true, onSelect: () => setReview({request, kind: "revoke", refresh})} : null,
        request.status === "Pending" && request.user === account?.name ? {
          key: "cancel",
          label: i18next.t("general:Cancel"),
          confirm: {title: i18next.t("accessRequest:Cancel this request?")},
          onSelect: async() => {
            const res = await AccessRequestBackend.cancelAccessRequest(request.owner, request.name);
            if (res.status === "ok") {
              refresh();
            } else {
              Setting.showMessage("error", res.msg);
            }
          },
        } : null,
      ]}
      toolbar={({refresh}) => (
        <>
          <Button size="sm" onClick={() => setCreating(true)}>
            <Plus />
            {i18next.t("accessRequest:Request access")}
          </Button>
          <NewRequestDialog open={creating} onOpenChange={closeNew} onCreated={refresh} />
          {review ? (
            <ReviewDialog
              request={review.request}
              kind={review.kind}
              onClose={(changed) => {
                if (changed) {
                  review.refresh();
                }
                setReview(null);
              }}
            />
          ) : null}
        </>
      )}
    />
  );
}
