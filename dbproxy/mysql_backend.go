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
	"bufio"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/casvisor/casvisor/object"
)

// mysqlBackend is the connection of the proxy to the MySQL server of an asset, signed in and ready for commands.
type mysqlBackend struct {
	conn     net.Conn
	r        *bufio.Reader
	w        *bufio.Writer
	threadId uint32
	version  string
	// ok is the OK packet that ended the sign-in, which the client gets as its own
	ok []byte
}

type mysqlHandshake struct {
	capabilities uint32
	charset      byte
	database     string
	attrs        []byte
}

// connectMysqlBackend signs in to the server with the account of the asset, asking for the capabilities
// the client was given, so that the packets of the client and the server mean the same to both.
func connectMysqlBackend(client *object.DatabaseClient, asset *object.Asset, hs *mysqlHandshake) (*mysqlBackend, error) {
	conn, err := client.Dial()
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(handshakeTimeout))

	b := &mysqlBackend{conn: conn, r: bufio.NewReader(conn), w: bufio.NewWriter(conn)}
	err = b.signIn(client, asset, hs)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = b.conn.SetDeadline(time.Time{})
	return b, nil
}

func (b *mysqlBackend) write(payload []byte, seq byte) (byte, error) {
	next, err := writeMysqlPacket(b.w, payload, seq)
	if err != nil {
		return next, err
	}
	return next, b.w.Flush()
}

func (b *mysqlBackend) signIn(client *object.DatabaseClient, asset *object.Asset, hs *mysqlHandshake) error {
	greeting, err := readMysqlPacket(b.r)
	if err != nil {
		return err
	}
	p := greeting.payload
	if len(p) > 0 && p[0] == 0xff {
		return fmt.Errorf("%s", parseMysqlErr(p))
	}
	if len(p) == 0 || p[0] != 10 {
		return fmt.Errorf("the database server speaks an unknown version of the MySQL protocol")
	}

	b.version, p = readNulString(p[1:])
	if len(p) < 4+8+1+2 {
		return fmt.Errorf("the greeting of the database server is too short")
	}
	b.threadId = binary.LittleEndian.Uint32(p)
	scramble := append([]byte{}, p[4:12]...)
	serverCapabilities := uint32(binary.LittleEndian.Uint16(p[13:15]))
	p = p[15:]
	plugin := mysqlNativePassword
	if len(p) >= 16 {
		serverCapabilities |= uint32(binary.LittleEndian.Uint16(p[3:5])) << 16
		authDataLength := int(p[5])
		p = p[16:]
		if serverCapabilities&clientSecureConnection != 0 {
			n := max(13, authDataLength-8)
			if len(p) < n {
				return fmt.Errorf("the greeting of the database server is too short")
			}
			scramble = append(scramble, p[:12]...)
			p = p[n:]
		}
		if serverCapabilities&clientPluginAuth != 0 {
			plugin, _ = readNulString(p)
		}
	}
	if serverCapabilities&clientProtocol41 == 0 {
		return fmt.Errorf("the database server is too old, it doesn't speak the MySQL protocol 4.1")
	}

	capabilities := hs.capabilities&mysqlProxyCapabilities&serverCapabilities | clientProtocol41
	capabilities &^= clientConnectWithDb | clientConnectAttrs | clientSsl
	if hs.database != "" && serverCapabilities&clientConnectWithDb != 0 {
		capabilities |= clientConnectWithDb
	}
	capabilities |= serverCapabilities & (clientSecureConnection | clientPluginAuth | clientPluginAuthLenencClientData | clientConnectAttrs)

	seq := greeting.seq + 1
	isTls := false
	if serverCapabilities&clientSsl != 0 {
		capabilities |= clientSsl
		request := binary.LittleEndian.AppendUint32(nil, capabilities)
		request = binary.LittleEndian.AppendUint32(request, mysqlMaxPacketSize)
		request = append(request, hs.charset)
		request = append(request, make([]byte, 23)...)
		seq, err = b.write(request, seq)
		if err != nil {
			return err
		}
		tlsConn := tls.Client(&bufferedConn{Conn: b.conn, r: b.r}, client.GetTlsConfig())
		err = tlsConn.Handshake()
		if err != nil {
			return fmt.Errorf("the TLS connection to the database server of the asset: %s failed: %s", asset.GetId(), err.Error())
		}
		b.conn = tlsConn
		b.r = bufio.NewReader(tlsConn)
		b.w = bufio.NewWriter(tlsConn)
		isTls = true
	} else if client.IsFingerprintPinned() {
		return fmt.Errorf("the database server of the asset: %s no longer has TLS, while the public key of its certificate is pinned", asset.GetId())
	} else {
		client.SetPlainText()
	}

	password := asset.Password
	authResponse, err := getMysqlAuthResponse(plugin, scramble, password, isTls)
	if err != nil {
		plugin = mysqlNativePassword
		authResponse = scrambleNativePassword(scramble, password)
	}

	response := binary.LittleEndian.AppendUint32(nil, capabilities)
	response = binary.LittleEndian.AppendUint32(response, mysqlMaxPacketSize)
	response = append(response, hs.charset)
	response = append(response, make([]byte, 23)...)
	response = append(append(response, asset.Username...), 0)
	switch {
	case capabilities&clientPluginAuthLenencClientData != 0:
		response = appendLenencString(response, authResponse)
	case capabilities&clientSecureConnection != 0:
		response = append(append(response, byte(len(authResponse))), authResponse...)
	default:
		response = append(append(response, authResponse...), 0)
	}
	if capabilities&clientConnectWithDb != 0 {
		response = append(append(response, hs.database...), 0)
	}
	if capabilities&clientPluginAuth != 0 {
		response = append(append(response, plugin...), 0)
	}
	if capabilities&clientConnectAttrs != 0 {
		response = appendLenencString(response, hs.attrs)
	}
	seq, err = b.write(response, seq)
	if err != nil {
		return err
	}

	for {
		packet, err := readMysqlPacket(b.r)
		if err != nil {
			return err
		}
		seq = packet.lastSeq + 1
		p := packet.payload
		if len(p) == 0 {
			return fmt.Errorf("the database server sent an empty packet")
		}

		switch p[0] {
		case 0x00:
			b.ok = p
			if capabilities&clientConnectWithDb == 0 && hs.database != "" {
				err = b.useDatabase(hs.database)
				if err != nil {
					return err
				}
			}
			return client.PinFingerprint()
		case 0xff:
			return fmt.Errorf("%s", parseMysqlErr(p))
		case 0xfe:
			if len(p) == 1 {
				return fmt.Errorf("the database server asks for the old password authentication, which is not supported")
			}
			var data []byte
			plugin, data = readNulString(p[1:])
			if len(data) >= 20 {
				scramble = append([]byte{}, data[:20]...)
			}
			authResponse, err = getMysqlAuthResponse(plugin, scramble, password, isTls)
			if err != nil {
				return err
			}
			_, err = b.write(authResponse, seq)
		case 0x01:
			if len(p) < 2 {
				return fmt.Errorf("the database server sent a broken packet")
			}
			switch {
			case plugin == mysqlCachingSha2Password && len(p) == 2 && p[1] == 3:
				// the server had the password in its cache, the OK packet follows
				continue
			case plugin == mysqlCachingSha2Password && len(p) == 2 && p[1] == 4 && isTls:
				_, err = b.write(append([]byte(password), 0), seq)
			case plugin == mysqlCachingSha2Password && len(p) == 2 && p[1] == 4:
				// ask for the public key of the server to send the password encrypted
				_, err = b.write([]byte{2}, seq)
			default:
				var encrypted []byte
				encrypted, err = encryptMysqlPassword(password, scramble, p[1:])
				if err != nil {
					return err
				}
				_, err = b.write(encrypted, seq)
			}
		default:
			return fmt.Errorf("the database server sent an unexpected packet while signing in")
		}
		if err != nil {
			return err
		}
	}
}

