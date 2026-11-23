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
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/gordp/nla"
)

const (
	protocolSsl    = 0x1
	protocolHybrid = 0x2

	tpduConnectionRequest = 0xE0
	tpduConnectionConfirm = 0xD0
	negRequest            = 0x1
	negResponse           = 0x2
	negFailure            = 0x3

	// the flags of RDP_NEG_RSP that have nothing to do with CredSSP
	negResponseFlags = 0x07

	csCore = 0xC001
	scCore = 0x0C01
	// serverSelectedProtocol in TS_UD_CS_CORE and clientRequestedProtocols in TS_UD_SC_CORE
	csCoreSelectedProtocolOffset  = 212
	scCoreRequestedProtocolOffset = 8

	mcsSendDataRequest = 25
	secInfoPkt         = 0x0040

	infoAutoLogon       = 0x00000008
	infoUnicode         = 0x00000010
	infoPasswordIsScPin = 0x00040000

	autoLogonTimeout = 30 * time.Second
)

var errFingerprint = errors.New("fingerprint mismatch")

// SignsIn tells whether the gateway signs in to the asset with the account's password, so that mstsc
// connects without CredSSP and the user is not asked for a password.
func SignsIn(target *object.Asset) bool {
	return target.Password != ""
}

// autoLogon stands between mstsc and an RDP asset: mstsc speaks TLS to Casvisor, Casvisor speaks TLS and
// CredSSP to the asset with the stored password, and the Client Info PDU carries the account, so the user
// lands on the desktop without knowing the password. Everything after that passes through as it is.
type autoLogon struct {
	client     net.Conn
	asset      net.Conn
	target     *object.Asset
	onSignedIn func()

	closeOnce sync.Once
	toClose   []io.Closer
}

// startAutoLogon returns what the tunnel uses as its connection to the asset.
func startAutoLogon(conn net.Conn, target *object.Asset, onSignedIn func()) net.Conn {
	res, end := net.Pipe()
	a := &autoLogon{client: end, asset: conn, target: target, onSignedIn: onSignedIn}
	go a.run()
	return res
}

func (a *autoLogon) close() {
	a.closeOnce.Do(func() {
		_ = a.client.Close()
		_ = a.asset.Close()
		for _, c := range a.toClose {
			_ = c.Close()
		}
	})
}

func (a *autoLogon) run() {
	defer a.close()
	_ = a.client.SetDeadline(time.Now().Add(autoLogonTimeout))

	request, err := readTpkt(a.client)
	if err != nil {
		return
	}
	requested, ok := parseConnectionRequest(request)
	if !ok || requested&protocolSsl == 0 {
		a.passThrough(request)
		return
	}

	assetTls, selected, flags, err := a.signIn()
	if err != nil {
		if errors.Is(err, errFingerprint) {
			logs.Info(fmt.Sprintf("Remote Desktop Gateway: refused to sign in to the asset: %s: %s", a.target.GetId(), err.Error()))
			return
		}
		logs.Info(fmt.Sprintf("Remote Desktop Gateway: failed to sign in to the asset: %s as %s, passing mstsc through: %s", a.target.GetId(), a.target.Username, err.Error()))
		_ = a.asset.Close()
		a.asset, err = object.DialAsset(a.target)
		if err != nil {
			return
		}
		a.passThrough(request)
		return
	}
	a.toClose = append(a.toClose, assetTls)

	_, err = a.client.Write(connectionConfirm(protocolSsl, flags&negResponseFlags))
	if err != nil {
		return
	}
	cert, _, err := object.GetRdpGatewayCertificate()
	if err != nil {
		logs.Error(fmt.Sprintf("Remote Desktop Gateway: %s", err.Error()))
		return
	}
	clientTls := tls.Server(a.client, &tls.Config{
		Certificates: []tls.Certificate{*cert},
		MinVersion:   tls.VersionTLS12,
		MaxVersion:   tls.VersionTLS12,
	})
	a.toClose = append(a.toClose, clientTls)
	err = clientTls.Handshake()
	if err != nil {
		return
	}
	_ = a.client.SetDeadline(time.Time{})
	_ = a.asset.SetDeadline(time.Time{})

	go func() {
		defer a.close()
		a.relayAsset(assetTls, clientTls, requested)
	}()
	a.relayClient(clientTls, assetTls, selected)
}

func (a *autoLogon) passThrough(request []byte) {
	_ = a.client.SetDeadline(time.Time{})
	_, err := a.asset.Write(request)
	if err != nil {
		return
	}
	go func() {
		defer a.close()
		_, _ = io.Copy(a.client, a.asset)
	}()
	_, _ = io.Copy(a.asset, a.client)
}

