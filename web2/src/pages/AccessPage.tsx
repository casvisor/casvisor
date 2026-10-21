// Copyright 2024 The casbin Authors. All Rights Reserved.
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

import {useParams, useSearchParams} from "react-router-dom";
import {AssetConnection} from "@/components/access/AssetConnection";
import {takeCredentials} from "@/components/access/ConnectButton";

/** One asset in a window of its own, opened by the Connect button of the asset list. */
export default function AccessPage() {
  const {owner = "", name = ""} = useParams();
  const [searchParams] = useSearchParams();
  const assetId = `${owner}/${name}`;

  // the Connect dialog hands over what was typed; ?username=&password= is the antd frontend's link
  const credentials = takeCredentials(assetId);
  const username = credentials.username || searchParams.get("username") || "";
  const password = credentials.password || searchParams.get("password") || "";

  return (
    <div className="h-screen w-screen overflow-hidden">
      <AssetConnection assetId={assetId} username={username} password={password} />
    </div>
  );
}
