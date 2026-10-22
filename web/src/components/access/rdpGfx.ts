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

// Executes the RDP graphics pipeline (MS-RDPEGFX) commands that the gateway
// sends per server frame. Windows draws into surfaces and copies pixels between
// surfaces, cache slots and the ClearCodec glyph cache, so they all live here;
// the gateway has already decoded the codecs into PNG bitmaps. The commands
// mirror controllers/tunnel_rdp_gfx.go.

const ResetGraphics = 1;
const CreateSurface = 2;
const DeleteSurface = 3;
const MapSurface = 4;
const SolidFill = 5;
const SurfaceToSurface = 6;
const SurfaceToCache = 7;
const CacheToSurface = 8;
const EvictCache = 9;
const Bitmap = 10;
const StoreGlyph = 11;
const DrawGlyph = 12;

type Canvas = HTMLCanvasElement | OffscreenCanvas;
type Ctx = CanvasRenderingContext2D | OffscreenCanvasRenderingContext2D;

interface Surface {
  canvas: Canvas;
  ctx: Ctx;
  mapped: boolean;
  x: number;
  y: number;
  dirty: Rect | null;
}

interface Rect {x: number; y: number; w: number; h: number}

function newCanvas(w: number, h: number): Canvas {
  if (typeof OffscreenCanvas !== "undefined") {
    return new OffscreenCanvas(Math.max(1, w), Math.max(1, h));
  }
  const canvas = document.createElement("canvas");
  canvas.width = Math.max(1, w);
  canvas.height = Math.max(1, h);
  return canvas;
}

function context(canvas: Canvas): Ctx {
  return canvas.getContext("2d") as Ctx;
}

// clear fills a new or reset surface with white.
function clear(ctx: Ctx) {
  ctx.fillStyle = "#fff";
  ctx.fillRect(0, 0, ctx.canvas.width, ctx.canvas.height);
}

function union(a: Rect | null, b: Rect): Rect {
  if (!a) {
    return {...b};
  }
  const x = Math.min(a.x, b.x);
  const y = Math.min(a.y, b.y);
  return {x, y, w: Math.max(a.x + a.w, b.x + b.w) - x, h: Math.max(a.y + a.h, b.y + b.h) - y};
}

// copyRect copies part of a canvas, through a scratch canvas when the source
// and destination overlap on the same canvas (a scrolled window).
function copyRect(src: Canvas, r: Rect, dst: Ctx, x: number, y: number) {
  if (r.w <= 0 || r.h <= 0) {
    return;
  }
  if (dst.canvas === src) {
    const tmp = newCanvas(r.w, r.h);
    context(tmp).drawImage(src, r.x, r.y, r.w, r.h, 0, 0, r.w, r.h);
    dst.drawImage(tmp, x, y);
    return;
  }
  dst.drawImage(src, r.x, r.y, r.w, r.h, x, y, r.w, r.h);
}

export class GfxRenderer {
  private surfaces = new Map<number, Surface>();
  private slots = new Map<number, Canvas>();
  private glyphs = new Map<number, Canvas>();

  // output is the visible canvas; resize is called when the desktop size
  // changes, before anything is drawn at the new size.
  constructor(private output: () => CanvasRenderingContext2D | null, private resize: (w: number, h: number) => void) {}

  // frame executes one frame: the bitmaps are decoded first, then every
  // command runs at once and the changed parts reach the screen together.
  async frame(data: ArrayBuffer) {
    const view = new DataView(data);
    const bytes = new Uint8Array(data);
    const pending: Promise<ImageBitmap>[] = [];
    // First pass: find the bitmaps and start decoding them.
    this.walk(view, (cmd, at) => {
      if (cmd === Bitmap) {
        const length = view.getUint32(at + 10, true);
        pending.push(createImageBitmap(new Blob([bytes.subarray(at + 14, at + 14 + length)], {type: "image/png"})));
      }
    });
    const images = await Promise.allSettled(pending);
    let next = 0;
    this.walk(view, (cmd, at) => this.run(cmd, at, view, () => {
      const result = images[next++];
      return result.status === "fulfilled" ? result.value : null;
    }));
    this.present();
  }

  // walk calls fn with each command and the offset of its fields, stopping at
  // anything malformed.
  private walk(view: DataView, fn: (cmd: number, at: number) => void) {
    let p = 1;
    const u16 = (o: number) => view.getUint16(o, true);
    while (p < view.byteLength) {
      const cmd = view.getUint8(p);
      const at = p + 1;
      let size = 0;
      switch (cmd) {
      case ResetGraphics: size = 4; break;
      case CreateSurface: size = 6; break;
      case DeleteSurface: size = 2; break;
      case MapSurface: size = 6; break;
      case SolidFill: size = 7 + u16(at + 5) * 8; break;
      case SurfaceToSurface: size = 14 + u16(at + 12) * 4; break;
      case SurfaceToCache: size = 12; break;
      case CacheToSurface: size = 6 + u16(at + 4) * 4; break;
      case EvictCache: size = 2; break;
      case Bitmap: size = 14 + view.getUint32(at + 10, true); break;
      case StoreGlyph: size = 12; break;
      case DrawGlyph: size = 12; break;
      default: return;
      }
      if (at + size > view.byteLength) {
        return;
      }
      fn(cmd, at);
      p = at + size;
    }
  }

