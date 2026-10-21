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

import {useIsDark} from "@/hooks/use-theme";
import * as Setting from "@/lib/setting";
import {cn} from "@/lib/utils";

/**
 * The Casvisor wordmark. The CDN has no white version of it, so the dark theme
 * pairs the icon with the name in text instead of the dark-lettered image.
 */
export function Logo({className}: {className?: string}) {
  const isDark = useIsDark();
  if (!isDark) {
    return <img src={Setting.getLogo()} alt="Casvisor" className={cn("h-7 object-contain", className)} />;
  }
  return (
    <span className={cn("inline-flex h-7 items-center gap-1.5 text-lg font-semibold tracking-tight", className)}>
      <img src={Setting.getFaviconUrl()} alt="" className="h-[85%] object-contain" />
      casvisor
    </span>
  );
}
