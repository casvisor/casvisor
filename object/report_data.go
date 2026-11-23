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
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	ReportSectionLogin    = "login"
	ReportSectionSession  = "session"
	ReportSectionCommand  = "command"
	ReportSectionFile     = "file"
	ReportSectionApproval = "approval"
	ReportSectionAccount  = "account"
	ReportSectionAsset    = "asset"
)

var ReportSections = []string{ReportSectionLogin, ReportSectionSession, ReportSectionCommand, ReportSectionFile, ReportSectionApproval, ReportSectionAccount, ReportSectionAsset}

const (
	ReportPeriodToday      = "today"
	ReportPeriodYesterday  = "yesterday"
	ReportPeriodThisWeek   = "this-week"
	ReportPeriodLastWeek   = "last-week"
	ReportPeriodThisMonth  = "this-month"
	ReportPeriodLastMonth  = "last-month"
	ReportPeriodLast7Days  = "last-7-days"
	ReportPeriodLast30Days = "last-30-days"
	ReportPeriodCustom     = "custom"
)

var reportPeriods = []string{ReportPeriodToday, ReportPeriodYesterday, ReportPeriodThisWeek, ReportPeriodLastWeek, ReportPeriodThisMonth, ReportPeriodLastMonth, ReportPeriodLast7Days, ReportPeriodLast30Days}

const (
	reportTopCount   = 20
	reportEventCount = 200
	maxReportDays    = 366
)

var reportFileActions = []string{"sftp-upload", "sftp-download", "file-upload", "file-download", "sql-export"}

// ReportOptions is what a report covers: the organization, the time and the working hours, outside
// which sign-ins are counted as off-hours.
type ReportOptions struct {
	Owner         string
	Period        string
	StartDate     string
	EndDate       string
	TimeZone      string
	Weekdays      []int
	WorkStartTime string
	WorkEndTime   string
	Sections      []string
	Language      string
}

type ReportItem struct {
	Name      string `json:"name"`
	Count     int    `json:"count"`
	Failed    int    `json:"failed,omitempty"`
	Blocked   int    `json:"blocked,omitempty"`
	Uploads   int    `json:"uploads,omitempty"`
	Downloads int    `json:"downloads,omitempty"`
	Seconds   int64  `json:"seconds,omitempty"`
}

type ReportEvent struct {
	Time     string `json:"time"`
	User     string `json:"user"`
	ClientIp string `json:"clientIp,omitempty"`
	Asset    string `json:"asset,omitempty"`
	Action   string `json:"action"`
	Detail   string `json:"detail,omitempty"`
	Seconds  int64  `json:"seconds,omitempty"`
}

type ReportLogin struct {
	Succeeded   int            `json:"succeeded"`
	Failed      int            `json:"failed"`
	OffHours    int            `json:"offHours"`
	Rejected    int            `json:"rejected"`
	Reviewed    int            `json:"reviewed"`
	MfaRequired int            `json:"mfaRequired"`
	Users       []*ReportItem  `json:"users"`
	ClientIps   []*ReportItem  `json:"clientIps"`
	Events      []*ReportEvent `json:"events"`
}

type ReportSession struct {
	Total      int            `json:"total"`
	Active     int            `json:"active"`
	Users      int            `json:"users"`
	Assets     int            `json:"assets"`
	Seconds    int64          `json:"seconds"`
	ByUser     []*ReportItem  `json:"byUser"`
	ByAsset    []*ReportItem  `json:"byAsset"`
	ByProtocol []*ReportItem  `json:"byProtocol"`
	Longest    []*ReportEvent `json:"longest"`
}

type ReportCommand struct {
	Total    int            `json:"total"`
	Blocked  int            `json:"blocked"`
	Alerted  int            `json:"alerted"`
	Reviewed int            `json:"reviewed"`
	Rules    []*ReportItem  `json:"rules"`
	ByUser   []*ReportItem  `json:"byUser"`
	Events   []*ReportEvent `json:"events"`
}

type ReportFile struct {
	Uploads   int            `json:"uploads"`
	Downloads int            `json:"downloads"`
	ByUser    []*ReportItem  `json:"byUser"`
	ByAsset   []*ReportItem  `json:"byAsset"`
	Events    []*ReportEvent `json:"events"`
}

type ReportReviews struct {
	Total     int `json:"total"`
	Pending   int `json:"pending"`
	Approved  int `json:"approved"`
	Rejected  int `json:"rejected"`
	Cancelled int `json:"cancelled"`
	Expired   int `json:"expired"`
	Revoked   int `json:"revoked"`
}