  private touch(s: Surface, r: Rect) {
    s.dirty = union(s.dirty, r);
  }

  private run(cmd: number, at: number, view: DataView, nextImage: () => ImageBitmap | null) {
    const u16 = (o: number) => view.getUint16(o, true);
    const rect = (o: number): Rect => ({x: u16(o), y: u16(o + 2), w: u16(o + 4), h: u16(o + 6)});
    switch (cmd) {
    case ResetGraphics: {
      // Surfaces stay but are cleared, as the Windows client does.
      this.resize(u16(at), u16(at + 2));
      for (const s of this.surfaces.values()) {
        clear(s.ctx);
        this.touch(s, {x: 0, y: 0, w: s.canvas.width, h: s.canvas.height});
      }
      break;
    }
    case CreateSurface: {
      const canvas = newCanvas(u16(at + 2), u16(at + 4));
      const ctx = context(canvas);
      clear(ctx);
      this.surfaces.set(u16(at), {canvas, ctx, mapped: false, x: 0, y: 0, dirty: null});
      break;
    }
    case DeleteSurface:
      this.surfaces.delete(u16(at));
      break;
    case MapSurface: {
      const s = this.surfaces.get(u16(at));
      if (s) {
        s.mapped = true;
        s.x = u16(at + 2);
        s.y = u16(at + 4);
        this.touch(s, {x: 0, y: 0, w: s.canvas.width, h: s.canvas.height});
      }
      break;
    }
    case SolidFill: {
      const s = this.surfaces.get(u16(at));
      if (!s) {
        break;
      }
      s.ctx.fillStyle = `rgb(${view.getUint8(at + 2)},${view.getUint8(at + 3)},${view.getUint8(at + 4)})`;
      const n = u16(at + 5);
      for (let i = 0; i < n; i++) {
        const r = rect(at + 7 + i * 8);
        s.ctx.fillRect(r.x, r.y, r.w, r.h);
        this.touch(s, r);
      }
      break;
    }
    case SurfaceToSurface: {
      const src = this.surfaces.get(u16(at));
      const dst = this.surfaces.get(u16(at + 2));
      if (!src || !dst) {
        break;
      }
      const r = rect(at + 4);
      const n = u16(at + 12);
      for (let i = 0; i < n; i++) {
        const x = u16(at + 14 + i * 4);
        const y = u16(at + 16 + i * 4);
        copyRect(src.canvas, r, dst.ctx, x, y);
        this.touch(dst, {x, y, w: r.w, h: r.h});
      }
      break;
    }
    case SurfaceToCache: {
      const s = this.surfaces.get(u16(at));
      const r = rect(at + 2);
      if (s && r.w > 0 && r.h > 0) {
        const slot = newCanvas(r.w, r.h);
        context(slot).drawImage(s.canvas, r.x, r.y, r.w, r.h, 0, 0, r.w, r.h);
        this.slots.set(u16(at + 10), slot);
      }
      break;
    }
    case CacheToSurface: {
      const slot = this.slots.get(u16(at));
      const s = this.surfaces.get(u16(at + 2));
      if (!slot || !s) {
        break;
      }
      const n = u16(at + 4);
      for (let i = 0; i < n; i++) {
        const x = u16(at + 6 + i * 4);
        const y = u16(at + 8 + i * 4);
        s.ctx.drawImage(slot, x, y);
        this.touch(s, {x, y, w: slot.width, h: slot.height});
      }
      break;
    }
    case EvictCache:
      this.slots.delete(u16(at));
      break;
    case Bitmap: {
      const image = nextImage();
      const s = this.surfaces.get(u16(at));
      if (image && s) {
        const r = rect(at + 2);
        // Transparent pixels are not part of the bitmap and keep what is
        // on the surface.
        s.ctx.drawImage(image, r.x, r.y);
        image.close();
        this.touch(s, r);
      }
      break;
    }
    case StoreGlyph: {
      const s = this.surfaces.get(u16(at + 2));
      const r = rect(at + 4);
      if (s && r.w > 0 && r.h > 0) {
        const glyph = newCanvas(r.w, r.h);
        context(glyph).drawImage(s.canvas, r.x, r.y, r.w, r.h, 0, 0, r.w, r.h);
        this.glyphs.set(u16(at), glyph);
      }
      break;
    }
    case DrawGlyph: {
      const glyph = this.glyphs.get(u16(at));
      const s = this.surfaces.get(u16(at + 2));
      const r = rect(at + 4);
      if (glyph && s) {
        s.ctx.drawImage(glyph, 0, 0, r.w, r.h, r.x, r.y, r.w, r.h);
        this.touch(s, r);
      }
      break;
    }
    }
  }

  // present copies what changed on the mapped surfaces to the screen.
  private present() {
    const out = this.output();
    if (!out) {
      return;
    }
    for (const s of this.surfaces.values()) {
      if (!s.mapped || !s.dirty) {
        continue;
      }
      const r = s.dirty;
      s.dirty = null;
      const x = Math.max(0, r.x);
      const y = Math.max(0, r.y);
      const w = Math.min(s.canvas.width, r.x + r.w) - x;
      const h = Math.min(s.canvas.height, r.y + r.h) - y;
      if (w > 0 && h > 0) {
        out.drawImage(s.canvas, x, y, w, h, s.x + x, s.y + y, w, h);
      }
    }
  }
}
