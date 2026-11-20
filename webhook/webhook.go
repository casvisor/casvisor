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

// Package webhook sends event notifications as JSON to a configured HTTP endpoint.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/beego/beego/logs"
)

const (
	SignatureHeader = "X-Casvisor-Signature-256"
	queueSize       = 1000
	requestTimeout  = 5 * time.Second
)

type Event struct {
	Type         string         `json:"type"`
	Time         time.Time      `json:"time"`
	Organization string         `json:"organization,omitempty"`
	User         string         `json:"user,omitempty"`
	Summary      string         `json:"summary"`
	Data         map[string]any `json:"data,omitempty"`
}

type sender struct {
	url     string
	secret  string
	client  *http.Client
	ctx     context.Context
	cancel  context.CancelFunc
	queue   chan Event
	done    chan struct{}
	dropped atomic.Int64
}

var (
	mu      sync.Mutex
	current atomic.Pointer[sender]
)

// Enabled reports whether outbound webhook delivery is active.
func Enabled() bool {
	return current.Load() != nil
}

// Start enables outbound webhook delivery to rawUrl; an empty URL turns it off.
func Start(rawUrl, secret string) error {
	rawUrl = strings.TrimSpace(rawUrl)
	if rawUrl == "" {
		return nil
	}
	u, err := url.Parse(rawUrl)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return fmt.Errorf("invalid webhookUrl: scheme must be http:// or https:// with a host")
	}

	mu.Lock()
	defer mu.Unlock()
	if current.Load() != nil {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &sender{
		url:    rawUrl,
		secret: secret,
		client: &http.Client{Timeout: requestTimeout},
		ctx:    ctx,
		cancel: cancel,
		queue:  make(chan Event, queueSize),
		done:   make(chan struct{}),
	}
	current.Store(s)
	go s.loop()
	logs.Info("forwarding webhook events to %s://%s", u.Scheme, u.Host)
	return nil
}

func stop() {
	mu.Lock()
	defer mu.Unlock()
	if s := current.Swap(nil); s != nil {
		s.cancel()
		<-s.done
	}
}

// Send queues an event without waiting; when full the event is dropped and counted.
func Send(event Event) {
	s := current.Load()
	if s == nil {
		return
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	select {
	case s.queue <- event:
	default:
		s.dropped.Add(1)
	}
}

func (s *sender) loop() {
	defer close(s.done)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if n := s.dropped.Swap(0); n > 0 {
				logs.Warning("webhook: dropped %d notifications, the queue of %d was full", n, queueSize)
			}
		case event := <-s.queue:
			if err := s.deliver(event); err != nil && s.ctx.Err() == nil {
				logs.Warning("webhook: %s", err.Error())
			}
		}
	}
}

func (s *sender) deliver(event Event) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(s.ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Casvisor-Event", event.Type)
	if s.secret != "" {
		mac := hmac.New(sha256.New, []byte(s.secret))
		mac.Write(body)
		req.Header.Set(SignatureHeader, "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := s.client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) && urlErr.Err != nil {
			return fmt.Errorf("POST failed: %w", urlErr.Err)
		}
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("received HTTP status %d", resp.StatusCode)
	}
	return nil
}
