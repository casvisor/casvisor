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
import {useParams} from "react-router-dom";
import * as RecordBackend from "@/backend/RecordBackend";
import type {Record} from "@/backend/types";
import {Input} from "@/components/ui/input";
import {Loading} from "@/components/common/Loading";
import {UnauthorizedPage} from "@/components/common/UnauthorizedPage";
import {EditPageShell} from "@/components/crud/EditPageShell";
import {FormRow} from "@/components/crud/FormRow";
import {useEditRecord} from "@/hooks/use-edit-record";
import * as Setting from "@/lib/setting";
import NotFoundPage from "@/pages/NotFoundPage";

function JsonBlock({value}: {value: string}) {
  return (
    <pre className="max-h-[300px] overflow-auto whitespace-pre-wrap break-all rounded-md border bg-muted/50 p-3 text-xs scrollbar-thin">
      {Setting.formatJsonString(value) || "-"}
    </pre>
  );
}

/** Audit records are written by the backend; the page only shows one. */
export default function RecordEditPage() {
  const {owner = "", name = ""} = useParams();

  const {record, loading, notFound, denied} = useEditRecord<Record>({
    fetch: () => RecordBackend.getRecord(owner, name),
    deps: [owner, name],
  });

  if (loading) {
    return <Loading />;
  }
  if (denied) {
    return <UnauthorizedPage />;
  }
  if (notFound || !record) {
    return <NotFoundPage />;
  }

  const fields: [string, string][] = [
    ["general:Organization", record.organization || record.owner],
    ["general:Name", record.name],
    ["general:Client IP", record.clientIp],
    ["general:User", record.user],
    ["general:Method", record.method],
    ["general:Request URI", record.requestUri],
    ["general:Action", record.action],
    ["general:Language", record.language],
    ["general:Created time", Setting.getFormattedDate(record.createdTime) ?? ""],
  ];

  return (
    <EditPageShell
      title={i18next.t("record:View Record")}
      mode="view"
      backTo="/records"
      onSave={() => undefined}
      grid
    >
      {fields.map(([key, value]) => (
        <FormRow key={key} labelKey={key}>
          <Input value={value ?? ""} readOnly />
        </FormRow>
      ))}
      <FormRow labelKey="general:Object" block>
        <JsonBlock value={record.object} />
      </FormRow>
      <FormRow labelKey="general:Response" block>
        <JsonBlock value={record.response} />
      </FormRow>
    </EditPageShell>
  );
}