type ReportApproval struct {
	AccessRequests *ReportReviews `json:"accessRequests"`
	LoginReviews   *ReportReviews `json:"loginReviews"`
	CommandReviews *ReportReviews `json:"commandReviews"`
	Requesters     []*ReportItem  `json:"requesters"`
	Reviewers      []*ReportItem  `json:"reviewers"`
	Events         []*ReportEvent `json:"events"`
}

type ReportAccount struct {
	Rotated       int            `json:"rotated"`
	RotateFailed  int            `json:"rotateFailed"`
	Pushed        int            `json:"pushed"`
	PushFailed    int            `json:"pushFailed"`
	NewRisks      int            `json:"newRisks"`
	ResolvedRisks int            `json:"resolvedRisks"`
	OpenRisks     int            `json:"openRisks"`
	IgnoredRisks  int            `json:"ignoredRisks"`
	RiskKinds     []*ReportItem  `json:"riskKinds"`
	Events        []*ReportEvent `json:"events"`
}

type ReportAsset struct {
	Total              int            `json:"total"`
	Added              int            `json:"added"`
	Deleted            int            `json:"deleted"`
	ByProtocol         []*ReportItem  `json:"byProtocol"`
	Permissions        int            `json:"permissions"`
	PermissionsAdded   int            `json:"permissionsAdded"`
	PermissionsUpdated int            `json:"permissionsUpdated"`
	PermissionsDeleted int            `json:"permissionsDeleted"`
	Events             []*ReportEvent `json:"events"`
	PermissionEvents   []*ReportEvent `json:"permissionEvents"`
}

type ReportData struct {
	Owner         string   `json:"owner"`
	Period        string   `json:"period"`
	StartTime     string   `json:"startTime"`
	EndTime       string   `json:"endTime"`
	TimeZone      string   `json:"timeZone"`
	WorkingHours  string   `json:"workingHours"`
	Weekdays      []int    `json:"weekdays"`
	WorkStartTime string   `json:"workStartTime"`
	WorkEndTime   string   `json:"workEndTime"`
	GeneratedTime string   `json:"generatedTime"`
	Sections      []string `json:"sections"`

	Login    *ReportLogin    `json:"login,omitempty"`
	Session  *ReportSession  `json:"session,omitempty"`
	Command  *ReportCommand  `json:"command,omitempty"`
	File     *ReportFile     `json:"file,omitempty"`
	Approval *ReportApproval `json:"approval,omitempty"`
	Account  *ReportAccount  `json:"account,omitempty"`
	Asset    *ReportAsset    `json:"asset,omitempty"`
}

func getReportLocation(timeZone string) (*time.Location, error) {
	if timeZone == "" {
		return time.Local, nil
	}
	location, err := time.LoadLocation(timeZone)
	if err != nil {
		return nil, fmt.Errorf("invalid time zone: %s", timeZone)
	}
	return location, nil
}

// GetReportRange is the start and the end, exclusive, of the period at the time now.
func GetReportRange(period string, startDate string, endDate string, now time.Time) (time.Time, time.Time, error) {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	monday := day.AddDate(0, 0, -(int(day.Weekday())+6)%7)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	switch period {
	case ReportPeriodToday:
		return day, day.AddDate(0, 0, 1), nil
	case ReportPeriodYesterday:
		return day.AddDate(0, 0, -1), day, nil
	case ReportPeriodThisWeek, "":
		return monday, monday.AddDate(0, 0, 7), nil
	case ReportPeriodLastWeek:
		return monday.AddDate(0, 0, -7), monday, nil
	case ReportPeriodThisMonth:
		return month, month.AddDate(0, 1, 0), nil
	case ReportPeriodLastMonth:
		return month.AddDate(0, -1, 0), month, nil
	case ReportPeriodLast7Days:
		return day.AddDate(0, 0, -6), day.AddDate(0, 0, 1), nil
	case ReportPeriodLast30Days:
		return day.AddDate(0, 0, -29), day.AddDate(0, 0, 1), nil
	case ReportPeriodCustom:
		start, err := time.ParseInLocation("2006-01-02", startDate, now.Location())
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid start date: %s, it should be like 2026-11-01", startDate)
		}
		end, err := time.ParseInLocation("2006-01-02", endDate, now.Location())
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end date: %s, it should be like 2026-11-30", endDate)
		}
		end = end.AddDate(0, 0, 1)
		if !start.Before(end) {
			return time.Time{}, time.Time{}, fmt.Errorf("the start date should be before the end date")
		}
		if end.Sub(start) > maxReportDays*24*time.Hour {
			return time.Time{}, time.Time{}, fmt.Errorf("a report can cover %d days at most", maxReportDays)
		}
		return start, end, nil
	}
	return time.Time{}, time.Time{}, fmt.Errorf("unknown period: %s, it should be one of: %s, %s", period, strings.Join(reportPeriods, ", "), ReportPeriodCustom)
}

