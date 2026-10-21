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
import {CircleX, ClipboardPaste, Expand, Keyboard} from "lucide-react";
import {toast} from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {Button} from "@/components/ui/button";
import {Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {Textarea} from "@/components/ui/textarea";
import {Tooltip, TooltipContent, TooltipTrigger} from "@/components/ui/tooltip";
import {HotKeys} from "@/components/access/util";

/** One toast per view, replaced as the connection moves from state to state. */
export function useConnectionToast(assetId: string) {
  const id = React.useRef(`connection-${assetId}-${Math.random().toString(36).slice(2)}`).current;
  React.useEffect(() => () => {
    toast.dismiss(id);
  }, [id]);

  return React.useMemo(() => ({
    loading: (text: string) => toast.loading(text, {id, duration: Infinity}),
    success: (text: string) => toast.success(text, {id, duration: 3000}),
    error: (text: string) => toast.error(text, {id, duration: 3000}),
    dismiss: () => toast.dismiss(id),
  }), [id]);
}

function ToolbarButton({label, onClick, children}: {label: string; onClick?: () => void; children: React.ReactNode}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button variant="secondary" size="icon" className="h-8 w-8 shadow-md" aria-label={label} onClick={onClick}>
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent side="bottom">{label}</TooltipContent>
    </Tooltip>
  );
}

/** The buttons floating over a remote screen: fullscreen, clipboard and key combinations. */
export function ConnectionToolbar({
  onFullscreen,
  onClipboard,
  onHotKey,
}: {
  onFullscreen: () => void;
  onClipboard?: () => void;
  onHotKey?: (keys: number[]) => void;
}) {
  return (
    <div className="absolute right-4 top-4 z-10 flex gap-1.5 opacity-60 transition-opacity hover:opacity-100">
      {onHotKey ? (
        <DropdownMenu>
          <Tooltip>
            <TooltipTrigger asChild>
              <DropdownMenuTrigger asChild>
                <Button variant="secondary" size="icon" className="h-8 w-8 shadow-md" aria-label={i18next.t("asset:Key combinations")}>
                  <Keyboard />
                </Button>
              </DropdownMenuTrigger>
            </TooltipTrigger>
            <TooltipContent side="bottom">{i18next.t("asset:Key combinations")}</TooltipContent>
          </Tooltip>
          <DropdownMenuContent align="end">
            {HotKeys.map((hotKey) => (
              <DropdownMenuItem key={hotKey.label} onSelect={() => onHotKey(hotKey.keys)}>
                {hotKey.label}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      ) : null}
      {onClipboard ? (
        <ToolbarButton label={i18next.t("asset:Clipboard")} onClick={onClipboard}>
          <ClipboardPaste />
        </ToolbarButton>
      ) : null}
      <ToolbarButton label={i18next.t("asset:Fullscreen")} onClick={onFullscreen}>
        <Expand />
      </ToolbarButton>
    </div>
  );
}

/** Edits the text shared with the remote clipboard; OK sends it over. */
export function ClipboardDialog({
  open,
  text,
  onOk,
  onCancel,
}: {
  open: boolean;
  text: string;
  onOk: (text: string) => void;
  onCancel: () => void;
}) {
  const [draft, setDraft] = React.useState(text);
  React.useEffect(() => {
    if (open) {
      setDraft(text);
    }
  }, [open, text]);

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{i18next.t("asset:Clipboard")}</DialogTitle>
        </DialogHeader>
        <Textarea rows={10} value={draft} onChange={(e) => setDraft(e.target.value)} />
        <DialogFooter>
          <Button variant="outline" onClick={onCancel}>{i18next.t("general:Cancel")}</Button>
          <Button onClick={() => onOk(draft)}>{i18next.t("general:OK")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Shown when a connection fails: try again, or close the tab or window. */
export function FailureDialog({
  assetName,
  message,
  onReconnect,
  onClose,
}: {
  assetName: string;
  message: string | null;
  onReconnect: () => void;
  onClose: () => void;
}) {
  return (
    <AlertDialog open={message !== null}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle className="flex items-center gap-2">
            <CircleX className="h-5 w-5 text-destructive" />
            {i18next.t("asset:Failed to connect to")}: {assetName}
          </AlertDialogTitle>
          <AlertDialogDescription className="break-words">{message}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel className="text-destructive" onClick={onClose}>
            {i18next.t("asset:Close this page")}
          </AlertDialogCancel>
          <AlertDialogAction onClick={onReconnect}>{i18next.t("asset:Reconnect")}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
