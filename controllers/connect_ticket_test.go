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
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	beegocontext "github.com/beego/beego/context"
	"github.com/beego/beego/session"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/object"
	"github.com/gomodule/redigo/redis"
)

const (
	connectTicketTestUserId  = "org/alice"
	connectTicketTestAssetId = "org/target"
)

func newConnectTicketTestStore(maxEntries int, now func() time.Time) *connectTicketStore {
	return &connectTicketStore{ttl: connectTicketTTL, maxEntries: maxEntries, now: now}
}

func issueConnectTicketForTest(t *testing.T, store connectTicketBackend, userId, assetId, username, password string) string {
	t.Helper()
	ticket, err := store.Issue(userId, assetId, username, password)
	if err != nil || ticket == "" {
		t.Fatalf("failed to issue a ticket: %v", err)
	}
	return ticket
}

func assertConnectTicketRejected(t *testing.T, store connectTicketBackend, ticket, userId, assetId string) {
	t.Helper()
	username, password, err := store.Consume(ticket, userId, assetId)
	if !errors.Is(err, errInvalidConnectTicket) || username != "" || password != "" {
		t.Fatal("invalid ticket was not rejected without credentials")
	}
}

func TestConnectTicketStoreIssueAndConsume(t *testing.T) {
	store := newConnectTicketTestStore(2, time.Now)
	first := issueConnectTicketForTest(t, store, connectTicketTestUserId, connectTicketTestAssetId, "remote-user", "remote-password")
	second := issueConnectTicketForTest(t, store, connectTicketTestUserId, connectTicketTestAssetId, "remote-user", "remote-password")
	if first == second {
		t.Fatal("tickets were not unique")
	}
	for _, ticket := range []string{first, second} {
		decoded, err := base64.RawURLEncoding.Strict().DecodeString(ticket)
		if len(ticket) != 43 || err != nil || len(decoded) != 32 {
			t.Fatal("ticket did not encode 32 bytes of URL-safe data")
		}
		username, password, err := store.Consume(ticket, connectTicketTestUserId, connectTicketTestAssetId)
		if err != nil || username != "remote-user" || password != "remote-password" {
			t.Fatal("consuming a valid ticket did not return the original credentials")
		}
		assertConnectTicketRejected(t, store, ticket, connectTicketTestUserId, connectTicketTestAssetId)
	}
}

func TestConnectTicketStoreBinding(t *testing.T) {
	store := newConnectTicketTestStore(1, time.Now)
	ticket := issueConnectTicketForTest(t, store, connectTicketTestUserId, connectTicketTestAssetId, "remote-user", "remote-password")
	assertConnectTicketRejected(t, store, "", connectTicketTestUserId, connectTicketTestAssetId)
	assertConnectTicketRejected(t, store, ticket, "", connectTicketTestAssetId)
	assertConnectTicketRejected(t, store, ticket, connectTicketTestUserId, "")
	assertConnectTicketRejected(t, store, ticket, "org/bob", connectTicketTestAssetId)
	assertConnectTicketRejected(t, store, ticket, connectTicketTestUserId, "org/other")
	if _, _, err := store.Consume(ticket, connectTicketTestUserId, connectTicketTestAssetId); err != nil {
		t.Fatalf("a mismatched request burned the rightful user's ticket: %v", err)
	}
	assertConnectTicketRejected(t, store, ticket, connectTicketTestUserId, connectTicketTestAssetId)
}

func TestConnectTicketStoreTTL(t *testing.T) {
	for _, tt := range []struct {
		name    string
		offset  time.Duration
		expired bool
	}{
		{"before expiry", -time.Nanosecond, false},
		{"at expiry", 0, true},
		{"after expiry", time.Nanosecond, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Unix(0, 0)
			now := start
			store := newConnectTicketTestStore(1, func() time.Time { return now })
			ticket := issueConnectTicketForTest(t, store, connectTicketTestUserId, connectTicketTestAssetId, "", "remote-password")
			now = start.Add(connectTicketTTL - time.Nanosecond)
			store.pruneExpired() // The next interval prune is not due when Consume runs.
			now = start.Add(connectTicketTTL + tt.offset)
			if tt.expired {
				assertConnectTicketRejected(t, store, ticket, connectTicketTestUserId, connectTicketTestAssetId)
			} else if _, _, err := store.Consume(ticket, connectTicketTestUserId, connectTicketTestAssetId); err != nil {
				t.Fatalf("ticket was rejected before expiry: %v", err)
			}
			if len(store.entries) != 0 || len(store.userCounts) != 0 {
				t.Fatal("consumed or expired ticket was not fully removed from the store")
			}
		})
	}
}

func TestConnectTicketStoreCapacity(t *testing.T) {
	now := time.Unix(0, 0)
	store := newConnectTicketTestStore(1, func() time.Time { return now })
	expired := issueConnectTicketForTest(t, store, connectTicketTestUserId, connectTicketTestAssetId, "", "remote-password")
	now = now.Add(connectTicketTTL - time.Nanosecond)
	if ticket, err := store.Issue(connectTicketTestUserId, connectTicketTestAssetId, "", "another-password"); !errors.Is(err, errConnectTicketFull) || ticket != "" {
		t.Fatal("a full store did not reject issuance")
	}
	now = now.Add(time.Nanosecond)
	issueConnectTicketForTest(t, store, connectTicketTestUserId, connectTicketTestAssetId, "", "another-password")
	if _, ok := store.entries[expired]; ok || len(store.entries) != 1 {
		t.Fatal("issuing at capacity did not prune the expired ticket")
	}
}