func (options *ReportOptions) getWorkingHours() TimeWindow {
	window := TimeWindow{Weekdays: options.Weekdays, StartTime: options.WorkStartTime, EndTime: options.WorkEndTime, TimeZone: options.TimeZone}
	if len(window.Weekdays) == 0 {
		window.Weekdays = []int{1, 2, 3, 4, 5}
	}
	if window.StartTime == "" && window.EndTime == "" {
		window.StartTime, window.EndTime = "09:00", "18:00"
	}
	return window
}

func (options *ReportOptions) check() error {
	if options.Period != ReportPeriodCustom && !slices.Contains(reportPeriods, options.Period) {
		return fmt.Errorf("unknown period: %s, it should be one of: %s", options.Period, strings.Join(reportPeriods, ", "))
	}
	for _, section := range options.Sections {
		if !slices.Contains(ReportSections, section) {
			return fmt.Errorf("unknown section: %s, it should be one of: %s", section, strings.Join(ReportSections, ", "))
		}
	}
	return options.getWorkingHours().check()
}

func (options *ReportOptions) getSections() []string {
	res := []string{}
	for _, section := range ReportSections {
		if len(options.Sections) == 0 || slices.Contains(options.Sections, section) {
			res = append(res, section)
		}
	}
	return res
}

type reportCounter map[string]*ReportItem

func (counter reportCounter) get(name string) *ReportItem {
	item, ok := counter[name]
	if !ok {
		item = &ReportItem{Name: name}
		counter[name] = item
	}
	return item
}

// top is the items with the highest key, reportTopCount at most.
func (counter reportCounter) top(key func(item *ReportItem) int64) []*ReportItem {
	res := []*ReportItem{}
	for _, item := range counter {
		res = append(res, item)
	}
	sort.Slice(res, func(i, j int) bool {
		if key(res[i]) != key(res[j]) {
			return key(res[i]) > key(res[j])
		}
		return res[i].Name < res[j].Name
	})
	if len(res) > reportTopCount {
		res = res[:reportTopCount]
	}
	return res
}

func byCount(item *ReportItem) int64 {
	return int64(item.Count)
}

func addReportEvent(events []*ReportEvent, event *ReportEvent) []*ReportEvent {
	if len(events) >= reportEventCount {
		return events
	}
	return append(events, event)
}

type reportContext struct {
	options  *ReportOptions
	location *time.Location
	start    time.Time
	end      time.Time
}

func (ctx *reportContext) parseTime(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil || t.Before(ctx.start) || !t.Before(ctx.end) {
		return time.Time{}, false
	}
	return t, true
}

func (ctx *reportContext) formatTime(t time.Time) string {
	return t.In(ctx.location).Format(time.RFC3339)
}

// getTimeBounds are the bounds of a query on a time column, wider than the report by a day, as the
// times are kept as text with the offset of the server that wrote them; parseTime then picks the rows.
func (ctx *reportContext) getTimeBounds() (string, string) {
	return ctx.start.Add(-24 * time.Hour).In(time.Local).Format(time.RFC3339), ctx.end.Add(24 * time.Hour).In(time.Local).Format(time.RFC3339)
}

func (ctx *reportContext) iterateRecords(owners []string, actions []string, fn func(record *Record, t time.Time)) error {
	lo, hi := ctx.getTimeBounds()
	session := adapter.engine.In("owner", owners).In("action", actions).Where("created_time >= ? and created_time < ?", lo, hi).Asc("id")
	return session.Iterate(&Record{}, func(i int, bean interface{}) error {
		record := bean.(*Record)
		if t, ok := ctx.parseTime(record.CreatedTime); ok {
			fn(record, t)
		}
		return nil
	})
}

func isRecordOk(record *Record) bool {
	return strings.Contains(record.Response, `"status":"ok"`)
}

