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
	"database/sql"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	ProtocolMysql      = "MySQL"
	ProtocolPostgresql = "PostgreSQL"
	ProtocolSqlServer  = "SQLServer"
	ProtocolOracle     = "Oracle"
	ProtocolRedis      = "Redis"
	ProtocolMongodb    = "MongoDB"
	ProtocolClickhouse = "ClickHouse"
	ProtocolDameng     = "Dameng"

	SqlMaxRows = 1000
	// the longest value of a cell that is kept, and all the cells of a result together
	sqlMaxCellSize   = 64 * 1024
	sqlMaxResultSize = 8 * 1024 * 1024
	// the results a single batch of SQL Server can return
	sqlMaxBatchResults = 20
)

var DatabaseProtocols = []string{ProtocolMysql, ProtocolPostgresql, ProtocolSqlServer, ProtocolOracle, ProtocolRedis, ProtocolMongodb, ProtocolClickhouse, ProtocolDameng}

func IsDatabaseProtocol(protocol string) bool {
	for _, p := range DatabaseProtocols {
		if p == protocol {
			return true
		}
	}
	return false
}

type SqlColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// SqlResult is how a statement ran: the rows it returned, at most SqlMaxRows
// of them, or the rows it changed. A Redis command or a MongoDB call is shown the same way.
type SqlResult struct {
	Id        string      `json:"id"`
	Statement string      `json:"statement"`
	Columns   []SqlColumn `json:"columns"`
	Rows      [][]*string `json:"rows"`
	RowCount  int64       `json:"rowCount"`
	Truncated bool        `json:"truncated"`
	Duration  int64       `json:"duration"`
	Error     string      `json:"error"`
	Action    string      `json:"action"`
	Rule      string      `json:"rule"`
	// the default database after the statement, when the statement changed it
	Database string `json:"database,omitempty"`
}

type DatabaseTable struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
	Type   string `json:"type"`
}

type DatabaseColumn struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Key      string `json:"key"`
	Nullable bool   `json:"nullable"`
}

// databaseDriver is what a kind of database does for a DatabaseClient.
type databaseDriver interface {
	connect() error
	getVersionAndDatabase() (string, string, error)
	getDatabases() ([]string, error)
	getTables(database string) ([]*DatabaseTable, error)
	getColumns(database string, schema string, table string) ([]*DatabaseColumn, error)
	getUseStatement(database string) string
	useDatabase(database string) error
	// execute runs one statement typed by the user; a SQL Server batch may return several results
	execute(statement string) []*SqlResult
	close()
}

// DatabaseClient is one connection to the database of an asset, made through
// the asset's network gateway like any other connection.
type DatabaseClient struct {
	Asset    *Asset
	Database string
	Version  string

	ctx    context.Context
	cancel context.CancelFunc

	mu        sync.Mutex
	netConns  []net.Conn
	closed    bool
	closeOnce sync.Once

	driver databaseDriver

	// noSave keeps the pinned fingerprint in the asset only, for testing an asset before it is saved
	noSave      bool
	fingerprint string
	// plainText is set once a server without TLS has been seen, for the drivers that wrap the connections themselves
	plainText bool
}

// dial connects to the asset and keeps the connection, so that Close can cut it
// even while a statement is running.
func (c *DatabaseClient) dial() (net.Conn, error) {
	conn, err := DialAsset(c.Asset)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		_ = conn.Close()
		return nil, fmt.Errorf("the connection has been closed")
	}
	c.netConns = append(c.netConns, conn)
	return conn, nil
}

// dialTls connects to the asset with TLS when the server has it, and in plain text
// only while no public key is pinned, for the drivers that don't negotiate TLS themselves.
func (c *DatabaseClient) dialTls(ctx context.Context) (net.Conn, error) {
	c.mu.Lock()
	plainText := c.plainText
	c.mu.Unlock()
	pinned := c.getPinnedFingerprint() != ""
	if plainText && !pinned {
		return c.dial()
	}

	conn, err := c.dial()
	if err != nil {
		return nil, err
	}

	handshakeCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	tlsConn := tls.Client(conn, c.getTlsConfig())
	err = tlsConn.HandshakeContext(handshakeCtx)
	if err == nil {
		return tlsConn, nil
	}
	_ = conn.Close()
	if pinned {
		return nil, fmt.Errorf("the TLS connection to the database server of the asset: %s failed: %s", c.Asset.GetId(), err.Error())
	}

	c.mu.Lock()
	c.plainText = true
	c.mu.Unlock()
	return c.dial()
}

// databaseDialer dials the asset for the drivers that take a dialer, whatever address they ask for.
type databaseDialer struct {
	dial func(ctx context.Context) (net.Conn, error)
}

