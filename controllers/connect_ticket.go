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
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/object"
)

const (
	connectTicketTTL             = 60 * time.Second
	connectTicketMaxEntries      = 4096
	connectTicketMaxPerUser      = 32
	connectTicketPruneInterval   = 15 * time.Second
	connectTicketMaxRequestBytes = 16 * 1024
)

var (
	errInvalidConnectTicket       = errors.New("invalid or expired connect ticket")
	errConnectTicketFull          = errors.New("connect ticket store is full, try again shortly")
	errUserConnectTicketFull      = errors.New("too many pending connect tickets for this user, try again shortly")
	errConnectTicketCollision     = errors.New("failed to generate a unique connect ticket")
	errConnectTicketNeedsIdentity = errors.New("a user and asset are required to issue a connect ticket")
)

type connectTicketBackend interface {
	Issue(userId, assetId, username, password string) (string, error)
	Consume(ticket, userId, assetId string) (string, string, error)
}

type connectTicketEntry struct {
	userId    string
	assetId   string
	username  string
	password  string
	expiresAt time.Time
}

type connectTicketStore struct {
	ttl               time.Duration
	maxEntries        int
	maxPerUser        int
	now               func() time.Time
	backgroundCleanup bool
	cleanupOnce       sync.Once

	mu         sync.Mutex
	entries    map[string]connectTicketEntry
	userCounts map[string]int
	nextPrune  time.Time
}

var defaultConnectTicketStore = &connectTicketStore{
	ttl:               connectTicketTTL,
	maxEntries:        connectTicketMaxEntries,
	maxPerUser:        connectTicketMaxPerUser,
	now:               time.Now,
	backgroundCleanup: true,
}

// connectTicketBackendOverride replaces the configured store in tests.
var connectTicketBackendOverride connectTicketBackend

var connectTicketRedisCache struct {
	sync.Mutex
	endpoint string
	store    *redisConnectTicketStore
}

// getConnectTicketBackend keeps tickets in Redis when the sessions are, so any instance can redeem
// them. A bad Redis endpoint fails instead of falling back to this process.
func getConnectTicketBackend() (connectTicketBackend, error) {
	if connectTicketBackendOverride != nil {
		return connectTicketBackendOverride, nil
	}

	connectTicketRedisCache.Lock()
	defer connectTicketRedisCache.Unlock()

	endpoint := strings.TrimSpace(conf.GetConfigString("redisEndpoint"))
	if connectTicketRedisCache.store != nil {
		if connectTicketRedisCache.endpoint == endpoint {
			return connectTicketRedisCache.store, nil
		}
		_ = connectTicketRedisCache.store.Close()
		connectTicketRedisCache.store = nil
		connectTicketRedisCache.endpoint = ""
	}
	if endpoint == "" {
		return defaultConnectTicketStore, nil
	}

	cfg, err := parseRedisEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	connectTicketRedisCache.endpoint = endpoint
	connectTicketRedisCache.store = newRedisConnectTicketStore(cfg)
	return connectTicketRedisCache.store, nil
}

func generateConnectTicket() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", errors.New("failed to generate a connect ticket")
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

func (s *connectTicketStore) Issue(userId, assetId, username, password string) (string, error) {
	if userId == "" || assetId == "" {
		return "", errConnectTicketNeedsIdentity
	}

	ticket, err := generateConnectTicket()
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	maxPerUser := s.maxPerUser
	if maxPerUser <= 0 {
		maxPerUser = s.maxEntries
	}
	if len(s.entries) >= s.maxEntries || s.userCounts[userId] >= maxPerUser || !now.Before(s.nextPrune) {
		s.pruneExpiredLocked(now)
	}
	if len(s.entries) >= s.maxEntries {
		return "", errConnectTicketFull
	}
	if s.userCounts[userId] >= maxPerUser {
		return "", errUserConnectTicketFull
	}
	if _, exists := s.entries[ticket]; exists {
		return "", errConnectTicketCollision
	}
	if s.entries == nil {
		s.entries = make(map[string]connectTicketEntry)
		s.userCounts = make(map[string]int)
	}
	s.entries[ticket] = connectTicketEntry{userId: userId, assetId: assetId, username: username, password: password, expiresAt: now.Add(s.ttl)}
	s.userCounts[userId]++
	if s.backgroundCleanup {
		s.cleanupOnce.Do(func() { go s.runCleanup() })
	}
	return ticket, nil
}