// getRecordObject is the owner and the name of what an API call changed, from its body or its id.
func getRecordObject(record *Record) (string, string) {
	var obj struct {
		Owner string `json:"owner"`
		Name  string `json:"name"`
	}
	if json.Unmarshal([]byte(record.Object), &obj) == nil && obj.Name != "" {
		return obj.Owner, obj.Name
	}
	if u, err := url.Parse(record.RequestUri); err == nil {
		if id := u.Query().Get("id"); id != "" {
			owner, name, _ := strings.Cut(id, "/")
			return owner, name
		}
	}
	return "", ""
}

// iterateChangeRecords goes through the API calls that succeeded on the organization, also those made
// by the administrators of built-in, whose records are of built-in.
func (ctx *reportContext) iterateChangeRecords(actions []string, fn func(record *Record, t time.Time, name string)) error {
	owner := ctx.options.Owner
	return ctx.iterateRecords([]string{owner, "built-in"}, actions, func(record *Record, t time.Time) {
		if !isRecordOk(record) {
			return
		}
		objOwner, name := getRecordObject(record)
		if objOwner == owner || (objOwner == "" && record.Owner == owner) {
			fn(record, t, name)
		}
	})
}

// short drops the organization of the report from an id such as "org/alice".
func (ctx *reportContext) short(id string) string {
	return strings.TrimPrefix(id, ctx.options.Owner+"/")
}

func getIdName(id string) string {
	_, name, ok := strings.Cut(id, "/")
	if !ok {
		return id
	}
	return name
}

func getRecordUser(record *Record) string {
	if record.User == "" {
		return record.Owner
	}
	return record.User
}

func GetReportData(options *ReportOptions, now time.Time) (*ReportData, error) {
	err := options.check()
	if err != nil {
		return nil, err
	}
	location, err := getReportLocation(options.TimeZone)
	if err != nil {
		return nil, err
	}
	start, end, err := GetReportRange(options.Period, options.StartDate, options.EndDate, now.In(location))
	if err != nil {
		return nil, err
	}

	ctx := &reportContext{options: options, location: location, start: start, end: end}
	window := options.getWorkingHours()
	data := &ReportData{
		Owner:         options.Owner,
		Period:        options.Period,
		StartTime:     start.Format(time.RFC3339),
		EndTime:       end.Format(time.RFC3339),
		TimeZone:      getTimeZoneName(location, now),
		WorkingHours:  getWorkingHoursText(window),
		Weekdays:      window.Weekdays,
		WorkStartTime: window.StartTime,
		WorkEndTime:   window.EndTime,
		GeneratedTime: now.In(location).Format(time.RFC3339),
		Sections:      options.getSections(),
	}

	for _, section := range data.Sections {
		switch section {
		case ReportSectionLogin:
			data.Login, err = ctx.getLogin(window)
		case ReportSectionSession:
			data.Session, err = ctx.getSession(now)
		case ReportSectionCommand:
			data.Command, err = ctx.getCommand()
		case ReportSectionFile:
			data.File, err = ctx.getFile()
		case ReportSectionApproval:
			data.Approval, err = ctx.getApproval()
		case ReportSectionAccount:
			data.Account, err = ctx.getAccount()
		case ReportSectionAsset:
			data.Asset, err = ctx.getAsset()
		}
		if err != nil {
			return nil, err
		}
	}
	return data, nil
}

func getTimeZoneName(location *time.Location, now time.Time) string {
	if location != time.Local {
		return location.String()
	}
	return "UTC" + now.In(location).Format("-07:00")
}

func getWorkingHoursText(window TimeWindow) string {
	names := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	days := []string{}
	for _, weekday := range []int{1, 2, 3, 4, 5, 6, 0} {
		if slices.Contains(window.Weekdays, weekday) {
			days = append(days, names[weekday])
		}
	}
	return fmt.Sprintf("%s %s-%s", strings.Join(days, ","), window.StartTime, window.EndTime)
}

