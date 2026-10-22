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

package controllers

import (
	"bytes"
	"encoding/binary"
	"image"
	"runtime"
	"sync"
)

// Binary frames sent to the browser start with a kind byte.
const (
	rdpFrameBitmap = 1 // bitmap updates drawn straight onto the desktop
	rdpFrameGFX    = 2 // graphics pipeline commands
)

// Graphics pipeline commands as the browser's RdpView executes them. All
// numbers are little-endian uint16 unless noted; rectangles are x, y, width,
// height.
const (
	gfxResetGraphics = iota + 1
	gfxCreateSurface
	gfxDeleteSurface
	gfxMapSurface
	gfxSolidFill
	gfxSurfaceToSurface
	gfxSurfaceToCache
	gfxCacheToSurface
	gfxEvictCache
	gfxBitmap
	gfxStoreGlyph
	gfxDrawGlyph
)

// rdpGfxWriter turns graphics pipeline commands into one binary frame per
// server frame. The server copies pixels between surfaces and caches, so the
// browser keeps them and the gateway only decodes the codecs.
type rdpGfxWriter struct {
	send    func([]byte) error
	cmds    bytes.Buffer
	bitmaps []gfxPendingBitmap
	inFrame bool
	err     error
}

type gfxPendingBitmap struct {
	at   int // offset in cmds where the PNG length and data go
	rect image.Rectangle
	rgba []byte
}

func (w *rdpGfxWriter) u8(v byte) { w.cmds.WriteByte(v) }
func (w *rdpGfxWriter) u16(v int) { _ = binary.Write(&w.cmds, binary.LittleEndian, uint16(v)) }
func (w *rdpGfxWriter) rect(r image.Rectangle) {
	w.u16(r.Min.X)
	w.u16(r.Min.Y)
	w.u16(r.Dx())
	w.u16(r.Dy())
}

func (w *rdpGfxWriter) points(points []image.Point) {
	w.u16(len(points))
	for _, p := range points {
		w.u16(p.X)
		w.u16(p.Y)
	}
}

func (w *rdpGfxWriter) ResetGraphics(width, height int) {
	w.u8(gfxResetGraphics)
	w.u16(width)
	w.u16(height)
}

func (w *rdpGfxWriter) CreateSurface(id uint16, width, height int) {
	w.u8(gfxCreateSurface)
	w.u16(int(id))
	w.u16(width)
	w.u16(height)
}

func (w *rdpGfxWriter) DeleteSurface(id uint16) {
	w.u8(gfxDeleteSurface)
	w.u16(int(id))
}

func (w *rdpGfxWriter) MapSurfaceToOutput(id uint16, x, y int) {
	w.u8(gfxMapSurface)
	w.u16(int(id))
	w.u16(x)
	w.u16(y)
}

func (w *rdpGfxWriter) StartFrame(frameID uint32) {
	w.inFrame = true
}

func (w *rdpGfxWriter) EndFrame(frameID uint32) {
	w.inFrame = false
	w.flush()
}

func (w *rdpGfxWriter) SolidFill(id uint16, rgba [4]byte, rects []image.Rectangle) {
	w.u8(gfxSolidFill)
	w.u16(int(id))
	w.cmds.Write(rgba[:3])
	w.u16(len(rects))
	for _, r := range rects {
		w.rect(r)
	}
}

func (w *rdpGfxWriter) SurfaceToSurface(src, dst uint16, rect image.Rectangle, points []image.Point) {
	w.u8(gfxSurfaceToSurface)
	w.u16(int(src))
	w.u16(int(dst))
	w.rect(rect)
	w.points(points)
}

func (w *rdpGfxWriter) SurfaceToCache(id uint16, rect image.Rectangle, slot uint16) {
	w.u8(gfxSurfaceToCache)
	w.u16(int(id))
	w.rect(rect)
	w.u16(int(slot))
}

func (w *rdpGfxWriter) CacheToSurface(slot uint16, id uint16, points []image.Point) {
	w.u8(gfxCacheToSurface)
	w.u16(int(slot))
	w.u16(int(id))
	w.points(points)
}

func (w *rdpGfxWriter) EvictCache(slot uint16) {
	w.u8(gfxEvictCache)
	w.u16(int(slot))
}

func (w *rdpGfxWriter) Bitmap(id uint16, rect image.Rectangle, rgba []byte) {
	w.u8(gfxBitmap)
	w.u16(int(id))
	w.rect(rect)
	w.bitmaps = append(w.bitmaps, gfxPendingBitmap{at: w.cmds.Len(), rect: rect, rgba: rgba})
}

func (w *rdpGfxWriter) StoreGlyph(index uint16, id uint16, rect image.Rectangle) {
	w.u8(gfxStoreGlyph)
	w.u16(int(index))
	w.u16(int(id))
	w.rect(rect)
}

func (w *rdpGfxWriter) DrawGlyph(index uint16, id uint16, rect image.Rectangle) {
	w.u8(gfxDrawGlyph)
	w.u16(int(index))
	w.u16(int(id))
	w.rect(rect)
}

// flush encodes the frame's bitmaps as PNGs in parallel and sends the frame.
// Commands outside a frame (a cache eviction, say) go with the next one.
func (w *rdpGfxWriter) flush() {
	if w.cmds.Len() == 0 || w.inFrame || w.err != nil {
		return
	}
	encoded := make([][]byte, len(w.bitmaps))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	for i, b := range w.bitmaps {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, b gfxPendingBitmap) {
			defer func() { <-sem; wg.Done() }()
			img := &image.NRGBA{Pix: b.rgba, Stride: b.rect.Dx() * 4, Rect: image.Rect(0, 0, b.rect.Dx(), b.rect.Dy())}
			var buf bytes.Buffer
			if tileEncoder.Encode(&buf, img) == nil {
				encoded[i] = buf.Bytes()
			}
		}(i, b)
	}
	wg.Wait()

	cmds := w.cmds.Bytes()
	var out bytes.Buffer
	out.WriteByte(rdpFrameGFX)
	last := 0
	var n [4]byte
	for i, b := range w.bitmaps {
		out.Write(cmds[last:b.at])
		binary.LittleEndian.PutUint32(n[:], uint32(len(encoded[i])))
		out.Write(n[:])
		out.Write(encoded[i])
		last = b.at
	}
	out.Write(cmds[last:])
	w.cmds.Reset()
	w.bitmaps = w.bitmaps[:0]
	w.err = w.send(out.Bytes())
}
