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
	"crypto/des"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	securityInvalid = 0
	securityNone    = 1
	securityVncAuth = 2

	protocolVersion = "RFB 003.008\n"
)

// DialServer connects to a VNC server and finishes the RFB handshake up to the
// point where the client sends ClientInit, authenticating with the password.
func DialServer(address string, password string, timeout time.Duration) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return nil, err
	}

	_ = conn.SetDeadline(time.Now().Add(timeout))
	err = handshakeServer(conn, password)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})

	return conn, nil
}

func handshakeServer(conn io.ReadWriter, password string) error {
	minor, err := readVersion(conn)
	if err != nil {
		return err
	}

	var version string
	switch {
	case minor >= 8:
		minor = 8
		version = "RFB 003.008\n"
	case minor == 7:
		version = "RFB 003.007\n"
	default:
		minor = 3
		version = "RFB 003.003\n"
	}

	_, err = conn.Write([]byte(version))
	if err != nil {
		return err
	}

	var securityType uint32
	if minor == 3 {
		securityType, err = readUint32(conn)
		if err != nil {
			return err
		}
		if securityType == securityInvalid {
			return readReason(conn)
		}
	} else {
		count := make([]byte, 1)
		_, err = io.ReadFull(conn, count)
		if err != nil {
			return err
		}
		if count[0] == 0 {
			return readReason(conn)
		}

		types := make([]byte, count[0])
		_, err = io.ReadFull(conn, types)
		if err != nil {
			return err
		}

		securityType = chooseSecurityType(types, password)
		if securityType == securityInvalid {
			return fmt.Errorf("the VNC server requires an unsupported security type: %v", types)
		}

		_, err = conn.Write([]byte{byte(securityType)})
		if err != nil {
			return err
		}
	}

	switch securityType {
	case securityNone:
		if minor < 8 {
			return nil
		}
	case securityVncAuth:
		challenge := make([]byte, 16)
		_, err = io.ReadFull(conn, challenge)
		if err != nil {
			return err
		}

		response, err := encryptChallenge(challenge, password)
		if err != nil {
			return err
		}

		_, err = conn.Write(response)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("the VNC server requires an unsupported security type: %d", securityType)
	}

	result, err := readUint32(conn)
	if err != nil {
		return err
	}
	if result != 0 {
		if minor >= 8 {
			return readReason(conn)
		}
		return fmt.Errorf("VNC authentication failed")
	}

	return nil
}

func chooseSecurityType(types []byte, password string) uint32 {
	hasNone := false
	hasVncAuth := false
	for _, t := range types {
		switch t {
		case securityNone:
			hasNone = true
		case securityVncAuth:
			hasVncAuth = true
		}
	}

	if hasVncAuth && (password != "" || !hasNone) {
		return securityVncAuth
	}
	if hasNone {
		return securityNone
	}
	return securityInvalid
}

// AcceptClient finishes the RFB handshake with the browser, offering no
// authentication since the gateway has already authenticated to the server.
func AcceptClient(conn io.ReadWriter) error {
	_, err := conn.Write([]byte(protocolVersion))
	if err != nil {
		return err
	}

	_, err = readVersion(conn)
	if err != nil {
		return err
	}

	_, err = conn.Write([]byte{1, securityNone})
	if err != nil {
		return err
	}

	chosen := make([]byte, 1)
	_, err = io.ReadFull(conn, chosen)
	if err != nil {
		return err
	}
	if chosen[0] != securityNone {
		return fmt.Errorf("the client chose an unsupported security type: %d", chosen[0])
	}

	return binary.Write(conn, binary.BigEndian, uint32(0))
}

// RejectClient ends the RFB handshake with the browser with a failure reason.
func RejectClient(conn io.ReadWriter, reason string) error {
	_, err := conn.Write([]byte(protocolVersion))
	if err != nil {
		return err
	}

	_, err = readVersion(conn)
	if err != nil {
		return err
	}

	message := make([]byte, 5+len(reason))
	binary.BigEndian.PutUint32(message[1:5], uint32(len(reason)))
	copy(message[5:], reason)
	_, err = conn.Write(message)
	return err
}

func readVersion(conn io.Reader) (int, error) {
	buf := make([]byte, 12)
	_, err := io.ReadFull(conn, buf)
	if err != nil {
		return 0, err
	}

	var major, minor int
	_, err = fmt.Sscanf(string(buf), "RFB %03d.%03d\n", &major, &minor)
	if err != nil || major != 3 {
		return 0, fmt.Errorf("unsupported RFB protocol version: %q", string(buf))
	}

	return minor, nil
}

func readUint32(conn io.Reader) (uint32, error) {
	var value uint32
	err := binary.Read(conn, binary.BigEndian, &value)
	return value, err
}

func readReason(conn io.Reader) error {
	length, err := readUint32(conn)
	if err != nil {
		return err
	}
	if length > 4096 {
		return fmt.Errorf("the VNC server refused the connection")
	}

	reason := make([]byte, length)
	_, err = io.ReadFull(conn, reason)
	if err != nil {
		return err
	}

	return fmt.Errorf("the VNC server refused the connection: %s", string(reason))
}

func encryptChallenge(challenge []byte, password string) ([]byte, error) {
	key := make([]byte, 8)
	copy(key, password)
	for i, b := range key {
		key[i] = reverseBits(b)
	}

	block, err := des.NewCipher(key)
	if err != nil {
		return nil, err
	}

	response := make([]byte, len(challenge))
	for i := 0; i < len(challenge); i += block.BlockSize() {
		block.Encrypt(response[i:i+block.BlockSize()], challenge[i:i+block.BlockSize()])
	}

	return response, nil
}

func reverseBits(b byte) byte {
	var r byte
	for i := 0; i < 8; i++ {
		r = r<<1 | b&1
		b >>= 1
	}
	return r
}
