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
	"slices"
	"testing"

	"xorm.io/xorm"
)

func TestGetSessions(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open session test database: %v", err)
	}
	engine.SetMaxOpenConns(1)
	previous := adapter
	t.Cleanup(func() {
		adapter = previous
		if err := engine.Close(); err != nil {
			t.Errorf("close session test database: %v", err)
		}
	})
	if err := engine.Sync2(new(Session)); err != nil {
		t.Fatalf("create session test table: %v", err)
	}
	adapter = &Adapter{driverName: "sqlite", dataSourceName: ":memory:", engine: engine}

	for _, session := range []*Session{
		{Owner: "org1", Name: "s1", CreatedTime: "2024-01-01T00:00:00+08:00"},
		{Owner: "org1", Name: "s2", CreatedTime: "2024-01-03T00:00:00+08:00"},
		{Owner: "org1", Name: "s3", CreatedTime: "2024-01-02T00:00:00+08:00"},
		{Owner: "org2", Name: "s4", CreatedTime: "2024-01-04T00:00:00+08:00"},
	} {
		if _, err := AddSession(session); err != nil {
			t.Fatalf("add session %s: %v", session.Name, err)
		}
	}

	tests := []struct {
		owner string
		want  []string
	}{
		{"org1", []string{"s2", "s3", "s1"}},
		{"org2", []string{"s4"}},
		{"org3", []string{}},
		{"", []string{"s4", "s2", "s3", "s1"}},
	}
	for _, tt := range tests {
		sessions, err := GetSessions(tt.owner)
		if err != nil {
			t.Fatalf("GetSessions(%q): %v", tt.owner, err)
		}
		names := []string{}
		for _, session := range sessions {
			names = append(names, session.Name)
		}
		if !slices.Equal(names, tt.want) {
			t.Errorf("GetSessions(%q) = %v, want %v", tt.owner, names, tt.want)
		}
	}
}
