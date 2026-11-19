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
import {FileDown} from "lucide-react";
import type {TableQuery} from "@/components/crud/types";
import {Button} from "@/components/ui/button";
import * as Setting from "@/lib/setting";

interface ExportCsvButtonProps {
  /** the export API, such as /api/export-commands */
  path: string;
  owner: string;
  /** the list's search and sort, so the file has the rows the list shows on all of its pages */
  query: TableQuery;
  params?: Record<string, string>;
}

function getFileName(disposition: string | null, fallback: string) {
  const match = /filename="?([^";]+)"?/.exec(disposition ?? "");
  return match ? match[1] : fallback;
}

/** Downloads every row that matches the list's filter as a CSV file. */
export function ExportCsvButton({path, owner, query, params = {}}: ExportCsvButtonProps) {
  const [loading, setLoading] = React.useState(false);

  const download = async() => {
    const search = new URLSearchParams({
      owner,
      field: query.searchText ? query.searchedColumn : "",
      value: query.searchText,
      sortField: query.sortField,
      sortOrder: query.sortOrder,
      ...params,
    });
    setLoading(true);
    try {
      const res = await fetch(`${Setting.ServerUrl}${path}?${search.toString()}`, {credentials: "include"});
      if (!res.ok || !(res.headers.get("Content-Type") ?? "").includes("text/csv")) {
        const data = await Setting.handleFetchResponse(res);
        Setting.showMessage("error", data?.msg || `HTTP ${res.status}`);
        return;
      }
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = getFileName(res.headers.get("Content-Disposition"), "export.csv");
      document.body.appendChild(link);
      link.click();
      link.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 10000);
    } catch (error) {
      Setting.showMessage("error", `${i18next.t("general:Failed to connect to server")}: ${error}`);
    } finally {
      setLoading(false);
    }
  };

  return (
    <Button variant="outline" onClick={download} loading={loading}>
      <FileDown />
      {i18next.t("general:Export CSV")}
    </Button>
  );
}
