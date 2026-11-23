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

package dbproxy

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"io"
)

const (
	clientLongPassword               = 1
	clientFoundRows                  = 1 << 1
	clientLongFlag                   = 1 << 2
	clientConnectWithDb              = 1 << 3
	clientIgnoreSpace                = 1 << 8
	clientProtocol41                 = 1 << 9
	clientInteractive                = 1 << 10
	clientSsl                        = 1 << 11
	clientTransactions               = 1 << 13
	clientSecureConnection           = 1 << 15
	clientMultiStatements            = 1 << 16
	clientMultiResults               = 1 << 17
	clientPsMultiResults             = 1 << 18
	clientPluginAuth                 = 1 << 19
	clientConnectAttrs               = 1 << 20
	clientPluginAuthLenencClientData = 1 << 21

	// the capabilities of the proxy; without CLIENT_DEPRECATE_EOF, a result set always ends with an EOF packet,
	// and without CLIENT_LOCAL_FILES and CLIENT_COMPRESS, nothing else comes in between
	mysqlProxyCapabilities = clientLongPassword | clientFoundRows | clientLongFlag | clientConnectWithDb | clientIgnoreSpace |
		clientProtocol41 | clientInteractive | clientTransactions | clientSecureConnection | clientMultiStatements |
		clientMultiResults | clientPsMultiResults | clientPluginAuth | clientConnectAttrs | clientPluginAuthLenencClientData

	serverStatusAutocommit         = 0x0002
	serverMoreResultsExist         = 0x0008
	serverStatusCursorExists       = 0x0040
	mysqlMaxPacketSize             = 1<<24 - 1
	mysqlNativePassword            = "mysql_native_password"
	mysqlCachingSha2Password       = "caching_sha2_password"
	mysqlSha256Password            = "sha256_password"
	mysqlClearPassword             = "mysql_clear_password"
	mysqlDefaultCollationUtf8mb4   = 45
	mysqlErrAccessDenied           = 1045
	mysqlErrUnknown                = 1105
	mysqlErrSpecificAccessDenied   = 1227
	mysqlErrNotSupportedYet        = 1235
	mysqlStateAccessDenied         = "28000"
	mysqlStateGeneral              = "HY000"
	mysqlStateSyntaxOrAccess       = "42000"
	mysqlMaxCommandPacketForMemory = 1 << 30
)

// The commands of the MySQL protocol.
const (
	comQuit             = 0x01
	comInitDb           = 0x02
	comQuery            = 0x03
	comFieldList        = 0x04
	comRefresh          = 0x07
	comShutdown         = 0x08
	comStatistics       = 0x09
	comProcessInfo      = 0x0a
	comProcessKill      = 0x0c
	comDebug            = 0x0d
	comPing             = 0x0e
	comChangeUser       = 0x11
	comStmtPrepare      = 0x16
	comStmtExecute      = 0x17
	comStmtSendLongData = 0x18
	comStmtClose        = 0x19
	comStmtReset        = 0x1a
	comSetOption        = 0x1b
	comStmtFetch        = 0x1c
	comResetConnection  = 0x1f
)

// mysqlPacket is a packet of the MySQL protocol, with the packets a payload of 16 MB or more is split into put together.
type mysqlPacket struct {
	payload []byte
	seq     byte
	// lastSeq is the sequence id of the last packet the payload came in
	lastSeq byte
}

func readMysqlPacket(r io.Reader) (*mysqlPacket, error) {
	res := &mysqlPacket{}
	header := make([]byte, 4)
	for first := true; ; first = false {
		_, err := io.ReadFull(r, header)
		if err != nil {
			return nil, err
		}
		length := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
		if first {
			res.seq = header[3]
		}
		res.lastSeq = header[3]
		if len(res.payload)+length > mysqlMaxCommandPacketForMemory {
			return nil, fmt.Errorf("the packet is too large")
		}
		start := len(res.payload)
		res.payload = append(res.payload, make([]byte, length)...)
		_, err = io.ReadFull(r, res.payload[start:])
		if err != nil {
			return nil, err
		}
		if length < mysqlMaxPacketSize {
			return res, nil
		}
	}
}

// writeMysqlPacket writes the payload split into packets the way it was read, and returns the next sequence id.
func writeMysqlPacket(w io.Writer, payload []byte, seq byte) (byte, error) {
	for {
		length := len(payload)
		if length > mysqlMaxPacketSize {
			length = mysqlMaxPacketSize
		}
		header := []byte{byte(length), byte(length >> 8), byte(length >> 16), seq}
		_, err := w.Write(append(header, payload[:length]...))
		if err != nil {
			return seq, err
		}
		seq++
		payload = payload[length:]
		if length < mysqlMaxPacketSize {
			return seq, nil
		}
	}
}

func appendLenencInt(b []byte, n uint64) []byte {
	switch {
	case n < 251:
		return append(b, byte(n))
	case n < 1<<16:
		return append(b, 0xfc, byte(n), byte(n>>8))
	case n < 1<<24:
		return append(b, 0xfd, byte(n), byte(n>>8), byte(n>>16))
	default:
		return append(append(b, 0xfe), binary.LittleEndian.AppendUint64(nil, n)...)
	}
}

