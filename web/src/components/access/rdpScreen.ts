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

import {GfxRenderer} from "@/components/access/rdpGfx";

// Binary frames start with their kind: bitmap tiles or graphics pipeline
// commands (see controllers/tunnel_rdp_gfx.go).
const FrameGfx = 2;

export interface CursorMessage {
  hidden?: boolean;
  default?: boolean;
  png?: string;
  x?: number;
  y?: number;
  width?: number;
  height?: number;
}

/** Draws the frames the gateway sends for an RDP session on a canvas, for the
 *  live view and for replaying a recording alike. */
export class RdpScreen {
  private ctx: CanvasRenderingContext2D | null = null;
  private chain: Promise<void> = Promise.resolve();
  private gfx: GfxRenderer;

  constructor(private canvas: HTMLCanvasElement) {
    this.gfx = new GfxRenderer(() => this.ctx, (width, height) => this.setSize(width, height));
  }

  init(width: number, height: number) {
    this.canvas.width = width;
    this.canvas.height = height;
    this.ctx = this.canvas.getContext("2d");
  }

  setSize(width: number, height: number) {
    if (this.canvas.width !== width || this.canvas.height !== height) {
      this.canvas.width = width;
      this.canvas.height = height;
    }
  }

  /** Draws a frame after the ones before it; the promise settles once it is on the canvas. */
  frame(data: ArrayBuffer): Promise<void> {
    const kind = new DataView(data).getUint8(0);
    this.chain = this.chain
      .then(() => (kind === FrameGfx ? this.gfx.frame(data) : this.drawFrame(data)))
      .catch(() => undefined);
    return this.chain;
  }

  // A frame is every tile of one screen change. All of them are decoded first
  // and drawn together, in the order the frames arrived, so a repainted window
  // appears at once.
  private async drawFrame(data: ArrayBuffer) {
    const view = new DataView(data);
    const count = view.getUint16(1, true);
    const tiles: {x: number; y: number; image: Promise<ImageBitmap>}[] = [];
    let offset = 3;
    for (let i = 0; i < count && offset + 12 <= data.byteLength; i++) {
      const x = view.getUint16(offset, true);
      const y = view.getUint16(offset + 2, true);
      const length = view.getUint32(offset + 8, true);
      const blob = new Blob([new Uint8Array(data, offset + 12, length)], {type: "image/png"});
      tiles.push({x, y, image: createImageBitmap(blob)});
      offset += 12 + length;
    }
    const images = await Promise.allSettled(tiles.map((tile) => tile.image));
    images.forEach((result, i) => {
      if (result.status === "fulfilled") {
        this.ctx?.drawImage(result.value, tiles[i].x, tiles[i].y);
        result.value.close();
      }
    });
  }
}

/** Shows the remote cursor shape on the canvas. Its image is in remote pixels,
 *  so it is scaled to the size the desktop is displayed at. */
export function setCanvasCursor(canvas: HTMLCanvasElement, msg: CursorMessage) {
  if (msg.hidden) {
    canvas.style.cursor = "none";
    return;
  }
  if (msg.default || !msg.png || !msg.width || !msg.height) {
    canvas.style.cursor = "default";
    return;
  }
  const rect = canvas.getBoundingClientRect();
  const ratio = canvas.width ? Math.min(rect.width / canvas.width, rect.height / canvas.height) : 1;
  const width = Math.max(1, Math.round(msg.width * ratio));
  const height = Math.max(1, Math.round(msg.height * ratio));
  const hotX = Math.min(width - 1, Math.round((msg.x ?? 0) * ratio));
  const hotY = Math.min(height - 1, Math.round((msg.y ?? 0) * ratio));
  const image = new Image();
  image.onload = () => {
    const scaled = document.createElement("canvas");
    scaled.width = width;
    scaled.height = height;
    scaled.getContext("2d")?.drawImage(image, 0, 0, width, height);
    canvas.style.cursor = `url(${scaled.toDataURL()}) ${hotX} ${hotY}, default`;
  };
  image.src = `data:image/png;base64,${msg.png}`;
}