func (d databaseDialer) DialContext(ctx context.Context, network string, addr string) (net.Conn, error) {
	return d.dial(ctx)
}

func DialDatabase(asset *Asset) (*DatabaseClient, error) {
	return dialDatabase(asset, false)
}

func dialDatabase(asset *Asset, noSave bool) (*DatabaseClient, error) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &DatabaseClient{Asset: asset, Database: asset.Database, ctx: ctx, cancel: cancel, noSave: noSave}

	switch asset.Protocol {
	case ProtocolMysql:
		c.driver = &mysqlDriver{c: c}
	case ProtocolPostgresql:
		c.driver = &postgresqlDriver{c: c}
	case ProtocolSqlServer:
		c.driver = &sqlServerDriver{c: c}
	case ProtocolOracle:
		c.driver = &oracleDriver{c: c}
	case ProtocolRedis:
		c.driver = &redisDriver{c: c}
	case ProtocolMongodb:
		c.driver = &mongodbDriver{c: c}
	case ProtocolClickhouse:
		c.driver = &clickhouseDriver{c: c}
	case ProtocolDameng:
		c.driver = &damengDriver{c: c}
	default:
		cancel()
		return nil, fmt.Errorf("the protocol: %s is not a database protocol", asset.Protocol)
	}

	err := c.driver.connect()
	if err != nil {
		c.Close()
		return nil, err
	}

	c.Version, c.Database, err = c.driver.getVersionAndDatabase()
	if err != nil {
		c.Close()
		return nil, err
	}

	err = c.pinFingerprint()
	if err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// getTlsConfig checks the server by the public key pinned in the fingerprint of the asset at
// the first TLS connection, as database servers mostly have self-signed certificates.
func (c *DatabaseClient) getTlsConfig() *tls.Config {
	serverName := c.Asset.Host
	if net.ParseIP(serverName) != nil {
		serverName = ""
	}
	return &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: true,
		VerifyConnection:   c.verifyConnection,
	}
}

func (c *DatabaseClient) verifyConnection(state tls.ConnectionState) error {
	if len(state.PeerCertificates) == 0 {
		return fmt.Errorf("the database server of the asset: %s sent no certificate", c.Asset.GetId())
	}

	fingerprint := getCertificateFingerprint(state.PeerCertificates[0].RawSubjectPublicKeyInfo)
	pinned := c.getPinnedFingerprint()
	if pinned == "" {
		c.mu.Lock()
		c.fingerprint = fingerprint
		c.mu.Unlock()
		return nil
	}
	if pinned != fingerprint {
		return fmt.Errorf("the public key of the database server of the asset: %s has changed from %s to %s, clear the fingerprint of the asset if the change is expected", c.Asset.GetId(), pinned, fingerprint)
	}
	return nil
}

func (c *DatabaseClient) getPinnedFingerprint() string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(c.Asset.Fingerprint), ":", ""))
}

// pinFingerprint saves the public key of the database server, once the password has been
// accepted; from then on the connection must use TLS, so it can't be downgraded to plain text.
func (c *DatabaseClient) pinFingerprint() error {
	c.mu.Lock()
	fingerprint := c.fingerprint
	c.fingerprint = ""
	plainText := c.plainText
	c.mu.Unlock()

	if fingerprint == "" || plainText || c.Asset.Fingerprint != "" {
		return nil
	}
	c.Asset.Fingerprint = fingerprint
	if c.noSave {
		return nil
	}
	return UpdateAssetFingerprint(c.Asset.GetId(), fingerprint)
}

// Kill cuts the connections to the database, which also ends a statement still
// running; it may be called while the client is in use.
func (c *DatabaseClient) Kill() {
	c.mu.Lock()
	c.closed = true
	netConns := c.netConns
	c.netConns = nil
	c.mu.Unlock()

	c.cancel()
	for _, conn := range netConns {
		_ = conn.Close()
	}
}

// Close kills the connections and frees the client, once it is no longer in use.
func (c *DatabaseClient) Close() {
	c.Kill()
	c.closeOnce.Do(func() {
		if c.driver != nil {
			c.driver.close()
		}
	})
}

// GetDatabases lists the databases, with the current one even when it can't be listed.
func (c *DatabaseClient) GetDatabases() ([]string, error) {
	databases, err := c.driver.getDatabases()
	if err != nil {
		return nil, err
	}
	if c.Database != "" {
		found := false
		for _, database := range databases {
			if database == c.Database {
				found = true
				break
			}
		}
		if !found {
			databases = append([]string{c.Database}, databases...)
		}
	}
	return databases, nil
}

// GetTables lists the tables and views of a database, the keys of a Redis
// database or the collections of a MongoDB database.
func (c *DatabaseClient) GetTables(database string) ([]*DatabaseTable, error) {
	return c.driver.getTables(database)
}

