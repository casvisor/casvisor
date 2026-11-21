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
	"fmt"
	"slices"
	"time"
)

// TimeWindow is a weekly window such as Monday to Friday from 09:00 to 18:00. Empty weekdays
// mean every day, and empty times mean the whole day.
type TimeWindow struct {
	Weekdays  []int
	StartTime string
	EndTime   string
	TimeZone  string
}

func parseClockTime(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, fmt.Errorf("invalid time: %s, it should be like 09:00", s)
	}
	return t.Hour()*60 + t.Minute(), nil
}

func (w TimeWindow) check() error {
	for _, weekday := range w.Weekdays {
		if weekday < 0 || weekday > 6 {
			return fmt.Errorf("invalid weekday: %d, it should be from 0 (Sunday) to 6 (Saturday)", weekday)
		}
	}

	if (w.StartTime == "") != (w.EndTime == "") {
		return fmt.Errorf("the start time and the end time should be both set or both empty")
	}
	for _, t := range []string{w.StartTime, w.EndTime} {
		if t == "" {
			continue
		}
		if _, err := parseClockTime(t); err != nil {
			return err
		}
	}

	if w.TimeZone != "" {
		if _, err := time.LoadLocation(w.TimeZone); err != nil {
			return fmt.Errorf("invalid time zone: %s", w.TimeZone)
		}
	}
	return nil
}

func (w TimeWindow) match(now time.Time) bool {
	if w.TimeZone != "" {
		if location, err := time.LoadLocation(w.TimeZone); err == nil {
			now = now.In(location)
		}
	}

	minute := now.Hour()*60 + now.Minute()
	weekday := int(now.Weekday())
	if w.StartTime != "" && w.EndTime != "" {
		start, err1 := parseClockTime(w.StartTime)
		end, err2 := parseClockTime(w.EndTime)
		if err1 != nil || err2 != nil {
			return false
		}
		switch {
		case start < end:
			if minute < start || minute >= end {
				return false
			}
		case start > end:
			// a window over midnight, such as 22:00 to 06:00, belongs to the day it starts on
			if minute < start && minute >= end {
				return false
			}
			if minute < end {
				weekday = (weekday + 6) % 7
			}
		}
	}

	return len(w.Weekdays) == 0 || slices.Contains(w.Weekdays, weekday)
}
