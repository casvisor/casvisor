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
	"strings"
	"sync"

	"gitee.com/chunanyong/dm"
)

const damengDialName = "casvisor"

var (
	damengDialOnce sync.Once
	damengClients  sync.Map
)

// damengDriver connects to a Dameng (DM8) database; like Oracle, its databases are the
// schemas, and switching to one sets the current schema.
type damengDriver struct {
	c    *DatabaseClient
	db   *sql.DB
	conn *sql.Conn
	key  string
}

var damengQueryKeywords = map[string]bool{"SELECT": true, "WITH": true}

func (d *damengDriver) connect() error {
	c := d.c
	damengDialOnce.Do(func() {
		dm.RegisterDialContext(damengDialName, func(ctx context.Context, addr string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				host = addr
			}
			value, ok := damengClients.Load(host)
			if !ok {
				return nil, fmt.Errorf("unknown database connection: %s", addr)
			}
			return value.(*DatabaseClient).dial()
		})
	})

	// the host of the address finds the client again in the dialer
	d.key = fmt.Sprintf("casvisor%p", c)
	d.key = strings.ReplaceAll(d.key, "0x", "")
	damengClients.Store(d.key, c)

	query := url.Values{}
	query.Set("dialName", damengDialName)
	query.Set("appName", "casvisor")
	if c.Asset.Database != "" {
		query.Set("schema", c.Asset.Database)
	}
	dsn := (&url.URL{
		Scheme:   "dm",
		User:     url.UserPassword(c.Asset.Username, c.Asset.Password),
		Host:     fmt.Sprintf("%s:%d", d.key, c.Asset.Port),
		RawQuery: query.Encode(),
	}).String()

	var err error
	d.db, err = sql.Open("dm", dsn)
	if err != nil {
		return err
	}
	d.db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(c.ctx, 2*dialTimeout)
	defer cancel()
	d.conn, err = d.db.Conn(ctx)
	if err != nil {
		return err
	}
	return d.conn.PingContext(ctx)
}

func (d *damengDriver) close() {
	if d.conn != nil {
		_ = d.conn.Close()
	}
	if d.db != nil {
		_ = d.db.Close()
	}
	if d.key != "" {
		damengClients.Delete(d.key)
	}
}

func (d *damengDriver) query(query string, args ...interface{}) ([][]string, error) {
	return queryConnStrings(d.c.ctx, d.conn, query, args...)
}

func (d *damengDriver) getCurrentSchema() (string, error) {
	rows, err := d.query("SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA')")
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", fmt.Errorf("failed to read the current schema")
	}
	return rows[0][0], nil
}

func (d *damengDriver) getVersionAndDatabase() (string, string, error) {
	schema, err := d.getCurrentSchema()
	if err != nil {
		return "", "", err
	}

	version := ""
	rows, err := d.query("SELECT BANNER FROM V$VERSION")
	if err == nil && len(rows) > 0 {
		version = rows[0][0]
	}
	return version, schema, nil
}

func (d *damengDriver) getDatabases() ([]string, error) {
	rows, err := d.query("SELECT DISTINCT OWNER FROM ALL_OBJECTS WHERE OBJECT_TYPE IN ('TABLE', 'VIEW') ORDER BY 1")
	if err != nil {
		return nil, err
	}

	res := []string{}
	for _, row := range rows {
		res = append(res, row[0])
	}
	return res, nil
}

func (d *damengDriver) getTables(database string) ([]*DatabaseTable, error) {
	rows, err := d.query("SELECT OWNER, TABLE_NAME, 'TABLE' FROM ALL_TABLES WHERE OWNER = ? AND TABLE_NAME NOT LIKE '##%' UNION ALL SELECT OWNER, VIEW_NAME, 'VIEW' FROM ALL_VIEWS WHERE OWNER = ? ORDER BY 2", database, database)
	if err != nil {
		return nil, err
	}

	res := []*DatabaseTable{}
	for _, row := range rows {
		res = append(res, &DatabaseTable{Schema: row[0], Name: row[1], Type: row[2]})
	}
	return res, nil
}