// signIn negotiates TLS, or CredSSP when the asset asks for it, with the asset.
func (a *autoLogon) signIn() (*tls.Conn, uint32, byte, error) {
	_ = a.asset.SetDeadline(time.Now().Add(autoLogonTimeout))
	_, err := a.asset.Write(connectionRequest(protocolSsl | protocolHybrid))
	if err != nil {
		return nil, 0, 0, err
	}
	confirm, err := readTpkt(a.asset)
	if err != nil {
		return nil, 0, 0, err
	}
	selected, flags, err := parseConnectionConfirm(confirm)
	if err != nil {
		return nil, 0, 0, err
	}

	conn := tls.Client(a.asset, &tls.Config{
		// RDP hosts use self-signed certificates, the fingerprint of the asset is checked instead
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
		// Windows 11 aborts CredSSP over TLS 1.3
		MaxVersion: tls.VersionTLS12,
	})
	err = conn.Handshake()
	if err != nil {
		return nil, 0, 0, err
	}
	fingerprint := getFingerprint(conn)
	pinned := strings.ToLower(strings.ReplaceAll(a.target.Fingerprint, ":", ""))
	if pinned != "" && fingerprint != pinned {
		_ = conn.Close()
		return nil, 0, 0, fmt.Errorf("%w: the server certificate %s does not match the pinned %s, clear the fingerprint of the asset if the server certificate was renewed", errFingerprint, fingerprint, pinned)
	}

	if selected == protocolHybrid {
		err = nla.Authenticate(conn, nla.Credentials{Username: a.target.Username, Password: a.target.Password})
		if err != nil {
			_ = conn.Close()
			return nil, 0, 0, err
		}
	}

	if pinned == "" && fingerprint != "" {
		err = object.UpdateAssetFingerprint(a.target.GetId(), fingerprint)
		if err != nil {
			logs.Error(fmt.Sprintf("Remote Desktop Gateway: failed to pin the certificate of the asset: %s: %s", a.target.GetId(), err.Error()))
		}
	}
	return conn, selected, flags, nil
}

// relayClient changes the protocol mstsc says the server selected to the one the asset selected, and
// puts the account into the Client Info PDU, then passes everything else.
func (a *autoLogon) relayClient(src io.Reader, dst io.Writer, selected uint32) {
	first := true
	for {
		frame, err := readTpkt(src)
		if err != nil {
			return
		}
		done := false
		if first {
			first = false
			if !patchUserData(frame, "Duca", csCore, csCoreSelectedProtocolOffset, selected) {
				logs.Info("Remote Desktop Gateway: the MCS Connect Initial of mstsc has no selected protocol")
			}
		} else if info := a.rewriteClientInfo(frame); info != nil {
			a.onSignedIn()
			frame = info
			done = true
		}
		_, err = dst.Write(frame)
		if err != nil {
			return
		}
		if done {
			break
		}
	}
	_, _ = io.Copy(dst, src)
}

// relayAsset changes the protocols the asset says the client requested to what mstsc requested.
func (a *autoLogon) relayAsset(src io.Reader, dst io.Writer, requested uint32) {
	frame, err := readTpkt(src)
	if err != nil {
		return
	}
	patchUserData(frame, "McDn", scCore, scCoreRequestedProtocolOffset, requested)
	_, err = dst.Write(frame)
	if err != nil {
		return
	}
	_, _ = io.Copy(dst, src)
}

// rewriteClientInfo returns the Client Info PDU with the account of the asset, or nil when the frame is not one.
func (a *autoLogon) rewriteClientInfo(frame []byte) []byte {
	if len(frame) < 14 || frame[7]>>2 != mcsSendDataRequest {
		return nil
	}
	header := frame[7:13]
	pos := 13
	if frame[pos]&0x80 != 0 {
		pos += 2
	} else {
		pos++
	}
	if pos+4+18 > len(frame) || binary.LittleEndian.Uint16(frame[pos:])&secInfoPkt == 0 {
		return nil
	}

	domain, username := "", a.target.Username
	if i := strings.Index(username, "\\"); i >= 0 {
		domain, username = username[:i], username[i+1:]
	}
	info, err := rebuildClientInfo(frame[pos+4:], domain, username, a.target.Password)
	if err != nil {
		logs.Info(fmt.Sprintf("Remote Desktop Gateway: failed to read the Client Info PDU of mstsc: %s", err.Error()))
		return nil
	}

	data := append(append([]byte{}, frame[pos:pos+4]...), info...)
	payload := append(append([]byte{}, header...), perLength(len(data))...)
	payload = append(payload, data...)
	res := []byte{3, 0, 0, 0, 2, 0xF0, 0x80}
	res = append(res, payload...)
	binary.BigEndian.PutUint16(res[2:], uint16(len(res)))
	return res
}

