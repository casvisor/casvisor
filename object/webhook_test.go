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
	"maps"
	"testing"
	"time"
)

func TestLoginReviewEvent(t *testing.T) {
	created := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	reviewed := created.Add(time.Minute)
	for _, tt := range []struct {
		action, status, reviewer, comment string
		when                              time.Time
	}{
		{"created", LoginReviewPending, "", "", created},
		{"approved", LoginReviewApproved, "org/admin", "", reviewed},
		{"rejected", LoginReviewRejected, "org/admin", "unknown client", reviewed},
	} {
		t.Run(tt.action, func(t *testing.T) {
			review := &LoginReview{
				Owner:        "org",
				User:         "alice",
				Name:         "r1",
				Rule:         "office",
				Method:       LoginMethodSsh,
				ClientIp:     "192.0.2.1",
				CreatedTime:  created.Format(time.RFC3339),
				ReviewedTime: reviewed.Format(time.RFC3339),
				Status:       tt.status,
				Reviewer:     tt.reviewer,
				Comment:      tt.comment,
			}
			got := loginReviewEvent(tt.action, review)
			if got.Type != "login_review."+tt.action || !got.Time.Equal(tt.when) || got.Organization != "org" || got.User != "alice" || got.Summary != "Login review "+tt.action {
				t.Errorf("unexpected event: %+v", got)
			}
			want := map[string]any{"review": "r1", "status": tt.status, "rule": "office", "method": LoginMethodSsh, "clientIp": "192.0.2.1"}
			if tt.reviewer != "" {
				want["reviewer"] = tt.reviewer
			}
			if tt.comment != "" {
				want["comment"] = tt.comment
			}
			if !maps.Equal(got.Data, want) {
				t.Errorf("event data = %v, want %v", got.Data, want)
			}
		})
	}
}
