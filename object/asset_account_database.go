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
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/bson"
)

var oracleSimpleName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_$#]*$`)

func quoteSqlString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func quotePostgresName(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func getOracleUserName(name string) string {
	if oracleSimpleName.MatchString(name) {
		return strings.ToUpper(name)
	}
	return quoteOracleName(name)
}

// databaseAccount is a database user found on the asset.
type databaseAccount struct {
	username   string
	privileged bool
	noPassword bool
	detail     string
}

func (c *DatabaseClient) sqlConn() *sql.Conn {
	switch d := c.driver.(type) {
	case *mysqlDriver:
		return d.conn
	case *sqlServerDriver:
		return d.conn
	case *oracleDriver:
		return d.conn
	}
	return nil
}

// execAccount runs a statement of Casvisor itself on the database, not one typed by a user.
func (c *DatabaseClient) execAccount(statement string, args ...interface{}) error {
	if d, ok := c.driver.(*postgresqlDriver); ok {
		_, err := d.pg.Exec(c.ctx, statement, args...)
		return err
	}
	conn := c.sqlConn()
	if conn == nil {
		return fmt.Errorf("the protocol: %s does not run SQL", c.Asset.Protocol)
	}
	_, err := conn.ExecContext(c.ctx, statement, args...)
	return err
}

// queryAccountMaps runs a query of Casvisor itself and returns its rows by the lower-case names of the columns.
func (c *DatabaseClient) queryAccountMaps(query string, args ...interface{}) ([]map[string]string, error) {
	res := []map[string]string{}
	if d, ok := c.driver.(*postgresqlDriver); ok {
		rows, err := d.pg.Query(c.ctx, query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			values, err := rows.Values()
			if err != nil {
				return nil, err
			}
			row := map[string]string{}
			for i, field := range rows.FieldDescriptions() {
				if values[i] != nil {
					row[strings.ToLower(field.Name)] = fmt.Sprint(values[i])
				}
			}
			res = append(res, row)
		}
		return res, rows.Err()
	}

	conn := c.sqlConn()
	if conn == nil {
		return nil, fmt.Errorf("the protocol: %s does not run SQL", c.Asset.Protocol)
	}
	rows, err := conn.QueryContext(c.ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
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
		row := map[string]string{}
		for i, column := range columns {
			if b := formatDriverValue(values[i]); b != nil {
				row[strings.ToLower(column)] = string(b)
			}
		}
		res = append(res, row)
	}
	return res, rows.Err()
}

func (c *DatabaseClient) redisDo(args ...interface{}) (interface{}, error) {
	d, ok := c.driver.(*redisDriver)
	if !ok {
		return nil, fmt.Errorf("the asset is not Redis")
	}
	return d.client.Do(c.ctx, args...).Result()
}

func (c *DatabaseClient) mongoCommand(database string, command bson.D) (bson.M, error) {
	d, ok := c.driver.(*mongodbDriver)
	if !ok {
		return nil, fmt.Errorf("the asset is not MongoDB")
	}
	res := bson.M{}
	err := d.client.Database(database).RunCommand(c.ctx, command).Decode(&res)
	return res, err
}

func isRedisDefaultUser(username string) bool {
	return username == "" || username == "default"
}

// getMysqlHosts lists the hosts the MySQL user may sign in from, as each is an account of its own.
func (c *DatabaseClient) getMysqlHosts(username string) ([]string, error) {
	rows, err := c.queryAccountMaps("SELECT host FROM mysql.user WHERE user = ?", username)
	if err != nil {
		return nil, err
	}
	hosts := []string{}
	for _, row := range rows {
		hosts = append(hosts, row["host"])
	}
	return hosts, nil
}

// persistRedisAccounts saves the ACL users to the ACL file, or the config file when there is none,
// and tells when neither works and the change goes with a restart.
func (c *DatabaseClient) persistRedisAccounts(isConfig bool) string {
	if !isConfig {
		if _, err := c.redisDo("ACL", "SAVE"); err == nil {
			return ""
		}
	}
	if _, err := c.redisDo("CONFIG", "REWRITE"); err != nil {
		return fmt.Sprintf("the change is in effect, but Redis could not save it and loses it when it restarts: %s", err.Error())
	}
	return ""
}

func (c *DatabaseClient) hasRedisAcl() bool {
	_, err := c.redisDo("ACL", "WHOAMI")
	return err == nil
}

// setDatabasePassword changes the password of the user, who is the client's own user when self is
// true and is then given oldPassword where the database wants it.
func (c *DatabaseClient) setDatabasePassword(username string, password string, oldPassword string, self bool, create bool) (string, error) {
	switch c.Asset.Protocol {
	case ProtocolMysql:
		if self {
			return "", c.execAccount("ALTER USER CURRENT_USER() IDENTIFIED BY " + quoteSqlString(password))
		}
		hosts, err := c.getMysqlHosts(username)
		if err != nil {
			return "", err
		}
		if len(hosts) == 0 {
			if !create {
				return "", fmt.Errorf("the MySQL user: %s does not exist", username)
			}
			return "", c.execAccount(fmt.Sprintf("CREATE USER %s@'%%' IDENTIFIED BY %s", quoteSqlString(username), quoteSqlString(password)))
		}
		for _, host := range hosts {
			err = c.execAccount(fmt.Sprintf("ALTER USER %s@%s IDENTIFIED BY %s", quoteSqlString(username), quoteSqlString(host), quoteSqlString(password)))
			if err != nil {
				return "", err
			}
		}
		return "", nil
	case ProtocolPostgresql:
		if create && !self {
			rows, err := c.queryAccountMaps("SELECT rolname FROM pg_roles WHERE rolname = $1", username)
			if err != nil {
				return "", err
			}
			if len(rows) == 0 {
				return "", c.execAccount(fmt.Sprintf("CREATE ROLE %s WITH LOGIN PASSWORD %s", quotePostgresName(username), quoteSqlString(password)))
			}
		}
		return "", c.execAccount(fmt.Sprintf("ALTER ROLE %s WITH PASSWORD %s", quotePostgresName(username), quoteSqlString(password)))
	case ProtocolSqlServer:
		statement := fmt.Sprintf("ALTER LOGIN %s WITH PASSWORD = N%s", quoteSqlServerName(username), quoteSqlString(password))
		if self {
			statement += " OLD_PASSWORD = N" + quoteSqlString(oldPassword)
		}
		return "", c.execAccount(statement)
	case ProtocolOracle:
		statement := fmt.Sprintf(`ALTER USER %s IDENTIFIED BY "%s"`, getOracleUserName(username), password)
		if self {
			statement += fmt.Sprintf(` REPLACE "%s"`, oldPassword)
		}
		return "", c.execAccount(statement)
	case ProtocolRedis:
		if isRedisDefaultUser(username) {
			if _, err := c.redisDo("CONFIG", "SET", "requirepass", password); err == nil {
				return c.persistRedisAccounts(true), nil
			}
			username = "default"
		}
		if !c.hasRedisAcl() {
			return "", fmt.Errorf("the Redis server has no ACL users, and CONFIG SET requirepass is not allowed")
		}
		user, err := c.redisDo("ACL", "GETUSER", username)
		if err != nil && !errors.Is(err, redis.Nil) {
			return "", err
		}
		if user == nil {
			return "", fmt.Errorf("the Redis user: %s does not exist", username)
		}
		_, err = c.redisDo("ACL", "SETUSER", username, "resetpass", ">"+password)
		if err != nil {
			return "", err
		}
		return c.persistRedisAccounts(false), nil
	case ProtocolMongodb:
		database, err := c.getMongoUserDatabase(username)
		if err != nil {
			return "", err
		}
		_, err = c.mongoCommand(database, bson.D{{Key: "updateUser", Value: username}, {Key: "pwd", Value: password}})
		return "", err
	}
	return "", fmt.Errorf("the protocol: %s has no accounts to change", c.Asset.Protocol)
}

// getMongoUserDatabase is the database the MongoDB user is defined in: admin, or the asset's database.
func (c *DatabaseClient) getMongoUserDatabase(username string) (string, error) {
	databases := []string{"admin"}
	if c.Asset.Database != "" && c.Asset.Database != "admin" {
		databases = append(databases, c.Asset.Database)
	}
	var lastErr error
	for _, database := range databases {
		res, err := c.mongoCommand(database, bson.D{{Key: "usersInfo", Value: username}})
		if err != nil {
			lastErr = err
			continue
		}
		if users, ok := res["users"].(bson.A); ok && len(users) > 0 {
			return database, nil
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("the MongoDB user: %s is in neither admin nor %s", username, c.Asset.Database)
}

// setDatabaseAccountPassword changes the password through the asset's own account, or else, for an
// account other than it, as the account itself with its current password.
func setDatabaseAccountPassword(asset *Asset, platform *Platform, username string, password string, oldPassword string, create bool) (string, error) {
	self := username == asset.Username || asset.Protocol == ProtocolRedis && isRedisDefaultUser(username) && isRedisDefaultUser(asset.Username)
	client, err := dialDatabase(asset, false)
	if err != nil {
		return "", err
	}
	note, err := client.setDatabasePassword(username, password, oldPassword, self, create)
	client.Close()
	if err == nil || self || create || oldPassword == "" || asset.Protocol == ProtocolRedis {
		return note, err
	}

	target := *asset
	target.Username, target.Password, target.PrivateKey = username, oldPassword, ""
	own, ownErr := dialDatabase(&target, true)
	if ownErr == nil {
		note, ownErr = own.setDatabasePassword(username, password, oldPassword, true, false)
		own.Close()
	}
	if ownErr != nil {
		return "", fmt.Errorf("%s, and changing it as the account itself failed too: %s", err.Error(), ownErr.Error())
	}
	return note, nil
}

// verifyDatabaseLogin signs in as the account, to a database it may surely open.
func verifyDatabaseLogin(target *Asset) error {
	switch target.Protocol {
	case ProtocolMysql, ProtocolSqlServer:
		target.Database = ""
	case ProtocolPostgresql:
		if target.Database == "" {
			target.Database = "postgres"
		}
	}
	client, err := dialDatabase(target, true)
	if err != nil {
		// a Redis user that may not run PING has signed in all the same
		if target.Protocol == ProtocolRedis && strings.Contains(err.Error(), "NOPERM") {
			return nil
		}
		return err
	}
	client.Close()
	return nil
}

// lockDatabaseAccount keeps the database user from signing in.
func lockDatabaseAccount(asset *Asset, username string) error {
	if username == asset.Username || asset.Protocol == ProtocolRedis && isRedisDefaultUser(username) {
		return fmt.Errorf("the account: %s can't be locked from Casvisor", username)
	}
	client, err := dialDatabase(asset, false)
	if err != nil {
		return err
	}
	defer client.Close()

	switch asset.Protocol {
	case ProtocolMysql:
		hosts, err := client.getMysqlHosts(username)
		if err != nil {
			return err
		}
		for _, host := range hosts {
			err = client.execAccount(fmt.Sprintf("ALTER USER %s@%s ACCOUNT LOCK", quoteSqlString(username), quoteSqlString(host)))
			if err != nil {
				return err
			}
		}
		return nil
	case ProtocolPostgresql:
		return client.execAccount(fmt.Sprintf("ALTER ROLE %s NOLOGIN", quotePostgresName(username)))
	case ProtocolSqlServer:
		return client.execAccount(fmt.Sprintf("ALTER LOGIN %s DISABLE", quoteSqlServerName(username)))
	case ProtocolOracle:
		return client.execAccount(fmt.Sprintf("ALTER USER %s ACCOUNT LOCK", getOracleUserName(username)))
	case ProtocolRedis:
		_, err = client.redisDo("ACL", "SETUSER", username, "off")
		if err != nil {
			return err
		}
		if note := client.persistRedisAccounts(false); note != "" {
			return fmt.Errorf("%s", note)
		}
		return nil
	}
	return fmt.Errorf("the accounts of %s can't be locked", asset.Protocol)
}

var mysqlSystemUsers = []string{"mysql.sys", "mysql.session", "mysql.infoschema", "mariadb.sys"}

// getDatabaseAccounts lists the users of the database that may sign in.
func getDatabaseAccounts(asset *Asset) ([]*databaseAccount, error) {
	client, err := dialDatabase(asset, false)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	res := []*databaseAccount{}
	add := func(account *databaseAccount) {
		for _, a := range res {
			if a.username == account.username {
				a.privileged = a.privileged || account.privileged
				a.noPassword = a.noPassword || account.noPassword
				return
			}
		}
		res = append(res, account)
	}

	switch asset.Protocol {
	case ProtocolMysql:
		rows, err := client.queryAccountMaps("SELECT * FROM mysql.user")
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if slices.Contains(mysqlSystemUsers, row["user"]) || row["account_locked"] == "Y" || row["user"] == "" {
				continue
			}
			secret, plugin := row["authentication_string"], row["plugin"]
			if _, ok := row["authentication_string"]; !ok {
				secret = row["password"]
			}
			socket := strings.Contains(plugin, "socket")
			add(&databaseAccount{
				username:   row["user"],
				privileged: row["super_priv"] == "Y" || row["grant_priv"] == "Y" || row["create_user_priv"] == "Y",
				noPassword: secret == "" && !socket,
				detail:     "from " + row["host"],
			})
		}
	case ProtocolPostgresql:
		rows, err := client.queryAccountMaps("SELECT rolname, rolsuper OR rolcreaterole AS privileged FROM pg_roles WHERE rolcanlogin AND rolname NOT LIKE 'pg\\_%'")
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			add(&databaseAccount{username: row["rolname"], privileged: row["privileged"] == "true"})
		}
	case ProtocolSqlServer:
		rows, err := client.queryAccountMaps("SELECT name, IS_SRVROLEMEMBER('sysadmin', name) AS sysadmin, PWDCOMPARE('', password_hash) AS empty FROM sys.sql_logins WHERE is_disabled = 0 AND name NOT LIKE '##%'")
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			add(&databaseAccount{username: row["name"], privileged: row["sysadmin"] == "1", noPassword: row["empty"] == "1"})
		}
	case ProtocolOracle:
		rows, err := client.queryAccountMaps("SELECT u.username, (SELECT COUNT(*) FROM dba_role_privs r WHERE r.grantee = u.username AND r.granted_role = 'DBA') AS dba FROM dba_users u WHERE u.account_status = 'OPEN' AND u.oracle_maintained = 'N'")
		if err != nil {
			return nil, fmt.Errorf("reading dba_users needs the SELECT_CATALOG_ROLE: %s", err.Error())
		}
		for _, row := range rows {
			add(&databaseAccount{username: row["username"], privileged: row["dba"] != "0"})
		}
	case ProtocolRedis:
		value, err := client.redisDo("ACL", "LIST")
		if err != nil {
			value, err = client.redisDo("CONFIG", "GET", "requirepass")
			if err != nil {
				return nil, err
			}
			values, _ := value.([]interface{})
			empty := len(values) < 2 || fmt.Sprint(values[1]) == ""
			add(&databaseAccount{username: "default", privileged: true, noPassword: empty})
			return res, nil
		}
		lines, _ := value.([]interface{})
		for _, line := range lines {
			fields := strings.Fields(fmt.Sprint(line))
			if len(fields) < 3 || fields[0] != "user" || !slices.Contains(fields, "on") {
				continue
			}
			privileged := slices.ContainsFunc(fields, func(f string) bool {
				return f == "+@all" || f == "allcommands" || f == "+@admin" || f == "+@dangerous" || f == "+acl"
			})
			add(&databaseAccount{username: fields[1], privileged: privileged, noPassword: slices.Contains(fields, "nopass")})
		}
	case ProtocolMongodb:
		res2, err := client.mongoCommand("admin", bson.D{{Key: "usersInfo", Value: bson.D{{Key: "forAllDBs", Value: true}}}})
		if err != nil {
			return nil, err
		}
		users, _ := res2["users"].(bson.A)
		for _, u := range users {
			user, ok := u.(bson.M)
			if !ok {
				continue
			}
			privileged := false
			roles, _ := user["roles"].(bson.A)
			for _, r := range roles {
				if role, ok := r.(bson.M); ok && slices.Contains([]string{"root", "userAdminAnyDatabase", "userAdmin", "dbOwner", "clusterAdmin", "__system"}, fmt.Sprint(role["role"])) {
					privileged = true
				}
			}
			add(&databaseAccount{username: fmt.Sprint(user["user"]), privileged: privileged, detail: "in " + fmt.Sprint(user["db"])})
		}
	default:
		return nil, fmt.Errorf("the accounts of %s can't be read", asset.Protocol)
	}
	return res, nil
}
