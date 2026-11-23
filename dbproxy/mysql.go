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
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/object"
)

// the KILL statement mysql sends from a new connection when Ctrl+C stops a query
var mysqlKillStatement = regexp.MustCompile(`(?i)^\s*KILL\s+(?:(QUERY|CONNECTION)\s+)?(\d+)\s*;?\s*$`)

type mysqlConn struct {
	*proxyConn
	r       *bufio.Reader
	w       *bufio.Writer
	backend *mysqlBackend
}

func getMysqlServerVersion() string {
	if version := conf.GetConfigString("dbProxyMysqlVersion"); version != "" {
		return version
	}
	return "8.0.40-casvisor"
}

func (m *mysqlConn) write(payload []byte, seq byte) error {
	_, err := writeMysqlPacket(m.w, payload, seq)
	if err != nil {
		return err
	}
	return m.w.Flush()
}

func (m *mysqlConn) fail(seq byte, code uint16, state string, message string) {
	_ = m.write(makeMysqlErr(code, state, "casvisor: "+message), seq)
}

func serveMysql(conn net.Conn) {
	m := &mysqlConn{proxyConn: newProxyConn(object.ProtocolMysql, conn), r: bufio.NewReader(conn), w: bufio.NewWriter(conn)}
	defer func() {
		m.close("Normal user exit")
	}()
	_ = conn.SetDeadline(time.Now().Add(handshakeTimeout))

	err := m.handshake()
	if err != nil {
		return
	}
	_ = m.conn.SetDeadline(time.Time{})
	m.relay()
}

