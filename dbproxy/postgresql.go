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
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/casvisor/casvisor/object"
)

const (
	pgSslRequest    = 80877103
	pgGssEncRequest = 80877104
	pgCancelRequest = 80877102
	pgMaxStartupLen = 10000
	pgMaxMessageLen = 1 << 30

	pgScramIterations = 4096
)

type pgMessage struct {
	typ  byte
	body []byte
}

func readPgMessage(r io.Reader) (*pgMessage, error) {
	header := make([]byte, 5)
	_, err := io.ReadFull(r, header)
	if err != nil {
		return nil, err
	}
	length := int(binary.BigEndian.Uint32(header[1:]))
	if length < 4 || length > pgMaxMessageLen {
		return nil, fmt.Errorf("invalid message length: %d", length)
	}
	body := make([]byte, length-4)
	_, err = io.ReadFull(r, body)
	if err != nil {
		return nil, err
	}
	return &pgMessage{typ: header[0], body: body}, nil
}

func appendPgMessage(b []byte, typ byte, body []byte) []byte {
	b = append(b, typ)
	b = binary.BigEndian.AppendUint32(b, uint32(len(body)+4))
	return append(b, body...)
}

// makePgError is an ErrorResponse or NoticeResponse with the severity, the SQLSTATE code and the message.
func makePgError(typ byte, severity string, code string, message string) []byte {
	body := []byte{}
	for _, field := range []struct {
		key   byte
		value string
	}{{'S', severity}, {'V', severity}, {'C', code}, {'M', message}} {
		body = append(append(append(body, field.key), field.value...), 0)
	}
	return appendPgMessage(nil, typ, append(body, 0))
}

// getPgErrorField reads a field of an ErrorResponse, such as 'M' for the message.
func getPgErrorField(body []byte, key byte) string {
	for len(body) > 0 && body[0] != 0 {
		k := body[0]
		var value string
		value, body = readNulString(body[1:])
		if k == key {
			return value
		}
	}
	return ""
}

// pgRound is what the client sent up to a Sync, a simple query or a function call, which the server answers with
// ReadyForQuery: the statements recorded in it, which the results that come back are counted on.
type pgRound struct {
	commands []*pgCommand
	start    time.Time
}

const (
	// a simple query, which may hold several statements, each with its own result
	pgEntrySimple = iota
	pgEntryParse
	pgEntryExecute
)

// pgCommand is a statement recorded in a round: a simple query, the parsing of a statement, or
// an execution of a statement parsed before, whose results are counted on the statement.
type pgCommand struct {
	command  *object.Command
	kind     int
	blocked  bool
	done     bool
	rows     int64
	dataRows int64
	errors   []string
}

type postgresqlConn struct {
	*proxyConn
	r     *bufio.Reader
	isTls bool

	writeMu sync.Mutex
	w       *bufio.Writer

	backendConn net.Conn
	backendR    *bufio.Reader
	backendW    *bufio.Writer
	backendPid  uint32
	backendKey  []byte
	secretKey   []byte

	roundMu sync.Mutex
	rounds  []*pgRound
	current *pgRound
	// the statements and portals of the extended protocol, by name, to count their results on what was parsed
	statements map[string]*object.Command
	portals    map[string]*object.Command
	// blocked are the queries put in place of blocked statements, which the server answers with an error that
	// is replaced by the reason, by a word of the query that the error quotes
	blocked map[string]string
}

func (c *postgresqlConn) writeClient(b []byte, flush bool) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := c.w.Write(b)
	if err == nil && flush {
		err = c.w.Flush()
	}
	return err
}

func (c *postgresqlConn) fatal(code string, message string) {
	_ = c.writeClient(makePgError('E', "FATAL", code, "casvisor: "+message), true)
}

func servePostgresql(conn net.Conn) {
	c := &postgresqlConn{
		proxyConn:  newProxyConn(object.ProtocolPostgresql, conn),
		r:          bufio.NewReader(conn),
		w:          bufio.NewWriter(conn),
		blocked:    map[string]string{},
		statements: map[string]*object.Command{},
		portals:    map[string]*object.Command{},
	}
	defer func() {
		c.close("Normal user exit")
	}()
	_ = conn.SetDeadline(time.Now().Add(handshakeTimeout))

	err := c.handshake()
	if err != nil {
		return
	}
	_ = c.conn.SetDeadline(time.Time{})
	c.relay()
}

