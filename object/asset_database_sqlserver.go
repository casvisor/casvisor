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
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/golang-sql/sqlexp"
	mssql "github.com/microsoft/go-mssqldb"
	"github.com/microsoft/go-mssqldb/msdsn"
)

// sqlServerDriver runs each batch the way SQL Server Management Studio does: the
// text between GO lines goes to the server as a whole, with all its results.
type sqlServerDriver struct {
	c    *DatabaseClient
	db   *sql.DB
	conn *sql.Conn
}

// a batch that may switch the database, after which the current one is read again
var sqlServerUseStatement = regexp.MustCompile(`(?i)\bUSE\s`)

func quoteSqlServerName(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
}

func (d *sqlServerDriver) connect() error {
	err := d.connectWithEncryption(msdsn.EncryptionRequired)
	// a server that has no certificate at all, allowed only until a public key is pinned
	if err != nil && d.c.getPinnedFingerprint() == "" && strings.Contains(strings.ToLower(err.Error()), "not support encryption") {
		d.close()
		d.c.plainText = true
		err = d.connectWithEncryption(msdsn.EncryptionDisabled)
	}
	return err
}

func (d *sqlServerDriver) connectWithEncryption(encryption msdsn.Encryption) error {
	c := d.c
	query := url.Values{}
	query.Set("app name", "casvisor")
	if c.Asset.Database != "" {
		query.Set("database", c.Asset.Database)
	}
	u := &url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(c.Asset.Username, c.Asset.Password),
		Host:     net.JoinHostPort(c.Asset.Host, strconv.Itoa(c.Asset.Port)),
		RawQuery: query.Encode(),
	}
	config, err := msdsn.Parse(u.String())
	if err != nil {
		return err
	}
	config.Encryption = encryption
	config.TLSConfig = c.getTlsConfig()
	config.DialTimeout = dialTimeout
	config.ConnTimeout = 2 * dialTimeout

	connector := mssql.NewConnectorConfig(config)
	connector.Dialer = databaseDialer{dial: func(ctx context.Context) (net.Conn, error) { return c.dial() }}

	d.db = sql.OpenDB(connector)
	d.db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(c.ctx, 2*dialTimeout)
	defer cancel()
	d.conn, err = d.db.Conn(ctx)
	if err != nil {
		return err
	}
	return d.conn.PingContext(ctx)
}

func (d *sqlServerDriver) close() {
	if d.conn != nil {
		_ = d.conn.Close()
		d.conn = nil
	}
	if d.db != nil {
		_ = d.db.Close()
		d.db = nil
	}
}

func (d *sqlServerDriver) query(query string, args ...interface{}) ([][]string, error) {
	return queryConnStrings(d.c.ctx, d.conn, query, args...)
}

func (d *sqlServerDriver) getVersionAndDatabase() (string, string, error) {
	rows, err := d.query("SELECT CAST(SERVERPROPERTY('ProductVersion') AS nvarchar(128)) + N' ' + CAST(SERVERPROPERTY('Edition') AS nvarchar(128)), DB_NAME()")
	if err != nil {
		return "", "", err
	}
	if len(rows) == 0 || len(rows[0]) < 2 {
		return "", "", fmt.Errorf("failed to read the version of the database")
	}
	return rows[0][0], rows[0][1], nil
}

func (d *sqlServerDriver) getDatabases() ([]string, error) {
	rows, err := d.query("SELECT name FROM sys.databases WHERE HAS_DBACCESS(name) = 1 ORDER BY name")
	if err != nil {
		return nil, err
	}

	res := []string{}
	for _, row := range rows {
		res = append(res, row[0])
	}
	return res, nil
}

func (d *sqlServerDriver) getTables(database string) ([]*DatabaseTable, error) {
	db := quoteSqlServerName(database)
	rows, err := d.query(fmt.Sprintf("SELECT TABLE_SCHEMA, TABLE_NAME, TABLE_TYPE FROM %s.INFORMATION_SCHEMA.TABLES ORDER BY CASE WHEN TABLE_SCHEMA = 'dbo' THEN 0 ELSE 1 END, TABLE_SCHEMA, TABLE_NAME", db))
	if err != nil {
		return nil, err
	}

	res := []*DatabaseTable{}
	for _, row := range rows {
		res = append(res, &DatabaseTable{Schema: row[0], Name: row[1], Type: row[2]})
	}
	return res, nil
}