// handshake signs in the client with its database token and signs in to the server of the asset.
func (m *mysqlConn) handshake() error {
	scramble := newScramble()
	capabilities := uint32(mysqlProxyCapabilities)
	if tlsConfig != nil {
		capabilities |= clientSsl
	}

	greeting := []byte{10}
	greeting = append(append(greeting, getMysqlServerVersion()...), 0)
	greeting = binary.LittleEndian.AppendUint32(greeting, m.id)
	greeting = append(append(greeting, scramble[:8]...), 0)
	greeting = binary.LittleEndian.AppendUint16(greeting, uint16(capabilities))
	greeting = append(greeting, mysqlDefaultCollationUtf8mb4)
	greeting = binary.LittleEndian.AppendUint16(greeting, serverStatusAutocommit)
	greeting = binary.LittleEndian.AppendUint16(greeting, uint16(capabilities>>16))
	greeting = append(greeting, 21)
	greeting = append(greeting, make([]byte, 10)...)
	greeting = append(append(greeting, scramble[8:]...), 0)
	greeting = append(append(greeting, mysqlCachingSha2Password...), 0)
	err := m.write(greeting, 0)
	if err != nil {
		return err
	}

	packet, err := readMysqlPacket(m.r)
	if err != nil {
		return err
	}
	p := packet.payload
	if len(p) < 32 {
		return fmt.Errorf("the handshake response is too short")
	}
	clientCapabilities := binary.LittleEndian.Uint32(p)
	if clientCapabilities&clientSsl != 0 && len(p) == 32 {
		if tlsConfig == nil {
			return fmt.Errorf("TLS is off")
		}
		tlsConn := tls.Server(&bufferedConn{Conn: m.conn, r: m.r}, tlsConfig)
		err = tlsConn.Handshake()
		if err != nil {
			return err
		}
		m.conn = tlsConn
		m.r = bufio.NewReader(tlsConn)
		m.w = bufio.NewWriter(tlsConn)
		packet, err = readMysqlPacket(m.r)
		if err != nil {
			return err
		}
		p = packet.payload
		if len(p) < 32 {
			return fmt.Errorf("the handshake response is too short")
		}
		clientCapabilities = binary.LittleEndian.Uint32(p)
	}
	seq := packet.lastSeq + 1
	if clientCapabilities&clientProtocol41 == 0 {
		m.fail(seq, mysqlErrNotSupportedYet, mysqlStateGeneral, "the client is too old, it doesn't speak the MySQL protocol 4.1")
		return fmt.Errorf("old client")
	}

	hs := &mysqlHandshake{capabilities: clientCapabilities, charset: p[8]}
	username, rest := readNulString(p[32:])
	var authResponse []byte
	switch {
	case clientCapabilities&clientPluginAuthLenencClientData != 0:
		n, size := readLenencInt(rest)
		if size == 0 || uint64(len(rest)-size) < n {
			return fmt.Errorf("broken handshake response")
		}
		authResponse, rest = rest[size:size+int(n)], rest[size+int(n):]
	case clientCapabilities&clientSecureConnection != 0:
		if len(rest) < 1 || len(rest) < 1+int(rest[0]) {
			return fmt.Errorf("broken handshake response")
		}
		authResponse, rest = rest[1:1+int(rest[0])], rest[1+int(rest[0]):]
	default:
		var s string
		s, rest = readNulString(rest)
		authResponse = []byte(s)
	}
	if clientCapabilities&clientConnectWithDb != 0 {
		hs.database, rest = readNulString(rest)
	}
	plugin := mysqlNativePassword
	if clientCapabilities&clientPluginAuth != 0 {
		plugin, rest = readNulString(rest)
	}
	if clientCapabilities&clientConnectAttrs != 0 {
		n, size := readLenencInt(rest)
		if size > 0 && uint64(len(rest)-size) >= n {
			hs.attrs = rest[size : size+int(n)]
		}
	}
	m.userAgent = getMysqlUserAgent(hs.attrs)

	// a client that answers with another plugin is asked for the native password, which every client has; a client
	// that picked the native password itself waits to be asked too, as the scramble was sent for another plugin
	if plugin != mysqlCachingSha2Password {
		plugin = mysqlNativePassword
		request := append([]byte{0xfe}, plugin...)
		request = append(append(append(request, 0), scramble...), 0)
		err = m.write(request, seq)
		if err != nil {
			return err
		}
		packet, err = readMysqlPacket(m.r)
		if err != nil {
			return err
		}
		authResponse = packet.payload
		seq = packet.lastSeq + 1
	}

	token := object.GetDbToken(username)
	if token == nil || !checkMysqlAuthResponse(plugin, scramble, authResponse, username) {
		m.fail(seq, mysqlErrAccessDenied, mysqlStateAccessDenied, "access denied, sign in with a database token from Casvisor as both the user and the password; the token may have expired or been revoked")
		return fmt.Errorf("access denied")
	}
	if plugin == mysqlCachingSha2Password {
		// fast authentication: the password is known, as the token is
		err = m.write([]byte{0x01, 0x03}, seq)
		if err != nil {
			return err
		}
		seq++
	}

	err = m.authorize(token)
	if err == nil {
		err = m.checkLoginRules(nil)
	}
	if err != nil {
		m.fail(seq, mysqlErrAccessDenied, mysqlStateAccessDenied, err.Error())
		return err
	}

	if hs.database == "" {
		hs.database = m.asset.Database
	}
	hs.attrs = appendMysqlAttr(hs.attrs, "casvisor_user", m.user.Owner+"/"+m.user.Name)
	m.client = object.NewNativeDatabaseClient(m.asset)
	m.backend, err = connectMysqlBackend(m.client, m.asset, hs)
	if err != nil {
		m.fail(seq, mysqlErrUnknown, mysqlStateGeneral, fmt.Sprintf("failed to connect to the database of the asset: %s: %s", m.asset.Name, err.Error()))
		return err
	}

	err = m.openSession(m.backend.conn)
	if err != nil {
		m.fail(seq, mysqlErrUnknown, mysqlStateGeneral, err.Error())
		return err
	}
	m.cancelQuery = func() {
		m.killBackendQuery()
	}

	err = m.write(m.backend.ok, seq)
	if err != nil {
		return err
	}
	return nil
}

// getMysqlUserAgent names the client from its connection attributes, such as "mysql 8.4.3" or "DBeaver".
func getMysqlUserAgent(attrs []byte) string {
	values := map[string]string{}
	for len(attrs) > 0 {
		n, size := readLenencInt(attrs)
		if size == 0 || uint64(len(attrs)-size) < n {
			break
		}
		key := string(attrs[size : size+int(n)])
		attrs = attrs[size+int(n):]
		n, size = readLenencInt(attrs)
		if size == 0 || uint64(len(attrs)-size) < n {
			break
		}
		values[key] = string(attrs[size : size+int(n)])
		attrs = attrs[size+int(n):]
	}

	name := values["program_name"]
	if name == "" {
		name = values["_client_name"]
	}
	if version := values["_client_version"]; version != "" {
		name = strings.TrimSpace(name + " " + version)
	}
	if name == "" {
		return "MySQL client"
	}
	return name
}

