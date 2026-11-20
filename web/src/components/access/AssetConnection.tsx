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

import * as React from "react";
import i18next from "i18next";
import * as AssetBackend from "@/backend/AssetBackend";
import * as SessionBackend from "@/backend/SessionBackend";
import type {Asset} from "@/backend/types";
import {ConnectCredentialsDialog} from "@/components/access/ConnectCredentialsDialog";
import {clearConnectTicket, closeView, type ConnectionProps} from "@/components/access/util";
import {Loading} from "@/components/common/Loading";
import {Button} from "@/components/ui/button";
import {getAssetCredentialRequirements, isDatabaseProtocol} from "@/lib/asset";
import * as Setting from "@/lib/setting";

// each client library is only downloaded for the protocol that needs it
const SshView = React.lazy(() => import("@/components/access/SshView"));
const VncView = React.lazy(() => import("@/components/access/VncView"));
const RdpView = React.lazy(() => import("@/components/access/RdpView"));
const DatabaseView = React.lazy(() => import("@/components/access/DatabaseView"));

const legacyPasswordNotice = "Passwords in URLs are no longer supported and were removed from the address bar. Please enter your credentials to connect.";

type PendingTicketRequest = {
  promise: Promise<string | null>;
  resolve: (ticket: string | null) => void;
};

