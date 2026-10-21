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

package demo

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"io"
	"net"
	"sync"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/util/vnc"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	desktopWidth  = 1024
	desktopHeight = 640

	encodingRaw     = 0
	encodingHextile = 5
)

var (
	colorBackground = color.RGBA{R: 0x1e, G: 0x29, B: 0x3b, A: 0xff}
	colorWindow     = color.RGBA{R: 0xf8, G: 0xfa, B: 0xfc, A: 0xff}
	colorTitleBar   = color.RGBA{R: 0x33, G: 0x41, B: 0x55, A: 0xff}
	colorTaskbar    = color.RGBA{R: 0x0f, G: 0x17, B: 0x2a, A: 0xff}
	colorCanvas     = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	colorBorder     = color.RGBA{R: 0xcb, G: 0xd5, B: 0xe1, A: 0xff}
	colorText       = color.RGBA{R: 0x0f, G: 0x17, B: 0x2a, A: 0xff}
	colorTextLight  = color.RGBA{R: 0xe2, G: 0xe8, B: 0xf0, A: 0xff}
	colorInk        = color.RGBA{R: 0x25, G: 0x63, B: 0xeb, A: 0xff}
)

var (
	canvasRect = image.Rect(40, 250, 984, 584)
	textRect   = image.Rect(40, 120, 984, 230)
	clockRect  = image.Rect(desktopWidth-110, desktopHeight-28, desktopWidth-10, desktopHeight-6)
)

func startVncServer(address string) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				logs.Error("demo vnc server: %s", err.Error())
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(30 * time.Minute))
				newDesktop(conn).serve()
			}()
		}
	}()
	return nil
}

type pixelFormat struct {
	BitsPerPixel uint8
	Depth        uint8
	BigEndian    uint8
	TrueColor    uint8
	RedMax       uint16
	GreenMax     uint16
	BlueMax      uint16
	RedShift     uint8
	GreenShift   uint8
	BlueShift    uint8
	_            [3]byte
}

type updateRequest struct {
	incremental bool
	rect        image.Rectangle
}

// desktop is one visitor's private demo desktop: a window to type in and a canvas to draw on.
type desktop struct {
	conn    net.Conn
	reader  *bufio.Reader
	writeMu sync.Mutex

	mu       sync.Mutex
	img      *image.RGBA
	dirty    image.Rectangle
	format   pixelFormat
	hextile  bool
	text     []string
	lastX    int
	lastY    int
	drawing  bool
	requests chan updateRequest
	changed  chan struct{}
}

func newDesktop(conn net.Conn) *desktop {
	d := &desktop{
		conn:   conn,
		reader: bufio.NewReader(conn),
		img:    image.NewRGBA(image.Rect(0, 0, desktopWidth, desktopHeight)),
		format: pixelFormat{
			BitsPerPixel: 32, Depth: 24, TrueColor: 1,
			RedMax: 255, GreenMax: 255, BlueMax: 255,
			RedShift: 16, GreenShift: 8, BlueShift: 0,
		},
		text:     []string{""},
		requests: make(chan updateRequest, 1),
		changed:  make(chan struct{}, 1),
	}
	d.paintDesktop()
	return d
}

func (d *desktop) serve() {
	if err := d.handshake(); err != nil {
		return
	}

	done := make(chan struct{})
	defer close(done)
	go d.tickClock(done)
	go d.sendUpdates(done)

	for {
		if err := d.readClientMessage(); err != nil {
			return
		}
	}
}