func (c *DatabaseClient) GetColumns(database string, schema string, table string) ([]*DatabaseColumn, error) {
	return c.driver.getColumns(database, schema, table)
}

// GetUseStatement is how switching to a database is shown in the command records.
func (c *DatabaseClient) GetUseStatement(database string) string {
	return c.driver.getUseStatement(database)
}

// UseDatabase makes a database the default one.
func (c *DatabaseClient) UseDatabase(database string) error {
	err := c.driver.useDatabase(database)
	if err != nil {
		return err
	}
	c.Database = database
	return nil
}

// Execute runs one statement typed by the user.
func (c *DatabaseClient) Execute(statement string) []*SqlResult {
	database := c.Database
	start := time.Now()
	results := c.driver.execute(statement)
	duration := time.Since(start).Milliseconds()
	for _, result := range results {
		result.Statement = statement
		result.Duration = duration
	}
	if c.Database != database && len(results) > 0 {
		results[len(results)-1].Database = c.Database
	}
	return results
}

func newErrorResult(err error) []*SqlResult {
	return []*SqlResult{{Error: err.Error()}}
}

// queryConnStrings runs a query of the client itself, not of the user, and returns its rows as strings.
func queryConnStrings(ctx context.Context, conn *sql.Conn, query string, args ...interface{}) ([][]string, error) {
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	res := [][]string{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		dest := make([]interface{}, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		err = rows.Scan(dest...)
		if err != nil {
			return nil, err
		}
		row := make([]string, len(columns))
		for i, value := range values {
			if b := formatDriverValue(value); b != nil {
				row[i] = string(b)
			}
		}
		res = append(res, row)
	}
	return res, rows.Err()
}

// formatDriverValue turns a value scanned by a database/sql driver into the text of a cell.
func formatDriverValue(value interface{}) []byte {
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		return v
	case string:
		return []byte(v)
	case time.Time:
		if v.Location() == time.UTC {
			return []byte(v.Format("2006-01-02 15:04:05.999999999"))
		}
		return []byte(v.Format("2006-01-02 15:04:05.999999999 -07:00"))
	case float64:
		return []byte(strconv.FormatFloat(v, 'f', -1, 64))
	case float32:
		return []byte(strconv.FormatFloat(float64(v), 'f', -1, 32))
	case bool:
		return []byte(strconv.FormatBool(v))
	default:
		return []byte(fmt.Sprint(v))
	}
}

// readSqlRows reads the rows of a result set of a database/sql driver into the result.
func readSqlRows(rows *sql.Rows, res *SqlResult, format func(value interface{}, columnType *sql.ColumnType) []byte) error {
	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return err
	}

	res.Columns = []SqlColumn{}
	for _, columnType := range columnTypes {
		res.Columns = append(res.Columns, SqlColumn{Name: columnType.Name(), Type: columnType.DatabaseTypeName()})
	}
	res.Rows = [][]*string{}

	r := resultRows{res: res}
	values := make([]interface{}, len(columnTypes))
	dest := make([]interface{}, len(columnTypes))
	for i := range values {
		dest[i] = &values[i]
	}
	for rows.Next() {
		if res.Truncated {
			res.RowCount++
			continue
		}
		err = rows.Scan(dest...)
		if err != nil {
			return err
		}
		row := make([][]byte, len(values))
		for i, value := range values {
			row[i] = format(value, columnTypes[i])
		}
		r.add(row)
	}
	return rows.Err()
}

func formatSqlValue(value []byte) *string {
	if value == nil {
		return nil
	}

	var res string
	if utf8.Valid(value) {
		if len(value) > sqlMaxCellSize {
			value = value[:sqlMaxCellSize]
			for len(value) > 0 && !utf8.Valid(value) {
				value = value[:len(value)-1]
			}
			res = string(value) + "..."
		} else {
			res = string(value)
		}
	} else {
		if len(value) > sqlMaxCellSize/2 {
			res = "0x" + strings.ToUpper(hex.EncodeToString(value[:sqlMaxCellSize/2])) + "..."
		} else {
			res = "0x" + strings.ToUpper(hex.EncodeToString(value))
		}
	}
	return &res
}

// resultRows keeps the first rows of a result, as long as they fit.
type resultRows struct {
	res  *SqlResult
	size int
}

func (r *resultRows) add(values [][]byte) {
	r.res.RowCount++
	if r.res.Truncated {
		return
	}
	if len(r.res.Rows) >= SqlMaxRows || r.size > sqlMaxResultSize {
		r.res.Truncated = true
		return
	}

	row := make([]*string, len(values))
	for i, value := range values {
		row[i] = formatSqlValue(value)
		if row[i] != nil {
			r.size += len(*row[i])
		}
	}
	r.res.Rows = append(r.res.Rows, row)
}
