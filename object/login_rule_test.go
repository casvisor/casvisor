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
	"time"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"xorm.io/xorm"
)

func TestLoginRuleMatchTime(t *testing.T) {
	utc := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	// 2026-11-16 is a Monday
	tests := []struct {
		name string
		rule LoginRule
		now  string
		want bool
	}{
		{"no limits", LoginRule{}, "2026-11-16T03:00:00Z", true},
		{"inside working hours", LoginRule{StartTime: "09:00", EndTime: "18:00"}, "2026-11-16T09:00:00Z", true},
		{"end is exclusive", LoginRule{StartTime: "09:00", EndTime: "18:00"}, "2026-11-16T18:00:00Z", false},
		{"weekday only", LoginRule{Weekdays: []int{1, 2, 3, 4, 5}}, "2026-11-15T12:00:00Z", false},
		{"over midnight late", LoginRule{StartTime: "22:00", EndTime: "06:00", Weekdays: []int{1}}, "2026-11-16T23:00:00Z", true},
		{"over midnight early counts for the day before", LoginRule{StartTime: "22:00", EndTime: "06:00", Weekdays: []int{1}}, "2026-11-17T05:00:00Z", true},
		{"over midnight early on the wrong day", LoginRule{StartTime: "22:00", EndTime: "06:00", Weekdays: []int{1}}, "2026-11-16T05:00:00Z", false},
		{"over midnight daytime", LoginRule{StartTime: "22:00", EndTime: "06:00"}, "2026-11-16T12:00:00Z", false},
		{"time zone", LoginRule{StartTime: "09:00", EndTime: "18:00", TimeZone: "Asia/Shanghai"}, "2026-11-16T02:00:00Z", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rule.matchTime(utc(tt.now)); got != tt.want {
				t.Errorf("matchTime = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestCheckLoginRules(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	engine.SetMaxOpenConns(1)
	previous := adapter
	adapter = &Adapter{driverName: "sqlite", dataSourceName: ":memory:", engine: engine}
	t.Cleanup(func() {
		adapter = previous
		_ = engine.Close()
	})
	if err = engine.Sync2(new(LoginRule)); err != nil {
		t.Fatal(err)
	}
	rules := []*LoginRule{
		{Owner: "org1", Name: "office", Priority: 1, ClientIps: []string{"10.0.0.0/8"}, Action: LoginActionAllow, IsEnabled: true},
		{Owner: "org1", Name: "ops-review", Priority: 2, Groups: []string{"ops"}, Action: LoginActionReview, IsEnabled: true},
		{Owner: "org1", Name: "bob", Priority: 3, Users: []string{"bob"}, Action: LoginActionMfa, IsEnabled: true},
		{Owner: "org1", Name: "web-only", Priority: 4, Users: []string{"carol"}, Methods: []string{LoginMethodSsh}, Action: LoginActionReject, IsEnabled: true},
		{Owner: "org1", Name: "disabled", Priority: 5, Action: LoginActionMfa, IsEnabled: false},
		{Owner: "org1", Name: "deny-rest", Priority: 9, Users: []string{"*"}, Action: LoginActionReject, IsEnabled: true},
	}
	for _, rule := range rules {
		if _, err = engine.Insert(rule); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	tests := []struct {
		name     string
		user     *casdoorsdk.User
		ip       string
		method   string
		wantRule string
		want     string
	}{
		{"office network first", &casdoorsdk.User{Owner: "org1", Name: "alice", Groups: []string{"org1/ops"}}, "10.1.2.3", LoginMethodWeb, "office", LoginActionAllow},
		{"group review", &casdoorsdk.User{Owner: "org1", Name: "alice", Groups: []string{"org1/ops"}}, "192.0.2.1", LoginMethodWeb, "ops-review", LoginActionReview},
		{"named user", &casdoorsdk.User{Owner: "org1", Name: "bob"}, "192.0.2.1", LoginMethodWeb, "bob", LoginActionMfa},
		{"method filter skips", &casdoorsdk.User{Owner: "org1", Name: "carol"}, "192.0.2.1", LoginMethodWeb, "deny-rest", LoginActionReject},
		{"method filter matches", &casdoorsdk.User{Owner: "org1", Name: "carol"}, "192.0.2.1", LoginMethodSsh, "web-only", LoginActionReject},
		{"wildcard does not lock out admins", &casdoorsdk.User{Owner: "org1", Name: "admin", IsAdmin: true}, "192.0.2.1", LoginMethodWeb, "", LoginActionAllow},
		{"other organization", &casdoorsdk.User{Owner: "org2", Name: "dave"}, "192.0.2.1", LoginMethodWeb, "", LoginActionAllow},
		{"built-in", &casdoorsdk.User{Owner: "built-in", Name: "admin"}, "192.0.2.1", LoginMethodWeb, "", LoginActionAllow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := CheckLoginRules(tt.user, tt.ip, tt.method, now)
			if err != nil {
				t.Fatal(err)
			}
			if decision.RuleName() != tt.wantRule || decision.Action != tt.want {
				t.Errorf("CheckLoginRules = (%q, %s), want (%q, %s)", decision.RuleName(), decision.Action, tt.wantRule, tt.want)
			}
		})
	}
}
