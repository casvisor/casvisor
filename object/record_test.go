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
	"testing"

	"xorm.io/xorm"
)

func useRecordTestDB(t *testing.T) *xorm.Engine {
	t.Helper()
	engine, err := xorm.NewEngine("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory SQLite: %v", err)
	}
	previousAdapter, previousLogPostOnly := adapter, logPostOnly
	t.Cleanup(func() {
		if err := engine.Close(); err != nil {
			t.Errorf("close in-memory SQLite: %v", err)
		}
		adapter, logPostOnly = previousAdapter, previousLogPostOnly
	})
	engine.SetMaxOpenConns(1)
	if err := engine.Sync2(new(Record)); err != nil {
		t.Fatalf("create record table: %v", err)
	}
	adapter = &Adapter{driverName: "sqlite", dataSourceName: ":memory:", engine: engine}
	logPostOnly = false
	return engine
}

func addTestRecord(t *testing.T, record *Record) {
	t.Helper()
	if affected, err := AddRecord(record); !affected || err != nil {
		t.Fatalf("AddRecord(%q) = (%v, %v), want (true, nil)", record.Name, affected, err)
	}
}

func assertStoredTestRecord(t *testing.T, record *Record) {
	t.Helper()
	got, err := GetRecord(record.getId())
	if err != nil || got == nil || *got != *record {
		t.Fatalf("GetRecord(%q) = (%+v, %v), want (%+v, nil)", record.getId(), got, err, record)
	}
}

func TestAddRecordSyncsTenant(t *testing.T) {
	useRecordTestDB(t)
	for _, tc := range []struct {
		name         string
		owner        string
		organization string
		want         string
	}{
		{"owner-only", "org-a", "", "org-a"},
		{"organization-only", "", "org-b", "org-b"},
		{"organization-takes-precedence", "org-a", "org-b", "org-b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := &Record{Owner: tc.owner, Organization: tc.organization, Name: tc.name, Method: "POST"}
			addTestRecord(t, record)
			if record.Owner != tc.want || record.Organization != tc.want {
				t.Fatalf("record tenant = (%q, %q), want (%q, %q)", record.Owner, record.Organization, tc.want, tc.want)
			}
			assertStoredTestRecord(t, record)
		})
	}
}

func TestGetRecordEnforcesOwnerAndName(t *testing.T) {
	useRecordTestDB(t)
	orgBRecord := &Record{Owner: "org-b", Name: "org-b-only"}
	orgAShared := &Record{Owner: "org-a", Name: "shared", Action: "org-a-action"}
	orgBShared := &Record{Owner: "org-b", Name: "shared", Action: "org-b-action"}
	records := []*Record{orgBRecord, orgAShared, orgBShared}
	for _, record := range records {
		addTestRecord(t, record)
	}
	for _, record := range records {
		assertStoredTestRecord(t, record)
	}

	for _, id := range []string{"org-a/" + orgBRecord.Name, "org-a/missing", "missing-slash", "", "/shared", "org-a/", "org-a/shared/extra"} {
		if record, err := GetRecord(id); record != nil || err != nil {
			t.Errorf("GetRecord(%q) = (%+v, %v), want (nil, nil)", id, record, err)
		}
	}
}

func TestAuditRecordIsImmutable(t *testing.T) {
	useRecordTestDB(t)
	record := &Record{Owner: "org-a", Name: "original", Action: "original-action"}
	addTestRecord(t, record)

	replacement := *record
	replacement.Action = "modified"
	if affected, err := UpdateRecord(record.getId(), &replacement); affected || err != ErrAuditRecordImmutable {
		t.Errorf("UpdateRecord() = (%v, %v), want (false, %v)", affected, err, ErrAuditRecordImmutable)
	}
	assertStoredTestRecord(t, record)

	if affected, err := DeleteRecord(record); affected || err != ErrAuditRecordImmutable {
		t.Errorf("DeleteRecord() = (%v, %v), want (false, %v)", affected, err, ErrAuditRecordImmutable)
	}
	assertStoredTestRecord(t, record)
}

func TestAddRecordDatabaseFailure(t *testing.T) {
	engine := useRecordTestDB(t)
	if _, err := engine.Exec("DROP TABLE record"); err != nil {
		t.Fatalf("drop record table: %v", err)
	}

	record := &Record{Owner: "org-a", Name: "entry", Method: "POST"}
	if affected, err := AddRecord(record); affected || err == nil {
		t.Errorf("AddRecord() = (%v, %v), want (false, non-nil error)", affected, err)
	}
	if got, err := GetRecord(record.getId()); got != nil || err == nil {
		t.Errorf("GetRecord() = (%+v, %v), want (nil, non-nil error)", got, err)
	}

	AddAuditRecord(&AuditEvent{Organization: "org-a", Method: "POST", Action: "test-audit"})
}

func TestAddRecordWithoutDatabaseAdapter(t *testing.T) {
	for _, tc := range []struct {
		name    string
		adapter *Adapter
	}{
		{"missing adapter", nil},
		{"missing engine", &Adapter{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previousAdapter := adapter
			t.Cleanup(func() { adapter = previousAdapter })
			adapter = tc.adapter

			if affected, err := AddRecord(&Record{Method: "POST"}); affected || err == nil || err.Error() != "database adapter is not initialized" {
				t.Errorf("AddRecord() = (%v, %v), want uninitialized adapter error", affected, err)
			}
			if record, err := GetRecord("org-a/entry"); record != nil || err == nil || err.Error() != "database adapter is not initialized" {
				t.Errorf("GetRecord() = (%+v, %v), want uninitialized adapter error", record, err)
			}
			for _, id := range []string{"   /shared", "org-a/   "} {
				if record, err := GetRecord(id); record != nil || err != nil {
					t.Errorf("GetRecord(%q) = (%+v, %v), want (nil, nil)", id, record, err)
				}
			}
		})
	}
}

func TestAddRecordRejectsNil(t *testing.T) {
	if affected, err := AddRecord(nil); affected || err == nil || err.Error() != "record is nil" {
		t.Errorf("AddRecord(nil) = (%v, %v), want nil record error", affected, err)
	}
}

func TestAddRecordSkipsExcludedEvents(t *testing.T) {
	previousAdapter, previousLogPostOnly := adapter, logPostOnly
	t.Cleanup(func() { adapter, logPostOnly = previousAdapter, previousLogPostOnly })
	adapter, logPostOnly = nil, true
	for _, record := range []*Record{{Method: "GET"}, {Method: "POST", Action: "add-record"}} {
		if affected, err := AddRecord(record); affected || err != nil {
			t.Errorf("AddRecord(%s %s) = (%v, %v), want skipped record", record.Method, record.Action, affected, err)
		}
	}
}