func (d *desktop) handshake() error {
	if _, err := d.conn.Write([]byte("RFB 003.008\n")); err != nil {
		return err
	}
	version := make([]byte, 12)
	if _, err := io.ReadFull(d.reader, version); err != nil {
		return err
	}

	if _, err := d.conn.Write([]byte{1, 2}); err != nil {
		return err
	}
	chosen, err := d.reader.ReadByte()
	if err != nil || chosen != 2 {
		return io.ErrUnexpectedEOF
	}

	challenge := make([]byte, 16)
	_, _ = rand.Read(challenge)
	if _, err = d.conn.Write(challenge); err != nil {
		return err
	}
	response := make([]byte, 16)
	if _, err = io.ReadFull(d.reader, response); err != nil {
		return err
	}
	expected, err := vnc.EncryptChallenge(challenge, Password)
	if err != nil {
		return err
	}
	if !bytes.Equal(response, expected) {
		reason := "Authentication failed, the demo password is \"" + Password + "\""
		_ = binary.Write(d.conn, binary.BigEndian, uint32(1))
		_ = binary.Write(d.conn, binary.BigEndian, uint32(len(reason)))
		_, _ = d.conn.Write([]byte(reason))
		return io.ErrUnexpectedEOF
	}
	if err = binary.Write(d.conn, binary.BigEndian, uint32(0)); err != nil {
		return err
	}

	// ClientInit (shared flag), then ServerInit.
	if _, err = d.reader.ReadByte(); err != nil {
		return err
	}
	name := "Casvisor demo desktop"
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, uint16(desktopWidth))
	_ = binary.Write(&buf, binary.BigEndian, uint16(desktopHeight))
	_ = binary.Write(&buf, binary.BigEndian, d.format)
	_ = binary.Write(&buf, binary.BigEndian, uint32(len(name)))
	buf.WriteString(name)
	_, err = d.conn.Write(buf.Bytes())
	return err
}

func (d *desktop) readClientMessage() error {
	messageType, err := d.reader.ReadByte()
	if err != nil {
		return err
	}

	switch messageType {
	case 0:
		var message struct {
			_      [3]byte
			Format pixelFormat
		}
		if err = binary.Read(d.reader, binary.BigEndian, &message); err != nil {
			return err
		}
		d.mu.Lock()
		if message.Format.TrueColor == 1 && (message.Format.BitsPerPixel == 8 || message.Format.BitsPerPixel == 16 || message.Format.BitsPerPixel == 32) {
			d.format = message.Format
		}
		d.mu.Unlock()
	case 2:
		var header struct {
			_     byte
			Count uint16
		}
		if err = binary.Read(d.reader, binary.BigEndian, &header); err != nil {
			return err
		}
		encodings := make([]int32, header.Count)
		if err = binary.Read(d.reader, binary.BigEndian, &encodings); err != nil {
			return err
		}
		d.mu.Lock()
		d.hextile = false
		for _, encoding := range encodings {
			if encoding == encodingHextile {
				d.hextile = true
			}
		}
		d.mu.Unlock()
	case 3:
		var message struct {
			Incremental         uint8
			X, Y, Width, Height uint16
		}
		if err = binary.Read(d.reader, binary.BigEndian, &message); err != nil {
			return err
		}
		request := updateRequest{
			incremental: message.Incremental != 0,
			rect:        image.Rect(int(message.X), int(message.Y), int(message.X)+int(message.Width), int(message.Y)+int(message.Height)),
		}
		select {
		case <-d.requests:
		default:
		}
		d.requests <- request
	case 4:
		var message struct {
			Down uint8
			_    [2]byte
			Key  uint32
		}
		if err = binary.Read(d.reader, binary.BigEndian, &message); err != nil {
			return err
		}
		if message.Down != 0 {
			d.typeKey(message.Key)
		}
	case 5:
		var message struct {
			Mask uint8
			X, Y uint16
		}
		if err = binary.Read(d.reader, binary.BigEndian, &message); err != nil {
			return err
		}
		d.movePointer(message.Mask&1 != 0, int(message.X), int(message.Y))
	case 6:
		var header struct {
			_      [3]byte
			Length uint32
		}
		if err = binary.Read(d.reader, binary.BigEndian, &header); err != nil {
			return err
		}
		if header.Length > 1<<20 {
			return io.ErrUnexpectedEOF
		}
		if _, err = d.reader.Discard(int(header.Length)); err != nil {
			return err
		}
	default:
		return io.ErrUnexpectedEOF
	}
	return nil
}

func (d *desktop) markDirty(rect image.Rectangle) {
	d.dirty = d.dirty.Union(rect)
	select {
	case d.changed <- struct{}{}:
	default:
	}
}

func (d *desktop) tickClock(done chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			d.mu.Lock()
			d.paintClock()
			d.mu.Unlock()
		}
	}
}

