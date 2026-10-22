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
import {ArrowUp, Download, File, Folder, FolderPlus, House, Pencil, RefreshCw, Trash2, Upload, X} from "lucide-react";
import * as SessionFileBackend from "@/backend/SessionFileBackend";
import type {SessionFile} from "@/backend/types";
import {ConfirmButton} from "@/components/common/ConfirmButton";
import {Button} from "@/components/ui/button";
import {Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle} from "@/components/ui/dialog";
import {Input} from "@/components/ui/input";
import {Tooltip, TooltipContent, TooltipTrigger} from "@/components/ui/tooltip";
import * as Setting from "@/lib/setting";
import {cn} from "@/lib/utils";

function joinPath(dir: string, name: string) {
  return dir === "/" ? `/${name}` : `${dir}/${name}`;
}

function getParentPath(path: string) {
  const i = path.lastIndexOf("/");
  return i <= 0 ? "/" : path.slice(0, i);
}

function formatSize(size: number) {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = size;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return unit === 0 ? `${value} B` : `${value.toFixed(1)} ${units[unit]}`;
}

function IconButton({label, onClick, disabled, children}: {label: string; onClick: () => void; disabled?: boolean; children: React.ReactNode}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button size="icon" variant="ghost" className="h-7 w-7" aria-label={label} disabled={disabled} onClick={onClick}>
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

/** Asks for a name: a new folder, or the new name of a file. */
function NameDialog({title, value, onOk, onCancel}: {title: string; value: string | null; onOk: (name: string) => void; onCancel: () => void}) {
  const [draft, setDraft] = React.useState("");
  React.useEffect(() => {
    if (value !== null) {
      setDraft(value);
    }
  }, [value]);

  const valid = draft.trim() !== "" && !draft.includes("/");
  return (
    <Dialog open={value !== null} onOpenChange={(next) => !next && onCancel()}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (valid) {
              onOk(draft.trim());
            }
          }}
        >
          <Input value={draft} autoFocus onChange={(e) => setDraft(e.target.value)} />
          <DialogFooter className="mt-4">
            <Button type="button" variant="outline" onClick={onCancel}>{i18next.t("general:Cancel")}</Button>
            <Button type="submit" disabled={!valid}>{i18next.t("general:OK")}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/**
 * The files of an SSH session's asset, docked beside the terminal, over the
 * session's own SSH connection; in an RDP session, the gateway directory the
 * remote desktop sees as \\tsclient\casvisor. Held to the Upload and Download
 * permissions.
 */
export function FilePanel({sessionId, operations, onClose}: {sessionId: string; operations: string[]; onClose: () => void}) {
  const canUpload = operations.includes("upload");
  const canDownload = operations.includes("download");

  const [path, setPath] = React.useState("");
  const [pathDraft, setPathDraft] = React.useState("");
  const [home, setHome] = React.useState("");
  const [files, setFiles] = React.useState<SessionFile[]>([]);
  const [loading, setLoading] = React.useState(false);
  const [uploading, setUploading] = React.useState(false);
  const [dragging, setDragging] = React.useState(false);
  const [newFolder, setNewFolder] = React.useState<string | null>(null);
  const [renaming, setRenaming] = React.useState<SessionFile | null>(null);
  const uploadRef = React.useRef<HTMLInputElement>(null);

  const load = React.useCallback((dir: string) => {
    setLoading(true);
    SessionFileBackend.getSessionFiles(sessionId, dir).then((res) => {
      setLoading(false);
      if (res.status !== "ok") {
        Setting.showMessage("error", res.msg);
        return;
      }
      setPath(res.data.path);
      setPathDraft(res.data.path);
      setFiles(res.data.files ?? []);
      if (dir === "") {
        setHome(res.data.path);
      }
    });
  }, [sessionId]);

  React.useEffect(() => {
    load("");
  }, [load]);

  const handle = (promise: Promise<any>) => promise.then((res) => {
    if (res.status === "ok") {
      load(path);
    } else {
      Setting.showMessage("error", res.msg);
    }
  });

  const upload = (selected: File[]) => {
    if (!canUpload || selected.length === 0) {
      return;
    }
    setUploading(true);
    SessionFileBackend.uploadSessionFiles(sessionId, path, selected).then((res) => {
      setUploading(false);
      if (res.status === "ok") {
        Setting.showMessage("success", i18next.t("asset:Uploaded successfully"));
        load(path);
      } else {
        Setting.showMessage("error", res.msg);
      }
    });
  };

  const download = (file: SessionFile) => {
    const link = document.createElement("a");
    link.href = SessionFileBackend.getDownloadUrl(sessionId, joinPath(path, file.name));
    link.download = file.name;
    document.body.appendChild(link);
    link.click();
    link.remove();
  };

  return (
    <div
      className={cn("flex h-full w-[380px] shrink-0 flex-col border-l bg-background text-foreground", dragging && "ring-2 ring-inset ring-primary")}
      onDragOver={(e) => {
        if (canUpload) {
          e.preventDefault();
          setDragging(true);
        }
      }}
      onDragLeave={() => setDragging(false)}
      onDrop={(e) => {
        e.preventDefault();
        setDragging(false);
        upload(Array.from(e.dataTransfer.files));
      }}
    >
      <div className="flex items-center gap-1 border-b px-2 py-1.5">
        <span className="flex-1 px-1 text-sm font-medium">{i18next.t("asset:Files")}</span>
        <IconButton label={i18next.t("asset:Parent folder")} disabled={path === "/" || path === ""} onClick={() => load(getParentPath(path))}>
          <ArrowUp />
        </IconButton>
        <IconButton label={i18next.t("asset:Home folder")} disabled={!home} onClick={() => load(home)}>
          <House />
        </IconButton>
        <IconButton label={i18next.t("general:Refresh")} onClick={() => load(path)}>
          <RefreshCw className={cn(loading && "animate-spin")} />
        </IconButton>
        {canUpload ? (
          <>
            <IconButton label={i18next.t("asset:New folder")} onClick={() => setNewFolder("")}>
              <FolderPlus />
            </IconButton>
            <IconButton label={i18next.t("asset:Upload")} disabled={uploading} onClick={() => uploadRef.current?.click()}>
              <Upload className={cn(uploading && "animate-pulse")} />
            </IconButton>
          </>
        ) : null}
        <IconButton label={i18next.t("general:Close")} onClick={onClose}>
          <X />
        </IconButton>
        <input
          ref={uploadRef}
          type="file"
          multiple
          className="hidden"
          onChange={(e) => {
            upload(Array.from(e.target.files ?? []));
            e.target.value = "";
          }}
        />
      </div>

      <form
        className="border-b p-2"
        onSubmit={(e) => {
          e.preventDefault();
          if (pathDraft.startsWith("/")) {
            load(pathDraft);
          }
        }}
      >
        <Input className="h-8 font-mono text-xs" value={pathDraft} onChange={(e) => setPathDraft(e.target.value)} />
      </form>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {files.length === 0 && !loading ? (
          <div className="p-6 text-center text-sm text-muted-foreground">{i18next.t("asset:Empty folder")}</div>
        ) : null}
        {files.map((file) => (
          <div key={file.name} className="group flex items-center gap-2 px-3 py-1.5 text-sm hover:bg-muted">
            {file.isDir ? <Folder className="h-4 w-4 shrink-0 text-sky-600" /> : <File className="h-4 w-4 shrink-0 text-muted-foreground" />}
            <button
              type="button"
              className={cn("min-w-0 flex-1 truncate text-left", file.isDir && "font-medium", file.isLink && "italic")}
              title={`${file.name}\n${file.mode}  ${Setting.getFormattedDate(file.modifiedTime)}`}
              onDoubleClick={() => !file.isDir && canDownload && download(file)}
              onClick={() => file.isDir && load(joinPath(path, file.name))}
            >
              {file.name}
            </button>
            <span className="shrink-0 text-xs text-muted-foreground group-hover:hidden">{file.isDir ? "" : formatSize(file.size)}</span>
            <div className="hidden shrink-0 items-center group-hover:flex">
              {canDownload && !file.isDir ? (
                <IconButton label={i18next.t("asset:Download")} onClick={() => download(file)}>
                  <Download />
                </IconButton>
              ) : null}
              {canUpload ? (
                <>
                  <IconButton label={i18next.t("asset:Rename")} onClick={() => setRenaming(file)}>
                    <Pencil />
                  </IconButton>
                  <ConfirmButton
                    size="icon"
                    variant="ghost"
                    className="h-7 w-7 text-destructive"
                    aria-label={i18next.t("general:Delete")}
                    description={joinPath(path, file.name)}
                    onConfirm={() => handle(SessionFileBackend.deleteSessionFile(sessionId, joinPath(path, file.name)))}
                  >
                    <Trash2 />
                  </ConfirmButton>
                </>
              ) : null}
            </div>
          </div>
        ))}
      </div>

      {canUpload ? (
        <div className="border-t px-3 py-1.5 text-xs text-muted-foreground">{i18next.t("asset:Drop files here to upload")}</div>
      ) : null}

      <NameDialog
        title={i18next.t("asset:New folder")}
        value={newFolder}
        onCancel={() => setNewFolder(null)}
        onOk={(name) => {
          setNewFolder(null);
          handle(SessionFileBackend.createSessionDir(sessionId, joinPath(path, name)));
        }}
      />
      <NameDialog
        title={i18next.t("asset:Rename")}
        value={renaming ? renaming.name : null}
        onCancel={() => setRenaming(null)}
        onOk={(name) => {
          const file = renaming!;
          setRenaming(null);
          handle(SessionFileBackend.renameSessionFile(sessionId, joinPath(path, file.name), joinPath(path, name)));
        }}
      />
    </div>
  );
}
