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
	"fmt"
	"net"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type postgresqlDriver struct {
	c  *DatabaseClient
	pg *pgx.Conn
}

func (d *postgresqlDriver) connect() error {
	var err error
	d.pg, err = d.connectDatabase(d.c.Asset.Database)
	return err
}

func (d *postgresqlDriver) connectDatabase(database string) (*pgx.Conn, error) {
	c := d.c
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
	// TLS when the server has it, plain otherwise until a public key is pinned
	config.TLSConfig = c.getTlsConfig()
	config.Fallbacks = nil
	if c.getPinnedFingerprint() == "" {
		config.Fallbacks = []*pgconn.FallbackConfig{{Host: asset.Host, Port: uint16(asset.Port)}}
	}
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

func (d *postgresqlDriver) close() {
	if d.pg != nil {
		_ = d.pg.Close(context.Background())
	}
}

func (d *postgresqlDriver) query(query string, args ...interface{}) ([][]string, error) {
	rows, err := d.pg.Query(d.c.ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := [][]string{}
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

func (d *postgresqlDriver) getVersionAndDatabase() (string, string, error) {
	rows, err := d.query("SELECT current_setting('server_version'), current_database()")
	if err != nil {
		return "", "", err
	}
	if len(rows) == 0 || len(rows[0]) < 2 {
		return "", "", fmt.Errorf("failed to read the version of the database")
	}
	return rows[0][0], rows[0][1], nil
}

func (d *postgresqlDriver) getDatabases() ([]string, error) {
	rows, err := d.query("SELECT datname FROM pg_database WHERE datallowconn AND NOT datistemplate ORDER BY datname")
	if err != nil {
		return nil, err
	}

	res := []string{}
	for _, row := range rows {
		res = append(res, row[0])
	}
	return res, nil
}

// getTables lists the tables and views of the database the connection is connected to, the only one it sees.
func (d *postgresqlDriver) getTables(database string) ([]*DatabaseTable, error) {
	if database != d.c.Database {
		return nil, fmt.Errorf("switch to the database: %s to see its tables", database)
	}
	rows, err := d.query("SELECT table_schema, table_name, table_type FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog', 'information_schema') ORDER BY table_schema <> 'public', table_schema, table_name")
	if err != nil {
		return nil, err
	}

	res := []*DatabaseTable{}
	for _, row := range rows {
		res = append(res, &DatabaseTable{Schema: row[0], Name: row[1], Type: row[2]})
	}
	return res, nil
}

func (d *postgresqlDriver) getColumns(database string, schema string, table string) ([]*DatabaseColumn, error) {
	if database != d.c.Database {
		return nil, fmt.Errorf("switch to the database: %s to see its tables", database)
	}
	rows, err := d.query(`SELECT c.column_name, c.data_type,
  CASE WHEN EXISTS (
    SELECT 1 FROM information_schema.table_constraints t JOIN information_schema.key_column_usage k
      ON k.constraint_name = t.constraint_name AND k.constraint_schema = t.constraint_schema
    WHERE t.constraint_type = 'PRIMARY KEY' AND t.table_schema = c.table_schema AND t.table_name = c.table_name AND k.column_name = c.column_name
  ) THEN 'PRI' ELSE '' END, c.is_nullable
FROM information_schema.columns c WHERE c.table_schema = $1 AND c.table_name = $2 ORDER BY c.ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}

	res := []*DatabaseColumn{}
	for _, row := range rows {
		res = append(res, &DatabaseColumn{Name: row[0], Type: row[1], Key: row[2], Nullable: row[3] == "YES"})
	}
	return res, nil
}

func (d *postgresqlDriver) getUseStatement(database string) string {
	return fmt.Sprintf("\\connect %s", database)
}

// useDatabase connects to the database anew, as a PostgreSQL connection can't switch databases.
func (d *postgresqlDriver) useDatabase(database string) error {
	conn, err := d.connectDatabase(database)
	if err != nil {
		return err
	}
	old := d.pg
	d.pg = conn
	_ = old.Close(d.c.ctx)
	return nil
}

func (d *postgresqlDriver) execute(statement string) []*SqlResult {
	res := &SqlResult{}
	err := d.executeStatement(statement, res)
	if err != nil {
		res.Error = err.Error()
	}
	return []*SqlResult{res}
}

func (d *postgresqlDriver) executeStatement(statement string, res *SqlResult) error {
	// the extended protocol runs a single statement, like the MySQL connection does
	rr := d.pg.PgConn().ExecParams(d.c.ctx, statement, nil, nil, nil, nil)
	fields := rr.FieldDescriptions()
	if len(fields) > 0 {
		res.Columns = []SqlColumn{}
		for _, field := range fields {
			typeName := strconv.FormatUint(uint64(field.DataTypeOID), 10)
			if t, ok := d.pg.TypeMap().TypeForOID(field.DataTypeOID); ok {
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
