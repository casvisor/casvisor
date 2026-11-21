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

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	ProtocolMysql      = "MySQL"
	ProtocolPostgresql = "PostgreSQL"

	SqlMaxRows = 1000
	// the longest value of a cell that is kept, and all the cells of a result together
	sqlMaxCellSize   = 64 * 1024
	sqlMaxResultSize = 8 * 1024 * 1024

	mysqlDialNetwork = "casvisor"
)

func IsDatabaseProtocol(protocol string) bool {
	return protocol == ProtocolMysql || protocol == ProtocolPostgresql
}

type SqlColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// SqlResult is how a statement ran: the rows it returned, at most SqlMaxRows
// of them, or the rows it changed.
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

	db       *sql.DB
	conn     *sql.Conn
	pg       *pgx.Conn
	mysqlKey string
}

var (
	mysqlDialOnce sync.Once
	mysqlClients  sync.Map
)

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

func DialDatabase(asset *Asset) (*DatabaseClient, error) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &DatabaseClient{Asset: asset, Database: asset.Database, ctx: ctx, cancel: cancel}

	var err error
	switch asset.Protocol {
	case ProtocolMysql:
		err = c.connectMysql()
	case ProtocolPostgresql:
		c.pg, err = c.connectPostgresql(asset.Database)
	default:
		err = fmt.Errorf("the protocol: %s is not a database protocol", asset.Protocol)
	}
	if err != nil {
		c.Close()
		return nil, err
	}

	c.Version, c.Database, err = c.getVersionAndDatabase()
	if err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *DatabaseClient) connectMysql() error {
	mysqlDialOnce.Do(func() {
		mysql.RegisterDialContext(mysqlDialNetwork, func(ctx context.Context, addr string) (net.Conn, error) {
			value, ok := mysqlClients.Load(addr)
			if !ok {
				return nil, fmt.Errorf("unknown database connection: %s", addr)
			}
			return value.(*DatabaseClient).dial()
		})
	})

	c.mysqlKey = fmt.Sprintf("%p", c)
	mysqlClients.Store(c.mysqlKey, c)

	cfg := mysql.NewConfig()
	cfg.Net = mysqlDialNetwork
	cfg.Addr = c.mysqlKey
	cfg.User = c.Asset.Username
	cfg.Passwd = c.Asset.Password
	cfg.DBName = c.Asset.Database
	cfg.TLSConfig = "preferred"
	cfg.Timeout = dialTimeout

	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return err
	}

	c.db = sql.OpenDB(connector)
	c.db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(c.ctx, 2*dialTimeout)
	defer cancel()
	c.conn, err = c.db.Conn(ctx)
	return err
}

func (c *DatabaseClient) connectPostgresql(database string) (*pgx.Conn, error) {
	config, err := pgx.ParseConfig("")
	if err != nil {
		return nil, err
	}

	asset := c.Asset
	config.Host = asset.Host
	config.Port = uint16(asset.Port)
	config.User = asset.Username
	config.Password = asset.Password
	config.Database = database
	config.ConnectTimeout = 2 * dialTimeout
	config.RuntimeParams = map[string]string{"application_name": "casvisor"}
	// sslmode=prefer: TLS when the server has it, plain otherwise
	config.TLSConfig = &tls.Config{InsecureSkipVerify: true, ServerName: asset.Host}
	config.Fallbacks = []*pgconn.FallbackConfig{{Host: asset.Host, Port: uint16(asset.Port)}}
	config.LookupFunc = func(ctx context.Context, host string) ([]string, error) {
		return []string{host}, nil
	}
	config.DialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return c.dial()
	}

	ctx, cancel := context.WithTimeout(c.ctx, 2*dialTimeout)
	defer cancel()
	return pgx.ConnectConfig(ctx, config)
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
		if c.conn != nil {
			_ = c.conn.Close()
		}
		if c.db != nil {
			_ = c.db.Close()
		}
		if c.mysqlKey != "" {
			mysqlClients.Delete(c.mysqlKey)
		}
		if c.pg != nil {
			_ = c.pg.Close(context.Background())
		}
	})
}

