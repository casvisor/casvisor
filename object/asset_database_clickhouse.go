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
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// clickhouseDriver speaks the native protocol of ClickHouse, port 9000, or 9440 with TLS.
type clickhouseDriver struct {
	c    *DatabaseClient
	db   *sql.DB
	conn *sql.Conn
}

var clickhouseQueryKeywords = map[string]bool{
	"SELECT": true, "SHOW": true, "DESC": true, "DESCRIBE": true, "EXPLAIN": true, "WITH": true, "EXISTS": true, "CHECK": true,
}

func quoteClickhouseName(name string) string {
	return "`" + strings.ReplaceAll(strings.ReplaceAll(name, `\`, `\\`), "`", "\\`") + "`"
}

func (d *clickhouseDriver) connect() error {
	c := d.c
	database := c.Asset.Database
	if database == "" {
		database = "default"
	}
	d.db = clickhouse.OpenDB(&clickhouse.Options{
		Addr: []string{net.JoinHostPort(c.Asset.Host, fmt.Sprintf("%d", c.Asset.Port))},
		Auth: clickhouse.Auth{Database: database, Username: c.Asset.Username, Password: c.Asset.Password},
		DialContext: func(ctx context.Context, addr string) (net.Conn, error) {
			return c.dialTls(ctx)
		},
		DialTimeout:  dialTimeout,
		MaxOpenConns: 1,
		ClientInfo:   clickhouse.ClientInfo{Products: []struct{ Name, Version string }{{Name: "casvisor", Version: "1"}}},
		Compression:  &clickhouse.Compression{Method: clickhouse.CompressionLZ4},
	})
	d.db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(c.ctx, 2*dialTimeout)
	defer cancel()
	var err error
	d.conn, err = d.db.Conn(ctx)
	if err != nil {
		return err
	}
	return d.conn.PingContext(ctx)
}

func (d *clickhouseDriver) close() {
	if d.conn != nil {
		_ = d.conn.Close()
	}
	if d.db != nil {
		_ = d.db.Close()
	}
}

func (d *clickhouseDriver) query(query string, args ...interface{}) ([][]string, error) {
	return queryConnStrings(d.c.ctx, d.conn, query, args...)
}

func (d *clickhouseDriver) getVersionAndDatabase() (string, string, error) {
	rows, err := d.query("SELECT version(), currentDatabase()")
	if err != nil {
		return "", "", err
	}
	if len(rows) == 0 || len(rows[0]) < 2 {
		return "", "", fmt.Errorf("failed to read the version of the database")
	}
	return rows[0][0], rows[0][1], nil
}

func (d *clickhouseDriver) getDatabases() ([]string, error) {
	rows, err := d.query("SELECT name FROM system.databases ORDER BY name")
	if err != nil {
		return nil, err
	}

	res := []string{}
	for _, row := range rows {
		res = append(res, row[0])
	}
	return res, nil
}

func (d *clickhouseDriver) getTables(database string) ([]*DatabaseTable, error) {
	rows, err := d.query("SELECT database, name, if(engine IN ('View', 'MaterializedView', 'LiveView', 'WindowView'), concat('VIEW ', engine), engine) FROM system.tables WHERE database = ? ORDER BY name", database)
	if err != nil {
		return nil, err
	}

	res := []*DatabaseTable{}
	for _, row := range rows {
		res = append(res, &DatabaseTable{Schema: row[0], Name: row[1], Type: row[2]})
	}
	return res, nil
}

func (d *clickhouseDriver) getColumns(database string, schema string, table string) ([]*DatabaseColumn, error) {
	rows, err := d.query("SELECT name, type, if(is_in_primary_key, 'PRI', if(is_in_sorting_key, 'MUL', '')) FROM system.columns WHERE database = ? AND table = ? ORDER BY position", database, table)
	if err != nil {
		return nil, err
	}

	res := []*DatabaseColumn{}
	for _, row := range rows {
		res = append(res, &DatabaseColumn{Name: row[0], Type: row[1], Key: row[2], Nullable: strings.HasPrefix(row[1], "Nullable(")})
	}
	return res, nil
}

func (d *clickhouseDriver) getUseStatement(database string) string {
	return "USE " + quoteClickhouseName(database)
}

func (d *clickhouseDriver) useDatabase(database string) error {
	_, err := d.conn.ExecContext(d.c.ctx, d.getUseStatement(database))
	return err
}

func (d *clickhouseDriver) execute(statement string) []*SqlResult {
	res := &SqlResult{}
	// the server takes a single statement, without the semicolon that ends it
	text := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(statement), ";"))
	keyword := getSqlKeyword(text, ProtocolClickhouse)
	err := d.executeStatement(text, keyword, res)
	if err != nil {
		res.Error = err.Error()
	}
	if keyword == "USE" && err == nil {
		rows, err := d.query("SELECT currentDatabase()")
		if err == nil && len(rows) > 0 {
			d.c.Database = rows[0][0]
		}
	}
	return []*SqlResult{res}
}

func (d *clickhouseDriver) executeStatement(statement string, keyword string, res *SqlResult) error {
	if !clickhouseQueryKeywords[keyword] {
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
		return formatClickhouseValue(value)
	})
}

// formatClickhouseValue writes the values the driver scans into pointers, slices and maps the way ClickHouse prints them.
func formatClickhouseValue(value interface{}) []byte {
	v := reflect.ValueOf(value)
	for v.IsValid() && v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return nil
	}
	value = v.Interface()
	switch x := value.(type) {
	case []byte, string, time.Time:
		return formatDriverValue(x)
	case fmt.Stringer:
		return []byte(x.String())
	}
	switch v.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.Struct:
		b, err := json.Marshal(value)
		if err == nil {
			return b
		}
	}
	return formatDriverValue(value)
}