func (s *connectTicketStore) Consume(ticket, userId, assetId string) (string, string, error) {
	if ticket == "" || userId == "" || assetId == "" {
		return "", "", errInvalidConnectTicket
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if !now.Before(s.nextPrune) {
		s.pruneExpiredLocked(now)
	}
	entry, ok := s.entries[ticket]
	if !ok {
		return "", "", errInvalidConnectTicket
	}
	if !now.Before(entry.expiresAt) {
		s.removeLocked(ticket, entry.userId)
		return "", "", errInvalidConnectTicket
	}
	if entry.userId != userId || entry.assetId != assetId {
		return "", "", errInvalidConnectTicket
	}
	s.removeLocked(ticket, entry.userId)
	return entry.username, entry.password, nil
}

func (s *connectTicketStore) removeLocked(ticket, userId string) {
	delete(s.entries, ticket)
	s.userCounts[userId]--
	if s.userCounts[userId] == 0 {
		delete(s.userCounts, userId)
	}
}

func (s *connectTicketStore) pruneExpiredLocked(now time.Time) {
	for ticket, entry := range s.entries {
		if !now.Before(entry.expiresAt) {
			s.removeLocked(ticket, entry.userId)
		}
	}
	s.nextPrune = now.Add(connectTicketPruneInterval)
}

func (s *connectTicketStore) pruneExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredLocked(s.now())
}

func (s *connectTicketStore) runCleanup() {
	ticker := time.NewTicker(connectTicketPruneInterval)
	defer ticker.Stop()
	for range ticker.C {
		s.pruneExpired()
	}
}

type connectTicketRequest struct {
	AssetId  string `json:"assetId"`
	Owner    string `json:"owner"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type connectTicketResponse struct {
	Ticket string `json:"ticket"`
}

func (req connectTicketRequest) assetId() (string, bool) {
	if req.AssetId != "" {
		owner, name, ok := strings.Cut(req.AssetId, "/")
		return req.AssetId, ok && owner != "" && name != "" && !strings.Contains(name, "/")
	}
	if req.Owner == "" || req.Name == "" || strings.Contains(req.Owner, "/") || strings.Contains(req.Name, "/") {
		return "", false
	}
	return req.Owner + "/" + req.Name, true
}

var authorizeConnectTicket = func(user *casdoorsdk.User, assetId, clientIp string) error {
	asset, err := object.GetAsset(assetId)
	if err != nil {
		return err
	}
	if asset == nil {
		return fmt.Errorf("the asset: %s does not exist", assetId)
	}

	account := ""
	actions, err := object.GetAssetAccountActions(user, asset, &account, clientIp)
	if err != nil {
		return err
	}
	if !slices.Contains(actions, object.ActionConnect) {
		return fmt.Errorf("you have no permission to connect to the asset: %s", assetId)
	}
	return nil
}

// CreateConnectTicket
// @Title CreateConnectTicket
// @Tag Session API
// @Description exchange manually entered asset credentials for a short-lived one-time connect ticket
// @Param body body controllers.connectTicketRequest true "The asset and manual credentials"
// @Success 200 {object} controllers.Response The Response object
// @router /create-connect-ticket [post]
func (c *ApiController) CreateConnectTicket() {
	c.Ctx.Output.Header("Cache-Control", "no-store")
	body := c.Ctx.Input.RequestBody
	// The audit record keeps the body, so it is only put back once it is known to be clean.
	c.Ctx.Input.RequestBody = nil

	if !checkWebSocketOrigin(c.Ctx.Request) {
		c.ResponseError("forbidden origin")
		return
	}

	user := c.GetSessionUser()
	if user == nil {
		c.ResponseError("please sign in first")
		return
	}
	if user.IsDeleted || user.IsForbidden {
		c.ResponseError("unauthorized")
		return
	}

	if len(body) > connectTicketMaxRequestBytes {
		c.ResponseError("connect ticket request is too large")
		return
	}
	var req connectTicketRequest
	if err := json.Unmarshal(body, &req); err != nil {
		c.ResponseError("invalid connect ticket request")
		return
	}
	assetId, valid := req.assetId()
	if !valid {
		c.ResponseError("assetId must be in owner/name format")
		return
	}
	if req.Username == "" && req.Password == "" {
		c.ResponseError("a username or password is required")
		return
	}
	// Only the asset and the password, which the audit record masks, are kept.
	c.Ctx.Input.RequestBody, _ = json.Marshal(map[string]string{"assetId": assetId, "password": req.Password})

	if err := authorizeConnectTicket(user, assetId, c.getAuthzClientIp()); err != nil {
		c.ResponseError(err.Error())
		return
	}

	backend, err := getConnectTicketBackend()
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	ticket, err := backend.Issue(getUserId(user), assetId, req.Username, req.Password)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(connectTicketResponse{Ticket: ticket})
}