func TestConnectTicketStorePerUserCapacity(t *testing.T) {
	start := time.Unix(0, 0)
	now := start
	store := newConnectTicketTestStore(4, func() time.Time { return now })
	store.maxPerUser = 2

	first := issueConnectTicketForTest(t, store, connectTicketTestUserId, connectTicketTestAssetId, "", "remote-password")
	expiring := issueConnectTicketForTest(t, store, connectTicketTestUserId, connectTicketTestAssetId, "", "remote-password")
	if ticket, err := store.Issue(connectTicketTestUserId, "org/other", "", "remote-password"); !errors.Is(err, errUserConnectTicketFull) || ticket != "" {
		t.Fatalf("a user at capacity was not rejected with the per-user error: %v", err)
	}

	now = start.Add(time.Second)
	issueConnectTicketForTest(t, store, "org/bob", connectTicketTestAssetId, "", "remote-password")
	if _, _, err := store.Consume(first, connectTicketTestUserId, connectTicketTestAssetId); err != nil {
		t.Fatalf("could not consume the first ticket: %v", err)
	}
	issueConnectTicketForTest(t, store, connectTicketTestUserId, connectTicketTestAssetId, "", "remote-password")

	now = start.Add(connectTicketTTL - time.Nanosecond)
	store.pruneExpired() // The interval alone will not trigger pruning on the next issue.
	now = start.Add(connectTicketTTL)
	issueConnectTicketForTest(t, store, connectTicketTestUserId, connectTicketTestAssetId, "", "remote-password")
	if _, ok := store.entries[expiring]; ok {
		t.Fatal("issuing at the per-user limit did not prune the expired ticket")
	}
}

func TestConnectTicketStoreLazyPrune(t *testing.T) {
	now := time.Unix(0, 0)
	store := newConnectTicketTestStore(4, func() time.Time { return now })
	first := issueConnectTicketForTest(t, store, connectTicketTestUserId, connectTicketTestAssetId, "", "remote-password")
	now = now.Add(connectTicketTTL)
	issueConnectTicketForTest(t, store, connectTicketTestUserId, connectTicketTestAssetId, "", "remote-password")
	if _, ok := store.entries[first]; ok {
		t.Fatal("issuing below capacity did not prune the expired ticket")
	}
	now = now.Add(connectTicketTTL)
	assertConnectTicketRejected(t, store, "unknown", connectTicketTestUserId, connectTicketTestAssetId)
	if len(store.entries) != 0 {
		t.Fatal("consuming did not prune other expired tickets")
	}
}

func TestConnectTicketStoreConcurrentConsume(t *testing.T) {
	store := newConnectTicketTestStore(1, time.Now)
	ticket := issueConnectTicketForTest(t, store, connectTicketTestUserId, connectTicketTestAssetId, "alice", "remote-password")
	start := make(chan struct{})
	var wg sync.WaitGroup
	var winners atomic.Int32
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			username, password, err := store.Consume(ticket, connectTicketTestUserId, connectTicketTestAssetId)
			if err == nil {
				winners.Add(1)
				if username != "alice" || password != "remote-password" {
					t.Error("concurrent consume returned incorrect credentials")
				}
			} else if !errors.Is(err, errInvalidConnectTicket) || username != "" || password != "" {
				t.Error("a rejected concurrent consume leaked credentials or returned an unexpected error")
			}
		}()
	}
	close(start)
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("got %d successful consumes, want one", winners.Load())
	}
}

