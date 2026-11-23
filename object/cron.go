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
	"strconv"
	"strings"
	"time"
)

// cronSchedule is a crontab line of five fields: minute, hour, day of month, month and day of week.
type cronSchedule struct {
	minutes  [60]bool
	hours    [24]bool
	days     [32]bool
	months   [13]bool
	weekdays [7]bool
	// when both days and weekdays are restricted, either one matching is enough, as in cron
	dayStar     bool
	weekdayStar bool
}

var cronMacros = map[string]string{
	"@hourly":   "0 * * * *",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@weekly":   "0 0 * * 0",
	"@monthly":  "0 0 1 * *",
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
}

var cronMonthNames = map[string]int{"JAN": 1, "FEB": 2, "MAR": 3, "APR": 4, "MAY": 5, "JUN": 6, "JUL": 7, "AUG": 8, "SEP": 9, "OCT": 10, "NOV": 11, "DEC": 12}

var cronWeekdayNames = map[string]int{"SUN": 0, "MON": 1, "TUE": 2, "WED": 3, "THU": 4, "FRI": 5, "SAT": 6}

func parseCronValue(s string, names map[string]int) (int, error) {
	if n, ok := names[strings.ToUpper(s)]; ok {
		return n, nil
	}
	return strconv.Atoi(s)
}

// parseCronField marks the values of one field, such as "*/15", "1-5", "0,30" or "MON-FRI".
func parseCronField(field string, low int, high int, names map[string]int, set func(int)) error {
	for _, part := range strings.Split(field, ",") {
		step := 1
		if i := strings.Index(part, "/"); i >= 0 {
			var err error
			step, err = strconv.Atoi(part[i+1:])
			if err != nil || step <= 0 {
				return fmt.Errorf("invalid step in: %s", part)
			}
			part = part[:i]
		}

		start, end := low, high
		if part != "*" {
			bounds := strings.SplitN(part, "-", 2)
			var err error
			start, err = parseCronValue(bounds[0], names)
			if err != nil {
				return fmt.Errorf("invalid value: %s", bounds[0])
			}
			end = start
			if len(bounds) == 2 {
				end, err = parseCronValue(bounds[1], names)
				if err != nil {
					return fmt.Errorf("invalid value: %s", bounds[1])
				}
			} else if step > 1 {
				end = high
			}
		}
		if start < low || end > high || start > end {
			return fmt.Errorf("%s is out of the range %d-%d", part, low, high)
		}
		for v := start; v <= end; v += step {
			set(v)
		}
	}
	return nil
}

func parseCron(expr string) (*cronSchedule, error) {
	expr = strings.TrimSpace(expr)
	if macro, ok := cronMacros[strings.ToLower(expr)]; ok {
		expr = macro
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("a cron expression has five fields: minute, hour, day of month, month and day of week, such as \"0 3 * * *\"")
	}

	c := &cronSchedule{dayStar: fields[2] == "*", weekdayStar: fields[4] == "*"}
	steps := []struct {
		name      string
		low, high int
		names     map[string]int
		set       func(int)
	}{
		{"minute", 0, 59, nil, func(v int) { c.minutes[v] = true }},
		{"hour", 0, 23, nil, func(v int) { c.hours[v] = true }},
		{"day of month", 1, 31, nil, func(v int) { c.days[v] = true }},
		{"month", 1, 12, cronMonthNames, func(v int) { c.months[v] = true }},
		// 7 is Sunday too
		{"day of week", 0, 7, cronWeekdayNames, func(v int) { c.weekdays[v%7] = true }},
	}
	for i, step := range steps {
		err := parseCronField(fields[i], step.low, step.high, step.names, step.set)
		if err != nil {
			return nil, fmt.Errorf("the %s field: %s", step.name, err.Error())
		}
	}
	return c, nil
}

func (c *cronSchedule) matches(t time.Time) bool {
	return c.minutes[t.Minute()] && c.hours[t.Hour()] && c.months[int(t.Month())] && c.matchesDay(t)
}

// next returns the first minute after the time that matches, or the zero time when none does
// within the next four years.
func (c *cronSchedule) next(after time.Time) time.Time {
	t := after.Truncate(time.Minute).Add(time.Minute)
	end := after.AddDate(4, 0, 1)
	for t.Before(end) {
		if !c.months[int(t.Month())] {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
			continue
		}
		if !c.matchesDay(t) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
			continue
		}
		if !c.hours[t.Hour()] {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
			continue
		}
		if c.minutes[t.Minute()] {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}
}

func (c *cronSchedule) matchesDay(t time.Time) bool {
	day, weekday := c.days[t.Day()], c.weekdays[int(t.Weekday())]
	switch {
	case c.dayStar && c.weekdayStar:
		return true
	case c.dayStar:
		return weekday
	case c.weekdayStar:
		return day
	}
	return day || weekday
}
