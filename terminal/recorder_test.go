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

package terminal

import (
	"bytes"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/object"
)

var terminalTestDbNumber int

func setupTerminalTestDb(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("disableRecording", "true")
	terminalTestDbNumber++
	dsn := fmt.Sprintf("file:term_test_%d?mode=memory&cache=shared&_pragma=busy_timeout(10000)", terminalTestDbNumber)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	testAdapter := object.NewAdapter("sqlite", dsn)
	previous := object.SwapAdapter(testAdapter)
	t.Cleanup(func() {
		object.SwapAdapter(previous)
		if err := db.Close(); err != nil {
			t.Error(err)
		}
		if err := testAdapter.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

func newTerminalTestOptions(t *testing.T, name string) Options {
	t.Helper()
	session := &object.Session{Owner: "test", Name: name, Protocol: "SSH", Status: object.Connected}
	if added, err := object.AddSession(session); err != nil || !added {
		t.Fatalf("add terminal session: added=%t, err=%v", added, err)
	}
	return Options{
		SessionId: session.GetId(),
		User:      &casdoorsdk.User{Owner: session.Owner, Name: "user"},
		Asset:     &object.Asset{Owner: session.Owner, Name: "host", Protocol: session.Protocol},
		ClientIp:  "192.0.2.1",
	}
}

func addTerminalTestBlockRule(t *testing.T, owner string) *object.CommandRule {
	t.Helper()
	rule := &object.CommandRule{
		Owner:       owner,
		Name:        "block-reboot",
		DisplayName: "Block reboot",
		Pattern:     "^reboot$",
		Action:      object.CommandActionBlock,
		IsEnabled:   true,
	}
	if added, err := object.AddCommandRule(rule); err != nil || !added {
		t.Fatalf("add terminal rule: added=%t, err=%v", added, err)
	}
	return rule
}

func runTerminalTestCommand(opts Options, text string) ([][]byte, string) {
	var (
		r       *Recorder
		mu      sync.Mutex
		sent    [][]byte
		notices []byte
	)
	opts.Send = func(p []byte) error {
		mu.Lock()
		sent = append(sent, bytes.Clone(p))
		mu.Unlock()
		if !bytes.ContainsAny(p, "\r\x03") {
			r.Output(p)
		}
		return nil
	}
	opts.Notify = func(p []byte) {
		mu.Lock()
		notices = append(notices, p...)
		mu.Unlock()
	}
	r = NewRecorder(opts)
	r.Output([]byte("$ "))
	r.Input([]byte(text + "\r"))
	r.Close()
	<-r.done
	return sent, string(notices)
}

func requireTerminalBlock(t *testing.T, sent [][]byte, notices, reason string) {
	t.Helper()
	cancelled := false
	for _, p := range sent {
		if bytes.IndexByte(p, '\r') >= 0 {
			t.Errorf("blocked command sent a carriage return upstream: %q", p)
		}
		cancelled = cancelled || bytes.Equal(p, []byte{3})
	}
	if !cancelled {
		t.Errorf("blocked command did not send Ctrl+C: %q", sent)
	}
	if !strings.Contains(notices, "The command is blocked") || !strings.Contains(notices, reason) {
		t.Errorf("terminal notice = %q, want a block notice containing %q", notices, reason)
	}
}

func requireRecordedTerminalCommand(t *testing.T, sessionId, text, action, wantError string) *object.Command {
	t.Helper()
	commands, err := object.GetSessionCommands(sessionId)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 {
		t.Fatalf("got %d recorded commands, want 1", len(commands))
	}
	command := commands[0]
	if command.Text != text || command.Action != action {
		t.Errorf("recorded command: text=%q, action=%q; want %q and %q", command.Text, command.Action, text, action)
	}
	if wantError == "" {
		if command.Error != "" {
			t.Errorf("unexpected recorded error: %q", command.Error)
		}
	} else if !strings.Contains(command.Error, wantError) {
		t.Errorf("recorded error = %q, want it to contain %q", command.Error, wantError)
	}
	return command
}

func TestRecorderEnterFailClosedOnRuleQueryError(t *testing.T) {
	db := setupTerminalTestDb(t)
	opts := newTerminalTestOptions(t, "rule-query-error")
	if _, err := db.Exec("DROP TABLE command_rule"); err != nil {
		t.Fatal(err)
	}

	const (
		text      = "rm -rf /tmp/test"
		wantError = "failed to evaluate command rules"
	)
	sent, notices := runTerminalTestCommand(opts, text)
	requireTerminalBlock(t, sent, notices, wantError)
	requireRecordedTerminalCommand(t, opts.SessionId, text, object.CommandActionBlock, wantError)
}

func TestRecorderEnterFailClosedOnMalformedRules(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		wantError string
	}{
		{name: "invalid regex", pattern: "[unclosed", wantError: "invalid pattern in command rule"},
		{name: "empty pattern", pattern: "", wantError: "empty pattern in command rule"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := setupTerminalTestDb(t)
			opts := newTerminalTestOptions(t, "malformed-rule")
			rule := addTerminalTestBlockRule(t, opts.Asset.Owner)
			if _, err := db.Exec("UPDATE command_rule SET pattern = ? WHERE owner = ? AND name = ?", test.pattern, rule.Owner, rule.Name); err != nil {
				t.Fatal(err)
			}

			const text = "whoami"
			wantError := "failed to evaluate command rules: " + test.wantError
			sent, notices := runTerminalTestCommand(opts, text)
			requireTerminalBlock(t, sent, notices, wantError)
			requireRecordedTerminalCommand(t, opts.SessionId, text, object.CommandActionBlock, wantError)
		})
	}

	t.Run("valid block and allow", func(t *testing.T) {
		setupTerminalTestDb(t)
		blocked := newTerminalTestOptions(t, "blocked")
		rule := addTerminalTestBlockRule(t, blocked.Asset.Owner)
		sent, notices := runTerminalTestCommand(blocked, "reboot")
		requireTerminalBlock(t, sent, notices, rule.DisplayName)
		command := requireRecordedTerminalCommand(t, blocked.SessionId, "reboot", object.CommandActionBlock, "")
		if command.Rule != rule.GetId() {
			t.Errorf("recorded rule = %q, want %q", command.Rule, rule.GetId())
		}

		allowed := newTerminalTestOptions(t, "allowed")
		sent, notices = runTerminalTestCommand(allowed, "echo ok")
		if got := string(bytes.Join(sent, nil)); got != "echo ok\r" {
			t.Errorf("allowed command sent %q upstream, want %q", got, "echo ok\r")
		}
		if notices != "" {
			t.Errorf("allowed command got an unexpected notice: %q", notices)
		}
		requireRecordedTerminalCommand(t, allowed.SessionId, "echo ok", "", "")
	})
}

func TestCheckCommandFailClosedRecordsBlock(t *testing.T) {
	db := setupTerminalTestDb(t)
	opts := newTerminalTestOptions(t, "exec-rule-query-error")
	if _, err := db.Exec("DROP TABLE command_rule"); err != nil {
		t.Fatal(err)
	}

	const text = "whoami"
	rule, err := CheckCommand(opts.SessionId, opts.User, opts.Asset, opts.ClientIp, text)
	if rule != nil || err == nil {
		t.Fatalf("check command: rule=%v, err=%v; want nil rule and an error", rule, err)
	}
	requireRecordedTerminalCommand(t, opts.SessionId, text, object.CommandActionBlock, err.Error())
}

func TestCheckCommandWithoutUserOrAsset(t *testing.T) {
	setupTerminalTestDb(t)
	opts := newTerminalTestOptions(t, "exec-missing-identity")
	for _, test := range []struct {
		name  string
		user  *casdoorsdk.User
		asset *object.Asset
	}{
		{name: "user", asset: opts.Asset},
		{name: "asset", user: opts.User},
	} {
		rule, err := CheckCommand(opts.SessionId, test.user, test.asset, opts.ClientIp, "whoami")
		if rule != nil || err == nil {
			t.Errorf("check command without %s: rule=%v, err=%v; want nil rule and an error", test.name, rule, err)
		}
	}
	commands, err := object.GetSessionCommands(opts.SessionId)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range commands {
		if command.Action != object.CommandActionBlock || command.Error == "" {
			t.Errorf("recorded command = %+v, want a block with the error", command)
		}
	}
}