func TestConnectTicketStoreConcurrentPruning(t *testing.T) {
	const workers = 16
	var elapsed atomic.Int64
	store := newConnectTicketTestStore(workers, func() time.Time {
		return time.Unix(0, elapsed.Load())
	})
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for range 128 {
			elapsed.Add(int64(connectTicketTTL))
			store.pruneExpired()
		}
	}()
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range 32 {
				ticket, err := store.Issue(connectTicketTestUserId, connectTicketTestAssetId, "alice", "remote-password")
				if err != nil {
					t.Errorf("concurrent issuance failed: %v", err)
					return
				}
				username, password, err := store.Consume(ticket, connectTicketTestUserId, connectTicketTestAssetId)
				if err == nil && (username != "alice" || password != "remote-password") {
					t.Error("consume returned incorrect credentials during pruning")
				} else if err != nil && (!errors.Is(err, errInvalidConnectTicket) || username != "" || password != "") {
					t.Error("consume during expiry was not rejected without credentials")
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	if len(store.entries) != 0 {
		t.Fatal("tickets remained after concurrent issue, consume and prune")
	}
}

func TestParseRedisEndpoint(t *testing.T) {
	for _, tt := range []struct {
		name, endpoint string
		want           redisEndpointConfig
	}{
		{name: "address", endpoint: "redis.invalid:6379", want: redisEndpointConfig{addr: "redis.invalid:6379", poolSize: 16}},
		{name: "pool", endpoint: "redis.invalid:6379,8", want: redisEndpointConfig{addr: "redis.invalid:6379", poolSize: 8}},
		{name: "password", endpoint: "redis.invalid:6379,8,secret", want: redisEndpointConfig{addr: "redis.invalid:6379", poolSize: 8, password: "secret"}},
		{name: "database", endpoint: " redis.invalid:6379 , 8 , secret , 2 ", want: redisEndpointConfig{addr: "redis.invalid:6379", poolSize: 8, password: " secret ", dbNum: 2}},
		{name: "IPv6", endpoint: "[2001:db8::1]:6379", want: redisEndpointConfig{addr: "[2001:db8::1]:6379", poolSize: 16}},
		{name: "empty pool", endpoint: "redis.invalid:6379, ,secret", want: redisEndpointConfig{addr: "redis.invalid:6379", poolSize: 16, password: "secret"}},
		{name: "empty password", endpoint: "redis.invalid:6379,8,,2", want: redisEndpointConfig{addr: "redis.invalid:6379", poolSize: 8, dbNum: 2}},
		{name: "all defaults", endpoint: "redis.invalid:6379,,,", want: redisEndpointConfig{addr: "redis.invalid:6379", poolSize: 16}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseRedisEndpoint(tt.endpoint)
			if err != nil || cfg != tt.want {
				t.Fatalf("parseRedisEndpoint(%q) = %#v, %v; want %#v", tt.endpoint, cfg, err, tt.want)
			}
		})
	}
	for _, endpoint := range []string{
		"", " ", " ,8", "redis.invalid:6379,8,secret,2,extra", "redis.invalid:6379,no", "redis.invalid:6379,0", "redis.invalid:6379,-1",
		"redis.invalid:6379,,,no", "redis.invalid:6379,,,-1",
	} {
		if _, err := parseRedisEndpoint(endpoint); err == nil {
			t.Errorf("parseRedisEndpoint(%q) succeeded for an invalid endpoint", endpoint)
		}
	}
}

func TestGetConnectTicketBackend(t *testing.T) {
	connectTicketRedisCache.Lock()
	originalEndpoint, originalStore := connectTicketRedisCache.endpoint, connectTicketRedisCache.store
	connectTicketRedisCache.endpoint = ""
	connectTicketRedisCache.store = nil
	connectTicketRedisCache.Unlock()
	t.Cleanup(func() {
		connectTicketRedisCache.Lock()
		defer connectTicketRedisCache.Unlock()
		if connectTicketRedisCache.store != nil {
			_ = connectTicketRedisCache.store.Close()
		}
		connectTicketRedisCache.endpoint = originalEndpoint
		connectTicketRedisCache.store = originalStore
	})

	t.Setenv("redisEndpoint", " ")
	if backend, err := getConnectTicketBackend(); err != nil || backend != defaultConnectTicketStore {
		t.Fatalf("expected the memory backend without a Redis endpoint, got %T, %v", backend, err)
	}

	t.Setenv("redisEndpoint", "  redis.invalid:6379,8  ")
	backend, err := getConnectTicketBackend()
	first, ok := backend.(*redisConnectTicketStore)
	if err != nil || !ok || first.pool.MaxIdle != 8 {
		t.Fatalf("expected a Redis backend with the configured pool size, got %T, %v", backend, err)
	}
	t.Setenv("redisEndpoint", "redis.invalid:6379,8")
	if cached, err := getConnectTicketBackend(); err != nil || cached != first {
		t.Fatal("a pool was not reused for the same endpoint")
	}
	// Never dial even if the old pool was not closed.
	unexpectedDial := errors.New("unexpected dial of the old pool")
	first.pool.Dial = func() (redis.Conn, error) { return nil, unexpectedDial }
	t.Setenv("redisEndpoint", "[::1]:6379")
	backend, err = getConnectTicketBackend()
	second, ok := backend.(*redisConnectTicketStore)
	if err != nil || !ok || second == first {
		t.Fatalf("expected a new Redis pool after changing endpoint, got %T, %v", backend, err)
	}
	conn := first.pool.Get()
	defer conn.Close()
	if err := conn.Err(); err == nil || errors.Is(err, unexpectedDial) {
		t.Fatal("the old pool was not closed")
	}

	t.Setenv("redisEndpoint", "redis.invalid:6379,-1")
	if backend, err := getConnectTicketBackend(); err == nil || backend != nil {
		t.Fatalf("invalid endpoint did not fail closed, got %T, %v", backend, err)
	}
	stub := newConnectTicketTestStore(1, time.Now)
	stubConnectTicketController(t, stub, authorizeConnectTicket)
	if backend, err := getConnectTicketBackend(); err != nil || backend != stub {
		t.Fatal("the backend override did not bypass Redis configuration")
	}
}

type redisConnectTicketTestIndex struct {
	members   map[string]int64
	expiresAt int64
}

// This server models the script contract under a single lock, including the
// shared script cache and both expiring indexes.
type redisConnectTicketTestServer struct {
	mu      sync.Mutex
	clock   atomic.Int64
	entries map[string]connectTicketEntry
	indexes map[string]redisConnectTicketTestIndex
	scripts map[string]string
	evalSHA int
	eval    int
	misses  int
	failure error
}

func newRedisConnectTicketTestServer() *redisConnectTicketTestServer {
	s := &redisConnectTicketTestServer{
		entries: make(map[string]connectTicketEntry),
		indexes: make(map[string]redisConnectTicketTestIndex),
		scripts: make(map[string]string),
	}
	s.clock.Store(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli())
	return s
}

