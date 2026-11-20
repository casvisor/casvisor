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
	"bytes"
	"database/sql"
	"encoding/csv"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/object"
)

var controllerTestDbNumber int

func setupControllerTestDb(t *testing.T) *sql.DB {
	t.Helper()
	controllerTestDbNumber++
	dsn := fmt.Sprintf("file:ctrl_db_test_%d?mode=memory&cache=shared&_pragma=busy_timeout(10000)", controllerTestDbNumber)
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

func newTestDatabaseSession(t *testing.T) *databaseSession {
	t.Helper()
	session := &object.Session{Owner: "test", Name: "sql-session", Protocol: object.ProtocolMysql, Status: object.Connected}
	if added, err := object.AddSession(session); err != nil || !added {
		t.Fatalf("add database session: added=%t, err=%v", added, err)
	}
	return &databaseSession{
		id:    session.GetId(),
		user:  &casdoorsdk.User{Owner: session.Owner, Name: "user"},
		asset: &object.Asset{Owner: session.Owner, Name: "db", Protocol: session.Protocol},
	}
}

func requireDatabaseResultBlocked(t *testing.T, result *object.SqlResult, statement, wantError string) {
	t.Helper()
	if result == nil || result.Action != object.CommandActionBlock || result.Statement != statement || !strings.Contains(result.Error, wantError) {
		t.Fatalf("result for %q = %+v, want a blocked result with %q", statement, result, wantError)
	}
}

func requireRecordedDatabaseBlock(t *testing.T, sessionId string, result *object.SqlResult, count int) {
	t.Helper()
	commands, err := object.GetSessionCommands(sessionId)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != count {
		t.Fatalf("got %d recorded commands, want %d", len(commands), count)
	}
	for _, command := range commands {
		if command.Action == object.CommandActionBlock && command.Error == result.Error && command.Text == result.Statement {
			return
		}
	}
	t.Fatalf("no blocked command recorded for result %+v", result)
}

func TestDatabaseSessionFailClosedOnRuleQueryError(t *testing.T) {
	db := setupControllerTestDb(t)
	s := newTestDatabaseSession(t)
	if _, err := db.Exec("DROP TABLE command_rule"); err != nil {
		t.Fatal(err)
	}

	const wantError = "failed to evaluate command rules"
	// There is no database client; a failure to block would try to use it.
	result := s.execute("SELECT 1")
	requireDatabaseResultBlocked(t, result, "SELECT 1", wantError)
	requireRecordedDatabaseBlock(t, s.id, result, 1)

	_, blocked := s.record("USE prod")
	requireDatabaseResultBlocked(t, blocked, "USE prod", wantError)
	requireRecordedDatabaseBlock(t, s.id, blocked, 2)
}

func TestDatabaseSessionFailClosedOnInvalidRuleRegex(t *testing.T) {
	db := setupControllerTestDb(t)
	s := newTestDatabaseSession(t)
	invalidRule := &object.CommandRule{
		Owner:     s.asset.Owner,
		Name:      "invalid",
		Pattern:   "^PLACEHOLDER$",
		Action:    object.CommandActionBlock,
		IsEnabled: true,
	}
	if added, err := object.AddCommandRule(invalidRule); err != nil || !added {
		t.Fatalf("add placeholder rule: added=%t, err=%v", added, err)
	}
	if _, err := db.Exec("UPDATE command_rule SET pattern = ? WHERE owner = ? AND name = ?", "[unclosed", invalidRule.Owner, invalidRule.Name); err != nil {
		t.Fatal(err)
	}
	result := s.execute("SELECT 1")
	requireDatabaseResultBlocked(t, result, "SELECT 1", "failed to evaluate command rules: invalid pattern in command rule")
	requireRecordedDatabaseBlock(t, s.id, result, 1)

	if _, err := db.Exec("UPDATE command_rule SET is_enabled = 0 WHERE owner = ? AND name = ?", invalidRule.Owner, invalidRule.Name); err != nil {
		t.Fatal(err)
	}
	rule := &object.CommandRule{
		Owner:       s.asset.Owner,
		Name:        "block-select",
		DisplayName: "Block SELECT",
		Pattern:     "^SELECT",
		Action:      object.CommandActionBlock,
		IsEnabled:   true,
	}
	if added, err := object.AddCommandRule(rule); err != nil || !added {
		t.Fatalf("add Block rule: added=%t, err=%v", added, err)
	}
	const statement = "/* comment */ SELECT 1"
	result = s.execute(statement)
	requireDatabaseResultBlocked(t, result, statement, "the statement is blocked by the rule: Block SELECT")
	if result.Rule != rule.DisplayName {
		t.Errorf("blocking rule=%q, want %q", result.Rule, rule.DisplayName)
	}
	requireRecordedDatabaseBlock(t, s.id, result, 2)
}

func TestWriteDatabaseResultCsvEscapesFormulas(t *testing.T) {
	cell := func(s string) *string { return &s }
	result := &object.SqlResult{
		Columns: []object.SqlColumn{{Name: "=header"}, {Name: "+col"}, {Name: "normal"}},
		Rows: [][]*string{
			{cell("=formula"), cell("+sum"), cell("-1")},
			{cell("@formula"), cell("\tformula"), cell("\rformula")},
			{nil, cell(""), cell("with, comma")},
			{cell(`"quoted"`), cell("'=already escaped"), cell("ordinary")},
		},
	}
	var buf bytes.Buffer
	if err := writeDatabaseResultCsv(&buf, result); err != nil {
		t.Fatal(err)
	}
	bom := []byte("\xEF\xBB\xBF")
	if !bytes.HasPrefix(buf.Bytes(), bom) {
		t.Fatalf("CSV has no UTF-8 BOM: %q", buf.Bytes())
	}
	got, err := csv.NewReader(bytes.NewReader(buf.Bytes()[len(bom):])).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"'=header", "'+col", "normal"},
		{"'=formula", "'+sum", "'-1"},
		{"'@formula", "'\tformula", "'\rformula"},
		{"", "", "with, comma"},
		{`"quoted"`, "'=already escaped", "ordinary"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CSV records=%q, want %q", got, want)
	}
}
