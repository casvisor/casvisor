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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisMaxKeys = 1000

// the commands that would change who the connection is or turn it into a stream of pushed messages
var redisRejectedCommands = map[string]bool{
	"AUTH": true, "HELLO": true, "QUIT": true, "RESET": true,
	"SUBSCRIBE": true, "PSUBSCRIBE": true, "SSUBSCRIBE": true, "UNSUBSCRIBE": true, "PUNSUBSCRIBE": true, "SUNSUBSCRIBE": true,
	"MONITOR": true, "SYNC": true, "PSYNC": true, "REPLCONF": true,
}

// the replies that are a list of pairs, shown as two columns
var redisPairCommands = map[string][2]string{
	"HGETALL":          {"field", "value"},
	"CONFIG GET":       {"name", "value"},
	"HRANDFIELD":       {"field", "value"},
	"ZRANGE":           {"member", "score"},
	"ZREVRANGE":        {"member", "score"},
	"ZRANGEBYSCORE":    {"member", "score"},
	"ZREVRANGEBYSCORE": {"member", "score"},
	"ZPOPMIN":          {"member", "score"},
	"ZPOPMAX":          {"member", "score"},
}

// the pair commands that return pairs only with an option
var redisPairOptions = map[string]string{
	"HRANDFIELD":       "WITHVALUES",
	"ZRANGE":           "WITHSCORES",
	"ZREVRANGE":        "WITHSCORES",
	"ZRANGEBYSCORE":    "WITHSCORES",
	"ZREVRANGEBYSCORE": "WITHSCORES",
}

// redisDriver keeps a single connection, so that MULTI, WATCH and the like work
// across the commands of a session; its databases are the numbered ones.
type redisDriver struct {
	c      *DatabaseClient
	client *redis.Client
	db     int
	probed bool
}

// ParseRedisCommand splits a command line the way redis-cli does: words separated by
// spaces, double quotes with escapes such as \n and \x41, and single quotes.
func ParseRedisCommand(line string) ([]string, error) {
	args := []string{}
	i := 0
	for {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		if i >= len(line) {
			return args, nil
		}

		var sb strings.Builder
		inDouble, inSingle, done := false, false, false
		for !done {
			if i >= len(line) {
				if inDouble || inSingle {
					return nil, fmt.Errorf("unbalanced quotes in the command")
				}
				break
			}
			ch := line[i]
			switch {
			case inDouble:
				if ch == '\\' && i+1 < len(line) {
					i++
					switch line[i] {
					case 'n':
						sb.WriteByte('\n')
					case 'r':
						sb.WriteByte('\r')
					case 't':
						sb.WriteByte('\t')
					case 'b':
						sb.WriteByte('\b')
					case 'a':
						sb.WriteByte('\a')
					case 'x':
						if i+2 < len(line) {
							if v, err := strconv.ParseUint(line[i+1:i+3], 16, 8); err == nil {
								sb.WriteByte(byte(v))
								i += 2
								break
							}
						}
						sb.WriteByte('x')
					default:
						sb.WriteByte(line[i])
					}
				} else if ch == '"' {
					if i+1 < len(line) && line[i+1] != ' ' && line[i+1] != '\t' {
						return nil, fmt.Errorf("a closing quote must be followed by a space in the command")
					}
					done = true
				} else {
					sb.WriteByte(ch)
				}
			case inSingle:
				if ch == '\\' && i+1 < len(line) && line[i+1] == '\'' {
					i++
					sb.WriteByte('\'')
				} else if ch == '\'' {
					if i+1 < len(line) && line[i+1] != ' ' && line[i+1] != '\t' {
						return nil, fmt.Errorf("a closing quote must be followed by a space in the command")
					}
					done = true
				} else {
					sb.WriteByte(ch)
				}
			default:
				switch ch {
				case ' ', '\t':
					done = true
				case '"':
					inDouble = true
				case '\'':
					inSingle = true
				default:
					sb.WriteByte(ch)
				}
			}
			i++
		}
		args = append(args, sb.String())
	}
}

// quoteRedisArg writes an argument so that ParseRedisCommand reads it back.
func quoteRedisArg(arg string) string {
	if arg != "" && !strings.ContainsAny(arg, " \t\r\n\"'\\") {
		return arg
	}
	return strconv.Quote(arg)
}

// dial connects with TLS unless the server answers a plain PING, which carries
// no secret: a plain Redis server waits forever for the line end that a TLS
// handshake never sends, while a TLS one drops the PING at once.
func (d *redisDriver) dial(ctx context.Context) (net.Conn, error) {
	c := d.c
	if c.getPinnedFingerprint() == "" && !d.probed {
		conn, err := c.dial()
		if err != nil {
			return nil, err
		}
		_ = conn.SetDeadline(time.Now().Add(dialTimeout))
		reply := make([]byte, 1)
		_, err = conn.Write([]byte("PING\r\n"))
		if err == nil {
			_, err = io.ReadFull(conn, reply)
		}
		d.probed = true
		if err == nil && (reply[0] == '+' || reply[0] == '-') {
			c.mu.Lock()
			c.plainText = true
			c.mu.Unlock()
		}
		_ = conn.Close()
	}
	return c.dialTls(ctx)
}

