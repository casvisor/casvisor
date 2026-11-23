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

package object

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/jackc/pgx/v5/pgconn"
)

// NewNativeDatabaseClient is a connection to the database of an asset for the database proxy, which speaks the
// protocol of the database itself; it dials through the asset's network gateway and pins the public key
// of the server like the web console does.
func NewNativeDatabaseClient(asset *Asset) *DatabaseClient {
	ctx, cancel := context.WithCancel(context.Background())
	return &DatabaseClient{Asset: asset, Database: asset.Database, ctx: ctx, cancel: cancel}
}

func (c *DatabaseClient) Dial() (net.Conn, error) {
	return c.dial()
}

// GetTlsConfig checks the server by the pinned public key, or records it for PinFingerprint.
func (c *DatabaseClient) GetTlsConfig() *tls.Config {
	return c.getTlsConfig()
}

func (c *DatabaseClient) IsFingerprintPinned() bool {
	return c.getPinnedFingerprint() != ""
}

// SetPlainText tells that the server has no TLS, so its public key isn't pinned.
func (c *DatabaseClient) SetPlainText() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.plainText = true
}

// PinFingerprint saves the public key seen in the TLS handshake, once the server accepted the password.
func (c *DatabaseClient) PinFingerprint() error {
	return c.pinFingerprint()
}

// ConnectPostgresqlNative signs in to the PostgreSQL server of the asset with the run-time parameters of the
// client, and hands over the connection to pass the messages of the client through.
func (c *DatabaseClient) ConnectPostgresqlNative(database string, params map[string]string) (*pgconn.HijackedConn, error) {
	d := &postgresqlDriver{c: c}
	pg, err := d.connectDatabaseWithParams(database, params)
	if err != nil {
		return nil, err
	}
	err = c.pinFingerprint()
	if err == nil {
		err = pg.PgConn().SyncConn(c.ctx)
	}
	if err != nil {
		_ = pg.Close(context.Background())
		return nil, err
	}
	return pg.PgConn().Hijack()
}

// CancelPostgresqlQuery asks the PostgreSQL server of the asset to cancel what a connection of the proxy runs.
func CancelPostgresqlQuery(asset *Asset, pid uint32, secretKey []byte) error {
	conn, err := DialAsset(asset)
	if err != nil {
		return err
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(dialTimeout))
	b := binary.BigEndian.AppendUint32(nil, uint32(12+len(secretKey)))
	b = binary.BigEndian.AppendUint32(b, 80877102)
	b = binary.BigEndian.AppendUint32(b, pid)
	b = append(b, secretKey...)
	_, err = conn.Write(b)
	if err != nil {
		return err
	}
	// the server closes the connection once it has read the request
	_, _ = conn.Read(make([]byte, 1))
	return nil
}

// CheckDatabaseStatement applies the command rules to SQL text a database client sent, which may hold
// several statements, and fills in the command: its action and rule, and why the text may not run. Text
// a Review rule matches waits here until it is reviewed or cancel is closed; onHeld is told the review.
func CheckDatabaseStatement(user *casdoorsdk.User, asset *Asset, command *Command, cancel <-chan struct{}, onHeld func(message string, review *CommandReview)) (*CommandRule, bool, error) {
	var rule *CommandRule
	statements := SplitSqlStatements(command.Text, asset.Protocol)
	if len(statements) != 1 || statements[0] != strings.TrimSpace(command.Text) {
		statements = append([]string{command.Text}, statements...)
	}
	for _, statement := range statements {
		r, err := MatchStatementRule(user, asset, statement)
		if err != nil {
			command.Action = CommandActionBlock
			command.Error = "the statement is blocked because the command rules can't be read"
			return nil, false, err
		}
		rule = MoreSevereRule(rule, r)
		if rule != nil && rule.Action == CommandActionBlock {
			break
		}
	}
	if rule == nil {
		return nil, true, nil
	}

	command.Action = rule.Action
	command.Rule = rule.GetId()
	switch rule.Action {
	case CommandActionBlock:
		command.Error = fmt.Sprintf("the statement is blocked by the rule: %s", rule.GetDisplayName())
		return rule, false, nil
	case CommandActionReview:
		approved, err := HoldCommand(command, rule, cancel, func(review *CommandReview) {
			if onHeld != nil {
				onHeld(GetHeldMessage(rule, review), review)
			}
		})
		return rule, approved, err
	}
	return rule, true, nil
}