func (ctx *reportContext) getLogin(window TimeWindow) (*ReportLogin, error) {
	res := &ReportLogin{Events: []*ReportEvent{}}
	users, ips := reportCounter{}, reportCounter{}
	actions := []string{"signin", "login-reject", "login-review", "login-mfa-setup"}
	err := ctx.iterateRecords([]string{ctx.options.Owner}, actions, func(record *Record, t time.Time) {
		user := getRecordUser(record)
		event := &ReportEvent{Time: ctx.formatTime(t), User: user, ClientIp: record.ClientIp, Action: record.Action, Detail: record.RequestUri}
		switch record.Action {
		case "signin":
			event.Detail = ""
			if isRecordOk(record) {
				res.Succeeded++
				users.get(user).Count++
				ips.get(record.ClientIp).Count++
				if !window.match(t) {
					res.OffHours++
					event.Action = "signin-off-hours"
					res.Events = addReportEvent(res.Events, event)
				}
			} else {
				res.Failed++
				users.get(user).Failed++
				ips.get(record.ClientIp).Failed++
				event.Action = "signin-failed"
				res.Events = addReportEvent(res.Events, event)
			}
		case "login-reject":
			res.Rejected++
			res.Events = addReportEvent(res.Events, event)
		case "login-review":
			res.Reviewed++
			res.Events = addReportEvent(res.Events, event)
		case "login-mfa-setup":
			res.MfaRequired++
		}
	})
	if err != nil {
		return nil, err
	}
	total := func(item *ReportItem) int64 { return int64(item.Count + item.Failed) }
	res.Users, res.ClientIps = users.top(total), ips.top(total)
	return res, nil
}