func (d *redisDriver) newClient(db int) *redis.Client {
	c := d.c
	return redis.NewClient(&redis.Options{
		Addr:     net.JoinHostPort(c.Asset.Host, strconv.Itoa(c.Asset.Port)),
		Username: c.Asset.Username,
		Password: c.Asset.Password,
		DB:       db,
		Protocol: 2,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return d.dial(ctx)
		},
		PoolSize:      1,
		MaxRetries:    -1,
		DialerRetries: 1,
		DialTimeout:   2 * dialTimeout,
		ReadTimeout:   -1,
		WriteTimeout:  -1,
		// commands such as BLPOP wait as long as they are told to, until the session ends
		ContextTimeoutEnabled: true,
		DisableIdentity:       true,
	})
}

func (d *redisDriver) connect() error {
	db := 0
	if d.c.Asset.Database != "" {
		var err error
		db, err = strconv.Atoi(strings.TrimSpace(d.c.Asset.Database))
		if err != nil || db < 0 {
			return fmt.Errorf("the database of a Redis asset should be a number such as 0, not: %s", d.c.Asset.Database)
		}
	}

	d.db = db
	d.client = d.newClient(db)
	ctx, cancel := context.WithTimeout(d.c.ctx, 2*dialTimeout)
	defer cancel()
	return d.client.Ping(ctx).Err()
}

func (d *redisDriver) close() {
	if d.client != nil {
		_ = d.client.Close()
	}
}

func (d *redisDriver) getVersionAndDatabase() (string, string, error) {
	info, err := d.client.Info(d.c.ctx, "server").Result()
	version := ""
	if err == nil {
		for _, line := range strings.Split(info, "\n") {
			for _, key := range []string{"redis_version:", "valkey_version:"} {
				if strings.HasPrefix(line, key) {
					version = strings.TrimSpace(strings.TrimPrefix(line, key))
				}
			}
		}
	}
	return version, strconv.Itoa(d.db), nil
}

func (d *redisDriver) getDatabases() ([]string, error) {
	count := 0
	values, err := d.client.ConfigGet(d.c.ctx, "databases").Result()
	if err == nil {
		count, _ = strconv.Atoi(values["databases"])
	}
	if count <= 0 {
		// the managed services that don't allow CONFIG still list the databases with keys
		info, err := d.client.Info(d.c.ctx, "keyspace").Result()
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(info, "\n") {
			if strings.HasPrefix(line, "db") {
				if i := strings.IndexByte(line, ':'); i > 2 {
					if n, err := strconv.Atoi(line[2:i]); err == nil && n+1 > count {
						count = n + 1
					}
				}
			}
		}
		if count < d.db+1 {
			count = d.db + 1
		}
	}

	res := []string{}
	for i := 0; i < count; i++ {
		res = append(res, strconv.Itoa(i))
	}
	return res, nil
}

// getTables lists the first keys of the current database, with their types.
func (d *redisDriver) getTables(database string) ([]*DatabaseTable, error) {
	if database != strconv.Itoa(d.db) {
		return nil, fmt.Errorf("switch to the database: %s to see its keys", database)
	}

	ctx := d.c.ctx
	keys := []string{}
	seen := map[string]bool{}
	var cursor uint64
	for i := 0; i < 100; i++ {
		batch, next, err := d.client.Scan(ctx, cursor, "", 500).Result()
		if err != nil {
			return nil, err
		}
		for _, key := range batch {
			if !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
		cursor = next
		if cursor == 0 || len(keys) >= redisMaxKeys {
			break
		}
	}
	if len(keys) > redisMaxKeys {
		keys = keys[:redisMaxKeys]
	}
	sort.Strings(keys)

	pipe := d.client.Pipeline()
	types := make([]*redis.StatusCmd, len(keys))
	for i, key := range keys {
		types[i] = pipe.Type(ctx, key)
	}
	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}

	res := []*DatabaseTable{}
	for i, key := range keys {
		res = append(res, &DatabaseTable{Name: key, Type: types[i].Val()})
	}
	return res, nil
}