func rebuildClientInfo(info []byte, domain string, username string, password string) ([]byte, error) {
	flags := binary.LittleEndian.Uint32(info[4:])
	unicode := flags&infoUnicode != 0
	terminator := 1
	if unicode {
		terminator = 2
	}

	fields := make([][]byte, 5)
	pos := 18
	for i := range fields {
		n := int(binary.LittleEndian.Uint16(info[8+2*i:]))
		if pos+n+terminator > len(info) {
			return nil, fmt.Errorf("the field %d is out of the packet", i)
		}
		fields[i] = info[pos : pos+n]
		pos += n + terminator
	}
	fields[0] = encodeInfoString(domain, unicode)
	fields[1] = encodeInfoString(username, unicode)
	fields[2] = encodeInfoString(password, unicode)

	res := append([]byte{}, info[:4]...)
	res = binary.LittleEndian.AppendUint32(res, (flags|infoAutoLogon)&^infoPasswordIsScPin)
	for _, field := range fields {
		res = binary.LittleEndian.AppendUint16(res, uint16(len(field)))
	}
	for _, field := range fields {
		res = append(res, field...)
		res = append(res, make([]byte, terminator)...)
	}
	return append(res, info[pos:]...), nil
}

func encodeInfoString(s string, unicode bool) []byte {
	if !unicode {
		return []byte(s)
	}
	res := []byte{}
	for _, unit := range utf16.Encode([]rune(s)) {
		res = binary.LittleEndian.AppendUint16(res, unit)
	}
	return res
}

func perLength(n int) []byte {
	if n < 0x80 {
		return []byte{byte(n)}
	}
	return []byte{byte(n>>8) | 0x80, byte(n)}
}

// patchUserData sets a 32-bit field of a block in the GCC user data that follows the H.221 key.
func patchUserData(frame []byte, key string, blockType uint16, offset int, value uint32) bool {
	i := bytes.Index(frame, []byte(key))
	if i < 0 || i+5 > len(frame) {
		return false
	}
	pos := i + 5
	if frame[i+4]&0x80 != 0 {
		pos++
	}
	for pos+4 <= len(frame) {
		kind := binary.LittleEndian.Uint16(frame[pos:])
		n := int(binary.LittleEndian.Uint16(frame[pos+2:]))
		if n < 4 || pos+n > len(frame) {
			return false
		}
		if kind == blockType {
			if n < offset+4 {
				return false
			}
			binary.LittleEndian.PutUint32(frame[pos+offset:], value)
			return true
		}
		pos += n
	}
	return false
}

func readTpkt(r io.Reader) ([]byte, error) {
	header := make([]byte, 4)
	_, err := io.ReadFull(r, header)
	if err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(header[2:]))
	if header[0] != 3 || n < 7 {
		return nil, fmt.Errorf("not a TPKT packet")
	}
	res := make([]byte, n)
	copy(res, header)
	_, err = io.ReadFull(r, res[4:])
	return res, err
}

// parseConnectionRequest returns the protocols mstsc asks for in the X.224 Connection Request.
func parseConnectionRequest(frame []byte) (uint32, bool) {
	if len(frame) < 11 || frame[5]&0xF0 != tpduConnectionRequest {
		return 0, false
	}
	end := 5 + int(frame[4])
	if end > len(frame) {
		return 0, false
	}
	if end-8 >= 11 && frame[end-8] == negRequest && binary.LittleEndian.Uint16(frame[end-6:]) == 8 {
		return binary.LittleEndian.Uint32(frame[end-4:]), true
	}
	return 0, true
}

func parseConnectionConfirm(frame []byte) (uint32, byte, error) {
	if len(frame) < 11 || frame[5]&0xF0 != tpduConnectionConfirm {
		return 0, 0, fmt.Errorf("the asset did not confirm the connection")
	}
	if len(frame) < 19 {
		return 0, 0, fmt.Errorf("the asset only supports the standard RDP security")
	}
	value := binary.LittleEndian.Uint32(frame[15:])
	switch frame[11] {
	case negResponse:
		if value != protocolSsl && value != protocolHybrid {
			return 0, 0, fmt.Errorf("the asset selected the security protocol: %d", value)
		}
		return value, frame[12], nil
	case negFailure:
		return 0, 0, fmt.Errorf("the asset refused the negotiation with the failure code: %d", value)
	default:
		return 0, 0, fmt.Errorf("unexpected negotiation type: %d", frame[11])
	}
}

func connectionRequest(protocols uint32) []byte {
	res := []byte{3, 0, 0, 19, 14, tpduConnectionRequest, 0, 0, 0, 0, 0, negRequest, 0, 8, 0}
	return binary.LittleEndian.AppendUint32(res, protocols)
}

func connectionConfirm(protocol uint32, flags byte) []byte {
	res := []byte{3, 0, 0, 19, 14, tpduConnectionConfirm, 0, 0, 0x12, 0x34, 0, negResponse, flags, 8, 0}
	return binary.LittleEndian.AppendUint32(res, protocol)
}

func getFingerprint(conn *tls.Conn) string {
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return ""
	}
	sum := sha256.Sum256(certs[0].Raw)
	return hex.EncodeToString(sum[:])
}
