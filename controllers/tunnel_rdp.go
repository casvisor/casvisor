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
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/object"
	gordp "github.com/casvisor/gordp"
	"github.com/casvisor/gordp/bitmap"
	"github.com/casvisor/gordp/rdpdr"
	"github.com/gorilla/websocket"
)

// GetAssetRdp connects to an RDP asset with the native gordp client,
// decodes the screen on the gateway and streams each frame of changed
// rectangles to the browser as PNG tiles, while relaying the browser's mouse
// and keyboard input.
func (c *ApiController) GetAssetRdp() {
	c.EnableRender = false
	ws, err := nativeUpgrader.Upgrade(c.Ctx.ResponseWriter, c.Ctx.Request, nil)
	if err != nil {
		c.ResponseError("WebSocket upgrade failed:", err)
		return
	}
	stream := newWsStream(ws)

	fail := func(code int, err error) {
		_ = stream.WriteJSON(rdpMessage{Type: "error", Message: err.Error()})
		stream.Close(code, err.Error())
	}

	sessionId := c.Input().Get("sessionId")
	width, err := strconv.Atoi(c.Input().Get("width"))
	if err != nil || width <= 0 {
		width = 1024
	}
	height, err := strconv.Atoi(c.Input().Get("height"))
	if err != nil || height <= 0 {
		height = 768
	}
	// The browser asks for its physical pixel size and its display scaling,
	// so the remote desktop is drawn 1:1 on high-DPI screens.
	scale, _ := strconv.Atoi(c.Input().Get("scale"))

	session, asset, code, err := c.getTunnelSessionAndAsset(sessionId, c.Input().Get("username"), c.Input().Get("password"))
	if err != nil {
		fail(code, err)
		return
	}

	addr := net.JoinHostPort(asset.Host, strconv.Itoa(asset.Port))
	options := []gordp.Option{
		gordp.WithDesktopSize(uint16(width), uint16(height)),
		gordp.WithServerCertFingerprint(asset.Fingerprint),
		gordp.WithScaleFactor(scale),
	}

	// The remote desktop sees a gateway directory of its own as
	// \\tsclient\casvisor, which the file panel uploads into and downloads
	// from. Without Download nothing may leave the remote host, so it cannot
	// write there; without Upload it cannot read what the user put there.
	canUpload := slices.Contains(session.Operations, "upload")
	canDownload := slices.Contains(session.Operations, "download")
	driveDir := ""
	if canUpload || canDownload {
		driveDir = getSessionDriveDir(session)
		if err = os.MkdirAll(driveDir, 0o700); err != nil {
			fail(NewTunnelError, err)
			return
		}
		// The files are only reachable while the session is connected.
		defer os.RemoveAll(driveDir)
		options = append(options, gordp.WithDrive(gordp.Drive{
			Name:      "casvisor",
			Root:      driveDir,
			ReadOnly:  !canDownload,
			WriteOnly: !canUpload,
		}))
	}

	// The graphics pipeline is how current Windows clients get the screen:
	// whole frames, caches and the lossless and progressive codecs. The
	// browser keeps the surfaces; the gateway decodes the codecs.
	var gfx *rdpGfxWriter
	if c.Input().Get("gfx") == "1" {
		gfx = &rdpGfxWriter{send: func(frame []byte) error {
			_, err := stream.Write(frame)
			return err
		}}
		options = append(options, gordp.WithGFX(gfx, gordp.GFXOptions{}))
	}

	client := gordp.NewClient(addr, "", asset.Username, asset.Password, options...)

	// Clipboard and resize calls are queued to run between pumps, on the
	// goroutine that owns the client state.
	ops := make(chan func(), 16)
	lastW, lastH := 0, 0
	syncSize := func() {
		w, h := client.DesktopSize()
		if w != lastW || h != lastH {
			lastW, lastH = w, h
			_ = stream.WriteJSON(rdpMessage{Type: "size", Width: w, Height: h})
		}
	}

	// Rectangles are collected while a frame is pumped and sent together, so
	// the browser shows a repainted window at once instead of tile by tile.
	var dirty []image.Rectangle
	client.OnBitmap = func(rects []bitmap.Rect) {
		for _, rect := range rects {
			dirty = append(dirty, image.Rect(rect.DestLeft, rect.DestTop, rect.DestLeft+rect.Width, rect.DestTop+rect.Height))
		}
	}
	flush := func() error {
		if len(dirty) == 0 {
			return nil
		}
		syncSize()
		frame := encodeFrame(client.Framebuffer(), dirty)
		dirty = dirty[:0]
		if frame == nil {
			return nil
		}
		_, err := stream.Write(frame)
		return err
	}

	client.OnPointer = func(p *gordp.Pointer) {
		_ = stream.WriteJSON(cursorMessage(p))
	}

	if slices.Contains(session.Operations, "copy") {
		client.OnClipboard = func(text string) {
			_ = stream.WriteJSON(rdpMessage{Type: "clipboard", Text: text})
			c.auditRdp(asset, "clipboard-copy", truncateRunes(text, 1000))
		}
	}

	client.OnDriveEvent = func(drive string, ev rdpdr.Event) {
		detail := ev.Path
		switch ev.Op {
		case rdpdr.OpRename:
			detail = fmt.Sprintf("%s -> %s", ev.Path, ev.NewPath)
		case rdpdr.OpRead, rdpdr.OpWrite:
			detail = fmt.Sprintf("%s (%d bytes)", ev.Path, ev.Bytes)
		}
		c.auditRdp(asset, "file-"+ev.Op, detail)
	}

	if err := client.Connect(); err != nil {
		if asset.Fingerprint != "" && strings.Contains(err.Error(), "does not match pinned") {
			err = fmt.Errorf("%s, clear the fingerprint of the asset if the server certificate was renewed", err.Error())
		}
		fail(NewTunnelError, err)
		return
	}
	defer client.Close()

	if asset.Fingerprint == "" {
		if err := object.UpdateAssetFingerprint(asset.GetId(), client.ServerCertFingerprint()); err != nil {
			fail(NewTunnelError, err)
			return
		}
	}

	if err := setSessionConnected(sessionId, session, width, height); err != nil {
		fail(SessionUpdateError, err)
		return
	}
	if driveDir != "" {
		sessionDrives.Store(sessionId, driveDir)
		defer sessionDrives.Delete(sessionId)
	}
	object.AddSessionCloser(sessionId, func(code int, msg string) {
		_ = client.Close()
		stream.Close(code, msg)
	})
	c.addLiveSession(sessionId, asset)

	lastW, lastH = client.DesktopSize()
	_ = stream.WriteJSON(rdpMessage{Type: "init", Width: lastW, Height: lastH})

	go c.readRdpInput(ws, client, session, asset, ops)

	for {
		if _, err := client.PumpFrame(50 * time.Millisecond); err != nil {
			break
		}
		if err := flush(); err != nil {
			break
		}
		if gfx != nil {
			gfx.flush()
			if gfx.err != nil {
				break
			}
		}
		for pending := true; pending; {
			select {
			case op := <-ops:
				op()
			default:
				pending = false
			}
		}
		syncSize()
	}

	if unsupported := client.GFXUnsupported(); len(unsupported) > 0 {
		logs.Warning(fmt.Sprintf("GetAssetRdp(): %s used graphics codecs that are not decoded (codec ID: commands): %v", asset.GetId(), unsupported))
	}
	closeTunnelSession(sessionId, "Normal user exit")
}