/** Looks the asset up and opens it with the client its protocol needs. */
export function AssetConnection(props: ConnectionProps) {
  const {assetId} = props;
  const [loadedAsset, setLoadedAsset] = React.useState<{assetId: string; asset: Asset} | null>(null);
  const [activeTicket, setActiveTicket] = React.useState("");
  const [legacyEntryReady, setLegacyEntryReady] = React.useState(false);
  const [credentialsOpen, setCredentialsOpen] = React.useState(false);
  const pendingTicketRequest = React.useRef<PendingTicketRequest | null>(null);
  const connectionGeneration = React.useRef(0);
  const warnedAsset = React.useRef("");
  const asset = loadedAsset?.assetId === assetId ? loadedAsset.asset : null;
  const legacyEntry = Boolean(props.legacyPasswordRemoved && !props.account);
  // Keep the view mounted on reconnect, even when its initial ticket is cleared.
  const waitingForLegacyCredentials = legacyEntry && !legacyEntryReady && !activeTicket;

  React.useEffect(() => {
    let cancelled = false;
    setLoadedAsset(null);
    warnedAsset.current = "";
    setActiveTicket("");
    setLegacyEntryReady(false);
    setCredentialsOpen(false);
    const [owner, name] = assetId.split("/");
    const showLoadError = (message: string) => {
      if (!cancelled) {
        Setting.showMessage("error", `${i18next.t("asset:Failed to get the asset")}: ${message}`);
      }
    };
    AssetBackend.getAsset(owner, name).then((res) => {
      if (cancelled) {
        return;
      }
      if (res.status === "ok" && res.data) {
        setLoadedAsset({assetId, asset: res.data});
      } else {
        showLoadError(res.msg || assetId);
      }
    }).catch(() => showLoadError(assetId));

    return () => {
      cancelled = true;
      connectionGeneration.current++;
      pendingTicketRequest.current?.resolve(null);
      pendingTicketRequest.current = null;
    };
  }, [assetId]);

  React.useEffect(() => {
    const shouldWarn = waitingForLegacyCredentials || (props.legacyPasswordRemoved && props.account);
    if (asset && shouldWarn && warnedAsset.current !== assetId) {
      warnedAsset.current = assetId;
      Setting.showMessage("warning", legacyPasswordNotice);
      if (waitingForLegacyCredentials) {
        setCredentialsOpen(true);
      }
    }
  }, [assetId, asset, waitingForLegacyCredentials, props.legacyPasswordRemoved, props.account]);

  const fallback = <Loading className="h-full bg-[#1b1b1b]" />;
  if (!asset) {
    return fallback;
  }

  const {protocol} = asset;
  const {needsUsername, needsPassword} = getAssetCredentialRequirements(asset);
  // typed credentials travel as a one-time ticket, so a reconnect has to ask for them again
  const needsManualCredentials = !props.account && (Boolean(props.ticket) || Boolean(activeTicket) || Boolean(props.legacyPasswordRemoved) || needsPassword || (needsUsername && !props.username));

  const requestConnectTicket = () => {
    if (pendingTicketRequest.current) {
      return pendingTicketRequest.current.promise;
    }
    clearConnectTicket(assetId);
    setActiveTicket("");
    let resolveTicket: PendingTicketRequest["resolve"];
    const promise = new Promise<string | null>((resolve) => {
      resolveTicket = resolve;
    });
    pendingTicketRequest.current = {promise, resolve: resolveTicket!};
    setCredentialsOpen(true);
    return promise;
  };

  const onCredentialsOpenChange = (open: boolean) => {
    setCredentialsOpen(open);
    if (!open) {
      pendingTicketRequest.current?.resolve(null);
      pendingTicketRequest.current = null;
    }
  };

  const submitCredentials = async (manualUsername: string, manualPassword: string) => {
    const generation = connectionGeneration.current;
    try {
      const res = await SessionBackend.createConnectTicket(assetId, manualUsername, manualPassword);
      if (connectionGeneration.current !== generation) {
        return false;
      }
      const ticket = res?.data?.ticket;
      if (res?.status !== "ok" || typeof ticket !== "string" || !ticket) {
        throw new Error(res?.msg || i18next.t("asset:Failed to connect"));
      }
      if (pendingTicketRequest.current) {
        pendingTicketRequest.current.resolve(ticket);
        pendingTicketRequest.current = null;
      } else {
        clearConnectTicket(assetId);
        setActiveTicket(ticket);
        setLegacyEntryReady(true);
      }
      return true;
    } catch (err) {
      if (connectionGeneration.current !== generation) {
        return false;
      }
      throw err;
    }
  };

  const connectionProps: ConnectionProps = {
    ...props,
    ticket: legacyEntry ? activeTicket : props.ticket,
    requestConnectTicket: needsManualCredentials ? requestConnectTicket : undefined,
  };
  // A legacy link or an explicit ticket can require a fresh entry even if all defaults are saved.
  const credentialAsset = needsManualCredentials && !needsUsername && !needsPassword
    ? (protocol === "VNC" ? {...asset, password: ""} : {...asset, username: ""})
    : asset;

  let connection: React.ReactNode;
  if (waitingForLegacyCredentials) {
    connection = (
      <div className="flex h-full items-center justify-center bg-[#1b1b1b] p-6">
        <div className="w-full max-w-md space-y-4 rounded-lg border bg-background p-6 text-foreground">
          <p className="text-sm">{legacyPasswordNotice}</p>
          <div className="flex flex-wrap gap-2">
            <Button onClick={() => setCredentialsOpen(true)}>{i18next.t("general:Connect")}</Button>
            <Button variant="outline" onClick={() => closeView(props)}>{i18next.t("asset:Close this page")}</Button>
          </div>
        </div>
      </div>
    );
  } else if (protocol === "VNC") {
    connection = <VncView {...connectionProps} />;
  } else if (protocol === "RDP") {
    connection = <RdpView {...connectionProps} />;
  } else if (isDatabaseProtocol(protocol)) {
    connection = <DatabaseView {...connectionProps} />;
  } else {
    connection = <SshView {...connectionProps} protocol={protocol} />;
  }

  return (
    <>
      <React.Suspense fallback={fallback}>{connection}</React.Suspense>
      <ConnectCredentialsDialog
        asset={credentialAsset}
        open={credentialsOpen}
        onOpenChange={onCredentialsOpenChange}
        initialUsername={props.username || asset.username}
        notice={legacyEntry ? legacyPasswordNotice : undefined}
        onSubmit={submitCredentials}
      />
    </>
  );
}