func (d *damengDriver) getColumns(database string, schema string, table string) ([]*DatabaseColumn, error) {
	rows, err := d.query(`SELECT c.COLUMN_NAME,
  c.DATA_TYPE || CASE WHEN c.DATA_TYPE IN ('VARCHAR', 'VARCHAR2', 'CHAR', 'NVARCHAR', 'NCHAR', 'VARBINARY', 'BINARY') THEN '(' || c.DATA_LENGTH || ')' ELSE '' END,
  CASE WHEN EXISTS (
    SELECT 1 FROM ALL_CONSTRAINTS k JOIN ALL_CONS_COLUMNS cc ON cc.OWNER = k.OWNER AND cc.CONSTRAINT_NAME = k.CONSTRAINT_NAME
    WHERE k.CONSTRAINT_TYPE = 'P' AND k.OWNER = c.OWNER AND k.TABLE_NAME = c.TABLE_NAME AND cc.COLUMN_NAME = c.COLUMN_NAME
  ) THEN 'PRI' ELSE '' END, c.NULLABLE
FROM ALL_TAB_COLUMNS c WHERE c.OWNER = ? AND c.TABLE_NAME = ? ORDER BY c.COLUMN_ID`, schema, table)
	if err != nil {
		return nil, err
	}

	res := []*DatabaseColumn{}
	for _, row := range rows {
		res = append(res, &DatabaseColumn{Name: row[0], Type: row[1], Key: row[2], Nullable: row[3] == "Y"})
	}
	return res, nil
}

func (d *damengDriver) getUseStatement(database string) string {
	return "SET SCHEMA " + quoteOracleName(database)
}

func (d *damengDriver) useDatabase(database string) error {
	_, err := d.conn.ExecContext(d.c.ctx, d.getUseStatement(database))
	return err
}

func (d *damengDriver) execute(statement string) []*SqlResult {
	res := &SqlResult{}
	text := statement
	if !isOraclePlsqlBlock(statement) {
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(statement), ";"))
	}

	keyword := getSqlKeyword(text, ProtocolDameng)
	err := d.executeStatement(text, keyword, res)
	if err != nil {
		res.Error = err.Error()
	}
	if (keyword == "SET" || keyword == "ALTER") && err == nil {
		schema, err := d.getCurrentSchema()
		if err == nil {
			d.c.Database = schema
		}
	}
	return []*SqlResult{res}
}

func (d *damengDriver) executeStatement(statement string, keyword string, res *SqlResult) error {
	if !damengQueryKeywords[keyword] {
		result, err := d.conn.ExecContext(d.c.ctx, statement)
		if err != nil {
			return err
		}
		res.RowCount, _ = result.RowsAffected()
		return nil
	}

	rows, err := d.conn.QueryContext(d.c.ctx, statement)
	if err != nil {
		return err
	}
	defer rows.Close()
	return readSqlRows(rows, res, func(value interface{}, columnType *sql.ColumnType) []byte {
		return formatDamengValue(value)
	})
}

// formatDamengValue reads the large objects the driver returns as handles.
func formatDamengValue(value interface{}) []byte {
	switch v := value.(type) {
	case *dm.DmClob:
		length, err := v.GetLength()
		if err != nil {
			return []byte(err.Error())
		}
		if length > sqlMaxCellSize {
			length = sqlMaxCellSize
		}
		s, err := v.ReadString(1, int(length))
		if err != nil {
			return []byte(err.Error())
		}
		return []byte(s)
	case *dm.DmBlob:
		length, err := v.GetLength()
		if err != nil {
			return []byte(err.Error())
		}
		if length > sqlMaxCellSize {
			length = sqlMaxCellSize
		}
		b := make([]byte, length)
		n, err := v.Read(b)
		if err != nil && n == 0 {
			return []byte(err.Error())
		}
		return b[:n]
	}
	return formatDriverValue(value)
}
