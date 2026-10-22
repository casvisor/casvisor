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

// Screens that draw recorded frames with the same code the live connections
// use: xterm.js for SSH and Telnet, noVNC for VNC and RdpScreen for RDP. They
// serve both the replay of a recording and live watching of a session.

import {Terminal} from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import RFB from "@novnc/novnc/core/rfb.js";
import {RdpScreen, type CursorMessage} from "@/components/access/rdpScreen";
import {KindData, KindResize, KindText, parseText, toArrayBuffer, type RecordingHeader} from "@/lib/recording";

export interface ReplayScreen {
  /** Draws a frame; a promise when it is drawn later, which the next frame waits for. */
  apply(kind: number, data: Uint8Array): void | Promise<void>;
  /** Clears the screen to before the first frame, to replay from the start. */
  reset(): void;
  dispose(): void;
}

export function createScreen(container: HTMLElement, header: RecordingHeader): ReplayScreen {
  switch (header.protocol) {
  case "VNC":
    return new VncScreen(container);
  case "RDP":
    return new RdpReplayScreen(container, header);
  default:
    return new TerminalScreen(container, header);
  }
}

class TerminalScreen implements ReplayScreen {
  private terminal: Terminal;

  constructor(container: HTMLElement, private header: RecordingHeader) {
    this.terminal = new Terminal({
      disableStdin: true,
      cursorBlink: false,
      fontSize: 14,
      fontFamily: "Menlo, Consolas, \"DejaVu Sans Mono\", monospace",
      theme: {background: "#1b1b1b"},
      cols: header.cols || 80,
      rows: header.rows || 24,
      scrollback: 5000,
    });
    this.terminal.open(container);
  }

  apply(kind: number, data: Uint8Array) {
    if (kind === KindData) {
      this.terminal.write(data);
    } else if (kind === KindResize) {
      const size = parseText(data);
      if (size?.cols > 0 && size?.rows > 0) {
        this.terminal.resize(size.cols, size.rows);
      }
    }
  }

  reset() {
    this.terminal.reset();
    this.terminal.resize(this.header.cols || 80, this.header.rows || 24);
  }

  dispose() {
    this.terminal.dispose();
  }
}

/** A WebSocket stand-in that noVNC reads the recorded RFB stream from; what
 *  noVNC sends back goes nowhere. */
class ReplayChannel {
  binaryType = "arraybuffer";
  protocol = "";
  readyState = "open";
  onmessage: ((event: {data: ArrayBuffer}) => void) | null = null;
  onopen: (() => void) | null = null;
  onclose: ((event: any) => void) | null = null;
  onerror: ((event: any) => void) | null = null;

  send() {}

  close() {
    this.readyState = "closed";
  }
}

class VncScreen implements ReplayScreen {
  private rfb: any = null;
  private channel: ReplayChannel | null = null;

  constructor(private container: HTMLElement) {
    this.start();
  }

  private start() {
    this.channel = new ReplayChannel();
    this.rfb = new RFB(this.container, this.channel);
    this.rfb.viewOnly = true;
    this.rfb.scaleViewport = true;
    this.rfb.background = "#1b1b1b";
  }

  apply(kind: number, data: Uint8Array) {
    if (kind === KindData) {
      this.channel?.onmessage?.({data: toArrayBuffer(data)});
    }
  }

  reset() {
    this.dispose();
    this.start();
  }

  dispose() {
    try {
      this.rfb?.disconnect();
    } catch {
      // the replayed stream may have ended in any state
    }
    this.rfb = null;
    this.channel = null;
    this.container.replaceChildren();
  }
}

/** The RDP desktop, with the user's mouse pointer drawn on a layer above it,
 *  since the pointer is drawn by the browser and only its position is recorded. */
class RdpReplayScreen implements ReplayScreen {
  private canvas!: HTMLCanvasElement;
  private pointerCanvas!: HTMLCanvasElement;
  private screen!: RdpScreen;
  private cursor: {image: HTMLImageElement | null; x: number; y: number; hidden: boolean} = {image: null, x: 0, y: 0, hidden: false};
  private pointer: {x: number; y: number} | null = null;

