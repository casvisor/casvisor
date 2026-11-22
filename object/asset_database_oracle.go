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

	go_ora "github.com/sijms/go-ora/v2"
)

// oracleDriver connects to the service named in the database of the asset; its
// databases are the schemas, and switching to one sets the current schema.
type oracleDriver struct {
	c    *DatabaseClient
	db   *sql.DB
	conn *sql.Conn
}

var oracleQueryKeywords = map[string]bool{"SELECT": true, "WITH": true}

func quoteOracleName(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func (d *oracleDriver) connect() error {
	c := d.c
	if c.Asset.Database == "" {
		return fmt.Errorf("fill in the service name of the Oracle database in the database of the asset, such as ORCLPDB1 or FREEPDB1")
	}

	connector, ok := go_ora.NewConnector(go_ora.BuildUrl(c.Asset.Host, c.Asset.Port, c.Asset.Database, c.Asset.Username, c.Asset.Password, nil)).(*go_ora.OracleConnector)
	if !ok {
		return fmt.Errorf("failed to create the Oracle connector")
	}
	connector.Dialer(databaseDialer{dial: func(ctx context.Context) (net.Conn, error) { return c.dial() }})

	d.db = sql.OpenDB(connector)
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

func (d *oracleDriver) close() {
	if d.conn != nil {
		_ = d.conn.Close()
	}
	if d.db != nil {
		_ = d.db.Close()
	}
}

func (d *oracleDriver) query(query string, args ...interface{}) ([][]string, error) {
	return queryConnStrings(d.c.ctx, d.conn, query, args...)
}

func (d *oracleDriver) getCurrentSchema() (string, error) {
	rows, err := d.query("SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') FROM DUAL")
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", fmt.Errorf("failed to read the current schema")
	}
	return rows[0][0], nil
}

func (d *oracleDriver) getVersionAndDatabase() (string, string, error) {
	schema, err := d.getCurrentSchema()
	if err != nil {
		return "", "", err
	}

	// the banner reads like "Oracle Database 19c Enterprise Edition Release 19.0.0.0.0 - Production"
	version := ""
	rows, err := d.query("SELECT banner FROM v$version WHERE banner LIKE 'Oracle%'")
	if err == nil && len(rows) > 0 {
		version, _, _ = strings.Cut(strings.TrimPrefix(rows[0][0], "Oracle "), " - ")
	}
	return version, schema, nil
}

func (d *oracleDriver) getDatabases() ([]string, error) {
	rows, err := d.query("SELECT owner FROM all_tables UNION SELECT owner FROM all_views ORDER BY 1")
	if err != nil {
		return nil, err
	}

	res := []string{}
	for _, row := range rows {
		res = append(res, row[0])
	}
	return res, nil
}

func (d *oracleDriver) getTables(database string) ([]*DatabaseTable, error) {
	rows, err := d.query("SELECT owner, table_name, 'TABLE' FROM all_tables WHERE owner = :1 UNION ALL SELECT owner, view_name, 'VIEW' FROM all_views WHERE owner = :2 ORDER BY 2", database, database)
	if err != nil {
		return nil, err
	}

	res := []*DatabaseTable{}
	for _, row := range rows {
		res = append(res, &DatabaseTable{Schema: row[0], Name: row[1], Type: row[2]})
	}
	return res, nil
}

func (d *oracleDriver) getColumns(database string, schema string, table string) ([]*DatabaseColumn, error) {
	rows, err := d.query(`SELECT c.column_name,
  c.data_type || CASE WHEN c.data_type IN ('VARCHAR2', 'NVARCHAR2', 'CHAR', 'NCHAR', 'RAW') THEN '(' || c.char_length || ')' ELSE '' END,
  CASE WHEN EXISTS (
    SELECT 1 FROM all_constraints k JOIN all_cons_columns cc ON cc.owner = k.owner AND cc.constraint_name = k.constraint_name
    WHERE k.constraint_type = 'P' AND k.owner = c.owner AND k.table_name = c.table_name AND cc.column_name = c.column_name
  ) THEN 'PRI' ELSE '' END, c.nullable
FROM all_tab_columns c WHERE c.owner = :1 AND c.table_name = :2 ORDER BY c.column_id`, schema, table)
	if err != nil {
		return nil, err
	}

	res := []*DatabaseColumn{}
	for _, row := range rows {
		res = append(res, &DatabaseColumn{Name: row[0], Type: row[1], Key: row[2], Nullable: row[3] == "Y"})
	}
	return res, nil
}

func (d *oracleDriver) getUseStatement(database string) string {
	return "ALTER SESSION SET CURRENT_SCHEMA = " + quoteOracleName(database)
}

func (d *oracleDriver) useDatabase(database string) error {
	_, err := d.conn.ExecContext(d.c.ctx, d.getUseStatement(database))
	return err
}

func (d *oracleDriver) execute(statement string) []*SqlResult {
	res := &SqlResult{}
	text := statement
	// the server takes the closing semicolon only as the end of a PL/SQL block
	if !isOraclePlsqlBlock(statement) {
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(statement), ";"))
	}

	keyword := getSqlKeyword(text, ProtocolOracle)
	err := d.executeStatement(text, keyword, res)
	if err != nil {
		res.Error = err.Error()
	}
	if keyword == "ALTER" && err == nil {
		schema, err := d.getCurrentSchema()
		if err == nil {
			d.c.Database = schema
		}
	}
	return []*SqlResult{res}
}

func (d *oracleDriver) executeStatement(statement string, keyword string, res *SqlResult) error {
	if !oracleQueryKeywords[keyword] {
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
		return formatDriverValue(value)
	})
}