// readStartup reads the startup message, answering the requests for TLS that come before it; nil params
// means the connection was a cancel request.
func (c *postgresqlConn) readStartup() (map[string]string, uint32, error) {
	for {
		header := make([]byte, 8)
		_, err := io.ReadFull(c.r, header)
		if err != nil {
			return nil, 0, err
		}
		length := int(binary.BigEndian.Uint32(header))
		code := binary.BigEndian.Uint32(header[4:])
		if length < 8 || length > pgMaxStartupLen {
			return nil, 0, fmt.Errorf("invalid startup message length")
		}
		body := make([]byte, length-8)
		_, err = io.ReadFull(c.r, body)
		if err != nil {
			return nil, 0, err
		}

		switch code {
		case pgSslRequest:
			if tlsConfig == nil || c.isTls {
				err = c.writeClient([]byte{'N'}, true)
			} else {
				err = c.writeClient([]byte{'S'}, true)
				if err == nil {
					tlsConn := tls.Server(&bufferedConn{Conn: c.conn, r: c.r}, tlsConfig)
					err = tlsConn.Handshake()
					c.conn = tlsConn
					c.isTls = true
					c.r = bufio.NewReader(tlsConn)
					c.w = bufio.NewWriter(tlsConn)
				}
			}
			if err != nil {
				return nil, 0, err
			}
		case pgGssEncRequest:
			err = c.writeClient([]byte{'N'}, true)
			if err != nil {
				return nil, 0, err
			}
		case pgCancelRequest:
			if len(body) >= 4 {
				c.cancel(binary.BigEndian.Uint32(body), body[4:])
			}
			return nil, 0, nil
		default:
			if code>>16 != 3 {
				return nil, 0, fmt.Errorf("unsupported protocol version: %d.%d", code>>16, code&0xffff)
			}
			params := map[string]string{}
			for len(body) > 1 {
				var key, value string
				key, body = readNulString(body)
				value, body = readNulString(body)
				params[key] = value
			}
			return params, code & 0xffff, nil
		}
	}
}

// cancel takes a cancel request, which a client such as psql sends from a new connection when Ctrl+C stops a query.
func (c *postgresqlConn) cancel(pid uint32, key []byte) {
	value, ok := pgConns.Load(pid)
	if !ok {
		return
	}
	target := value.(*postgresqlConn)
	if subtle.ConstantTimeCompare(target.secretKey, key) != 1 {
		return
	}
	if !target.cancelReview() && target.cancelQuery != nil {
		target.cancelQuery()
	}
}

var pgConns sync.Map

func (c *postgresqlConn) handshake() error {
	params, minor, err := c.readStartup()
	if err != nil || params == nil {
		return fmt.Errorf("no startup message")
	}

	// a client asking for a newer minor version or protocol options is told what the proxy speaks
	unsupported := []string{}
	for key := range params {
		if strings.HasPrefix(key, "_pq_.") {
			unsupported = append(unsupported, key)
			delete(params, key)
		}
	}
	if minor > 0 || len(unsupported) > 0 {
		sort.Strings(unsupported)
		body := binary.BigEndian.AppendUint32(nil, 0)
		body = binary.BigEndian.AppendUint32(body, uint32(len(unsupported)))
		for _, key := range unsupported {
			body = append(append(body, key...), 0)
		}
		err = c.writeClient(appendPgMessage(nil, 'v', body), false)
		if err != nil {
			return err
		}
	}

	username := params["user"]
	c.userAgent = params["application_name"]
	if c.userAgent == "" {
		c.userAgent = "PostgreSQL client"
	}
	if replication := params["replication"]; replication != "" && replication != "false" && replication != "0" && replication != "off" {
		c.fatal("42501", "replication connections are not allowed through the database proxy")
		return fmt.Errorf("replication")
	}

	token := object.GetDbToken(username)
	if token == nil {
		c.fatal("28P01", "authentication failed, sign in with a database token from Casvisor as both the user and the password; the token may have expired or been revoked")
		return fmt.Errorf("unknown token")
	}
	err = c.authenticateScram(username)
	if err != nil {
		c.fatal("28P01", fmt.Sprintf("password authentication failed: %s", err.Error()))
		return err
	}
	err = c.writeClient(appendPgMessage(nil, 'R', binary.BigEndian.AppendUint32(nil, 0)), true)
	if err != nil {
		return err
	}

	err = c.authorize(token)
	if err == nil {
		err = c.checkLoginRules(func(message string) {
			_ = c.writeClient(makePgError('N', "NOTICE", "00000", message), true)
		})
	}
	if err != nil {
		c.fatal("28000", err.Error())
		return err
	}

	database := params["database"]
	if database == "" || database == username {
		database = c.asset.Database
	}
	runtimeParams := map[string]string{}
	for key, value := range params {
		if key != "user" && key != "database" && key != "replication" {
			runtimeParams[key] = value
		}
	}
	if runtimeParams["application_name"] == "" {
		runtimeParams["application_name"] = "casvisor"
	}

	c.client = object.NewNativeDatabaseClient(c.asset)
	hijacked, err := c.client.ConnectPostgresqlNative(database, runtimeParams)
	if err != nil {
		c.fatal("08006", fmt.Sprintf("failed to connect to the database of the asset: %s: %s", c.asset.Name, err.Error()))
		return err
	}
	c.backendConn = hijacked.Conn
	c.backendR = bufio.NewReader(hijacked.Conn)
	c.backendW = bufio.NewWriter(hijacked.Conn)
	c.backendPid = hijacked.PID
	c.backendKey = hijacked.SecretKey

	err = c.openSession(hijacked.Conn)
	if err != nil {
		c.fatal("08006", err.Error())
		return err
	}
	c.cancelQuery = func() {
		_ = object.CancelPostgresqlQuery(c.asset, c.backendPid, c.backendKey)
	}
	c.secretKey = make([]byte, 4)
	_, _ = rand.Read(c.secretKey)
	pgConns.Store(c.id, c)

	b := []byte{}
	keys := []string{}
	for key := range hijacked.ParameterStatuses {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		body := append(append([]byte(key), 0), hijacked.ParameterStatuses[key]...)
		b = appendPgMessage(b, 'S', append(body, 0))
	}
	b = appendPgMessage(b, 'K', append(binary.BigEndian.AppendUint32(nil, c.id), c.secretKey...))
	txStatus := hijacked.TxStatus
	if txStatus == 0 {
		txStatus = 'I'
	}
	b = appendPgMessage(b, 'Z', []byte{txStatus})
	return c.writeClient(b, true)
}

