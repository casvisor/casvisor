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
	"strings"
	"sync"

	"github.com/go-sql-driver/mysql"
)

const mysqlDialNetwork = "casvisor"

var (
	mysqlDialOnce sync.Once
	mysqlClients  sync.Map
)

type mysqlDriver struct {
	c    *DatabaseClient
	db   *sql.DB
	conn *sql.Conn
	key  string
}

var mysqlQueryKeywords = map[string]bool{
	"SELECT": true, "SHOW": true, "DESC": true, "DESCRIBE": true, "EXPLAIN": true, "WITH": true, "TABLE": true,
	"VALUES": true, "HELP": true, "CALL": true, "CHECK": true, "ANALYZE": true, "OPTIMIZE": true, "REPAIR": true, "CHECKSUM": true,
}

func (d *mysqlDriver) connect() error {
	c := d.c
	mysqlDialOnce.Do(func() {
		mysql.RegisterDialContext(mysqlDialNetwork, func(ctx context.Context, addr string) (net.Conn, error) {
			value, ok := mysqlClients.Load(addr)
			if !ok {
				return nil, fmt.Errorf("unknown database connection: %s", addr)
			}
			return value.(*DatabaseClient).dial()
		})
	})

	d.key = fmt.Sprintf("%p", c)
	mysqlClients.Store(d.key, c)

	cfg := mysql.NewConfig()
	cfg.Net = mysqlDialNetwork
	cfg.Addr = d.key
	cfg.User = c.Asset.Username
	cfg.Passwd = c.Asset.Password
	cfg.DBName = c.Asset.Database
	cfg.TLS = c.getTlsConfig()
	// plain text only for a server that never had TLS
	cfg.AllowFallbackToPlaintext = c.getPinnedFingerprint() == ""
	cfg.Timeout = dialTimeout

	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return err
	}

	d.db = sql.OpenDB(connector)
	d.db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(c.ctx, 2*dialTimeout)
	defer cancel()
	d.conn, err = d.db.Conn(ctx)
	return err
}

func (d *mysqlDriver) close() {
	if d.conn != nil {
		_ = d.conn.Close()
	}
	if d.db != nil {
		_ = d.db.Close()
	}
	if d.key != "" {
		mysqlClients.Delete(d.key)
	}
}

func (d *mysqlDriver) query(query string, args ...interface{}) ([][]string, error) {
	return queryConnStrings(d.c.ctx, d.conn, query, args...)
}

func (d *mysqlDriver) getVersionAndDatabase() (string, string, error) {
	rows, err := d.query("SELECT VERSION(), COALESCE(DATABASE(), '')")
	if err != nil {
		return "", "", err
	}
	if len(rows) == 0 || len(rows[0]) < 2 {
		return "", "", fmt.Errorf("failed to read the version of the database")
	}
	return rows[0][0], rows[0][1], nil
}

func (d *mysqlDriver) getDatabases() ([]string, error) {
	rows, err := d.query("SHOW DATABASES")
	if err != nil {
		return nil, err
	}

	res := []string{}
	for _, row := range rows {
		res = append(res, row[0])
	}
	return res, nil
}

func (d *mysqlDriver) getTables(database string) ([]*DatabaseTable, error) {
	rows, err := d.query("SELECT TABLE_SCHEMA, TABLE_NAME, TABLE_TYPE FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME", database)
	if err != nil {
		return nil, err
	}

	res := []*DatabaseTable{}
	for _, row := range rows {
		res = append(res, &DatabaseTable{Schema: row[0], Name: row[1], Type: row[2]})
	}
	return res, nil
}

func (d *mysqlDriver) getColumns(database string, schema string, table string) ([]*DatabaseColumn, error) {
	rows, err := d.query("SELECT COLUMN_NAME, COLUMN_TYPE, COLUMN_KEY, IS_NULLABLE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION", database, table)
	if err != nil {
		return nil, err
	}

	res := []*DatabaseColumn{}
	for _, row := range rows {
		res = append(res, &DatabaseColumn{Name: row[0], Type: row[1], Key: row[2], Nullable: row[3] == "YES"})
	}
	return res, nil
}

func (d *mysqlDriver) getUseStatement(database string) string {
	return fmt.Sprintf("USE `%s`", strings.ReplaceAll(database, "`", "``"))
}

func (d *mysqlDriver) useDatabase(database string) error {
	_, err := d.conn.ExecContext(d.c.ctx, d.getUseStatement(database))
	return err
}

func (d *mysqlDriver) execute(statement string) []*SqlResult {
	res := &SqlResult{}
	keyword := getSqlKeyword(statement, ProtocolMysql)
	err := d.executeStatement(statement, keyword, res)
	if err != nil {
		res.Error = err.Error()
	}
	if keyword == "USE" && err == nil {
		rows, err := d.query("SELECT COALESCE(DATABASE(), '')")
		if err == nil && len(rows) > 0 {
			d.c.Database = rows[0][0]
		}
	}
	return []*SqlResult{res}
}

func (d *mysqlDriver) executeStatement(statement string, keyword string, res *SqlResult) error {
	if !mysqlQueryKeywords[keyword] {
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

	columns, err := rows.Columns()
	if err != nil {
		return err
	}
	if len(columns) == 0 {
		return rows.Err()
	}
	return readSqlRows(rows, res, func(value interface{}, columnType *sql.ColumnType) []byte {
		return formatDriverValue(value)
	})
}