func (d *desktop) sendUpdates(done chan struct{}) {
	for {
		var request updateRequest
		select {
		case <-done:
			return
		case request = <-d.requests:
		}

		for {
			d.mu.Lock()
			rect := d.dirty
			if !request.incremental {
				rect = request.rect
			}
			rect = rect.Intersect(request.rect).Intersect(d.img.Bounds())
			if !rect.Empty() {
				d.dirty = image.Rectangle{}
				data := d.encodeUpdate(rect)
				d.mu.Unlock()
				_ = d.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
				if _, err := d.conn.Write(data); err != nil {
					_ = d.conn.Close()
					return
				}
				break
			}
			d.mu.Unlock()

			select {
			case <-done:
				return
			case <-d.changed:
			case newer := <-d.requests:
				request = newer
			}
		}
	}
}

func (d *desktop) encodeUpdate(rect image.Rectangle) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0, 0})
	_ = binary.Write(&buf, binary.BigEndian, uint16(1))
	_ = binary.Write(&buf, binary.BigEndian, uint16(rect.Min.X))
	_ = binary.Write(&buf, binary.BigEndian, uint16(rect.Min.Y))
	_ = binary.Write(&buf, binary.BigEndian, uint16(rect.Dx()))
	_ = binary.Write(&buf, binary.BigEndian, uint16(rect.Dy()))

	if !d.hextile {
		_ = binary.Write(&buf, binary.BigEndian, int32(encodingRaw))
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			for x := rect.Min.X; x < rect.Max.X; x++ {
				d.writePixel(&buf, d.img.RGBAAt(x, y))
			}
		}
		return buf.Bytes()
	}

	// Hextile: a solid 16x16 tile costs one pixel, which keeps the mostly flat desktop small.
	_ = binary.Write(&buf, binary.BigEndian, int32(encodingHextile))
	for ty := rect.Min.Y; ty < rect.Max.Y; ty += 16 {
		for tx := rect.Min.X; tx < rect.Max.X; tx += 16 {
			tile := image.Rect(tx, ty, min(tx+16, rect.Max.X), min(ty+16, rect.Max.Y))
			d.encodeHextileTile(&buf, tile)
		}
	}
	return buf.Bytes()
}

// encodeHextileTile sends a tile as one color, as two colors with foreground runs, or as raw pixels.
func (d *desktop) encodeHextileTile(buf *bytes.Buffer, tile image.Rectangle) {
	counts := map[color.RGBA]int{}
	for y := tile.Min.Y; y < tile.Max.Y && len(counts) <= 2; y++ {
		for x := tile.Min.X; x < tile.Max.X; x++ {
			counts[d.img.RGBAAt(x, y)]++
		}
	}

	if len(counts) == 1 {
		buf.WriteByte(2)
		d.writePixel(buf, d.img.RGBAAt(tile.Min.X, tile.Min.Y))
		return
	}

	if len(counts) == 2 {
		var background, foreground color.RGBA
		most := -1
		for c, n := range counts {
			if n > most {
				background, most = c, n
			}
		}
		for c := range counts {
			if c != background {
				foreground = c
			}
		}

		runs := [][2]byte{}
		for y := tile.Min.Y; y < tile.Max.Y; y++ {
			for x := tile.Min.X; x < tile.Max.X; {
				if d.img.RGBAAt(x, y) != foreground {
					x++
					continue
				}
				start := x
				for x < tile.Max.X && d.img.RGBAAt(x, y) == foreground {
					x++
				}
				runs = append(runs, [2]byte{byte((start-tile.Min.X)<<4 | (y - tile.Min.Y)), byte((x-start-1)<<4 | 0)})
			}
		}

		if len(runs) <= 255 && 2*len(runs) < tile.Dx()*tile.Dy()*int(d.format.BitsPerPixel/8) {
			buf.WriteByte(2 | 4 | 8)
			d.writePixel(buf, background)
			d.writePixel(buf, foreground)
			buf.WriteByte(byte(len(runs)))
			for _, run := range runs {
				buf.Write(run[:])
			}
			return
		}
	}

	buf.WriteByte(1)
	for y := tile.Min.Y; y < tile.Max.Y; y++ {
		for x := tile.Min.X; x < tile.Max.X; x++ {
			d.writePixel(buf, d.img.RGBAAt(x, y))
		}
	}
}