// authenticateScram checks with SCRAM-SHA-256 that the client knows the password, which is the token.
func (c *postgresqlConn) authenticateScram(password string) error {
	err := c.writeClient(appendPgMessage(nil, 'R', append(binary.BigEndian.AppendUint32(nil, 10), "SCRAM-SHA-256\x00\x00"...)), true)
	if err != nil {
		return err
	}

	message, err := readPgMessage(c.r)
	if err != nil {
		return err
	}
	if message.typ != 'p' {
		return fmt.Errorf("expected a SASL response")
	}
	mechanism, rest := readNulString(message.body)
	if mechanism != "SCRAM-SHA-256" || len(rest) < 4 {
		return fmt.Errorf("the client must use SCRAM-SHA-256")
	}
	clientFirst := string(rest[4:])
	// the GS2 header: "n,," or "y,," without channel binding, which the proxy doesn't offer
	parts := strings.SplitN(clientFirst, ",", 3)
	if len(parts) != 3 || (parts[0] != "n" && parts[0] != "y") {
		return fmt.Errorf("channel binding is not supported")
	}
	gs2Header := parts[0] + "," + parts[1] + ","
	clientFirstBare := parts[2]
	clientNonce := ""
	for _, attr := range strings.Split(clientFirstBare, ",") {
		if strings.HasPrefix(attr, "r=") {
			clientNonce = attr[2:]
		}
	}
	if clientNonce == "" {
		return fmt.Errorf("no client nonce")
	}

	b := make([]byte, 34)
	_, _ = rand.Read(b)
	nonce := clientNonce + base64.StdEncoding.EncodeToString(b[:18])
	salt := b[18:]
	serverFirst := fmt.Sprintf("r=%s,s=%s,i=%d", nonce, base64.StdEncoding.EncodeToString(salt), pgScramIterations)
	err = c.writeClient(appendPgMessage(nil, 'R', append(binary.BigEndian.AppendUint32(nil, 11), serverFirst...)), true)
	if err != nil {
		return err
	}

	message, err = readPgMessage(c.r)
	if err != nil {
		return err
	}
	if message.typ != 'p' {
		return fmt.Errorf("expected a SASL response")
	}
	clientFinal := string(message.body)
	i := strings.LastIndex(clientFinal, ",p=")
	if i < 0 {
		return fmt.Errorf("no client proof")
	}
	withoutProof := clientFinal[:i]
	proof, err := base64.StdEncoding.DecodeString(clientFinal[i+3:])
	if err != nil {
		return err
	}
	if !strings.HasPrefix(withoutProof, "c="+base64.StdEncoding.EncodeToString([]byte(gs2Header))+",r="+nonce) {
		return fmt.Errorf("wrong channel binding or nonce")
	}

	saltedPassword, err := pbkdf2.Key(sha256.New, password, salt, pgScramIterations, sha256.Size)
	if err != nil {
		return err
	}
	clientKey := computeHmac(saltedPassword, "Client Key")
	storedKey := sha256.Sum256(clientKey)
	authMessage := clientFirstBare + "," + serverFirst + "," + withoutProof
	clientSignature := computeHmac(storedKey[:], authMessage)
	if len(proof) != len(clientSignature) {
		return fmt.Errorf("wrong proof")
	}
	for j := range proof {
		proof[j] ^= clientSignature[j]
	}
	expected := sha256.Sum256(proof)
	if subtle.ConstantTimeCompare(expected[:], storedKey[:]) != 1 {
		return fmt.Errorf("wrong password")
	}

	serverSignature := computeHmac(computeHmac(saltedPassword, "Server Key"), authMessage)
	serverFinal := "v=" + base64.StdEncoding.EncodeToString(serverSignature)
	return c.writeClient(appendPgMessage(nil, 'R', append(binary.BigEndian.AppendUint32(nil, 12), serverFinal...)), false)
}