// getColumns describes a key: its type, the seconds it has to live and its size.
func (d *redisDriver) getColumns(database string, schema string, table string) ([]*DatabaseColumn, error) {
	if database != strconv.Itoa(d.db) {
		return nil, fmt.Errorf("switch to the database: %s to see its keys", database)
	}

	ctx := d.c.ctx
	keyType, err := d.client.Type(ctx, table).Result()
	if err != nil {
		return nil, err
	}
	ttl, err := d.client.Do(ctx, "TTL", table).Int64()
	if err != nil {
		return nil, err
	}

	var size int64
	switch keyType {
	case "string":
		size, err = d.client.StrLen(ctx, table).Result()
	case "list":
		size, err = d.client.LLen(ctx, table).Result()
	case "set":
		size, err = d.client.SCard(ctx, table).Result()
	case "zset":
		size, err = d.client.ZCard(ctx, table).Result()
	case "hash":
		size, err = d.client.HLen(ctx, table).Result()
	case "stream":
		size, err = d.client.XLen(ctx, table).Result()
	}
	if err != nil {
		return nil, err
	}

	return []*DatabaseColumn{
		{Name: "type", Type: keyType},
		{Name: "ttl", Type: strconv.FormatInt(ttl, 10)},
		{Name: "size", Type: strconv.FormatInt(size, 10)},
	}, nil
}

func (d *redisDriver) getUseStatement(database string) string {
	return "SELECT " + database
}

// useDatabase opens a connection to the database, as SELECT on a pooled
// connection would be forgotten when it reconnects.
func (d *redisDriver) useDatabase(database string) error {
	db, err := strconv.Atoi(database)
	if err != nil || db < 0 {
		return fmt.Errorf("the Redis database should be a number, not: %s", database)
	}

	client := d.newClient(db)
	err = client.Ping(d.c.ctx).Err()
	if err != nil {
		_ = client.Close()
		return err
	}
	old := d.client
	d.client = client
	d.db = db
	_ = old.Close()
	return nil
}

func formatRedisValue(value interface{}) *string {
	var s string
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		s = v
	case []byte:
		return formatSqlValue(v)
	case int64:
		s = strconv.FormatInt(v, 10)
	case float64:
		s = strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		s = strconv.FormatBool(v)
	case error:
		s = "(error) " + v.Error()
	default:
		b, err := json.Marshal(v)
		if err != nil {
			s = fmt.Sprint(v)
		} else {
			s = string(b)
		}
	}
	return formatSqlValue([]byte(s))
}

func (d *redisDriver) execute(statement string) []*SqlResult {
	args, err := ParseRedisCommand(statement)
	if err != nil {
		return newErrorResult(err)
	}
	if len(args) == 0 {
		return newErrorResult(fmt.Errorf("no command to run"))
	}

	name := strings.ToUpper(args[0])
	if redisRejectedCommands[name] {
		return newErrorResult(fmt.Errorf("the command: %s is not allowed in the console", name))
	}
	if name == "SELECT" {
		if len(args) != 2 {
			return newErrorResult(fmt.Errorf("wrong number of arguments for 'select' command"))
		}
		err = d.useDatabase(args[1])
		if err != nil {
			return newErrorResult(err)
		}
		d.c.Database = strconv.Itoa(d.db)
		return []*SqlResult{{}}
	}

	values := make([]interface{}, len(args))
	for i, arg := range args {
		values[i] = arg
	}
	reply, err := d.client.Do(d.c.ctx, values...).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return newErrorResult(err)
	}

	res := &SqlResult{Rows: [][]*string{}}
	addRow := func(cells ...*string) {
		res.RowCount++
		if len(res.Rows) >= SqlMaxRows {
			res.Truncated = true
			return
		}
		res.Rows = append(res.Rows, cells)
	}

	pairName := name
	if name == "CONFIG" && len(args) > 1 {
		pairName = name + " " + strings.ToUpper(args[1])
	}
	pair, isPair := redisPairCommands[pairName]
	if option, ok := redisPairOptions[name]; ok {
		isPair = false
		for _, arg := range args[1:] {
			if strings.EqualFold(arg, option) {
				isPair = true
			}
		}
	}

	switch v := reply.(type) {
	case []interface{}:
		if isPair && len(v)%2 == 0 {
			res.Columns = []SqlColumn{{Name: pair[0]}, {Name: pair[1]}}
			for i := 0; i+1 < len(v); i += 2 {
				addRow(formatRedisValue(v[i]), formatRedisValue(v[i+1]))
			}
		} else {
			res.Columns = []SqlColumn{{Name: "value"}}
			for _, item := range v {
				addRow(formatRedisValue(item))
			}
		}
	case string:
		if name == "INFO" {
			res.Columns = []SqlColumn{{Name: "name"}, {Name: "value"}}
			for _, line := range strings.Split(v, "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				key, value, _ := strings.Cut(line, ":")
				addRow(formatRedisValue(key), formatRedisValue(value))
			}
		} else {
			res.Columns = []SqlColumn{{Name: "value"}}
			addRow(formatRedisValue(v))
		}
	default:
		res.Columns = []SqlColumn{{Name: "value"}}
		addRow(formatRedisValue(v))
	}
	return []*SqlResult{res}
}
