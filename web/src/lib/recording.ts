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

// Session recordings, as written by recording/recording.go: the magic line,
// a JSON header line, then frames of a uint32 offset in milliseconds, a kind
// byte, a uint32 length and the data, all little-endian.

const Magic = "CASVREC1\n";

export const KindData = 1;
export const KindText = 2;
export const KindResize = 3;

export interface RecordingHeader {
  version: number;
  protocol: string;
  session: string;
  asset: string;
  user: string;
  startTime: string;
  cols?: number;
  rows?: number;
  width?: number;
  height?: number;
}

export interface RecordingFrame {
  time: number;
  kind: number;
  data: Uint8Array;
}

export interface Recording {
  header: RecordingHeader;
  frames: RecordingFrame[];
  duration: number;
}

export function parseRecording(buffer: ArrayBuffer): Recording {
  const bytes = new Uint8Array(buffer);
  const decoder = new TextDecoder();
  if (decoder.decode(bytes.subarray(0, Magic.length)) !== Magic) {
    throw new Error("not a Casvisor recording");
  }

  const lineEnd = bytes.indexOf(10, Magic.length);
  if (lineEnd < 0) {
    throw new Error("the recording has no header");
  }
  const header = JSON.parse(decoder.decode(bytes.subarray(Magic.length, lineEnd))) as RecordingHeader;

  const view = new DataView(buffer);
  const frames: RecordingFrame[] = [];
  let offset = lineEnd + 1;
  // a recording cut off by a crash ends with a partial frame, which is left out
  while (offset + 9 <= bytes.length) {
    const time = view.getUint32(offset, true);
    const kind = view.getUint8(offset + 4);
    const length = view.getUint32(offset + 5, true);
    if (offset + 9 + length > bytes.length) {
      break;
    }
    frames.push({time, kind, data: bytes.subarray(offset + 9, offset + 9 + length)});
    offset += 9 + length;
  }

  return {header, frames, duration: frames.length > 0 ? frames[frames.length - 1].time : 0};
}

/** The data of a frame in an ArrayBuffer of its own, as the screens take it. */
export function toArrayBuffer(data: Uint8Array): ArrayBuffer {
  return data.slice().buffer;
}

export function parseText(data: Uint8Array): any {
  try {
    return JSON.parse(new TextDecoder().decode(data));
  } catch {
    return null;
  }
}

export function formatDuration(ms: number) {
  const total = Math.max(0, Math.floor(ms / 1000));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = total % 60;
  const pad = (n: number) => String(n).padStart(2, "0");
  return hours > 0 ? `${hours}:${pad(minutes)}:${pad(seconds)}` : `${pad(minutes)}:${pad(seconds)}`;
}