func (ctx *reportContext) getSession(now time.Time) (*ReportSession, error) {
	res := &ReportSession{Longest: []*ReportEvent{}}
	users, assets, protocols := reportCounter{}, reportCounter{}, reportCounter{}
	sessions := []*ReportEvent{}
	lo, hi := ctx.getTimeBounds()
	err := adapter.engine.Where("owner = ? and created_time >= ? and created_time < ?", ctx.options.Owner, lo, hi).Iterate(&Session{}, func(i int, bean interface{}) error {
		session := bean.(*Session)
		startTime := session.StartTime
		if startTime == "" {
			startTime = session.CreatedTime
		}
		start, ok := ctx.parseTime(startTime)
		if !ok {
			return nil
		}

		var seconds int64
		if end, err := time.Parse(time.RFC3339, session.EndTime); err == nil {
			seconds = int64(end.Sub(start).Seconds())
		} else if session.Status == "connected" {
			seconds = int64(now.Sub(start).Seconds())
			res.Active++
		}
		seconds = max(seconds, 0)

		res.Total++
		res.Seconds += seconds
		user, asset := ctx.short(session.Creator), ctx.short(session.Asset)
		for _, item := range []*ReportItem{users.get(user), assets.get(asset), protocols.get(session.Protocol)} {
			item.Count++
			item.Seconds += seconds
		}
		sessions = append(sessions, &ReportEvent{
			Time: ctx.formatTime(start), User: user, ClientIp: session.ClientIp, Asset: asset,
			Action: session.Protocol, Detail: session.Name, Seconds: seconds,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	res.Users, res.Assets = len(users), len(assets)
	res.ByUser, res.ByAsset, res.ByProtocol = users.top(byCount), assets.top(byCount), protocols.top(byCount)
	sort.SliceStable(sessions, func(i, j int) bool { return sessions[i].Seconds > sessions[j].Seconds })
	if len(sessions) > 10 {
		sessions = sessions[:10]
	}
	res.Longest = sessions
	return res, nil
}

func (ctx *reportContext) getCommand() (*ReportCommand, error) {
	res := &ReportCommand{Events: []*ReportEvent{}}
	rules, users := reportCounter{}, reportCounter{}
	lo, hi := ctx.getTimeBounds()
	err := adapter.engine.Where("owner = ? and created_time >= ? and created_time < ?", ctx.options.Owner, lo, hi).Asc("created_time").
		Iterate(&Command{}, func(i int, bean interface{}) error {
			command := bean.(*Command)
			t, ok := ctx.parseTime(command.CreatedTime)
			if !ok {
				return nil
			}
			res.Total++
			user := users.get(ctx.short(command.User))
			user.Count++
			rule := ctx.short(command.Rule)
			if rule != "" {
				rules.get(rule).Count++
			}
			switch command.Action {
			case CommandActionBlock:
				res.Blocked++
				user.Blocked++
			case CommandActionAlert:
				res.Alerted++
			case CommandActionReview:
				res.Reviewed++
			default:
				return nil
			}
			detail := truncate(command.Text, 500)
			if rule != "" {
				detail = fmt.Sprintf("[%s] %s", rule, detail)
			}
			res.Events = addReportEvent(res.Events, &ReportEvent{Time: ctx.formatTime(t), User: ctx.short(command.User), ClientIp: command.ClientIp, Asset: ctx.short(command.Asset), Action: command.Action, Detail: detail})
			return nil
		})
	if err != nil {
		return nil, err
	}
	res.Rules, res.ByUser = rules.top(byCount), users.top(byCount)
	return res, nil
}

func (ctx *reportContext) getFile() (*ReportFile, error) {
	res := &ReportFile{Events: []*ReportEvent{}}
	users, assets := reportCounter{}, reportCounter{}
	err := ctx.iterateRecords([]string{ctx.options.Owner}, reportFileActions, func(record *Record, t time.Time) {
		user := getRecordUser(record)
		asset := getIdName(record.Object)
		upload := strings.HasSuffix(record.Action, "-upload")
		for _, item := range []*ReportItem{users.get(user), assets.get(asset)} {
			item.Count++
			if upload {
				item.Uploads++
			} else {
				item.Downloads++
			}
		}
		if upload {
			res.Uploads++
		} else {
			res.Downloads++
		}
		res.Events = addReportEvent(res.Events, &ReportEvent{Time: ctx.formatTime(t), User: user, ClientIp: record.ClientIp, Asset: asset, Action: record.Action, Detail: record.RequestUri})
	})
	if err != nil {
		return nil, err
	}
	res.ByUser, res.ByAsset = users.top(byCount), assets.top(byCount)
	return res, nil
}

func countReview(reviews *ReportReviews, status string) {
	reviews.Total++
	switch status {
	case AccessRequestPending:
		reviews.Pending++
	case AccessRequestApproved, CommandReviewUsed:
		reviews.Approved++
	case AccessRequestRejected:
		reviews.Rejected++
	case AccessRequestCancelled, CommandReviewCanceled:
		reviews.Cancelled++
	case CommandReviewExpired:
		reviews.Expired++
	case AccessRequestRevoked:
		reviews.Revoked++
	}
}

func (ctx *reportContext) getApproval() (*ReportApproval, error) {
	res := &ReportApproval{AccessRequests: &ReportReviews{}, LoginReviews: &ReportReviews{}, CommandReviews: &ReportReviews{}, Events: []*ReportEvent{}}
	requesters, reviewers := reportCounter{}, reportCounter{}
	lo, hi := ctx.getTimeBounds()
	where := "owner = ? and created_time >= ? and created_time < ?"

	countReviewer := func(reviewer string, status string) {
		if reviewer != "" && status != AccessRequestPending && status != AccessRequestCancelled && status != CommandReviewCanceled && status != CommandReviewExpired {
			reviewers.get(ctx.short(reviewer)).Count++
		}
	}

	requests := []*AccessRequest{}
	err := adapter.engine.Where(where, ctx.options.Owner, lo, hi).Asc("created_time").Find(&requests)
	if err != nil {
		return nil, err
	}
	for _, request := range requests {
		t, ok := ctx.parseTime(request.CreatedTime)
		if !ok {
			continue
		}
		countReview(res.AccessRequests, request.Status)
		requesters.get(ctx.short(request.User)).Count++
		countReviewer(request.Reviewer, request.Status)
		detail := strings.Join(request.Assets, ", ")
		if request.Reason != "" {
			detail += ": " + truncate(request.Reason, 300)
		}
		if request.Reviewer != "" {
			detail += fmt.Sprintf(" (%s)", ctx.short(request.Reviewer))
		}
		res.Events = addReportEvent(res.Events, &ReportEvent{Time: ctx.formatTime(t), User: ctx.short(request.User), Action: request.Status, Detail: detail})
	}

	loginReviews := []*LoginReview{}
	err = adapter.engine.Where(where, ctx.options.Owner, lo, hi).Find(&loginReviews)
	if err != nil {
		return nil, err
	}
	for _, review := range loginReviews {
		if _, ok := ctx.parseTime(review.CreatedTime); ok {
			countReview(res.LoginReviews, review.Status)
			countReviewer(review.Reviewer, review.Status)
		}
	}

	commandReviews := []*CommandReview{}
	err = adapter.engine.Where(where, ctx.options.Owner, lo, hi).Omit("text").Find(&commandReviews)
	if err != nil {
		return nil, err
	}
	for _, review := range commandReviews {
		if _, ok := ctx.parseTime(review.CreatedTime); ok {
			countReview(res.CommandReviews, review.Status)
			countReviewer(review.Reviewer, review.Status)
		}
	}

	res.Requesters, res.Reviewers = requesters.top(byCount), reviewers.top(byCount)
	return res, nil
}

func (ctx *reportContext) getAccount() (*ReportAccount, error) {
	res := &ReportAccount{Events: []*ReportEvent{}}
	err := ctx.iterateRecords([]string{ctx.options.Owner}, []string{"rotate-asset-account", "push-asset-account"}, func(record *Record, t time.Time) {
		// the API calls are recorded with the same actions, and only the results are counted
		if record.Method != "Account" {
			return
		}
		asset := getIdName(record.Object)
		failed := strings.Contains(record.RequestUri, ": "+AccountRotateFailed)
		push := record.Action == "push-asset-account"
		switch {
		case push && failed:
			res.PushFailed++
		case push:
			res.Pushed++
		case failed:
			res.RotateFailed++
		default:
			res.Rotated++
		}
		res.Events = addReportEvent(res.Events, &ReportEvent{Time: ctx.formatTime(t), User: getRecordUser(record), Asset: asset, Action: record.Action, Detail: record.RequestUri})
	})
	if err != nil {
		return nil, err
	}

	kinds := reportCounter{}
	risks := []*AccountRisk{}
	err = adapter.engine.Where("owner = ?", ctx.options.Owner).Find(&risks)
	if err != nil {
		return nil, err
	}
	for _, risk := range risks {
		switch risk.Status {
		case RiskOpen:
			res.OpenRisks++
		case RiskIgnored:
			res.IgnoredRisks++
		}
		firstSeen := risk.FirstSeenTime
		if firstSeen == "" {
			firstSeen = risk.CreatedTime
		}
		if _, ok := ctx.parseTime(firstSeen); ok {
			res.NewRisks++
			kinds.get(risk.Kind).Count++
		}
		if _, ok := ctx.parseTime(risk.ResolvedTime); ok && risk.Status == RiskResolved {
			res.ResolvedRisks++
		}
	}
	res.RiskKinds = kinds.top(byCount)
	return res, nil
}

func (ctx *reportContext) getAsset() (*ReportAsset, error) {
	res := &ReportAsset{Events: []*ReportEvent{}, PermissionEvents: []*ReportEvent{}}
	protocols := reportCounter{}
	assets := []*Asset{}
	err := adapter.engine.Where("owner = ?", ctx.options.Owner).Cols("owner", "name", "created_time", "protocol", "host", "port").Asc("created_time").Find(&assets)
	if err != nil {
		return nil, err
	}
	res.Total = len(assets)
	for _, asset := range assets {
		protocols.get(asset.Protocol).Count++
		if t, ok := ctx.parseTime(asset.CreatedTime); ok {
			res.Added++
			res.Events = addReportEvent(res.Events, &ReportEvent{Time: ctx.formatTime(t), Asset: asset.Name, Action: "add-asset", Detail: fmt.Sprintf("%s %s:%d", asset.Protocol, asset.Host, asset.Port)})
		}
	}
	res.ByProtocol = protocols.top(byCount)

	err = ctx.iterateChangeRecords([]string{"delete-asset"}, func(record *Record, t time.Time, name string) {
		res.Deleted++
		res.Events = addReportEvent(res.Events, &ReportEvent{Time: ctx.formatTime(t), User: getRecordUser(record), ClientIp: record.ClientIp, Asset: name, Action: record.Action})
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(res.Events, func(i, j int) bool { return res.Events[i].Time < res.Events[j].Time })

	count, err := adapter.engine.Where("owner = ?", ctx.options.Owner).Count(&Permission{})
	if err != nil {
		return nil, err
	}
	res.Permissions = int(count)
	actions := []string{"add-permission", "update-permission", "delete-permission", "approve-access-request", "revoke-access-request"}
	err = ctx.iterateChangeRecords(actions, func(record *Record, t time.Time, name string) {
		switch record.Action {
		case "add-permission", "approve-access-request":
			res.PermissionsAdded++
		case "update-permission":
			res.PermissionsUpdated++
		default:
			res.PermissionsDeleted++
		}
		res.PermissionEvents = addReportEvent(res.PermissionEvents, &ReportEvent{Time: ctx.formatTime(t), User: getRecordUser(record), ClientIp: record.ClientIp, Action: record.Action, Detail: name})
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}