func (c *DatabaseClient) isMysql() bool {
	return c.Asset.Protocol == ProtocolMysql
}

func (c *DatabaseClient) exec(statement string) error {
	ctx, cancel := context.WithTimeout(c.ctx, 2*dialTimeout)
	defer cancel()

	if c.isMysql() {
		_, err := c.conn.ExecContext(ctx, statement)
		return err
	}
	rr := c.pg.PgConn().ExecParams(ctx, statement, nil, nil, nil, nil)
	_, err := rr.Close()
	return err
}

// queryStrings runs a query of the client itself, not of the user, and returns its rows as strings.
func (c *DatabaseClient) queryStrings(query string, args ...interface{}) ([][]string, error) {
	res := [][]string{}
	if c.isMysql() {
		rows, err := c.conn.QueryContext(c.ctx, query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		columns, err := rows.Columns()
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			values := make([]sql.NullString, len(columns))
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
				row[i] = value.String
			}
			res = append(res, row)
		}
		return res, rows.Err()
	}

	rows, err := c.pg.Query(c.ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := []string{}
		for _, value := range values {
			if value == nil {
				row = append(row, "")
			} else {
				row = append(row, fmt.Sprint(value))
			}
		}
		res = append(res, row)
	}
	return res, rows.Err()
}

func (c *DatabaseClient) getVersionAndDatabase() (string, string, error) {
	query := "SELECT VERSION(), COALESCE(DATABASE(), '')"
	if !c.isMysql() {
		query = "SELECT current_setting('server_version'), current_database()"
	}
	rows, err := c.queryStrings(query)
	if err != nil {
		return "", "", err
	}
	if len(rows) == 0 || len(rows[0]) < 2 {
		return "", "", fmt.Errorf("failed to read the version of the database")
	}
	return rows[0][0], rows[0][1], nil
}

func (c *DatabaseClient) GetDatabases() ([]string, error) {
	query := "SHOW DATABASES"
	if !c.isMysql() {
		query = "SELECT datname FROM pg_database WHERE datallowconn AND NOT datistemplate ORDER BY datname"
	}
	rows, err := c.queryStrings(query)
	if err != nil {
		return nil, err
	}

	res := []string{}
	for _, row := range rows {
		res = append(res, row[0])
	}
	return res, nil
}

// GetTables lists the tables and views of a database; a PostgreSQL connection
// only sees the database it is connected to.
func (c *DatabaseClient) GetTables(database string) ([]*DatabaseTable, error) {
	var rows [][]string
	var err error
	if c.isMysql() {
		rows, err = c.queryStrings("SELECT TABLE_SCHEMA, TABLE_NAME, TABLE_TYPE FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME", database)
	} else {
		if database != c.Database {
			return nil, fmt.Errorf("switch to the database: %s to see its tables", database)
		}
		rows, err = c.queryStrings("SELECT table_schema, table_name, table_type FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog', 'information_schema') ORDER BY table_schema <> 'public', table_schema, table_name")
	}
	if err != nil {
		return nil, err
	}

	res := []*DatabaseTable{}
	for _, row := range rows {
		res = append(res, &DatabaseTable{Schema: row[0], Name: row[1], Type: row[2]})
	}
	return res, nil
}

func (c *DatabaseClient) GetColumns(database string, schema string, table string) ([]*DatabaseColumn, error) {
	var rows [][]string
	var err error
	if c.isMysql() {
		rows, err = c.queryStrings("SELECT COLUMN_NAME, COLUMN_TYPE, COLUMN_KEY, IS_NULLABLE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION", database, table)
	} else {
		if database != c.Database {
			return nil, fmt.Errorf("switch to the database: %s to see its tables", database)
		}
		rows, err = c.queryStrings(`SELECT c.column_name, c.data_type,
  CASE WHEN EXISTS (
    SELECT 1 FROM information_schema.table_constraints t JOIN information_schema.key_column_usage k
      ON k.constraint_name = t.constraint_name AND k.constraint_schema = t.constraint_schema
    WHERE t.constraint_type = 'PRIMARY KEY' AND t.table_schema = c.table_schema AND t.table_name = c.table_name AND k.column_name = c.column_name
  ) THEN 'PRI' ELSE '' END, c.is_nullable
FROM information_schema.columns c WHERE c.table_schema = $1 AND c.table_name = $2 ORDER BY c.ordinal_position`, schema, table)
	}
	if err != nil {
		return nil, err
	}

	res := []*DatabaseColumn{}
	for _, row := range rows {
		res = append(res, &DatabaseColumn{Name: row[0], Type: row[1], Key: row[2], Nullable: row[3] == "YES"})
	}
	return res, nil
}