  constructor(private container: HTMLElement, private header: RecordingHeader) {
    this.start();
  }

  private start() {
    const wrapper = document.createElement("div");
    wrapper.className = "relative h-full w-full";
    this.canvas = document.createElement("canvas");
    this.canvas.className = "absolute inset-0 h-full w-full object-contain";
    this.pointerCanvas = document.createElement("canvas");
    this.pointerCanvas.className = "pointer-events-none absolute inset-0 h-full w-full object-contain";
    wrapper.append(this.canvas, this.pointerCanvas);
    this.container.replaceChildren(wrapper);

    this.screen = new RdpScreen(this.canvas);
    this.setSize(this.header.width || 1024, this.header.height || 768);
    this.screen.init(this.canvas.width, this.canvas.height);
    this.cursor = {image: null, x: 0, y: 0, hidden: false};
    this.pointer = null;
  }

  private setSize(width: number, height: number) {
    this.screen.setSize(width, height);
    if (this.pointerCanvas.width !== width || this.pointerCanvas.height !== height) {
      this.pointerCanvas.width = width;
      this.pointerCanvas.height = height;
      this.drawPointer();
    }
  }

  apply(kind: number, data: Uint8Array) {
    if (kind === KindData) {
      return this.screen.frame(toArrayBuffer(data));
    }
    if (kind !== KindText) {
      return;
    }

    const msg = parseText(data);
    if (msg?.t === "init") {
      this.screen.init(msg.width, msg.height);
      this.setSize(msg.width, msg.height);
    } else if (msg?.t === "size") {
      this.setSize(msg.width, msg.height);
    } else if (msg?.t === "cursor") {
      this.setCursor(msg);
    } else if (msg?.t === "pointer") {
      this.pointer = {x: msg.x ?? 0, y: msg.y ?? 0};
      this.drawPointer();
    }
  }

  private setCursor(msg: CursorMessage) {
    if (msg.hidden) {
      this.cursor = {image: null, x: 0, y: 0, hidden: true};
      this.drawPointer();
      return;
    }
    if (msg.default || !msg.png) {
      this.cursor = {image: null, x: 0, y: 0, hidden: false};
      this.drawPointer();
      return;
    }
    const image = new Image();
    const cursor = {image, x: msg.x ?? 0, y: msg.y ?? 0, hidden: false};
    this.cursor = cursor;
    image.onload = () => {
      if (this.cursor === cursor) {
        this.drawPointer();
      }
    };
    image.src = `data:image/png;base64,${msg.png}`;
  }

  private drawPointer() {
    const ctx = this.pointerCanvas.getContext("2d");
    if (!ctx) {
      return;
    }
    ctx.clearRect(0, 0, this.pointerCanvas.width, this.pointerCanvas.height);
    if (!this.pointer || this.cursor.hidden) {
      return;
    }

    const {x, y} = this.pointer;
    const image = this.cursor.image;
    if (image && image.complete && image.naturalWidth > 0) {
      ctx.drawImage(image, x - this.cursor.x, y - this.cursor.y);
      return;
    }

    // the default arrow, scaled to the desktop like the cursors Windows sends
    const scale = Math.max(1, this.pointerCanvas.width / 1600);
    ctx.save();
    ctx.translate(x, y);
    ctx.scale(scale, scale);
    ctx.beginPath();
    ctx.moveTo(0, 0);
    ctx.lineTo(0, 17);
    ctx.lineTo(4.5, 13);
    ctx.lineTo(7.5, 19.5);
    ctx.lineTo(10, 18.5);
    ctx.lineTo(7, 12);
    ctx.lineTo(12.5, 12);
    ctx.closePath();
    ctx.fillStyle = "#fff";
    ctx.strokeStyle = "#000";
    ctx.lineWidth = 1;
    ctx.fill();
    ctx.stroke();
    ctx.restore();
  }

  reset() {
    this.start();
  }

  dispose() {
    this.container.replaceChildren();
  }
}
