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
import {Copy} from "lucide-react";
import {useNavigate, useParams} from "react-router-dom";
import * as NetworkGatewayBackend from "@/backend/NetworkGatewayBackend";
import * as SshKeyBackend from "@/backend/SshKeyBackend";
import type {NetworkGateway} from "@/backend/types";
import {Button} from "@/components/ui/button";
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle} from "@/components/ui/dialog";
import {Input} from "@/components/ui/input";
import {Switch} from "@/components/ui/switch";
import {Textarea} from "@/components/ui/textarea";
import {ConfirmButton} from "@/components/common/ConfirmButton";
import {Loading} from "@/components/common/Loading";
import {PasswordInput} from "@/components/common/PasswordInput";
import {SelectField} from "@/components/common/SelectField";
import {UnauthorizedPage} from "@/components/common/UnauthorizedPage";
import {EditPageShell} from "@/components/crud/EditPageShell";
import {FormRow} from "@/components/crud/FormRow";
import {useEditRecord} from "@/hooks/use-edit-record";
import {getModeTitleKey, submitEdit} from "@/lib/crud";
import {copyText} from "@/lib/gateway";
import * as Setting from "@/lib/setting";
import {NetworkGatewayStatus} from "@/pages/NetworkGatewayListPage";
import NotFoundPage from "@/pages/NotFoundPage";

function CommandLine({command}: {command: string}) {
  return (
    <div className="flex items-center gap-2 rounded-md bg-muted px-3 py-2 font-mono text-xs">
      <span className="min-w-0 flex-1 break-all">{command}</span>
      <Button size="icon" variant="ghost" className="h-7 w-7 shrink-0" aria-label={i18next.t("general:Copy")} onClick={() => copyText(command)}>
        <Copy />
      </Button>
    </div>
  );
}

