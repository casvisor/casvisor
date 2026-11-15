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

package vnc

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

const (
	encodingRaw      = 0
	encodingCopyRect = 1
	encodingZrle     = 16

	pseudoDesktopSize         = -223
	pseudoLastRect            = -224
	pseudoCursor              = -239
	pseudoXCursor             = -240
	pseudoQemuKeyEvent        = -258
	pseudoDesktopName         = -307
	pseudoExtendedDesktopSize = -308
	pseudoExtendedClipboard   = -1063131698 // 0xC0A1E5CE
)

const filterBufferSize = 64 * 1024

// ProtocolError is a message the filter cannot read, which ends the session
// rather than letting an unread clipboard through.
type ProtocolError struct {
	Message string
}

func (e *ProtocolError) Error() string {
	return e.Message
}

func protocolError(format string, args ...interface{}) error {
	return &ProtocolError{Message: fmt.Sprintf(format, args...)}
}

// Filter relays an RFB session from ClientInit on and drops the clipboard
// messages the user is not allowed: server cut text without Copy, client cut
// text without Paste. Only the browser enforcing them is not enough.
type Filter struct {
	AllowCopy  bool
	AllowPaste bool

	mu            sync.Mutex
	bytesPerPixel int
}

func (f *Filter) isOpen() bool {
	return f.AllowCopy && f.AllowPaste
}

func (f *Filter) setPixelFormat(pixelFormat []byte) {
	f.mu.Lock()
	f.bytesPerPixel = int(pixelFormat[0]) / 8
	f.mu.Unlock()
}

func (f *Filter) getBytesPerPixel() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bytesPerPixel
}

// keepEncoding tells whether the server may use an encoding. Extended
// clipboard is always left out, since there the side that copies asks the
// other side for the data, so dropping one direction would break the other.
// Without Copy the server stream is read message by message, which is only
// possible for encodings whose length can be told without decoding them.
func (f *Filter) keepEncoding(encoding int32) bool {
	if encoding == pseudoExtendedClipboard {
		return false
	}
	if f.AllowCopy {
		return true
	}

	switch encoding {
	case encodingRaw, encodingCopyRect, encodingZrle,
		pseudoDesktopSize, pseudoLastRect, pseudoCursor, pseudoXCursor,
		pseudoQemuKeyEvent, pseudoDesktopName, pseudoExtendedDesktopSize:
		return true
	}
	// JPEG quality and compression levels only tune the encoder
	return (encoding >= -32 && encoding <= -23) || (encoding >= -256 && encoding <= -247)
}

type relay struct {
	r *bufio.Reader
	w *bufio.Writer
}

func newRelay(dst io.Writer, src io.Reader) *relay {
	return &relay{r: bufio.NewReaderSize(src, filterBufferSize), w: bufio.NewWriterSize(dst, filterBufferSize)}
}

// read reads n bytes and passes them on.
func (r *relay) read(n int) ([]byte, error) {
	buf := make([]byte, n)
	_, err := io.ReadFull(r.r, buf)
	if err != nil {
		return nil, err
	}
	_, err = r.w.Write(buf)
	return buf, err
}

// pass passes n bytes on without keeping them.
func (r *relay) pass(n int64) error {
	_, err := io.CopyN(r.w, r.r, n)
	return err
}

func (r *relay) drop(n int64) error {
	_, err := r.r.Discard(int(n))
	return err
}

// next flushes what was passed on when no more is waiting and reads the type
// of the next message.
func (r *relay) next() (byte, error) {
	if r.r.Buffered() == 0 {
		err := r.w.Flush()
		if err != nil {
			return 0, err
		}
	}
	return r.r.ReadByte()
}

// cutTextLength reads the length of cut text, which extended clipboard sends
// as a negative number.
func cutTextLength(b []byte) int64 {
	length := int64(int32(binary.BigEndian.Uint32(b)))
	if length < 0 {
		length = -length
	}
	return length
}

// RelayClient passes what the browser sends on to the server.
func (f *Filter) RelayClient(dst io.Writer, src io.Reader) error {
	if f.isOpen() {
		_, err := io.Copy(dst, src)
		return err
	}

	r := newRelay(dst, src)
	// ClientInit
	_, err := r.read(1)
	if err != nil {
		return err
	}

	for {
		t, err := r.next()
		if err != nil {
			return err
		}

		switch t {
		case 0: // SetPixelFormat
			err = r.w.WriteByte(t)
			if err == nil {
				var buf []byte
				buf, err = r.read(19)
				if err == nil {
					f.setPixelFormat(buf[3:])
				}
			}
		case 2: // SetEncodings
			err = f.relayEncodings(r)
		case 3: // FramebufferUpdateRequest
			err = r.passMessage(t, 10)
		case 4: // KeyEvent
			err = r.passMessage(t, 8)
		case 5: // PointerEvent
			err = r.passMessage(t, 6)
		case 6: // ClientCutText
			header := make([]byte, 7)
			_, err = io.ReadFull(r.r, header)
			if err != nil {
				return err
			}
			length := cutTextLength(header[3:])
			if !f.AllowPaste {
				err = r.drop(length)
				break
			}
			_ = r.w.WriteByte(t)
			_, _ = r.w.Write(header)
			err = r.pass(length)
		case 150: // EnableContinuousUpdates
			err = r.passMessage(t, 10)
		case 248: // ClientFence
			err = r.passWithLength(t, 8)
		case 250: // xvp
			err = r.passMessage(t, 4)
		case 251: // SetDesktopSize
			err = r.w.WriteByte(t)
			if err == nil {
				var buf []byte
				buf, err = r.read(7)
				if err == nil {
					err = r.pass(int64(buf[5]) * 16)
				}
			}
		case 255: // QEMU client message
			var sub byte
			sub, err = r.r.ReadByte()
			if err == nil && sub != 0 {
				err = protocolError("unsupported QEMU client message: %d", sub)
			}
			if err == nil {
				_ = r.w.WriteByte(t)
				_ = r.w.WriteByte(sub)
				err = r.pass(10)
			}
		default:
			err = protocolError("unsupported RFB client message: %d", t)
		}
		if err != nil {
			return err
		}
	}
}

