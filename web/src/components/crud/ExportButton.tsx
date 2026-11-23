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
import {ChevronDown, FileDown} from "lucide-react";
import type {TableQuery} from "@/components/crud/types";
import {Button} from "@/components/ui/button";
import {DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger} from "@/components/ui/dropdown-menu";
import {downloadFile, XlsxContentType} from "@/lib/download";
import * as Setting from "@/lib/setting";

interface ExportButtonProps {
  /** the export API, such as /api/export-commands */
  path: string;
  owner: string;
  /** the list's search and sort, so the file has the rows the list shows on all of its pages */
  query: TableQuery;
  params?: Record<string, string>;
}

/** Downloads every row that matches the list's filter as a CSV file or an Excel workbook. */
export function ExportButton({path, owner, query, params = {}}: ExportButtonProps) {
  const [loading, setLoading] = React.useState(false);

  const download = async(format: "csv" | "xlsx") => {
    const search = new URLSearchParams({
      owner,
      field: query.searchText ? query.searchedColumn : "",
      value: query.searchText,
      sortField: query.sortField,
      sortOrder: query.sortOrder,
      ...params,
      format,
    });
    setLoading(true);
    try {
      const error = await downloadFile(path, search, format === "xlsx" ? XlsxContentType : "text/csv", `export.${format}`);
      if (error) {
        Setting.showMessage("error", error);
      }
    } catch (error) {
      Setting.showMessage("error", `${i18next.t("general:Failed to connect to server")}: ${error}`);
    } finally {
      setLoading(false);
    }
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" loading={loading}>
          <FileDown />
          {i18next.t("general:Export")}
          <ChevronDown />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onSelect={() => download("csv")}>CSV (.csv)</DropdownMenuItem>
        <DropdownMenuItem onSelect={() => download("xlsx")}>Excel (.xlsx)</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