export default function NetworkGatewayEditPage() {
  const {owner = "", name = ""} = useParams();
  const navigate = useNavigate();
  const [saving, setSaving] = React.useState(false);
  const [testing, setTesting] = React.useState(false);
  const [token, setToken] = React.useState("");
  const [hostKeyFingerprint, setHostKeyFingerprint] = React.useState("");

  const {record: gateway, updateField, updateFields, loading, notFound, denied, mode} = useEditRecord<NetworkGateway>({
    fetch: () => NetworkGatewayBackend.getNetworkGateway(owner, name),
    deps: [owner, name],
  });

  const isAgent = gateway?.type === "Agent";

  // Agents check Casvisor's SSH host key, whichever way they connect.
  React.useEffect(() => {
    if (!isAgent) {
      return;
    }
    SshKeyBackend.getSshGateway().then((res) => {
      if (res?.status === "ok") {
        setHostKeyFingerprint(res.data.fingerprint);
      }
    }).catch(() => {});
  }, [isAgent]);

  // Keeps the agent's status current while the page is open.
  React.useEffect(() => {
    if (!isAgent || mode === "add") {
      return;
    }
    const timer = window.setInterval(() => {
      NetworkGatewayBackend.getNetworkGateway(owner, name).then((res) => {
        if (res.status === "ok" && res.data) {
          const {agentKey, agentVersion, agentIp, lastOnlineTime, isOnline} = res.data as NetworkGateway;
          updateFields({agentKey, agentVersion, agentIp, lastOnlineTime, isOnline});
        }
      }).catch(() => {});
    }, 5000);
    return () => window.clearInterval(timer);
  }, [isAgent, mode, owner, name, updateFields]);

  if (loading) {
    return <Loading />;
  }
  if (denied) {
    return <UnauthorizedPage />;
  }
  if (notFound || !gateway) {
    return <NotFoundPage />;
  }

  const save = (exitAfterSave: boolean) => {
    setSaving(true);
    return submitEdit({
      mode,
      record: gateway,
      add: NetworkGatewayBackend.addNetworkGateway,
      update: (record) => NetworkGatewayBackend.updateNetworkGateway(owner, name, record),
      onSaved: (record) => {
        if (exitAfterSave) {
          navigate("/network-gateways");
        } else if (record.name !== name) {
          navigate(`/network-gateways/${record.owner}/${encodeURIComponent(record.name)}`, {replace: true});
        }
      },
      onFailed: () => updateField("name", name),
    }).finally(() => setSaving(false));
  };

  // The test connects with the saved settings, so the form is saved first.
  const test = async() => {
    setTesting(true);
    try {
      const saved = await save(false);
      if (!saved) {
        return;
      }
      const res = await NetworkGatewayBackend.testNetworkGateway(gateway.owner, gateway.name);
      if (res.status === "ok") {
        if (!isAgent) {
          updateField("fingerprint", res.data);
        }
        Setting.showMessage("success", `${i18next.t("networkGateway:Connected")}: ${res.data2}`);
      } else {
        Setting.showMessage("error", `${i18next.t("networkGateway:Failed to connect")}: ${res.msg}`);
      }
    } catch (error: any) {
      Setting.showMessage("error", `${i18next.t("general:Failed to connect to server")}: ${error}`);
    } finally {
      setTesting(false);
    }
  };

  // A token belongs to a saved gateway, so the form is saved first.
  const generateToken = async() => {
    const saved = await save(false);
    if (!saved) {
      return;
    }
    const res = await NetworkGatewayBackend.generateNetworkGatewayToken(gateway.owner, gateway.name);
    if (res.status === "ok") {
      setToken(res.data);
      updateFields({agentKey: "***", isOnline: false});
    } else {
      Setting.showMessage("error", `${i18next.t("general:Failed to save")}: ${res.msg}`);
    }
  };

  const revokeToken = async() => {
    const res = await NetworkGatewayBackend.revokeNetworkGatewayToken(gateway.owner, gateway.name);
    if (res.status === "ok") {
      updateFields({agentKey: "", isOnline: false});
      Setting.showMessage("success", i18next.t("networkGateway:Token revoked"));
    } else {
      Setting.showMessage("error", `${i18next.t("general:Failed to save")}: ${res.msg}`);
    }
  };

  const agentCommand = `casvisor agent --server ${window.location.origin} --token ${token} --fingerprint ${hostKeyFingerprint}`;

  return (
    <EditPageShell
      title={i18next.t(getModeTitleKey("networkGateway:Edit Network Gateway", mode))}
      description={isAgent
        ? i18next.t("networkGateway:An agent in the other network connects out to Casvisor over HTTPS and keeps the connection open, Casvisor reaches the assets that use the gateway through it")
        : i18next.t("networkGateway:Casvisor connects to the gateway over SSH and reaches the assets that use it from there, the gateway needs AllowTcpForwarding")}
      mode={mode}
      backTo="/network-gateways"
      onSave={save}
      saving={saving}
      remove={() => NetworkGatewayBackend.deleteNetworkGateway(gateway)}
      extraActions={
        <Button variant="outline" disabled={testing || saving} onClick={test}>
          {i18next.t("networkGateway:Test connection")}
        </Button>
      }
      grid
    >
      <FormRow labelKey="general:Organization">
        <Input value={gateway.owner} disabled />
      </FormRow>
      <FormRow labelKey="general:Name">
        <Input value={gateway.name} onChange={(e) => updateField("name", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Display name">
        <Input value={gateway.displayName} onChange={(e) => updateField("displayName", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Description">
        <Input value={gateway.description} onChange={(e) => updateField("description", e.target.value)} />
      </FormRow>
      <FormRow labelKey="general:Type">
        <SelectField
          value={gateway.type || "SSH"}
          options={[
            {id: "SSH", name: i18next.t("networkGateway:SSH jump host")},
            {id: "Agent", name: i18next.t("networkGateway:Agent")},
          ]}
          onChange={(type) => updateField("type", type)}
        />
      </FormRow>
      {isAgent ? (
        <>
          <FormRow label={i18next.t("general:Status")}>
            <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
              <NetworkGatewayStatus gateway={gateway} />
              {gateway.lastOnlineTime ? (
                <span className="text-muted-foreground">
                  {i18next.t("networkGateway:Last online")}: {Setting.getFormattedDate(gateway.lastOnlineTime)}
                </span>
              ) : null}
              {gateway.agentVersion ? (
                <span className="text-muted-foreground">{i18next.t("networkGateway:Agent version")}: {gateway.agentVersion}</span>
              ) : null}
              {gateway.agentIp ? (
                <span className="text-muted-foreground">{i18next.t("networkGateway:Source IP")}: {gateway.agentIp}</span>
              ) : null}
            </div>
          </FormRow>
          <FormRow label={i18next.t("networkGateway:Token")}>
            <div className="flex w-full flex-col gap-2">
              <div className="flex flex-wrap items-center gap-2">
                {gateway.agentKey ? (
                  <>
                    <ConfirmButton
                      variant="outline"
                      destructive={false}
                      title={i18next.t("networkGateway:Generate a new token?")}
                      description={i18next.t("networkGateway:The agent using the current token is disconnected and has to be started again with the new one")}
                      onConfirm={generateToken}
                    >
                      {i18next.t("networkGateway:Regenerate token")}
                    </ConfirmButton>
                    <ConfirmButton
                      variant="outline"
                      title={i18next.t("networkGateway:Revoke the token?")}
                      description={i18next.t("networkGateway:The agent is disconnected right away and cannot connect again until a new token is generated")}
                      onConfirm={revokeToken}
                    >
                      {i18next.t("networkGateway:Revoke token")}
                    </ConfirmButton>
                  </>
                ) : (
                  <Button variant="outline" disabled={saving} onClick={generateToken}>
                    {i18next.t("networkGateway:Generate token")}
                  </Button>
                )}
              </div>
              <span className="text-xs text-muted-foreground">
                {gateway.agentKey
                  ? i18next.t("networkGateway:A token is set. It was shown only once, generate a new one if it is lost")
                  : i18next.t("networkGateway:No token yet, generate one and start the agent with it")}
              </span>
            </div>
          </FormRow>
          <FormRow label={i18next.t("networkGateway:Allowed networks")}>
            <div className="flex w-full flex-col gap-1">
              <Textarea
                value={(gateway.allowedNetworks ?? []).join("\n")}
                rows={3}
                className="font-mono text-xs"
                placeholder={"10.0.0.0/8\n192.168.1.0/24"}
                onChange={(e) => updateField("allowedNetworks", e.target.value.split("\n"))}
              />
              <span className="text-xs text-muted-foreground">
                {i18next.t("networkGateway:One network (CIDR) or IP address per line, the agent refuses to connect anywhere else. Leave it empty to allow every address")}
              </span>
            </div>
          </FormRow>
        </>
      ) : (
        <>
          <FormRow labelKey="asset:Host">
            <Input value={gateway.host} onChange={(e) => updateField("host", e.target.value)} />
          </FormRow>
          <FormRow labelKey="general:Port">
            <Input
              type="number"
              value={gateway.port}
              onChange={(e) => updateField("port", Setting.myParseInt(e.target.value))}
            />
          </FormRow>
          <FormRow labelKey="general:Username">
            <Input value={gateway.username} autoComplete="off" onChange={(e) => updateField("username", e.target.value)} />
          </FormRow>
          <FormRow labelKey="general:Password">
            <PasswordInput
              value={gateway.password}
              autoComplete="new-password"
              onChange={(e) => updateField("password", e.target.value)}
            />
          </FormRow>
          <FormRow labelKey="asset:Private key">
            <Textarea
              value={gateway.privateKey}
              rows={4}
              className="font-mono text-xs"
              placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
              onChange={(e) => updateField("privateKey", e.target.value)}
            />
          </FormRow>
          <FormRow labelKey="asset:Fingerprint">
            <div className="flex w-full flex-col gap-1">
              <Input
                value={gateway.fingerprint}
                className="font-mono text-xs"
                onChange={(e) => updateField("fingerprint", e.target.value)}
              />
              <span className="text-xs text-muted-foreground">
                {i18next.t("networkGateway:Recorded on the first connection, a different host key is rejected afterwards. Clear it if the gateway was reinstalled")}
              </span>
            </div>
          </FormRow>
        </>
      )}
      <FormRow labelKey="permission:Is enabled">
        <Switch checked={gateway.isEnabled} onCheckedChange={(checked) => updateField("isEnabled", checked)} />
      </FormRow>
      <Dialog open={token !== ""} onOpenChange={(open) => !open && setToken("")}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>{i18next.t("networkGateway:Start the agent")}</DialogTitle>
            <DialogDescription>
              {i18next.t("networkGateway:The token is shown only this once. Run the command on a machine in the other network, with the casvisor binary of the same release")}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <CommandLine command={agentCommand} />
            <div className="space-y-1">
              <div className="text-sm font-medium">{i18next.t("networkGateway:Token")}</div>
              <CommandLine command={token} />
            </div>
          </div>
          <DialogFooter>
            <Button onClick={() => setToken("")}>{i18next.t("general:OK")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </EditPageShell>
  );
}