func (s *redisConnectTicketTestServer) newStore(t *testing.T, maxEntries, maxPerUser int) *redisConnectTicketStore {
	t.Helper()
	store := &redisConnectTicketStore{
		pool: &redis.Pool{
			MaxIdle: 4,
			Dial:    func() (redis.Conn, error) { return redisConnectTicketTestConn{s}, nil },
		},
		ttl:        2 * time.Second,
		maxEntries: maxEntries,
		maxPerUser: maxPerUser,
		now:        func() time.Time { return time.UnixMilli(s.clock.Load()) },
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

type redisConnectTicketTestConn struct {
	server *redisConnectTicketTestServer
}

func (redisConnectTicketTestConn) Close() error { return nil }

func (redisConnectTicketTestConn) Err() error { return nil }

func (redisConnectTicketTestConn) Send(string, ...interface{}) error {
	return errors.New("unexpected Send")
}

func (redisConnectTicketTestConn) Flush() error { return errors.New("unexpected Flush") }

func (redisConnectTicketTestConn) Receive() (interface{}, error) {
	return nil, errors.New("unexpected Receive")
}

func (c redisConnectTicketTestConn) Do(command string, args ...interface{}) (interface{}, error) {
	if command == "" { // Returning a connection to the pool drains any replies.
		return nil, nil
	}
	s := c.server
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failure != nil {
		return nil, s.failure
	}
	const contractError = redis.Error("ERR invalid connect ticket script arguments")
	if len(args) < 8 || args[1] != 3 {
		return nil, contractError
	}
	script, ok := args[0].(string)
	if !ok {
		return nil, contractError
	}
	switch command {
	case "EVALSHA":
		s.evalSHA++
		script, ok = s.scripts[script]
		if !ok {
			s.misses++
			return nil, redis.Error("NOSCRIPT No matching script. Please use EVAL.")
		}
	case "EVAL":
		s.eval++
		s.scripts[fmt.Sprintf("%x", sha1.Sum([]byte(script)))] = script
	default:
		return nil, redis.Error("ERR unexpected Redis command")
	}

	ticket, ticketOK := args[5].(string)
	userId, userOK := args[6].(string)
	assetId, assetOK := args[7].(string)
	ticketKey, userKey, globalKey := "casvisor:{connect_ticket}:t:"+ticket, "casvisor:{connect_ticket}:u:"+userId, "casvisor:{connect_ticket}:all"
	if !ticketOK || !userOK || !assetOK || args[2] != ticketKey || args[3] != userKey || args[4] != globalKey {
		return nil, contractError
	}

	now := s.clock.Load()
	entry, exists := s.entries[ticketKey]
	if exists && entry.expiresAt.UnixMilli() <= now {
		delete(s.entries, ticketKey)
		exists = false
	}
	switch {
	case strings.Contains(script, "redis.call('HMSET', KEYS[1]") && len(args) == 15:
		username, usernameOK := args[8].(string)
		password, passwordOK := args[9].(string)
		nowMs, nowOK := args[10].(int64)
		expiresAt, expiresOK := args[11].(int64)
		ttlMs, ttlOK := args[12].(int64)
		maxEntries, entriesOK := args[13].(int)
		maxPerUser, perUserOK := args[14].(int)
		if !usernameOK || !passwordOK || !nowOK || !expiresOK || !ttlOK || !entriesOK || !perUserOK || nowMs != now || ttlMs <= 0 || expiresAt != nowMs+ttlMs {
			return nil, contractError
		}
		if exists {
			return int64(-2), nil
		}
		global := s.pruneIndex(globalKey, nowMs)
		if len(global.members) >= maxEntries {
			return int64(-3), nil
		}
		user := s.pruneIndex(userKey, nowMs)
		if len(user.members) >= maxPerUser {
			return int64(-1), nil
		}
		s.entries[ticketKey] = connectTicketEntry{userId: userId, assetId: assetId, username: username, password: password, expiresAt: time.UnixMilli(now + ttlMs)}
		for key, index := range map[string]redisConnectTicketTestIndex{userKey: user, globalKey: global} {
			index.members[ticket] = expiresAt
			index.expiresAt = now + ttlMs
			s.indexes[key] = index
		}
		return int64(1), nil
	case strings.Contains(script, "redis.call('HMGET', KEYS[1]") && len(args) == 8:
		if !exists || entry.userId != userId || entry.assetId != assetId {
			return nil, nil
		}
		delete(s.entries, ticketKey)
		delete(s.indexes[userKey].members, ticket)
		delete(s.indexes[globalKey].members, ticket)
		return []interface{}{[]byte(entry.username), []byte(entry.password)}, nil
	default:
		return nil, contractError
	}
}

func (s *redisConnectTicketTestServer) pruneIndex(key string, now int64) redisConnectTicketTestIndex {
	index := s.indexes[key]
	if index.expiresAt <= now {
		delete(s.indexes, key)
		return redisConnectTicketTestIndex{members: make(map[string]int64)}
	}
	for ticket, score := range index.members {
		if score <= now {
			delete(index.members, ticket)
		}
	}
	return index
}

func TestRedisConnectTicketStore(t *testing.T) {
	const (
		userId      = connectTicketTestUserId
		assetId     = connectTicketTestAssetId
		otherUserId = "org/bob"
		username    = "manual-user"
		password    = "manual-password"
	)
	consume := func(t *testing.T, store connectTicketBackend, ticket string) {
		t.Helper()
		gotUser, gotPassword, err := store.Consume(ticket, userId, assetId)
		if err != nil || gotUser != username || gotPassword != password {
			t.Fatalf("consume did not return the bound credentials: %v", err)
		}
	}
	t.Run("cross-instance issue and single-use consume", func(t *testing.T) {
		server := newRedisConnectTicketTestServer()
		issuer, consumer := server.newStore(t, 1, 1), server.newStore(t, 1, 1)
		ticket := issueConnectTicketForTest(t, issuer, userId, assetId, username, password)
		if bytes, err := base64.RawURLEncoding.DecodeString(ticket); err != nil || len(bytes) != 32 {
			t.Fatal("ticket is not 32 bytes encoded as raw URL-safe base64")
		}
		consume(t, consumer, ticket)
		assertConnectTicketRejected(t, issuer, ticket, userId, assetId)
		issueConnectTicketForTest(t, consumer, userId, assetId, username, password)

		server.mu.Lock()
		defer server.mu.Unlock()
		if server.evalSHA != 4 || server.misses != 2 || server.eval != 2 {
			t.Fatalf("expected one NOSCRIPT fallback per script, got %d EVALSHA, %d misses, %d EVAL", server.evalSHA, server.misses, server.eval)
		}
	})
	t.Run("mismatched binding does not burn ticket", func(t *testing.T) {
		store := newRedisConnectTicketTestServer().newStore(t, 2, 1)
		ticket := issueConnectTicketForTest(t, store, userId, assetId, username, password)
		assertConnectTicketRejected(t, store, ticket, otherUserId, assetId)
		assertConnectTicketRejected(t, store, ticket, userId, "org/other-target")
		consume(t, store, ticket)
	})
	t.Run("expires at TTL boundary", func(t *testing.T) {
		server := newRedisConnectTicketTestServer()
		store := server.newStore(t, 2, 2)
		live := issueConnectTicketForTest(t, store, userId, assetId, username, password)
		expired := issueConnectTicketForTest(t, store, userId, assetId, username, password)
		server.clock.Add(store.ttl.Milliseconds() - 1)
		consume(t, store, live)
		server.clock.Add(1)
		assertConnectTicketRejected(t, store, expired, userId, assetId)
	})
	t.Run("user cap and slot reclaim", func(t *testing.T) {
		server := newRedisConnectTicketTestServer()
		store, other := server.newStore(t, 5, 2), server.newStore(t, 5, 2)
		issueConnectTicketForTest(t, store, userId, assetId, username, password)
		server.clock.Add(store.ttl.Milliseconds() / 2)
		second := issueConnectTicketForTest(t, store, userId, assetId, username, password)
		if ticket, err := other.Issue(userId, "org/other-target", username, password); ticket != "" || !errors.Is(err, errUserConnectTicketFull) {
			t.Fatalf("expected shared per-user limit across assets, got %v", err)
		}
		issueConnectTicketForTest(t, other, otherUserId, assetId, username, password)
		consume(t, other, second)
		issueConnectTicketForTest(t, other, userId, assetId, username, password)
		server.clock.Add(store.ttl.Milliseconds() / 2)
		issueConnectTicketForTest(t, store, userId, assetId, username, password)
	})
	t.Run("global cap and slot reclaim", func(t *testing.T) {
		server := newRedisConnectTicketTestServer()
		store, other := server.newStore(t, 2, 2), server.newStore(t, 2, 2)
		issueConnectTicketForTest(t, store, userId, assetId, username, password)
		server.clock.Add(store.ttl.Milliseconds() / 2)
		issueConnectTicketForTest(t, other, otherUserId, assetId, username, password)
		if ticket, err := other.Issue("org/carol", assetId, username, password); ticket != "" || !errors.Is(err, errConnectTicketFull) {
			t.Fatalf("expected a shared global limit, got %v", err)
		}
		server.clock.Add(store.ttl.Milliseconds() / 2)
		issueConnectTicketForTest(t, store, "org/carol", assetId, username, password)
	})
	t.Run("concurrent consumption", func(t *testing.T) {
		server := newRedisConnectTicketTestServer()
		stores := [2]*redisConnectTicketStore{server.newStore(t, 2, 2), server.newStore(t, 2, 2)}
		ticket := issueConnectTicketForTest(t, stores[0], userId, assetId, username, password)
		start := make(chan struct{})
		var winners atomic.Int64
		var wg sync.WaitGroup
		for i := range 64 {
			wg.Add(1)
			go func(store *redisConnectTicketStore) {
				defer wg.Done()
				<-start
				gotUser, gotPassword, err := store.Consume(ticket, userId, assetId)
				if err == nil && gotUser == username && gotPassword == password {
					winners.Add(1)
				} else if !errors.Is(err, errInvalidConnectTicket) || gotUser != "" || gotPassword != "" {
					t.Errorf("unexpected consumption result: %v", err)
				}
			}(stores[i%len(stores)])
		}
		close(start)
		wg.Wait()
		if got := winners.Load(); got != 1 {
			t.Fatalf("expected exactly one winner, got %d", got)
		}
	})
	t.Run("Redis failures are closed and redacted", func(t *testing.T) {
		server := newRedisConnectTicketTestServer()
		store := server.newStore(t, 2, 2)
		ticket := issueConnectTicketForTest(t, store, userId, assetId, username, password)
		server.mu.Lock()
		server.failure = redis.Error("ERR command failed with an argument: " + password)
		server.mu.Unlock()
		if got, err := store.Issue(userId, assetId, username, password); got != "" || err != errRedisConnectTicketUnavailable {
			t.Fatal("issue did not fail closed with a redacted error")
		}
		if gotUser, gotPassword, err := store.Consume(ticket, userId, assetId); gotUser != "" || gotPassword != "" || err != errRedisConnectTicketUnavailable {
			t.Fatal("consume did not fail closed with a redacted error")
		}
		assertConnectTicketRejected(t, store, "", userId, assetId)
		assertConnectTicketRejected(t, store, ticket, "", assetId)
		assertConnectTicketRejected(t, store, ticket, userId, "")
	})
}

func TestResolveTunnelCredentials(t *testing.T) {
	user := &casdoorsdk.User{Owner: "org", Name: "alice"}
	for _, tt := range []struct {
		name, account                  string
		savedUsername, savedPassword   string
		ticketUsername, ticketPassword string
		queryUsername                  string
		wantUsername, wantPassword     string
	}{
		{name: "ticket credentials", ticketUsername: "manual", ticketPassword: "typed", queryUsername: "query", wantUsername: "manual", wantPassword: "typed"},
		{name: "saved credentials", savedUsername: "saved", savedPassword: "stored", ticketUsername: "manual", ticketPassword: "typed", wantUsername: "saved", wantPassword: "stored"},
		{name: "password-only ticket", ticketPassword: "typed", queryUsername: "query", wantUsername: "query", wantPassword: "typed"},
		{name: "passwordless", queryUsername: "direct", wantUsername: "direct"},
		{name: "direct saved password", savedPassword: "stored", wantPassword: "stored"},
		{name: "direct without credentials"},
		{name: "direct named account", account: "named", queryUsername: "query"},
		{name: "named account", account: "named", ticketUsername: "unused", ticketPassword: "unused", queryUsername: "query"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := newConnectTicketTestStore(1, time.Now)
			asset := &object.Asset{Owner: "org", Name: "target", Username: tt.savedUsername, Password: tt.savedPassword, PrivateKey: "key"}
			var ticket string
			var backend connectTicketBackend
			if tt.ticketUsername != "" || tt.ticketPassword != "" {
				backend = store
				ticket = issueConnectTicketForTest(t, backend, getUserId(user), asset.GetId(), tt.ticketUsername, tt.ticketPassword)
			}
			if err := resolveTunnelCredentials(backend, user, asset, tt.account, ticket, tt.queryUsername); err != nil {
				t.Fatalf("resolving credentials: %v", err)
			}
			if asset.Username != tt.wantUsername || asset.Password != tt.wantPassword || asset.PrivateKey != "key" {
				t.Fatal("asset had unexpected credentials after resolution")
			}
			if ticket != "" {
				assertConnectTicketRejected(t, store, ticket, getUserId(user), asset.GetId())
			}
		})
	}
}

func TestResolveTunnelCredentialsRejectsInvalidTickets(t *testing.T) {
	user := &casdoorsdk.User{Owner: "org", Name: "alice"}
	for _, name := range []string{"invalid", "expired", "reused", "wrong user", "wrong asset", "invalid with named account"} {
		t.Run(name, func(t *testing.T) {
			now := time.Unix(0, 0)
			store := newConnectTicketTestStore(1, func() time.Time { return now })
			asset := &object.Asset{Owner: "org", Name: "target", Password: "saved"}
			ticket := "invalid"
			account := ""
			switch name {
			case "expired":
				ticket = issueConnectTicketForTest(t, store, getUserId(user), asset.GetId(), "unused", "unused")
				now = now.Add(connectTicketTTL)
			case "reused":
				ticket = issueConnectTicketForTest(t, store, getUserId(user), asset.GetId(), "unused", "unused")
				if _, _, err := store.Consume(ticket, getUserId(user), asset.GetId()); err != nil {
					t.Fatalf("could not consume the ticket: %v", err)
				}
			case "wrong user":
				ticket = issueConnectTicketForTest(t, store, "org/bob", asset.GetId(), "unused", "unused")
			case "wrong asset":
				ticket = issueConnectTicketForTest(t, store, getUserId(user), "org/other", "unused", "unused")
			case "invalid with named account":
				account = "named"
			}
			err := resolveTunnelCredentials(store, user, asset, account, ticket, "fallback")
			if !errors.Is(err, errInvalidConnectTicket) || asset.Username != "" || asset.Password != "saved" {
				t.Fatal("a bad ticket did not fail without a credential fallback")
			}
		})
	}
}

func TestTunnelRejectsQueryPasswordBeforeSessionLookup(t *testing.T) {
	for _, query := range []string{"password=legacy", "password=&password=legacy"} {
		r := httptest.NewRequest(http.MethodGet, "/api/get-asset-ssh?"+query, nil)
		ctx := beegocontext.NewContext()
		ctx.Reset(httptest.NewRecorder(), r)
		c := &ApiController{}
		c.Init(ctx, "ApiController", "GetAssetSsh", c)
		if _, _, code, err := c.getTunnelSessionAndAsset("", "", ""); code != ParametersError || !errors.Is(err, errTunnelPasswordQuery) {
			t.Fatalf("expected an early parameters error, got %d and %v", code, err)
		}
	}
}

func TestCheckTunnelQuery(t *testing.T) {
	if err := checkTunnelQuery(nil); !errors.Is(err, errTunnelQuery) {
		t.Fatalf("a missing request was not rejected: %v", err)
	}
	for _, tt := range []struct {
		query   string
		wantErr bool
	}{
		{"sessionId=org/session&ticket=one", false},
		{"sessionId=org/session&username=root&password=", false},
		{"password=legacy", true},
		{"password=&password=legacy", true},
		{"ticket=one&ticket=two", true},
		{"password=%ZZ", true},
	} {
		r := httptest.NewRequest(http.MethodGet, "/api/get-asset-ssh", nil)
		r.URL.RawQuery = tt.query
		if err := checkTunnelQuery(r); (err != nil) != tt.wantErr {
			t.Errorf("checkTunnelQuery(%q) = %v, want error %v", tt.query, err, tt.wantErr)
		}
	}
}

type connectTicketTestSessionStore struct {
	user *casdoorsdk.User
}

var _ session.Store = connectTicketTestSessionStore{}

func (connectTicketTestSessionStore) Set(interface{}, interface{}) error {
	return nil
}

func (s connectTicketTestSessionStore) Get(key interface{}) interface{} {
	if key == "user" && s.user != nil {
		return casdoorsdk.Claims{User: *s.user}
	}
	return nil
}

func (connectTicketTestSessionStore) Delete(interface{}) error {
	return nil
}

func (connectTicketTestSessionStore) SessionID() string {
	return "connect-ticket-test"
}

func (connectTicketTestSessionStore) SessionRelease(http.ResponseWriter) {}

func (connectTicketTestSessionStore) Flush() error {
	return nil
}

func newConnectTicketController(body string, user *casdoorsdk.User) (*ApiController, *httptest.ResponseRecorder) {
	r := httptest.NewRequest(http.MethodPost, "/api/create-connect-ticket", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "192.0.2.1:1234"
	w := httptest.NewRecorder()
	ctx := beegocontext.NewContext()
	ctx.Reset(w, r)
	ctx.Input.RequestBody = []byte(body)
	c := &ApiController{}
	c.Init(ctx, "ApiController", "CreateConnectTicket", c)
	store := connectTicketTestSessionStore{user: user}
	ctx.Input.CruSession = store
	c.CruSession = store
	return c, w
}

// Only serial controller tests replace these dependencies.
func stubConnectTicketController(t *testing.T, store connectTicketBackend, authorize func(*casdoorsdk.User, string, string) error) {
	t.Helper()
	oldStore, oldAuthorize := connectTicketBackendOverride, authorizeConnectTicket
	connectTicketBackendOverride, authorizeConnectTicket = store, authorize
	t.Cleanup(func() {
		connectTicketBackendOverride, authorizeConnectTicket = oldStore, oldAuthorize
	})
}

type connectTicketTestResponse struct {
	Status string                `json:"status"`
	Msg    string                `json:"msg"`
	Data   connectTicketResponse `json:"data"`
}

func readConnectTicketResponse(t *testing.T, w *httptest.ResponseRecorder) connectTicketTestResponse {
	t.Helper()
	var response connectTicketTestResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("could not decode connect ticket response: %v", err)
	}
	return response
}

func TestCreateConnectTicket(t *testing.T) {
	user := &casdoorsdk.User{Owner: "org", Name: "alice"}
	const (
		passwordOnly  = `{"assetId":"org/target","password":"typed"}`
		usernameOnly  = `{"owner":"org","name":"target","username":"remote-user"}`
		allowedOrigin = "https://ui.example.net"
	)
	t.Setenv("allowedOrigins", allowedOrigin)
	denied := errors.New("connect permission denied")
	for _, tt := range []struct {
		name, body, origin         string
		user                       *casdoorsdk.User
		wantAuthorization          bool
		authorizeErr               error
		full                       bool
		wantError                  string
		wantUsername, wantPassword string
	}{
		{name: "unauthenticated", body: passwordOnly, wantError: "please sign in first"},
		{name: "deleted", body: passwordOnly, user: &casdoorsdk.User{IsDeleted: true}, wantError: "unauthorized"},
		{name: "forbidden", body: passwordOnly, user: &casdoorsdk.User{IsForbidden: true}, wantError: "unauthorized"},
		{name: "invalid JSON", body: `{"password":"typed"`, user: user, wantError: "invalid connect ticket request"},
		{name: "oversize request", body: strings.Repeat(" ", connectTicketMaxRequestBytes) + passwordOnly, user: user, wantError: "connect ticket request is too large"},
		{name: "missing asset ID", body: `{"password":"typed"}`, user: user, wantError: "assetId must be in owner/name format"},
		{name: "no separator", body: `{"assetId":"org","password":"typed"}`, user: user, wantError: "assetId must be in owner/name format"},
		{name: "extra separators", body: `{"assetId":"org/target/extra","password":"typed"}`, user: user, wantError: "assetId must be in owner/name format"},
		{name: "empty owner", body: `{"assetId":"/target","password":"typed"}`, user: user, wantError: "assetId must be in owner/name format"},
		{name: "empty name", body: `{"assetId":"org/","password":"typed"}`, user: user, wantError: "assetId must be in owner/name format"},
		{name: "slash in owner", body: `{"owner":"org/other","name":"target","password":"typed"}`, user: user, wantError: "assetId must be in owner/name format"},
		{name: "slash in name", body: `{"owner":"org","name":"target/extra","password":"typed"}`, user: user, wantError: "assetId must be in owner/name format"},
		{name: "empty credentials", body: `{"assetId":"org/target"}`, user: user, wantError: "a username or password is required"},
		{name: "access denied", body: passwordOnly, user: user, wantAuthorization: true, authorizeErr: denied, wantError: denied.Error()},
		{name: "disallowed origin", body: passwordOnly, origin: "https://untrusted.example.net", user: user, wantError: "forbidden origin"},
		{name: "same origin", body: passwordOnly, origin: "http://example.com", user: user, wantAuthorization: true, wantPassword: "typed"},
		{name: "allowlisted origin", body: passwordOnly, origin: allowedOrigin, user: user, wantAuthorization: true, wantPassword: "typed"},
		{name: "asset ID and VNC password", body: passwordOnly, user: user, wantAuthorization: true, wantPassword: "typed"},
		{name: "owner and name with SSH username", body: usernameOnly, user: user, wantAuthorization: true, wantUsername: "remote-user"},
		{name: "full store", body: passwordOnly, user: user, wantAuthorization: true, full: true, wantError: errConnectTicketFull.Error()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := newConnectTicketTestStore(1, time.Now)
			if tt.full {
				issueConnectTicketForTest(t, store, connectTicketTestUserId, connectTicketTestAssetId, "", "other-password")
			}
			calls := 0
			stubConnectTicketController(t, store, func(user *casdoorsdk.User, assetId, clientIp string) error {
				calls++
				if getUserId(user) != connectTicketTestUserId || assetId != connectTicketTestAssetId || clientIp != "192.0.2.1" {
					t.Fatal("authorization received the wrong user, asset ID or client IP")
				}
				return tt.authorizeErr
			})
			c, w := newConnectTicketController(tt.body, tt.user)
			if tt.origin != "" {
				c.Ctx.Request.Header.Set("Origin", tt.origin)
			}
			c.CreateConnectTicket()
			res := readConnectTicketResponse(t, w)
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("expected a non-cacheable response")
			}
			if (calls == 1) != tt.wantAuthorization || calls > 1 {
				t.Fatalf("authorization was called %d times", calls)
			}
			if tt.wantError != "" {
				if res.Status != "error" || res.Msg != tt.wantError || res.Data.Ticket != "" {
					t.Fatalf("expected error message %q without a ticket", tt.wantError)
				}
				if !tt.full && len(store.entries) != 0 {
					t.Fatal("a rejected request issued a ticket")
				}
				if tt.wantError == "forbidden origin" && c.Ctx.Input.RequestBody != nil {
					t.Fatal("the disallowed-origin request body was not cleared")
				}
				return
			}
			if res.Status != "ok" || res.Data.Ticket == "" {
				t.Fatal("expected a successful ticket response")
			}
			username, password, err := store.Consume(res.Data.Ticket, getUserId(user), connectTicketTestAssetId)
			if err != nil || username != tt.wantUsername || password != tt.wantPassword {
				t.Fatal("issued ticket had the wrong binding or credentials")
			}
		})
	}
}

