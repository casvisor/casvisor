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

/** A copied file or directory; name is its path within the copied selection. */
export interface ClipboardFileInfo {
  name: string;
  size: number;
  dir?: boolean;
}

/** What the browser pastes into a remote desktop. */
export interface LocalClipboard {
  text: string;
  image?: Blob;
  files: {info: ClipboardFileInfo; file?: File}[];
}

// Binary messages that carry clipboard files rather than the screen.
export const FrameClipboardFile = 3;
export const FrameClipboardData = 4;

const uploadChunkSize = 256 * 1024;

export function base64ToBlob(base64: string, type: string): Blob {
  const bytes = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
  return new Blob([bytes], {type});
}

export function blobToBase64(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result).replace(/^data:[^,]*,/, ""));
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(blob);
  });
}

function readEntries(reader: FileSystemDirectoryReader): Promise<FileSystemEntry[]> {
  return new Promise((resolve, reject) => reader.readEntries(resolve, reject));
}

function entryFile(entry: FileSystemFileEntry): Promise<File> {
  return new Promise((resolve, reject) => entry.file(resolve, reject));
}

// Lists a dropped or pasted directory, the directory before what is inside.
async function walkEntry(entry: FileSystemEntry, prefix: string, out: LocalClipboard["files"]) {
  const name = prefix + entry.name;
  if (entry.isFile) {
    const file = await entryFile(entry as FileSystemFileEntry);
    out.push({info: {name, size: file.size}, file});
    return;
  }
  out.push({info: {name, size: 0, dir: true}});
  const reader = (entry as FileSystemDirectoryEntry).createReader();
  for (;;) {
    const entries = await readEntries(reader);
    if (entries.length === 0) {
      break;
    }
    for (const child of entries) {
      await walkEntry(child, name + "/", out);
    }
  }
}

/** Reads what a paste or drop carries: text, an image, and files with the
 *  directories they are in. A pasted screenshot comes as a file named
 *  image.png and is taken as an image. */
export async function readDataTransfer(data: DataTransfer): Promise<LocalClipboard> {
  const result: LocalClipboard = {text: data.getData("text/plain") ?? "", files: []};
  const entries: FileSystemEntry[] = [];
  const files: File[] = [];
  for (const item of Array.from(data.items)) {
    if (item.kind !== "file") {
      continue;
    }
    const entry = item.webkitGetAsEntry?.();
    if (entry) {
      entries.push(entry);
    } else {
      const file = item.getAsFile();
      if (file) {
        files.push(file);
      }
    }
  }

  if (entries.length === 0 && files.length === 1 && files[0].type.startsWith("image/") && files[0].name === "image.png") {
    result.image = files[0];
    return result;
  }
  for (const entry of entries) {
    await walkEntry(entry, "", result.files);
  }
  for (const file of files) {
    result.files.push({info: {name: file.name, size: file.size}, file});
  }
  return result;
}

/** Converts an image to PNG, the format the gateway takes. */
export async function toPng(image: Blob): Promise<Blob> {
  if (image.type === "image/png") {
    return image;
  }
  const bitmap = await createImageBitmap(image);
  const canvas = new OffscreenCanvas(bitmap.width, bitmap.height);
  canvas.getContext("2d")!.drawImage(bitmap, 0, 0);
  return canvas.convertToBlob({type: "image/png"});
}

/** Sends the bytes of the files being pasted after their list, waiting while
 *  the connection is busy so a large file does not fill the browser's memory. */
export async function sendFileData(ws: WebSocket, files: LocalClipboard["files"], onProgress: (sent: number) => void) {
  let sent = 0;
  for (const {file} of files) {
    if (!file) {
      continue;
    }
    for (let offset = 0; offset < file.size; offset += uploadChunkSize) {
      while (ws.bufferedAmount > 4 * uploadChunkSize) {
        if (ws.readyState !== WebSocket.OPEN) {
          throw new Error("closed");
        }
        await new Promise((resolve) => setTimeout(resolve, 20));
      }
      const chunk = new Uint8Array(await file.slice(offset, offset + uploadChunkSize).arrayBuffer());
      const frame = new Uint8Array(1 + chunk.length);
      frame[0] = FrameClipboardData;
      frame.set(chunk, 1);
      ws.send(frame);
      sent += chunk.length;
      onProgress(sent);
    }
  }
}

/** A file download from the remote clipboard, put together from its chunks. */
export class ClipboardDownload {
  chunks: Uint8Array[] = [];
  received = 0;
  constructor(public name: string, public size: number) {}

  add(data: ArrayBuffer) {
    const chunk = new Uint8Array(data, 5);
    this.chunks.push(chunk);
    this.received += chunk.length;
  }

  save() {
    const url = URL.createObjectURL(new Blob(this.chunks as BlobPart[]));
    const link = document.createElement("a");
    link.href = url;
    link.download = this.name;
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 60000);
  }
}

export function formatBytes(n: number): string {
  if (n < 1024) {
    return `${n} B`;
  }
  const units = ["KB", "MB", "GB"];
  let value = n / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${value.toFixed(value < 10 ? 1 : 0)} ${units[unit]}`;
}
