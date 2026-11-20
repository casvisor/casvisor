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

// The screen of a Casvisor Desktop session: the H.264 stream the device sends
// is decoded with WebCodecs and drawn on a canvas. The messages are 1 byte
// type, 4 bytes big endian length and the payload, see docs/protocol.md of
// casvisor/casvisor-desktop.

export const DesktopMessage = {
  VideoConfig: 16,
  Video: 17,
  Cursor: 18,
  Displays: 19,
  Mouse: 32,
  Key: 33,
  Control: 34,
  Clipboard: 48,
  Notice: 64,
} as const;

export const MouseAction = {Move: 0, Down: 1, Up: 2, Wheel: 3} as const;

export interface DesktopDisplay {
  index: number;
  name: string;
  left: number;
  top: number;
  width: number;
  height: number;
  primary: boolean;
}

export function encodeMessage(kind: number, payload: Uint8Array): Uint8Array {
  const msg = new Uint8Array(5 + payload.length);
  msg[0] = kind;
  new DataView(msg.buffer).setUint32(1, payload.length);
  msg.set(payload, 5);
  return msg;
}

export function encodeJson(kind: number, value: unknown): Uint8Array {
  return encodeMessage(kind, new TextEncoder().encode(JSON.stringify(value)));
}

export function encodeText(kind: number, text: string): Uint8Array {
  return encodeMessage(kind, new TextEncoder().encode(text));
}

export function encodeMouse(action: number, button: number, x: number, y: number, dx = 0, dy = 0): Uint8Array {
  const payload = new Uint8Array(10);
  const view = new DataView(payload.buffer);
  payload[0] = action;
  payload[1] = button;
  view.setUint16(2, Math.max(0, Math.min(65535, Math.round(x))));
  view.setUint16(4, Math.max(0, Math.min(65535, Math.round(y))));
  view.setInt16(6, dx);
  view.setInt16(8, dy);
  return encodeMessage(DesktopMessage.Mouse, payload);
}

export function encodeKey(scancode: number, down: boolean): Uint8Array {
  const payload = new Uint8Array(3);
  new DataView(payload.buffer).setUint16(0, scancode);
  payload[2] = down ? 1 : 0;
  return encodeMessage(DesktopMessage.Key, payload);
}

export function messagePayload(msg: Uint8Array): Uint8Array {
  return msg.subarray(5, 5 + new DataView(msg.buffer, msg.byteOffset, msg.byteLength).getUint32(1));
}

export function parseJson(msg: Uint8Array): any {
  try {
    return JSON.parse(new TextDecoder().decode(messagePayload(msg)));
  } catch {
    return null;
  }
}

export function isWebCodecsSupported() {
  return typeof window !== "undefined" && "VideoDecoder" in window;
}

/** Decodes and draws the video of a session. It draws the remote cursor too
 *  when asked, for watching and replaying, where there is no local mouse. */
export class DesktopScreen {
  private ctx: CanvasRenderingContext2D;
  private decoder: VideoDecoder | null = null;
  private codec = "";
  private waitingForKey = true;
  private frame: VideoFrame | null = null;
  private cursor = {x: 0, y: 0, visible: false};
  /** called when the video size changes */
  onSize?: (width: number, height: number) => void;
  /** called when the decoder lost its place and needs a key frame */
  onNeedKeyFrame?: () => void;

  constructor(private canvas: HTMLCanvasElement, private drawCursor = false) {
    this.ctx = canvas.getContext("2d")!;
  }

  /** Takes a message from the device and returns whether it was a screen message. */
  apply(msg: Uint8Array): boolean {
    switch (msg[0]) {
    case DesktopMessage.VideoConfig: {
      const config = parseJson(msg);
      if (config) {
        this.configure(config.codec, config.width, config.height);
      }
      return true;
    }
    case DesktopMessage.Video:
      this.decode(messagePayload(msg));
      return true;
    case DesktopMessage.Cursor: {
      const payload = messagePayload(msg);
      const view = new DataView(payload.buffer, payload.byteOffset, payload.byteLength);
      if (payload.length >= 9) {
        this.cursor = {x: view.getInt32(0), y: view.getInt32(4), visible: payload[8] !== 0};
        if (this.drawCursor) {
          this.draw();
        }
      }
      return true;
    }
    default:
      return false;
    }
  }

  private configure(codec: string, width: number, height: number) {
    if (this.canvas.width !== width || this.canvas.height !== height) {
      this.canvas.width = width;
      this.canvas.height = height;
      this.onSize?.(width, height);
    }
    if (this.decoder && this.decoder.state !== "closed" && this.codec === codec) {
      return;
    }
    this.closeDecoder();
    this.codec = codec;
    this.waitingForKey = true;
    this.decoder = new VideoDecoder({
      output: (frame) => {
        this.frame?.close();
        this.frame = frame;
        this.draw();
      },
      error: () => {
        this.closeDecoder();
        this.waitingForKey = true;
        this.onNeedKeyFrame?.();
      },
    });
    this.decoder.configure({codec, codedWidth: width, codedHeight: height, optimizeForLatency: true});
  }

  private decode(payload: Uint8Array) {
    if (payload.length < 9) {
      return;
    }
    const key = (payload[0] & 1) !== 0;
    if (!this.decoder || this.decoder.state !== "configured") {
      if (this.codec && key) {
        this.configure(this.codec, this.canvas.width, this.canvas.height);
      } else {
        return;
      }
    }
    if (this.waitingForKey && !key) {
      return;
    }
    this.waitingForKey = false;
    const timestamp = Number(new DataView(payload.buffer, payload.byteOffset + 1, 8).getBigUint64(0));
    try {
      this.decoder!.decode(new EncodedVideoChunk({type: key ? "key" : "delta", timestamp, data: payload.subarray(9)}));
    } catch {
      this.waitingForKey = true;
      this.onNeedKeyFrame?.();
    }
  }

  private draw() {
    if (this.frame) {
      this.ctx.drawImage(this.frame, 0, 0, this.canvas.width, this.canvas.height);
    }
    if (this.drawCursor && this.cursor.visible) {
      const {x, y} = this.cursor;
      const ctx = this.ctx;
      ctx.save();
      ctx.beginPath();
      ctx.moveTo(x, y);
      ctx.lineTo(x, y + 18);
      ctx.lineTo(x + 5, y + 14);
      ctx.lineTo(x + 9, y + 22);
      ctx.lineTo(x + 12, y + 21);
      ctx.lineTo(x + 8, y + 13);
      ctx.lineTo(x + 14, y + 13);
      ctx.closePath();
      ctx.fillStyle = "#fff";
      ctx.strokeStyle = "#000";
      ctx.lineWidth = 1;
      ctx.fill();
      ctx.stroke();
      ctx.restore();
    }
  }

  private closeDecoder() {
    if (this.decoder && this.decoder.state !== "closed") {
      this.decoder.close();
    }
    this.decoder = null;
  }

  reset() {
    this.closeDecoder();
    this.frame?.close();
    this.frame = null;
    this.codec = "";
    this.waitingForKey = true;
    this.ctx.clearRect(0, 0, this.canvas.width, this.canvas.height);
  }

  dispose() {
    this.reset();
  }
}