func TestConnectTicketAuditRecord(t *testing.T) {
	user := &casdoorsdk.User{Owner: "org", Name: "alice"}
	store := newConnectTicketTestStore(2, time.Now)
	stubConnectTicketController(t, store, func(*casdoorsdk.User, string, string) error { return nil })
	const secret = "audit-example-password"
	for _, key := range []string{"password", "Password"} {
		t.Run(key, func(t *testing.T) {
			c, w := newConnectTicketController(`{"assetId":"org/target","`+key+`":"`+secret+`","extra":"`+secret+`"}`, user)
			c.CreateConnectTicket()
			res := readConnectTicketResponse(t, w)
			if res.Status != "ok" || res.Data.Ticket == "" {
				t.Fatal("expected a ticket for the audit test")
			}
			record, err := object.NewRecord(c.Ctx)
			if err != nil {
				t.Fatalf("creating the audit record: %v", err)
			}
			if !strings.Contains(record.Object, `"password":"***"`) || strings.Contains(record.Object, secret) {
				t.Fatal("the audit record did not mask the password")
			}
			if record.Response != `{"status":"ok","msg":""}` || strings.Contains(record.Response, res.Data.Ticket) {
				t.Fatal("the audit response did not record the successful status without the ticket")
			}
		})
	}
	t.Run("malformed body", func(t *testing.T) {
		c, _ := newConnectTicketController(`{"password":"`+secret+`"`, user)
		c.CreateConnectTicket()
		record, err := object.NewRecord(c.Ctx)
		if err != nil || record.Object != "" {
			t.Fatal("the malformed credential request was not omitted from the audit record")
		}
	})
}
