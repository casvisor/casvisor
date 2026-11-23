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

package rdgateway

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"unicode/utf16"
)

// The packets of the HTTP transport of the Terminal Services Gateway Server Protocol (MS-TSGU 2.2.10).
const (
	pktHandshakeRequest     = 0x1
	pktHandshakeResponse    = 0x2
	pktExtendedAuthMsg      = 0x3
	pktTunnelCreate         = 0x4
	pktTunnelResponse       = 0x5
	pktTunnelAuth           = 0x6
	pktTunnelAuthResponse   = 0x7
	pktChannelCreate        = 0x8
	pktChannelResponse      = 0x9
	pktData                 = 0xA
	pktServiceMessage       = 0xB
	pktReauthMessage        = 0xC
	pktKeepAlive            = 0xD
	pktCloseChannel         = 0x10
	pktCloseChannelResponse = 0x11

	extendedAuthPaa = 0x2

	tunnelCreateFieldPaaCookie = 0x1
	tunnelResponseFieldId      = 0x1
	tunnelResponseFieldCaps    = 0x2

	capabilityIdleTimeout = 0x2

	tunnelAuthResponseFieldRedirFlags  = 0x1
	tunnelAuthResponseFieldIdleTimeout = 0x2

	redirDisableDrive     = 0x1
	redirDisablePrinter   = 0x2
	redirDisablePort      = 0x4
	redirDisableClipboard = 0x8
	redirDisablePnp       = 0x10

	channelResponseFieldId      = 0x1
	closeChannelResponseFieldId = 0x1

	// the errors mstsc shows to the user
	errorCookieAccessDenied = 0x800759F8
	errorConnectFailed      = 0x000059DD
	errorInternal           = 0x800759D8

	headerSize    = 8
	maxPacketSize = 1 << 20
	maxDataSize   = 16 * 1024
	channelId     = 1
	tunnelId      = 1
)

type packet struct {
	kind uint16
	body []byte
}

func readPacket(r io.Reader) (*packet, error) {
	header := make([]byte, headerSize)
	_, err := io.ReadFull(r, header)
	if err != nil {
		return nil, err
	}
	kind := binary.LittleEndian.Uint16(header[0:2])
	size := binary.LittleEndian.Uint32(header[4:8])
	if size < headerSize || size > maxPacketSize {
		return nil, fmt.Errorf("invalid packet size: %d", size)
	}
	body := make([]byte, size-headerSize)
	_, err = io.ReadFull(r, body)
	if err != nil {
		return nil, err
	}
	return &packet{kind: kind, body: body}, nil
}

func encodePacket(kind uint16, body []byte) []byte {
	res := make([]byte, headerSize+len(body))
	binary.LittleEndian.PutUint16(res[0:2], kind)
	binary.LittleEndian.PutUint32(res[4:8], uint32(len(res)))
	copy(res[headerSize:], body)
	return res
}

// writer writes the fields of a packet in little-endian order.
type writer struct {
	bytes.Buffer
}

func (w *writer) u8(v uint8) *writer {
	w.WriteByte(v)
	return w
}

func (w *writer) u16(v uint16) *writer {
	_ = binary.Write(&w.Buffer, binary.LittleEndian, v)
	return w
}

func (w *writer) u32(v uint32) *writer {
	_ = binary.Write(&w.Buffer, binary.LittleEndian, v)
	return w
}

// reader reads the fields of a packet, remembering the first error.
type reader struct {
	b   []byte
	err error
}

func (r *reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n > len(r.b) {
		r.err = fmt.Errorf("the packet is too short")
		return nil
	}
	res := r.b[:n]
	r.b = r.b[n:]
	return res
}

func (r *reader) u8() uint8 {
	b := r.take(1)
	if b == nil {
		return 0
	}
	return b[0]
}

func (r *reader) u16() uint16 {
	b := r.take(2)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint16(b)
}