// passMessage passes a message of a fixed size on.
func (r *relay) passMessage(t byte, size int) error {
	err := r.w.WriteByte(t)
	if err != nil {
		return err
	}
	return r.pass(int64(size - 1))
}

// passWithLength passes a fence message on: its header, then as many bytes
// as the last byte of the header says.
func (r *relay) passWithLength(t byte, headerSize int) error {
	err := r.w.WriteByte(t)
	if err != nil {
		return err
	}
	buf, err := r.read(headerSize)
	if err != nil {
		return err
	}
	return r.pass(int64(buf[headerSize-1]))
}

func (f *Filter) relayEncodings(r *relay) error {
	header := make([]byte, 3)
	_, err := io.ReadFull(r.r, header)
	if err != nil {
		return err
	}

	count := int(binary.BigEndian.Uint16(header[1:]))
	encodings := make([]int32, count)
	err = binary.Read(r.r, binary.BigEndian, encodings)
	if err != nil {
		return err
	}

	kept := []int32{}
	for _, encoding := range encodings {
		if f.keepEncoding(encoding) {
			kept = append(kept, encoding)
		}
	}

	_ = r.w.WriteByte(2)
	_ = r.w.WriteByte(0)
	_ = binary.Write(r.w, binary.BigEndian, uint16(len(kept)))
	return binary.Write(r.w, binary.BigEndian, kept)
}

// RelayServer passes what the server sends on to the browser.
func (f *Filter) RelayServer(dst io.Writer, src io.Reader) error {
	r := newRelay(dst, src)
	// ServerInit
	buf, err := r.read(24)
	if err != nil {
		return err
	}
	f.setPixelFormat(buf[4:20])
	err = r.pass(int64(binary.BigEndian.Uint32(buf[20:24])))
	if err != nil {
		return err
	}

	if f.AllowCopy {
		err = r.w.Flush()
		if err != nil {
			return err
		}
		_, err = io.Copy(dst, r.r)
		return err
	}

	for {
		t, err := r.next()
		if err != nil {
			return err
		}

		switch t {
		case 0: // FramebufferUpdate
			err = f.relayUpdate(r)
		case 1: // SetColourMapEntries
			err = r.w.WriteByte(t)
			if err == nil {
				buf, err = r.read(5)
				if err == nil {
					err = r.pass(int64(binary.BigEndian.Uint16(buf[3:5])) * 6)
				}
			}
		case 2: // Bell
			err = r.w.WriteByte(t)
		case 3: // ServerCutText
			header := make([]byte, 7)
			_, err = io.ReadFull(r.r, header)
			if err == nil {
				err = r.drop(cutTextLength(header[3:]))
			}
		case 150: // EndOfContinuousUpdates
			err = r.w.WriteByte(t)
		case 248: // ServerFence
			err = r.passWithLength(t, 8)
		case 250: // xvp
			err = r.passMessage(t, 4)
		default:
			err = protocolError("unsupported RFB server message: %d", t)
		}
		if err != nil {
			return err
		}
	}
}

func (f *Filter) relayUpdate(r *relay) error {
	err := r.w.WriteByte(0)
	if err != nil {
		return err
	}
	buf, err := r.read(3)
	if err != nil {
		return err
	}

	count := int(binary.BigEndian.Uint16(buf[1:]))
	for i := 0; i < count; i++ {
		rect, err := r.read(12)
		if err != nil {
			return err
		}
		w := int64(binary.BigEndian.Uint16(rect[4:6]))
		h := int64(binary.BigEndian.Uint16(rect[6:8]))
		encoding := int32(binary.BigEndian.Uint32(rect[8:12]))
		bpp := int64(f.getBytesPerPixel())
		mask := (w + 7) / 8 * h

		switch encoding {
		case encodingRaw:
			err = r.pass(w * h * bpp)
		case encodingCopyRect:
			err = r.pass(4)
		case encodingZrle:
			buf, err = r.read(4)
			if err == nil {
				err = r.pass(int64(binary.BigEndian.Uint32(buf)))
			}
		case pseudoDesktopSize, pseudoQemuKeyEvent:
		case pseudoLastRect:
			return nil
		case pseudoCursor:
			err = r.pass(w*h*bpp + mask)
		case pseudoXCursor:
			if w*h > 0 {
				err = r.pass(6 + 2*mask)
			}
		case pseudoDesktopName:
			buf, err = r.read(4)
			if err == nil {
				err = r.pass(int64(binary.BigEndian.Uint32(buf)))
			}
		case pseudoExtendedDesktopSize:
			buf, err = r.read(4)
			if err == nil {
				err = r.pass(int64(buf[0]) * 16)
			}
		default:
			err = protocolError("the VNC server used an encoding the gateway did not offer: %d", encoding)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