func appendLenencString(b []byte, s []byte) []byte {
	return append(appendLenencInt(b, uint64(len(s))), s...)
}

// readLenencInt reads a length-encoded integer, returning the number and the bytes it took, or 0 bytes when it is broken.
func readLenencInt(b []byte) (uint64, int) {
	if len(b) == 0 {
		return 0, 0
	}
	switch b[0] {
	case 0xfb:
		return 0, 1
	case 0xfc:
		if len(b) < 3 {
			return 0, 0
		}
		return uint64(b[1]) | uint64(b[2])<<8, 3
	case 0xfd:
		if len(b) < 4 {
			return 0, 0
		}
		return uint64(b[1]) | uint64(b[2])<<8 | uint64(b[3])<<16, 4
	case 0xfe:
		if len(b) < 9 {
			return 0, 0
		}
		return binary.LittleEndian.Uint64(b[1:9]), 9
	case 0xff:
		return 0, 0
	}
	return uint64(b[0]), 1
}

// readNulString reads a string ending with a NUL byte, or with the end of the data.
func readNulString(b []byte) (string, []byte) {
	i := bytes.IndexByte(b, 0)
	if i < 0 {
		return string(b), nil
	}
	return string(b[:i]), b[i+1:]
}

func makeMysqlErr(code uint16, state string, message string) []byte {
	b := []byte{0xff, byte(code), byte(code >> 8), '#'}
	b = append(b, state...)
	return append(b, message...)
}

// parseMysqlErr reads the message of an ERR packet.
func parseMysqlErr(payload []byte) string {
	if len(payload) < 3 {
		return "unknown error"
	}
	code := uint16(payload[1]) | uint16(payload[2])<<8
	message := payload[3:]
	if len(message) > 0 && message[0] == '#' && len(message) >= 6 {
		message = message[6:]
	}
	return fmt.Sprintf("Error %d: %s", code, string(message))
}

func isMysqlEof(payload []byte) bool {
	return len(payload) > 0 && payload[0] == 0xfe && len(payload) < 9
}

// getMysqlEofStatus reads the status flags of an EOF packet.
func getMysqlEofStatus(payload []byte) uint16 {
	if len(payload) < 5 {
		return 0
	}
	return uint16(payload[3]) | uint16(payload[4])<<8
}

// parseMysqlOk reads the affected rows and the status flags of an OK packet.
func parseMysqlOk(payload []byte) (uint64, uint16) {
	rest := payload[1:]
	affected, n := readLenencInt(rest)
	if n == 0 {
		return 0, 0
	}
	rest = rest[n:]
	_, n = readLenencInt(rest)
	if n == 0 || len(rest) < n+2 {
		return affected, 0
	}
	rest = rest[n:]
	return affected, uint16(rest[0]) | uint16(rest[1])<<8
}

// scrambleNativePassword is SHA1(password) XOR SHA1(scramble + SHA1(SHA1(password))).
func scrambleNativePassword(scramble []byte, password string) []byte {
	if password == "" {
		return nil
	}
	stage1 := sha1.Sum([]byte(password))
	stage2 := sha1.Sum(stage1[:])
	h := sha1.New()
	h.Write(scramble[:20])
	h.Write(stage2[:])
	res := h.Sum(nil)
	for i := range res {
		res[i] ^= stage1[i]
	}
	return res
}

// scrambleSha256Password is SHA256(password) XOR SHA256(SHA256(SHA256(password)) + scramble).
func scrambleSha256Password(scramble []byte, password string) []byte {
	if password == "" {
		return nil
	}
	stage1 := sha256.Sum256([]byte(password))
	stage2 := sha256.Sum256(stage1[:])
	h := sha256.New()
	h.Write(stage2[:])
	h.Write(scramble[:20])
	res := h.Sum(nil)
	for i := range res {
		res[i] ^= stage1[i]
	}
	return res
}

// checkMysqlAuthResponse tells whether the client proved it knows the password with the plugin.
func checkMysqlAuthResponse(plugin string, scramble []byte, response []byte, password string) bool {
	var expected []byte
	switch plugin {
	case mysqlNativePassword:
		expected = scrambleNativePassword(scramble, password)
	case mysqlCachingSha2Password:
		expected = scrambleSha256Password(scramble, password)
	default:
		return false
	}
	return len(expected) > 0 && subtle.ConstantTimeCompare(expected, response) == 1
}

// encryptMysqlPassword encrypts the password for a server without TLS with its RSA public key.
func encryptMysqlPassword(password string, scramble []byte, publicKeyPem []byte) ([]byte, error) {
	block, _ := pem.Decode(publicKeyPem)
	if block == nil {
		return nil, fmt.Errorf("the database server sent an invalid public key")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	publicKey, ok := key.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("the public key of the database server is not an RSA key")
	}

	plain := append([]byte(password), 0)
	for i := range plain {
		plain[i] ^= scramble[i%len(scramble)]
	}
	return rsa.EncryptOAEP(sha1.New(), rand.Reader, publicKey, plain, nil)
}

// newScramble makes the 20 printable random bytes a server sends to check a password with.
func newScramble() []byte {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = b[i]%94 + 33
	}
	return b
}