func appendMysqlAttr(attrs []byte, key string, value string) []byte {
	res := append([]byte{}, attrs...)
	res = appendLenencString(res, []byte(key))
	return appendLenencString(res, []byte(value))
}

// killBackendQuery stops the statement the server runs for the connection, from a connection of its own.
func (m *mysqlConn) killBackendQuery() {
	if m.backend == nil {
		return
	}
	hs := &mysqlHandshake{capabilities: mysqlProxyCapabilities, charset: mysqlDefaultCollationUtf8mb4}
	b, err := connectMysqlBackend(object.NewNativeDatabaseClient(m.asset), m.asset, hs)
	if err != nil {
		return
	}
	defer b.conn.Close()
	_, err = b.write(append([]byte{comQuery}, fmt.Sprintf("KILL QUERY %d", m.backend.threadId)...), 0)
	if err == nil {
		_, _ = readMysqlPacket(b.r)
	}
}

func makeMysqlOk(status uint16) []byte {
	return []byte{0x00, 0x00, 0x00, byte(status), byte(status >> 8), 0x00, 0x00}
}

// relay passes the commands of the client to the server and the responses back, one command at a time.
func (m *mysqlConn) relay() {
	packets := make(chan *mysqlPacket, 16)
	go func() {
		defer close(packets)
		for {
			packet, err := readMysqlPacket(m.r)
			if err != nil {
				m.goneOnce.Do(func() { close(m.gone) })
				m.cancelReview()
				return
			}
			select {
			case packets <- packet:
			case <-m.gone:
				return
			}
		}
	}()

	status := uint16(serverStatusAutocommit)
	if _, s := parseMysqlOk(m.backend.ok); s != 0 {
		status = s
	}
	for packet := range packets {
		p := packet.payload
		seq := packet.lastSeq + 1
		if len(p) == 0 {
			return
		}

		text := ""
		switch p[0] {
		case comQuit:
			return
		case comQuery, comStmtPrepare:
			text = string(p[1:])
		case comInitDb:
			text = fmt.Sprintf("USE `%s`", strings.ReplaceAll(string(p[1:]), "`", "``"))
		case comProcessKill:
			if len(p) < 5 {
				return
			}
			text = fmt.Sprintf("KILL %d", binary.LittleEndian.Uint32(p[1:]))
		case comProcessInfo:
			text = "SHOW PROCESSLIST"
		case comRefresh:
			text = "FLUSH"
		case comShutdown:
			text = "SHUTDOWN"
		case comFieldList, comPing, comStatistics, comStmtExecute, comStmtSendLongData, comStmtClose, comStmtReset, comSetOption, comStmtFetch, comResetConnection, comDebug:
		default:
			name := "this command"
			if p[0] == comChangeUser {
				name = "changing the user"
			}
			if m.write(makeMysqlErr(mysqlErrNotSupportedYet, mysqlStateGeneral, fmt.Sprintf("casvisor: %s is not allowed through the database proxy", name)), seq) != nil {
				return
			}
			continue
		}

		var command *object.Command
		if text != "" {
			var allowed bool
			command, allowed = m.record(text, nil)
			if !allowed {
				if m.write(makeMysqlErr(mysqlErrSpecificAccessDenied, mysqlStateSyntaxOrAccess, "casvisor: "+command.Error), seq) != nil {
					return
				}
				continue
			}
			if p[0] == comQuery || p[0] == comProcessKill {
				handled, err := m.handleKill(text, seq, status)
				if err != nil {
					return
				}
				if handled {
					finish(command, time.Now(), 0, "")
					continue
				}
			}
		}

		start := time.Now()
		_, err := m.backend.write(p, packet.seq)
		if err != nil {
			return
		}
		rows, message, newStatus, err := m.relayResponse(p[0])
		if newStatus != 0 {
			status = newStatus
		}
		if command != nil {
			finish(command, start, rows, message)
		}
		if err != nil {
			return
		}
	}
}