func computeHmac(key []byte, message string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(message))
	return h.Sum(nil)
}

// relay passes the messages of the client to the server, checking the statements, and the messages of the server back.
func (c *postgresqlConn) relay() {
	messages := make(chan *pgMessage, 16)
	go func() {
		defer close(messages)
		for {
			message, err := readPgMessage(c.r)
			if err != nil {
				c.goneOnce.Do(func() { close(c.gone) })
				c.cancelReview()
				return
			}
			select {
			case messages <- message:
			case <-c.gone:
				return
			}
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.pump()
		c.kill()
	}()

	for message := range messages {
		if !c.forward(message, len(messages) == 0) {
			break
		}
	}
	c.kill()
	<-done
}

// forward checks a message of the client and passes it to the server; false ends the connection.
func (c *postgresqlConn) forward(message *pgMessage, flush bool) bool {
	switch message.typ {
	case 'Q', 'F':
		text := ""
		if message.typ == 'Q' {
			text, _ = readNulString(message.body)
		} else {
			text = "function call"
			if len(message.body) >= 4 {
				text = fmt.Sprintf("function call: %d", binary.BigEndian.Uint32(message.body))
			}
		}
		round := c.startRound()
		if strings.TrimSpace(text) != "" {
			command, allowed := c.record(text, c.notify)
			if allowed && message.typ == 'F' {
				// a function call names a function by its OID, which the rules can't tell
				allowed = false
				command.Error = "function calls are not allowed through the database proxy"
				_ = object.UpdateCommandResult(command)
			}
			round.commands = append(round.commands, &pgCommand{command: command, kind: pgEntrySimple, blocked: !allowed})
			if !allowed {
				message = &pgMessage{typ: 'Q', body: append([]byte(c.addBlocked(command.Error)), 0)}
			}
		}
		c.endRound(round)
		flush = true
	case 'P':
		name, rest := readNulString(message.body)
		text, _ := readNulString(rest)
		if strings.TrimSpace(text) != "" {
			command, allowed := c.record(text, c.notify)
			c.addCommand(&pgCommand{command: command, kind: pgEntryParse, blocked: !allowed})
			c.statements[name] = command
			if !allowed {
				c.statements[name] = nil
				body := append(append([]byte(name), 0), c.addBlocked(command.Error)...)
				body = append(body, 0, 0, 0)
				message = &pgMessage{typ: 'P', body: body}
			}
		}
	case 'B':
		portal, rest := readNulString(message.body)
		statement, _ := readNulString(rest)
		c.portals[portal] = c.statements[statement]
	case 'E':
		portal, _ := readNulString(message.body)
		if command := c.portals[portal]; command != nil {
			c.addCommand(&pgCommand{command: command, kind: pgEntryExecute})
		}
	case 'C':
		if len(message.body) > 0 {
			name, _ := readNulString(message.body[1:])
			if message.body[0] == 'S' {
				delete(c.statements, name)
			} else {
				delete(c.portals, name)
			}
		}
	case 'S':
		c.endRound(c.startRound())
		flush = true
	case 'X':
		_, _ = c.backendW.Write(appendPgMessage(nil, 'X', nil))
		_ = c.backendW.Flush()
		return false
	}

	_, err := c.backendW.Write(appendPgMessage(nil, message.typ, message.body))
	if err == nil && (flush || message.typ == 'H') {
		err = c.backendW.Flush()
	}
	return err == nil
}

// notify tells the client that its statement waits for review.
func (c *postgresqlConn) notify(message string) {
	_ = c.writeClient(makePgError('N', "NOTICE", "00000", "casvisor: "+message), true)
}

// addBlocked returns the query that takes the place of a blocked statement: a word the server can't parse,
// whose syntax error, which comes in order with the other results, is replaced by the reason.
func (c *postgresqlConn) addBlocked(reason string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	word := "casvisor_blocked_" + hex.EncodeToString(b)
	c.roundMu.Lock()
	c.blocked[word] = reason
	c.roundMu.Unlock()
	return word
}

func (c *postgresqlConn) startRound() *pgRound {
	c.roundMu.Lock()
	defer c.roundMu.Unlock()
	round := c.current
	if round == nil {
		round = &pgRound{start: time.Now()}
	}
	c.current = nil
	return round
}

func (c *postgresqlConn) endRound(round *pgRound) {
	c.roundMu.Lock()
	defer c.roundMu.Unlock()
	c.rounds = append(c.rounds, round)
}

func (c *postgresqlConn) addCommand(command *pgCommand) {
	c.roundMu.Lock()
	defer c.roundMu.Unlock()
	if c.current == nil {
		c.current = &pgRound{start: time.Now()}
	}
	c.current.commands = append(c.current.commands, command)
}

// getCommand returns the statement a result is counted on: the first one of the oldest round not answered yet.
func (c *postgresqlConn) getCommand() *pgCommand {
	rounds := c.rounds
	if len(rounds) == 0 {
		if c.current == nil {
			return nil
		}
		rounds = []*pgRound{c.current}
	}
	for _, command := range rounds[0].commands {
		if !command.done {
			return command
		}
	}
	return nil
}

// pump passes the messages of the server to the client, counting the results on the statements.
func (c *postgresqlConn) pump() {
	for {
		message, err := readPgMessage(c.backendR)
		if err != nil {
			return
		}

		switch message.typ {
		case '1', 'C', 'I', 'E', 's', 'D':
			c.roundMu.Lock()
			message = c.countResult(message)
			c.roundMu.Unlock()
		case 'Z':
			c.roundMu.Lock()
			if len(c.rounds) > 0 {
				c.finishRound(c.rounds[0])
				c.rounds = c.rounds[1:]
			}
			c.roundMu.Unlock()
		}

		err = c.writeClient(appendPgMessage(nil, message.typ, message.body), c.backendR.Buffered() == 0)
		if err != nil {
			return
		}
	}
}

// countResult counts a result of the server on the statement it answers, and replaces the error of a query
// put in place of a blocked statement by the reason.
func (c *postgresqlConn) countResult(message *pgMessage) *pgMessage {
	command := c.getCommand()
	switch message.typ {
	case '1':
		if command != nil && command.kind == pgEntryParse {
			command.done = true
		}
	case 'D':
		if command != nil {
			command.dataRows++
		}
	case 'C', 'I', 's':
		if command == nil {
			break
		}
		tag, _ := readNulString(message.body)
		fields := strings.Fields(tag)
		if command.dataRows > 0 {
			command.rows += command.dataRows
		} else if len(fields) > 1 {
			if n, err := strconv.ParseInt(fields[len(fields)-1], 10, 64); err == nil {
				command.rows += n
			}
		}
		command.dataRows = 0
		if command.kind != pgEntrySimple {
			command.done = true
		}
	case 'E':
		text := getPgErrorField(message.body, 'M')
		for word, reason := range c.blocked {
			if strings.Contains(text, word) {
				delete(c.blocked, word)
				message = &pgMessage{typ: 'E', body: makePgError('E', "ERROR", "42501", "casvisor: "+reason)[5:]}
				text = ""
				break
			}
		}
		if command != nil {
			if text != "" {
				command.errors = append(command.errors, text)
			}
			command.done = true
		}
	}
	return message
}

// finishRound saves the results of the statements of a round, once the server has answered it.
func (c *postgresqlConn) finishRound(round *pgRound) {
	rows := map[*object.Command]int64{}
	errors := map[*object.Command][]string{}
	commands := []*object.Command{}
	for _, command := range round.commands {
		if command.blocked || command.command.Name == "" {
			continue
		}
		if _, ok := rows[command.command]; !ok {
			commands = append(commands, command.command)
		}
		rows[command.command] += command.rows
		errors[command.command] = append(errors[command.command], command.errors...)
	}
	for _, command := range commands {
		finish(command, round.start, rows[command], strings.Join(errors[command], "; "))
	}
}

func (c *postgresqlConn) close(message string) {
	pgConns.Delete(c.id)
	c.proxyConn.close(message)
}