// readRdpInput reads input events from the browser and drives the RDP client.
func (c *ApiController) readRdpInput(ws *websocket.Conn, client *gordp.Client, session *object.Session, asset *object.Asset, ops chan<- func()) {
	canPaste := slices.Contains(session.Operations, "paste")
	queue := func(op func()) {
		select {
		case ops <- op:
		default:
		}
	}
	for {
		messageType, data, err := ws.ReadMessage()
		if err != nil {
			_ = client.Close()
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}
		var msg rdpMessage
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "mm":
			_ = client.MouseMove(uint16(msg.X), uint16(msg.Y))
		case "mb":
			_ = client.MouseButton(mouseButton(msg.Button), msg.Down, uint16(msg.X), uint16(msg.Y))
		case "mw":
			_ = client.MouseWheel(uint16(msg.X), uint16(msg.Y), int16(msg.Delta))
		case "k":
			_ = client.KeyScancode(byte(msg.Code), msg.Down, msg.Extended)
		case "u":
			_ = client.KeyUnicode(rune(msg.Code))
		case "clipboard":
			if !canPaste {
				continue
			}
			text := msg.Text
			queue(func() { _ = client.SetClipboard(text) })
			c.auditRdp(asset, "clipboard-paste", truncateRunes(text, 1000))
		case "resize":
			if msg.Width <= 0 || msg.Height <= 0 {
				continue
			}
			w, h := uint16(min(msg.Width, 8192)), uint16(min(msg.Height, 8192))
			queue(func() { _ = client.Resize(w, h) })
		}
	}
}

func (c *ApiController) auditRdp(asset *object.Asset, action string, detail string) {
	user := c.GetSessionUser()
	if user == nil {
		return
	}
	object.AddAuditRecord(&object.AuditEvent{
		Organization: asset.Owner,
		User:         user.Name,
		ClientIp:     c.getClientIp(),
		UserAgent:    c.getUserAgent(),
		Method:       "RDP",
		Action:       action,
		Asset:        asset.GetId(),
		Detail:       detail,
	})
}

func truncateRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// rdpMessage is the JSON control message exchanged with the browser. Pixel data
// travels as binary frames instead.
type rdpMessage struct {
	Type     string `json:"t"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	Message  string `json:"message,omitempty"`
	X        int    `json:"x,omitempty"`
	Y        int    `json:"y,omitempty"`
	Button   int    `json:"button,omitempty"`
	Down     bool   `json:"down,omitempty"`
	Delta    int    `json:"delta,omitempty"`
	Code     int    `json:"code,omitempty"`
	Extended bool   `json:"extended,omitempty"`
	Text     string `json:"text,omitempty"`
	Hidden   bool   `json:"hidden,omitempty"`
	Default  bool   `json:"default,omitempty"`
	Png      string `json:"png,omitempty"`
}

func mouseButton(button int) gordp.MouseButton {
	switch button {
	case 1:
		return gordp.ButtonMiddle
	case 2:
		return gordp.ButtonRight
	default:
		return gordp.ButtonLeft
	}
}

// cursorMessage tells the browser which mouse cursor the remote session shows,
// with the cursor image as a PNG and its hotspot.
func cursorMessage(p *gordp.Pointer) rdpMessage {
	msg := rdpMessage{Type: "cursor", Hidden: p.Hidden, Default: p.Default}
	if p.Hidden || p.Default || p.Width == 0 || p.Height == 0 {
		return msg
	}
	img := &image.NRGBA{Pix: p.RGBA, Stride: p.Width * 4, Rect: image.Rect(0, 0, p.Width, p.Height)}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		msg.Default = true
		return msg
	}
	msg.X, msg.Y = p.HotX, p.HotY
	msg.Width, msg.Height = p.Width, p.Height
	msg.Png = base64.StdEncoding.EncodeToString(buf.Bytes())
	return msg
}

var tileEncoder = png.Encoder{CompressionLevel: png.BestSpeed, BufferPool: &tileBufferPool{}}

type tileBufferPool struct{ pool sync.Pool }

func (p *tileBufferPool) Get() *png.EncoderBuffer {
	b, _ := p.pool.Get().(*png.EncoderBuffer)
	return b
}

func (p *tileBufferPool) Put(b *png.EncoderBuffer) { p.pool.Put(b) }

// encodeFrame builds one binary frame with the changed regions of the desktop:
// the kind byte, a tile count (uint16), then per tile an 8-byte header (x, y,
// width, height as little-endian uint16), the PNG length (uint32) and the PNG.
// Windows sends a repaint as many 64x64 tiles; when they cover most of their
// bounding box they are sent as that one region, which the browser decodes much
// faster.
func encodeFrame(fb *image.RGBA, dirty []image.Rectangle) []byte {
	bounds := fb.Bounds()
	var regions []image.Rectangle
	union := image.Rectangle{}
	area := 0
	for _, r := range dirty {
		r = r.Intersect(bounds)
		if r.Empty() {
			continue
		}
		union = union.Union(r)
		area += r.Dx() * r.Dy()
		regions = append(regions, r)
	}
	if len(regions) == 0 {
		return nil
	}
	if len(regions) > 1 && area*10 >= union.Dx()*union.Dy()*6 {
		regions = splitBands([]image.Rectangle{union})
	} else {
		regions = splitBands(mergeRegions(regions))
	}

	encoded := make([][]byte, len(regions))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	for i, r := range regions {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, r image.Rectangle) {
			defer func() { <-sem; wg.Done() }()
			var buf bytes.Buffer
			if tileEncoder.Encode(&buf, fb.SubImage(r)) == nil {
				encoded[i] = buf.Bytes()
			}
		}(i, r)
	}
	wg.Wait()

	var out bytes.Buffer
	var header [12]byte
	out.WriteByte(rdpFrameBitmap)
	binary.LittleEndian.PutUint16(header[0:], uint16(len(regions)))
	out.Write(header[:2])
	for i, r := range regions {
		data := encoded[i]
		binary.LittleEndian.PutUint16(header[0:], uint16(r.Min.X))
		binary.LittleEndian.PutUint16(header[2:], uint16(r.Min.Y))
		binary.LittleEndian.PutUint16(header[4:], uint16(r.Dx()))
		binary.LittleEndian.PutUint16(header[6:], uint16(r.Dy()))
		binary.LittleEndian.PutUint32(header[8:], uint32(len(data)))
		out.Write(header[:])
		out.Write(data)
	}
	return out.Bytes()
}

// splitBands cuts tall regions into bands of 128 rows, which the gateway
// encodes and the browser decodes in parallel.
func splitBands(regions []image.Rectangle) []image.Rectangle {
	const band = 128
	var out []image.Rectangle
	for _, r := range regions {
		for y := r.Min.Y; y < r.Max.Y; y += band {
			out = append(out, image.Rect(r.Min.X, y, r.Max.X, min(y+band, r.Max.Y)))
		}
	}
	return out
}

// mergeRegions drops duplicate rectangles (a tile repainted twice in one frame)
// and joins tiles that sit side by side in the same row, so a changed line of
// text is one image rather than a dozen.
func mergeRegions(regions []image.Rectangle) []image.Rectangle {
	slices.SortFunc(regions, func(a, b image.Rectangle) int {
		if a.Min.Y != b.Min.Y {
			return a.Min.Y - b.Min.Y
		}
		return a.Min.X - b.Min.X
	})
	var out []image.Rectangle
	for _, r := range regions {
		if n := len(out); n > 0 {
			last := &out[n-1]
			if r.In(*last) {
				continue
			}
			if r.Min.Y == last.Min.Y && r.Max.Y == last.Max.Y && r.Min.X <= last.Max.X {
				last.Max.X = max(last.Max.X, r.Max.X)
				continue
			}
		}
		out = append(out, r)
	}
	return out
}
