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
import {ChevronRight} from "lucide-react";
import {Link, useLocation} from "react-router-dom";

/** Console breadcrumbs; only the routes listed here get a trail. */
const RESOURCE_LABELS: Record<string, string> = {
  "assets": "general:Assets",
  "permissions": "general:Permissions",
  "sessions": "general:Sessions",
  "records": "general:Records",
};

interface Crumb {
  label: string;
  to?: string;
}

export function buildBreadcrumbItems(pathname: string): Crumb[] | null {
  const pathSegments = (pathname || "").split("/").filter(Boolean);
  if (pathSegments.length === 0) {
    return null;
  }

  const rootSegment = pathSegments[0];
  const listLabelKey = RESOURCE_LABELS[rootSegment];
  if (!listLabelKey) {
    return null;
  }

  const home: Crumb = {label: i18next.t("general:Home"), to: "/"};
  if (pathSegments.length === 1) {
    return [home, {label: i18next.t(listLabelKey)}];
  }

  const lastSegment = pathSegments[pathSegments.length - 1];
  const lastLabelKey = RESOURCE_LABELS[lastSegment];
  return [
    home,
    {label: i18next.t(listLabelKey), to: `/${rootSegment}`},
    {label: lastLabelKey ? i18next.t(lastLabelKey) : lastSegment},
  ];
}

export function BreadcrumbBar() {
  const location = useLocation();
  const items = buildBreadcrumbItems(location.pathname);
  if (!items) {
    return null;
  }

  return (
    <nav aria-label="Breadcrumb" className="min-w-0">
      <ol className="flex min-w-0 items-center gap-1 text-sm text-muted-foreground">
        {items.map((item, index) => (
          <li key={`${item.label}-${index}`} className="flex min-w-0 items-center gap-1">
            {index > 0 ? <ChevronRight className="h-3.5 w-3.5 shrink-0 opacity-60" aria-hidden /> : null}
            {item.to ? (
              <Link to={item.to} className="truncate underline-offset-4 hover:text-foreground hover:underline">
                {item.label}
              </Link>
            ) : (
              <span className="truncate font-medium text-foreground" aria-current="page">
                {item.label}
              </span>
            )}
          </li>
        ))}
      </ol>
    </nav>
  );
}

export default BreadcrumbBar;
