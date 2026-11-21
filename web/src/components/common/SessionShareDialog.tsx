import * as React from "react";
import i18next from "i18next";
import {Copy} from "lucide-react";
import * as SessionBackend from "@/backend/SessionBackend";
import {Button} from "@/components/ui/button";
import {Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle} from "@/components/ui/dialog";
import {copyText} from "@/lib/gateway";
import * as Setting from "@/lib/setting";

export function SessionShareDialog({sessionId, onClose}: {sessionId: string | null; onClose: () => void}) {
  const [ttl, setTtl] = React.useState(3600);
  const [share, setShare] = React.useState<any>(null);
  React.useEffect(() => setShare(null), [sessionId]);
  const url = share ? `${window.location.origin}/sessions/share/${share.token}` : "";
  const run = (p: Promise<any>, ok: (d: any) => void) => p.then((r) => (r.status === "ok" ? ok(r.data) : Setting.showMessage("error", r.msg)));
  return (
    <Dialog open={Boolean(sessionId)} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader><DialogTitle>{i18next.t("session:Share")} ({sessionId})</DialogTitle></DialogHeader>
        <div className="flex items-center gap-2"><span className="text-sm text-muted-foreground">{i18next.t("database:Duration")}:</span>{([[900, "15m"], [3600, "1h"], [14400, "4h"]] as const).map(([s, l]) => <Button key={s} size="sm" variant={ttl === s ? "default" : "outline"} onClick={() => setTtl(s)}>{l}</Button>)}</div>
        {share ? (
          <div className="space-y-2 text-sm">
            {[[url, url], [`${i18next.t("session:Verification code")}: ${share.code}`, share.code]].map(([label, text], i) => <div key={i} className="flex items-center justify-between gap-2 rounded-md bg-muted px-3 py-1.5 font-mono text-xs"><span className="min-w-0 flex-1 truncate">{label}</span><Button size="iconSm" variant="ghost" aria-label={i18next.t("general:Copy")} onClick={() => copyText(text)}><Copy /></Button></div>)}
            <p className="text-xs text-muted-foreground">{i18next.t("session:Expires at")}: {Setting.getFormattedDate(share.expiresAt)}</p>
          </div>
        ) : null}
        <DialogFooter>{share ? <Button variant="destructive" onClick={() => run(SessionBackend.revokeSessionShare(share.token), () => { setShare(null); Setting.showMessage("success", i18next.t("accessRequest:Revoked")); })}>{i18next.t("accessRequest:Revoke")}</Button> : null}<Button disabled={!sessionId} onClick={() => run(SessionBackend.createSessionShare(sessionId!, ttl), setShare)}>{i18next.t("session:Generate link")}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