// handleKill takes a KILL of another connection of the proxy that the user holds, whose id is the proxy's own, not
// the server's: it withdraws the statement waiting for review there, or stops the statement on the server.
func (m *mysqlConn) handleKill(text string, seq byte, status uint16) (bool, error) {
	match := mysqlKillStatement.FindStringSubmatch(text)
	if match == nil {
		return false, nil
	}
	id, err := strconv.ParseUint(match[2], 10, 32)
	if err != nil {
		return false, nil
	}
	target := getProxyConn(uint32(id), m.user)
	if target == nil {
		return false, nil
	}

	if strings.EqualFold(match[1], "QUERY") {
		if !target.cancelReview() && target.cancelQuery != nil {
			target.cancelQuery()
		}
	} else {
		go target.close("Killed by the user")
	}
	return true, m.write(makeMysqlOk(status), seq)
}

// relayResponse passes the response of the server to a command to the client, reading the rows it returned or
// changed, the error and the status flags; the error is the one of the connection, when it broke.
func (m *mysqlConn) relayResponse(command byte) (int64, string, uint16, error) {
	var rows int64
	var status uint16
	next := func() ([]byte, error) {
		packet, err := readMysqlPacket(m.backend.r)
		if err != nil {
			return nil, err
		}
		_, err = writeMysqlPacket(m.w, packet.payload, packet.seq)
		if err != nil {
			return nil, err
		}
		if m.backend.r.Buffered() == 0 {
			err = m.w.Flush()
		}
		if len(packet.payload) == 0 {
			return nil, fmt.Errorf("the database server sent an empty packet")
		}
		return packet.payload, err
	}
	// readUntilEof passes column definitions or rows up to their EOF packet, or an ERR packet
	readUntilEof := func(countRows bool) ([]byte, error) {
		for {
			p, err := next()
			if err != nil {
				return nil, err
			}
			if isMysqlEof(p) || p[0] == 0xff {
				return p, nil
			}
			if countRows {
				rows++
			}
		}
	}

	defer func() {
		_ = m.w.Flush()
	}()

	switch command {
	case comStmtSendLongData, comStmtClose:
		return 0, "", 0, nil
	case comStmtPrepare:
		p, err := next()
		if err != nil {
			return 0, "", 0, err
		}
		if p[0] == 0xff {
			return 0, parseMysqlErr(p), 0, nil
		}
		if len(p) < 9 {
			return 0, "", 0, fmt.Errorf("broken prepare response")
		}
		columns := binary.LittleEndian.Uint16(p[5:])
		params := binary.LittleEndian.Uint16(p[7:])
		for _, n := range []uint16{params, columns} {
			if n > 0 {
				_, err = readUntilEof(false)
				if err != nil {
					return 0, "", 0, err
				}
			}
		}
		return 0, "", 0, nil
	case comFieldList:
		p, err := readUntilEof(false)
		if err != nil {
			return 0, "", 0, err
		}
		if p[0] == 0xff {
			return 0, parseMysqlErr(p), 0, nil
		}
		return 0, "", 0, nil
	case comStmtFetch:
		p, err := readUntilEof(true)
		if err != nil {
			return rows, "", 0, err
		}
		if p[0] == 0xff {
			return rows, parseMysqlErr(p), 0, nil
		}
		return rows, "", getMysqlEofStatus(p), nil
	case comQuery, comStmtExecute, comProcessInfo:
		for {
			p, err := next()
			if err != nil {
				return rows, "", status, err
			}
			switch p[0] {
			case 0x00:
				affected, s := parseMysqlOk(p)
				rows += int64(affected)
				status = s
			case 0xff:
				return rows, parseMysqlErr(p), status, nil
			default:
				p, err = readUntilEof(false)
				if err != nil {
					return rows, "", status, err
				}
				if p[0] == 0xff {
					return rows, parseMysqlErr(p), status, nil
				}
				status = getMysqlEofStatus(p)
				if status&serverStatusCursorExists == 0 {
					p, err = readUntilEof(true)
					if err != nil {
						return rows, "", status, err
					}
					if p[0] == 0xff {
						return rows, parseMysqlErr(p), status, nil
					}
					status = getMysqlEofStatus(p)
				}
			}
			if status&serverMoreResultsExist == 0 {
				return rows, "", status, nil
			}
		}
	default:
		p, err := next()
		if err != nil {
			return 0, "", 0, err
		}
		if p[0] == 0xff {
			return 0, parseMysqlErr(p), 0, nil
		}
		if p[0] == 0x00 {
			_, status = parseMysqlOk(p)
		}
		return 0, "", status, nil
	}
}