func (r *reader) u32() uint32 {
	b := r.take(4)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(b)
}

// utf16String reads a string of UTF-16 code units, which mstsc ends with a NUL.
func (r *reader) utf16String(size int) string {
	b := r.take(size)
	if b == nil {
		return ""
	}
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return strings.TrimRight(string(utf16.Decode(units)), "\x00")
}

type handshakeRequest struct {
	major        uint8
	minor        uint8
	extendedAuth uint16
}

func parseHandshakeRequest(body []byte) (*handshakeRequest, error) {
	r := &reader{b: body}
	res := &handshakeRequest{major: r.u8(), minor: r.u8()}
	r.u16()
	res.extendedAuth = r.u16()
	return res, r.err
}

func handshakeResponse(errorCode uint32, major uint8, minor uint8, extendedAuth uint16) []byte {
	w := &writer{}
	w.u32(errorCode).u8(major).u8(minor).u16(0).u16(extendedAuth)
	return encodePacket(pktHandshakeResponse, w.Bytes())
}

// parseTunnelCreate returns the PAA cookie, which is the gatewayaccesstoken of the .rdp file.
func parseTunnelCreate(body []byte) (string, error) {
	r := &reader{b: body}
	r.u32()
	fields := r.u16()
	r.u16()
	if fields&tunnelCreateFieldPaaCookie == 0 {
		return "", r.err
	}
	size := r.u16()
	return r.utf16String(int(size)), r.err
}

func tunnelResponse(errorCode uint32) []byte {
	w := &writer{}
	w.u16(0).u32(errorCode)
	if errorCode != 0 {
		w.u16(0).u16(0)
		return encodePacket(pktTunnelResponse, w.Bytes())
	}
	w.u16(tunnelResponseFieldId | tunnelResponseFieldCaps).u16(0).u32(tunnelId).u32(capabilityIdleTimeout)
	return encodePacket(pktTunnelResponse, w.Bytes())
}

func parseTunnelAuth(body []byte) (string, error) {
	r := &reader{b: body}
	r.u16()
	size := r.u16()
	return r.utf16String(int(size)), r.err
}

func tunnelAuthResponse(errorCode uint32, redirFlags uint32) []byte {
	w := &writer{}
	w.u32(errorCode).u16(tunnelAuthResponseFieldRedirFlags | tunnelAuthResponseFieldIdleTimeout).u16(0).u32(redirFlags).u32(0)
	return encodePacket(pktTunnelAuthResponse, w.Bytes())
}

type channelCreate struct {
	resources []string
	port      uint16
}

func parseChannelCreate(body []byte) (*channelCreate, error) {
	r := &reader{b: body}
	count := r.u8()
	altCount := r.u8()
	res := &channelCreate{port: r.u16()}
	r.u16()
	for i := 0; i < int(count)+int(altCount); i++ {
		size := r.u16()
		res.resources = append(res.resources, r.utf16String(int(size)))
	}
	return res, r.err
}

func channelResponse(errorCode uint32) []byte {
	w := &writer{}
	w.u32(errorCode)
	if errorCode != 0 {
		w.u16(0).u16(0)
		return encodePacket(pktChannelResponse, w.Bytes())
	}
	w.u16(channelResponseFieldId).u16(0).u32(channelId)
	return encodePacket(pktChannelResponse, w.Bytes())
}

func parseData(body []byte) ([]byte, error) {
	r := &reader{b: body}
	size := r.u16()
	return r.take(int(size)), r.err
}

func dataPacket(data []byte) []byte {
	w := &writer{}
	w.u16(uint16(len(data)))
	w.Write(data)
	return encodePacket(pktData, w.Bytes())
}

func closeChannelResponse() []byte {
	w := &writer{}
	w.u32(0).u16(closeChannelResponseFieldId).u16(0).u32(channelId)
	return encodePacket(pktCloseChannelResponse, w.Bytes())
}