func (d *sqlServerDriver) getColumns(database string, schema string, table string) ([]*DatabaseColumn, error) {
	db := quoteSqlServerName(database)
	rows, err := d.query(fmt.Sprintf(`SELECT c.COLUMN_NAME,
  c.DATA_TYPE + CASE WHEN c.CHARACTER_MAXIMUM_LENGTH = -1 THEN '(max)'
    WHEN c.CHARACTER_MAXIMUM_LENGTH IS NOT NULL THEN '(' + CAST(c.CHARACTER_MAXIMUM_LENGTH AS varchar(10)) + ')' ELSE '' END,
  CASE WHEN EXISTS (
    SELECT 1 FROM %[1]s.INFORMATION_SCHEMA.TABLE_CONSTRAINTS t JOIN %[1]s.INFORMATION_SCHEMA.KEY_COLUMN_USAGE k
      ON k.CONSTRAINT_NAME = t.CONSTRAINT_NAME AND k.CONSTRAINT_SCHEMA = t.CONSTRAINT_SCHEMA
    WHERE t.CONSTRAINT_TYPE = 'PRIMARY KEY' AND t.TABLE_SCHEMA = c.TABLE_SCHEMA AND t.TABLE_NAME = c.TABLE_NAME AND k.COLUMN_NAME = c.COLUMN_NAME
  ) THEN 'PRI' ELSE '' END, c.IS_NULLABLE
FROM %[1]s.INFORMATION_SCHEMA.COLUMNS c WHERE c.TABLE_SCHEMA = @p1 AND c.TABLE_NAME = @p2 ORDER BY c.ORDINAL_POSITION`, db), schema, table)
	if err != nil {
		return nil, err
	}

	res := []*DatabaseColumn{}
	for _, row := range rows {
		res = append(res, &DatabaseColumn{Name: row[0], Type: row[1], Key: row[2], Nullable: row[3] == "YES"})
	}
	return res, nil
}

func (d *sqlServerDriver) getUseStatement(database string) string {
	return "USE " + quoteSqlServerName(database)
}

func (d *sqlServerDriver) useDatabase(database string) error {
	_, err := d.conn.ExecContext(d.c.ctx, d.getUseStatement(database))
	return err
}

func formatSqlServerValue(value interface{}, columnType *sql.ColumnType) []byte {
	if b, ok := value.([]byte); ok && len(b) == 16 && columnType.DatabaseTypeName() == "UNIQUEIDENTIFIER" {
		var id mssql.UniqueIdentifier
		if id.Scan(b) == nil {
			return []byte(id.String())
		}
	}
	return formatDriverValue(value)
}

// execute runs a batch and returns a result for each of its result sets and
// changed row counts, and the messages it printed.
func (d *sqlServerDriver) execute(statement string) []*SqlResult {
	ctx := d.c.ctx
	ret := &sqlexp.ReturnMessage{}
	rows, err := d.conn.QueryContext(ctx, statement, ret)
	if err != nil {
		return newErrorResult(err)
	}
	defer rows.Close()

	results := []*SqlResult{}
	messages := []string{}
	var last *SqlResult
	// a result set is followed by the count of its rows, which is already known
	counted := true
	add := func(res *SqlResult) bool {
		if len(results) >= sqlMaxBatchResults {
			return false
		}
		results = append(results, res)
		last = res
		return true
	}

	active := true
	for active {
		switch m := ret.Message(ctx).(type) {
		case sqlexp.MsgNext:
			res := &SqlResult{}
			readErr := readSqlRows(rows, res, formatSqlServerValue)
			if readErr != nil {
				res.Error = readErr.Error()
			}
			if !add(res) {
				active = rows.NextResultSet()
				continue
			}
			counted = false
		case sqlexp.MsgNextResultSet:
			active = rows.NextResultSet()
		case sqlexp.MsgRowsAffected:
			if !counted && last != nil && last.Columns != nil {
				counted = true
				continue
			}
			add(&SqlResult{RowCount: m.Count})
		case sqlexp.MsgNotice:
			messages = append(messages, m.Message.String())
		case sqlexp.MsgError:
			// an error is shown on the empty result it broke off, like SELECT 1/0
			if last != nil && last.Error == "" && last.RowCount == 0 {
				last.Error = m.Error.Error()
				last.Columns = nil
				last.Rows = nil
			} else {
				add(&SqlResult{Error: m.Error.Error()})
			}
		case nil:
			active = false
		}
	}
	if err = rows.Err(); err != nil && (last == nil || last.Error == "") {
		add(&SqlResult{Error: err.Error()})
	}

	if len(messages) > 0 {
		res := &SqlResult{Columns: []SqlColumn{{Name: "message"}}, Rows: [][]*string{}}
		for _, message := range messages {
			text := message
			res.Rows = append(res.Rows, []*string{&text})
		}
		res.RowCount = int64(len(res.Rows))
		results = append(results, res)
	}
	if len(results) == 0 {
		results = append(results, &SqlResult{})
	}

	if sqlServerUseStatement.MatchString(NormalizeSqlStatement(statement, ProtocolSqlServer)) {
		rows, err := d.query("SELECT DB_NAME()")
		if err == nil && len(rows) > 0 {
			d.c.Database = rows[0][0]
		}
	}
	return results
}