// getMysqlAuthResponse answers the scramble of the server with the password, for the plugin the server asks for.
func getMysqlAuthResponse(plugin string, scramble []byte, password string, isTls bool) ([]byte, error) {
	switch plugin {
	case mysqlNativePassword:
		return scrambleNativePassword(scramble, password), nil
	case mysqlCachingSha2Password:
		return scrambleSha256Password(scramble, password), nil
	case mysqlSha256Password:
		if password == "" {
			return []byte{0}, nil
		}
		if isTls {
			return append([]byte(password), 0), nil
		}
		// ask for the public key of the server
		return []byte{1}, nil
	case mysqlClearPassword:
		if !isTls {
			return nil, fmt.Errorf("the database server asks for the password in clear text, which is only sent over TLS")
		}
		return append([]byte(password), 0), nil
	}
	return nil, fmt.Errorf("the database server asks for the authentication plugin: %s, which is not supported", plugin)
}

// useDatabase switches to the database the client asked for, for a server that can't take it when signing in.
func (b *mysqlBackend) useDatabase(database string) error {
	_, err := b.write(append([]byte{comInitDb}, database...), 0)
	if err != nil {
		return err
	}
	packet, err := readMysqlPacket(b.r)
	if err != nil {
		return err
	}
	if len(packet.payload) > 0 && packet.payload[0] == 0xff {
		return fmt.Errorf("%s", parseMysqlErr(packet.payload))
	}
	return nil
}
