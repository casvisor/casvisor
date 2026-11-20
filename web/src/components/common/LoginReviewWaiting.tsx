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
import {Clock, ShieldX} from "lucide-react";
import * as LoginRuleBackend from "@/backend/LoginRuleBackend";
import {Button} from "@/components/ui/button";

interface LoginReviewInfo {
  rule?: string;
  expireTime?: string;
}

/** Waits for an administrator to approve a sign-in that a login rule holds, and reports when it is signed in. */
export function LoginReviewWaiting({info, onSignedIn, onBack}: {info: LoginReviewInfo; onSignedIn: () => void; onBack: () => void}) {
  const [status, setStatus] = React.useState("Pending");
  const [comment, setComment] = React.useState("");

  React.useEffect(() => {
    let stopped = false;
    let timer: number | undefined;
    const poll = async() => {
      try {
        const res = await LoginRuleBackend.getLoginReviewStatus();
        if (stopped) {
          return;
        }
        if (res.status !== "ok") {
          setStatus("Expired");
          return;
        }
        setComment(res.data?.comment ?? "");
        if (res.data?.status === "Used") {
          onSignedIn();
          return;
        }
        setStatus(res.data?.status ?? "Expired");
        if (res.data?.status !== "Pending") {
          return;
        }
      } catch {
        // try again on the next tick
      }
      timer = window.setTimeout(poll, 3000);
    };
    timer = window.setTimeout(poll, 2000);
    return () => {
      stopped = true;
      window.clearTimeout(timer);
    };
  }, [onSignedIn]);

  const cancel = async() => {
    await LoginRuleBackend.cancelLoginReview();
    onBack();
  };

  if (status !== "Pending") {
    return (
      <div className="space-y-4 text-center">
        <ShieldX className="mx-auto h-10 w-10 text-destructive" />
        <div>
          <p className="font-medium">
            {status === "Rejected" ? i18next.t("login:An administrator rejected this sign-in") : i18next.t("login:The sign-in request is no longer valid")}
          </p>
          {comment ? <p className="mt-1 text-sm text-muted-foreground">{comment}</p> : null}
        </div>
        <Button variant="outline" className="w-full" onClick={onBack}>{i18next.t("login:Back to sign in")}</Button>
      </div>
    );
  }

  return (
    <div className="space-y-4 text-center">
      <Clock className="mx-auto h-10 w-10 animate-pulse text-muted-foreground" />
      <div>
        <p className="font-medium">{i18next.t("login:Waiting for an administrator to approve this sign-in")}</p>
        <p className="mt-1 text-sm text-muted-foreground">
          {i18next.t("login:The login rule {{rule}} asks for a review. Keep this page open; it signs you in once approved", {rule: info.rule ?? ""})}
        </p>
        {info.expireTime ? (
          <p className="mt-1 text-xs text-muted-foreground">
            {i18next.t("login:The request expires at {{time}}", {time: dayjs(info.expireTime).format("HH:mm")})}
          </p>
        ) : null}
      </div>
      <Button variant="outline" className="w-full" onClick={cancel}>{i18next.t("general:Cancel")}</Button>
    </div>
  );
}
