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

package controllers

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gomodule/redigo/redis"
)

// The hash tag keeps a ticket and its quota indexes in one Redis Cluster slot, as the scripts
// touch them together.
const (
	redisConnectTicketKeyPrefix = "casvisor:{connect_ticket}:"
	redisConnectTicketGlobalKey = redisConnectTicketKeyPrefix + "all"
)

var errRedisConnectTicketUnavailable = errors.New("connect ticket store is unavailable")

type redisEndpointConfig struct {
	addr     string
	poolSize int
	password string
	dbNum    int
}

func parseRedisEndpoint(endpoint string) (redisEndpointConfig, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return redisEndpointConfig{}, errors.New("empty Redis connect ticket endpoint")
	}
	parts := strings.Split(endpoint, ",")
	if len(parts) > 4 {
		return redisEndpointConfig{}, errors.New("too many fields in Redis connect ticket endpoint")
	}
	cfg := redisEndpointConfig{addr: strings.TrimSpace(parts[0]), poolSize: 16}
	if cfg.addr == "" {
		return redisEndpointConfig{}, errors.New("empty Redis connect ticket address")
	}
	if len(parts) > 1 {
		if poolSize := strings.TrimSpace(parts[1]); poolSize != "" {
			size, err := strconv.Atoi(poolSize)
			if err != nil || size <= 0 {
				return redisEndpointConfig{}, errors.New("invalid Redis connect ticket pool size")
			}
			cfg.poolSize = size
		}
	}
	if len(parts) > 2 {
		cfg.password = parts[2]
	}
	if len(parts) > 3 {
		if dbNum := strings.TrimSpace(parts[3]); dbNum != "" {
			db, err := strconv.Atoi(dbNum)
			if err != nil || db < 0 {
				return redisEndpointConfig{}, errors.New("invalid Redis connect ticket database number")
			}
			cfg.dbNum = db
		}
	}
	return cfg, nil
}

type redisConnectTicketStore struct {
	pool       *redis.Pool
	ttl        time.Duration
	maxEntries int
	maxPerUser int
	now        func() time.Time
}

func newRedisConnectTicketStore(cfg redisEndpointConfig) *redisConnectTicketStore {
	pool := &redis.Pool{
		MaxIdle:     cfg.poolSize,
		IdleTimeout: 240 * time.Second,
		Dial: func() (redis.Conn, error) {
			options := []redis.DialOption{
				redis.DialConnectTimeout(5 * time.Second),
				redis.DialReadTimeout(5 * time.Second),
				redis.DialWriteTimeout(5 * time.Second),
			}
			if cfg.password != "" {
				options = append(options, redis.DialPassword(cfg.password))
			}
			if cfg.dbNum != 0 {
				options = append(options, redis.DialDatabase(cfg.dbNum))
			}
			return redis.Dial("tcp", cfg.addr, options...)
		},
		TestOnBorrow: func(conn redis.Conn, lastUsed time.Time) error {
			if time.Since(lastUsed) < time.Minute {
				return nil
			}
			_, err := conn.Do("PING")
			return err
		},
	}
	return &redisConnectTicketStore{
		pool:       pool,
		ttl:        connectTicketTTL,
		maxEntries: connectTicketMaxEntries,
		maxPerUser: connectTicketMaxPerUser,
		now:        time.Now,
	}
}

// Each script checks and changes a ticket and its quota indexes in one step, so instances cannot
// race to issue past a quota or to redeem the same ticket twice.
var redisIssueConnectTicketScript = redis.NewScript(3, `
local nowMs = ARGV[6]
local expiresAtMs = ARGV[7]
local ttlMs = ARGV[8]
local maxEntries = tonumber(ARGV[9])
local maxPerUser = tonumber(ARGV[10])
if redis.call('EXISTS', KEYS[1]) == 1 then return -2 end
redis.call('ZREMRANGEBYSCORE', KEYS[3], '-inf', nowMs)
if redis.call('ZCARD', KEYS[3]) >= maxEntries then return -3 end
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', nowMs)
if redis.call('ZCARD', KEYS[2]) >= maxPerUser then return -1 end
redis.call('HMSET', KEYS[1], 'userId', ARGV[2], 'assetId', ARGV[3], 'username', ARGV[4], 'password', ARGV[5])
redis.call('PEXPIRE', KEYS[1], ttlMs)
redis.call('ZADD', KEYS[2], expiresAtMs, ARGV[1])
redis.call('PEXPIRE', KEYS[2], ttlMs)
redis.call('ZADD', KEYS[3], expiresAtMs, ARGV[1])
redis.call('PEXPIRE', KEYS[3], ttlMs)
return 1
`)

var redisConsumeConnectTicketScript = redis.NewScript(3, `
local vals = redis.call('HMGET', KEYS[1], 'userId', 'assetId', 'username', 'password')
if not vals[1] or vals[1] ~= ARGV[2] or vals[2] ~= ARGV[3] or vals[3] == false or vals[4] == false then return nil end
redis.call('DEL', KEYS[1])
redis.call('ZREM', KEYS[2], ARGV[1])
redis.call('ZREM', KEYS[3], ARGV[1])
return {vals[3], vals[4]}
`)

func (s *redisConnectTicketStore) Issue(userId, assetId, username, password string) (string, error) {
	if userId == "" || assetId == "" {
		return "", errConnectTicketNeedsIdentity
	}
	ticket, err := generateConnectTicket()
	if err != nil {
		return "", err
	}

	conn := s.pool.Get()
	defer conn.Close()
	now := s.now()
	result, err := redis.Int(redisIssueConnectTicketScript.Do(conn,
		redisConnectTicketKeyPrefix+"t:"+ticket, redisConnectTicketKeyPrefix+"u:"+userId, redisConnectTicketGlobalKey,
		ticket, userId, assetId, username, password, now.UnixMilli(), now.Add(s.ttl).UnixMilli(), s.ttl.Milliseconds(), s.maxEntries, s.maxPerUser,
	))
	if err != nil {
		// Redis errors may echo command arguments. Never return them to the client.
		return "", errRedisConnectTicketUnavailable
	}
	switch result {
	case 1:
		return ticket, nil
	case -1:
		return "", errUserConnectTicketFull
	case -2:
		return "", errConnectTicketCollision
	case -3:
		return "", errConnectTicketFull
	default:
		return "", errRedisConnectTicketUnavailable
	}
}

func (s *redisConnectTicketStore) Consume(ticket, userId, assetId string) (string, string, error) {
	if ticket == "" || userId == "" || assetId == "" {
		return "", "", errInvalidConnectTicket
	}

	conn := s.pool.Get()
	defer conn.Close()
	vals, err := redis.Strings(redisConsumeConnectTicketScript.Do(conn,
		redisConnectTicketKeyPrefix+"t:"+ticket, redisConnectTicketKeyPrefix+"u:"+userId, redisConnectTicketGlobalKey,
		ticket, userId, assetId,
	))
	if errors.Is(err, redis.ErrNil) {
		return "", "", errInvalidConnectTicket
	}
	if err != nil || len(vals) != 2 {
		return "", "", errRedisConnectTicketUnavailable
	}
	return vals[0], vals[1], nil
}

func (s *redisConnectTicketStore) Close() error {
	return s.pool.Close()
}
