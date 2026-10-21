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
import {Link} from "react-router-dom";
import {Card, CardContent} from "@/components/ui/card";
import {PageHeader} from "@/components/crud/PageHeader";
import {useAccount} from "@/hooks/use-account";
import {getNavItems} from "@/lib/nav";

/** The shortcuts the antd home page showed as a grid of cards. */
export default function HomePage() {
  const {account} = useAccount();
  const items = getNavItems(account).filter((item) => item.description);

  return (
    <div className="space-y-4">
      <PageHeader
        title={i18next.t("general:Home")}
        description={account ? `${account.displayName || account.name} (${account.owner}/${account.name})` : undefined}
      />
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
        {items.map((item) => {
          const Icon = item.icon;
          return (
            <Link key={item.key} to={item.key} target={item.newTab ? "_blank" : undefined}>
              <Card className="h-full transition-colors hover:border-foreground/20">
                <CardContent className="flex items-start gap-3 p-4">
                  <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-md bg-muted">
                    <Icon className="h-5 w-5" />
                  </div>
                  <div className="min-w-0">
                    <div className="truncate font-medium">{item.label}</div>
                    <p className="mt-0.5 line-clamp-2 text-sm text-muted-foreground">{item.description}</p>
                  </div>
                </CardContent>
              </Card>
            </Link>
          );
        })}
      </div>
    </div>
  );
}