func (d *desktop) writePixel(buf *bytes.Buffer, c color.RGBA) {
	f := d.format
	value := uint32(c.R)*uint32(f.RedMax)/255<<f.RedShift |
		uint32(c.G)*uint32(f.GreenMax)/255<<f.GreenShift |
		uint32(c.B)*uint32(f.BlueMax)/255<<f.BlueShift

	switch f.BitsPerPixel {
	case 8:
		buf.WriteByte(byte(value))
	case 16:
		if f.BigEndian != 0 {
			_ = binary.Write(buf, binary.BigEndian, uint16(value))
		} else {
			_ = binary.Write(buf, binary.LittleEndian, uint16(value))
		}
	default:
		if f.BigEndian != 0 {
			_ = binary.Write(buf, binary.BigEndian, value)
		} else {
			_ = binary.Write(buf, binary.LittleEndian, value)
		}
	}
}

func (d *desktop) fill(rect image.Rectangle, c color.RGBA) {
	draw.Draw(d.img, rect, &image.Uniform{C: c}, image.Point{}, draw.Src)
}

func (d *desktop) drawText(x int, y int, text string, c color.RGBA) {
	drawer := &font.Drawer{
		Dst:  d.img,
		Src:  &image.Uniform{C: c},
		Face: basicfont.Face7x13,
		Dot:  fixed.P(x, y),
	}
	drawer.DrawString(text)
}

func (d *desktop) paintDesktop() {
	d.fill(d.img.Bounds(), colorBackground)

	d.fill(image.Rect(20, 20, desktopWidth-20, desktopHeight-48), colorWindow)
	d.fill(image.Rect(20, 20, desktopWidth-20, 48), colorTitleBar)
	d.drawText(32, 39, "Casvisor demo desktop", colorTextLight)

	d.drawText(40, 74, "This desktop is served by Casvisor's built-in demo VNC server. It is private to your", colorText)
	d.drawText(40, 92, "session and disappears when you disconnect. Type below, or draw on the canvas with the mouse.", colorText)

	d.fill(textRect.Inset(-1), colorBorder)
	d.fill(canvasRect.Inset(-1), colorBorder)
	d.paintText()
	d.fill(canvasRect, colorCanvas)
	d.drawText(canvasRect.Min.X+8, canvasRect.Min.Y+18, "Canvas: hold the left mouse button and drag", colorBorder)

	d.fill(image.Rect(0, desktopHeight-34, desktopWidth, desktopHeight), colorTaskbar)
	d.drawText(12, desktopHeight-13, "Casvisor | open-source bastion host for SSH, RDP and VNC", colorTextLight)
	d.paintClock()
	d.markDirty(d.img.Bounds())
}

func (d *desktop) paintClock() {
	d.fill(clockRect, colorTaskbar)
	d.drawText(clockRect.Min.X+14, clockRect.Min.Y+15, time.Now().UTC().Format("15:04:05")+" UTC", colorTextLight)
	d.markDirty(clockRect)
}

func (d *desktop) paintText() {
	d.fill(textRect, colorCanvas)
	lines := d.text
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	for i, line := range lines {
		if i == len(lines)-1 {
			line += "_"
		}
		d.drawText(textRect.Min.X+8, textRect.Min.Y+18+i*16, line, colorText)
	}
	d.markDirty(textRect)
}

func (d *desktop) typeKey(key uint32) {
	d.mu.Lock()
	defer d.mu.Unlock()

	last := len(d.text) - 1
	switch {
	case key >= 0x20 && key <= 0x7e:
		if len(d.text[last]) < 130 {
			d.text[last] += string(rune(key))
		}
	case key == 0xff08:
		if d.text[last] != "" {
			d.text[last] = d.text[last][:len(d.text[last])-1]
		} else if last > 0 {
			d.text = d.text[:last]
		}
	case key == 0xff0d || key == 0xff8d:
		d.text = append(d.text, "")
		if len(d.text) > 100 {
			d.text = d.text[len(d.text)-100:]
		}
	default:
		return
	}
	d.paintText()
}

func (d *desktop) movePointer(pressed bool, x int, y int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	point := image.Pt(x, y)
	if !pressed || !point.In(canvasRect) {
		d.drawing = false
		return
	}

	fromX, fromY := x, y
	if d.drawing {
		fromX, fromY = d.lastX, d.lastY
	}
	steps := max(abs(x-fromX), abs(y-fromY), 1)
	for i := 0; i <= steps; i++ {
		px := fromX + (x-fromX)*i/steps
		py := fromY + (y-fromY)*i/steps
		dot := image.Rect(px-2, py-2, px+2, py+2).Intersect(canvasRect)
		d.fill(dot, colorInk)
		d.markDirty(dot)
	}
	d.drawing = true
	d.lastX, d.lastY = x, y
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
