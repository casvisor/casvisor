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

package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func startServer(t *testing.T, secret string, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Cleanup(stop)
	if err := Start(" "+srv.URL+" ", secret); err != nil {
		t.Fatal(err)
	}
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for webhook")
		var zero T
		return zero
	}
}

func TestDisabled(t *testing.T) {
	t.Cleanup(stop)
	for _, rawUrl := range []string{"", "  "} {
		if err := Start(rawUrl, ""); err != nil {
			t.Fatal(err)
		}
		if Enabled() {
			t.Fatal("empty URL started a sender")
		}
	}
	Send(Event{Type: "disabled"})
}

func TestInvalidURL(t *testing.T) {
	t.Cleanup(stop)
	for _, rawUrl := range []string{"127.0.0.1/hook", "ftp://127.0.0.1", "http://", "https://:80", "http://%zz"} {
		if err := Start(rawUrl, ""); err == nil || Enabled() {
			t.Fatalf("Start(%q) = %v, enabled = %t", rawUrl, err, Enabled())
		}
	}
	hit := make(chan struct{}, 1)
	startServer(t, "", func(w http.ResponseWriter, r *http.Request) {
		hit <- struct{}{}
	})
	Send(Event{Type: "valid"})
	receive(t, hit)
}

func TestDelivery(t *testing.T) {
	for _, test := range []struct {
		name   string
		secret string
		when   time.Time
	}{
		{name: "unsigned"},
		{name: "signed", secret: "shared secret", when: time.Unix(123, 456).UTC()},
	} {
		t.Run(test.name, func(t *testing.T) {
			want := Event{
				Type:         "command.blocked",
				Time:         test.when,
				Organization: "example",
				User:         "operator",
				Summary:      "Command blocked",
				Data:         map[string]any{"command": "echo <hello>\n"},
			}
			done := make(chan struct{}, 1)
			startServer(t, test.secret, func(w http.ResponseWriter, r *http.Request) {
				defer func() {
					done <- struct{}{}
				}()
				if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("X-Casvisor-Event") != want.Type {
					t.Errorf("unexpected request: %s, headers %v", r.Method, r.Header)
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				signatures := r.Header.Values(SignatureHeader)
				if test.secret == "" {
					if len(signatures) != 0 {
						t.Errorf("unexpected signature: %v", signatures)
					}
				} else {
					mac := hmac.New(sha256.New, []byte(test.secret))
					mac.Write(body)
					if len(signatures) != 1 || signatures[0] != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
						t.Errorf("incorrect signature: %v", signatures)
					}
				}
				var got Event
				if err := json.Unmarshal(body, &got); err != nil {
					t.Error(err)
					return
				}
				if want.Time.IsZero() {
					if got.Time.IsZero() || got.Time.Location() != time.UTC {
						t.Errorf("expected a default UTC timestamp, got %v", got.Time)
					}
				} else if !got.Time.Equal(want.Time) {
					t.Errorf("decoded time = %v, want %v", got.Time, want.Time)
				}
				got.Time = time.Time{}
				wantCopy := want
				wantCopy.Time = time.Time{}
				if !reflect.DeepEqual(got, wantCopy) {
					t.Errorf("decoded event = %#v, want %#v", got, wantCopy)
				}
			})
			Send(want)
			receive(t, done)
		})
	}
}

func TestDeliveryAfterFailure(t *testing.T) {
	events := make(chan string, 2)
	startServer(t, "", func(w http.ResponseWriter, r *http.Request) {
		eventType := r.Header.Get("X-Casvisor-Event")
		if eventType == "first" {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		events <- eventType
	})
	Send(Event{Type: "first"})
	Send(Event{Type: "second"})
	for _, want := range []string{"first", "second"} {
		if got := receive(t, events); got != want {
			t.Fatalf("received %q, want %q", got, want)
		}
	}
}

func TestQueueDrop(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	startServer(t, "", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Casvisor-Event") != "hold" {
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	t.Cleanup(func() {
		close(release)
	})

	s := current.Load()
	Send(Event{Type: "hold"})
	receive(t, started)

	const overflow = 5
	for range queueSize + overflow {
		Send(Event{Type: "queued"})
	}
	if got := s.dropped.Load(); got != overflow {
		t.Fatalf("dropped %d events, want %d", got, overflow)
	}

	stop()
	if Enabled() {
		t.Fatal("sender is still enabled")
	}
}