// GetUseStatement is how switching to a database is shown in the command records.
func (c *DatabaseClient) GetUseStatement(database string) string {
	if c.isMysql() {
		return fmt.Sprintf("USE `%s`", strings.ReplaceAll(database, "`", "``"))
	}
	return fmt.Sprintf("\\connect %s", database)
}

// UseDatabase makes a database the default one; PostgreSQL connects to it anew.
func (c *DatabaseClient) UseDatabase(database string) error {
	if c.isMysql() {
		_, err := c.conn.ExecContext(c.ctx, c.GetUseStatement(database))
		if err != nil {
			return err
		}
		c.Database = database
		return nil
	}

	conn, err := c.connectPostgresql(database)
	if err != nil {
		return err
	}
	old := c.pg
	c.pg = conn
	c.Database = database
	_ = old.Close(c.ctx)
	return nil
}

var mysqlQueryKeywords = map[string]bool{
	"SELECT": true, "SHOW": true, "DESC": true, "DESCRIBE": true, "EXPLAIN": true, "WITH": true, "TABLE": true,
	"VALUES": true, "HELP": true, "CALL": true, "CHECK": true, "ANALYZE": true, "OPTIMIZE": true, "REPAIR": true, "CHECKSUM": true,
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

// Execute runs one statement typed by the user.
func (c *DatabaseClient) Execute(statement string) *SqlResult {
	res := &SqlResult{Statement: statement}
	start := time.Now()

	var err error
	if c.isMysql() {
		err = c.executeMysql(statement, res)
	} else {
		err = c.executePostgresql(statement, res)
	}

	res.Duration = time.Since(start).Milliseconds()
	if err != nil {
		res.Error = err.Error()
	}
	return res
}

func (c *DatabaseClient) executeMysql(statement string, res *SqlResult) error {
	if !mysqlQueryKeywords[getSqlKeyword(statement, ProtocolMysql)] {
		result, err := c.conn.ExecContext(c.ctx, statement)
		if err != nil {
			return err
		}
		res.RowCount, _ = result.RowsAffected()
		return nil
	}

	rows, err := c.conn.QueryContext(c.ctx, statement)
	if err != nil {
		return err
	}
	defer rows.Close()

	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return err
	}
	if len(columnTypes) == 0 {
		return rows.Err()
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
			switch v := value.(type) {
			case nil:
			case []byte:
				row[i] = v
			default:
				row[i] = []byte(fmt.Sprint(v))
			}
		}
		r.add(row)
	}
	return rows.Err()
}

func (c *DatabaseClient) executePostgresql(statement string, res *SqlResult) error {
	// the extended protocol runs a single statement, like the MySQL connection does
	rr := c.pg.PgConn().ExecParams(c.ctx, statement, nil, nil, nil, nil)
	fields := rr.FieldDescriptions()
	if len(fields) > 0 {
		res.Columns = []SqlColumn{}
		for _, field := range fields {
			typeName := strconv.FormatUint(uint64(field.DataTypeOID), 10)
			if t, ok := c.pg.TypeMap().TypeForOID(field.DataTypeOID); ok {
				typeName = t.Name
			}
			res.Columns = append(res.Columns, SqlColumn{Name: field.Name, Type: typeName})
		}
		res.Rows = [][]*string{}
	}

	r := resultRows{res: res}
	for rr.NextRow() {
		r.add(rr.Values())
	}
	tag, err := rr.Close()
	if err != nil {
		return err
	}
	if len(fields) == 0 {
		res.RowCount = tag.RowsAffected()
	}
	return nil
}
