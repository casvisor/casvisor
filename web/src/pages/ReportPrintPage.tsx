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
import {useSearchParams} from "react-router-dom";
import {Printer} from "lucide-react";
import * as ReportBackend from "@/backend/ReportBackend";
import type {ReportData} from "@/backend/types";
import {Loading} from "@/components/common/Loading";
import {ReportView} from "@/components/report/ReportView";
import {Button} from "@/components/ui/button";
import {useAccount} from "@/hooks/use-account";
import {useTheme} from "@/hooks/use-theme";
import * as Setting from "@/lib/setting";
import {ReportMeta, getReportQuery} from "@/pages/ReportPage";

/** The report on a plain page of its own, for the browser to print or save as a PDF file. */
export default function ReportPrintPage() {
  const {account} = useAccount();
  const {resolvedTheme} = useTheme();
  const owner = Setting.getRequestOrganization(account);
  const [params] = useSearchParams();
  const [data, setData] = React.useState<ReportData | null>(null);
  const [error, setError] = React.useState("");
  const autoPrint = params.get("autoPrint") === "1";
  const searchText = params.toString();

  React.useEffect(() => {
    if (!owner) {
      return;
    }
    ReportBackend.getReportData(getReportQuery(new URLSearchParams(searchText), owner)).then((res) => {
      if (res.status === "ok") {
        setData(res.data);
      } else {
        setError(res.msg);
      }
    });
  }, [owner, searchText]);

  // the page is printed on paper, so it is always light
  React.useEffect(() => {
    if (!data) {
      return;
    }
    const root = document.documentElement;
    root.classList.remove("dark");
    root.style.colorScheme = "light";
    return () => {
      root.classList.toggle("dark", resolvedTheme === "dark");
      root.style.colorScheme = resolvedTheme;
    };
  }, [data, resolvedTheme]);

  React.useEffect(() => {
    if (!data) {
      return;
    }
    document.title = `${i18next.t("report:Audit report")} - ${data.owner}`;
    if (autoPrint) {
      const timer = window.setTimeout(() => window.print(), 500);
      return () => window.clearTimeout(timer);
    }
  }, [data, autoPrint]);

  if (error) {
    return <div className="p-6 text-sm text-destructive">{error}</div>;
  }
  if (!data) {
    return <Loading className="min-h-screen" />;
  }

  return (
    <div className="mx-auto max-w-5xl space-y-4 bg-background p-6 print:max-w-none print:p-0 print:text-[12px]">
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-2">
          <h1 className="text-2xl font-semibold">{i18next.t("report:Audit report")}</h1>
          <ReportMeta data={data} />
        </div>
        <Button className="print:hidden" onClick={() => window.print()}>
          <Printer />
          {i18next.t("report:Print or save as PDF")}
        </Button>
      </div>
      <ReportView data={data} />
    </div>
  );
}
