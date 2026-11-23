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
	"strings"
	"time"
)

var reportLabelsZh = map[string]string{
	"Summary":                         "概览",
	"Logins":                          "登录",
	"Sessions":                        "会话",
	"Commands":                        "命令",
	"File transfers":                  "文件传输",
	"Approvals":                       "申请和审批",
	"Accounts":                        "账号",
	"Assets and permissions":          "资产和授权",
	"Casvisor audit report":           "Casvisor 审计报表",
	"Organization":                    "组织",
	"From":                            "开始",
	"To":                              "结束",
	"Time zone":                       "时区",
	"Working hours":                   "工作时间",
	"Generated at":                    "生成时间",
	"Item":                            "项目",
	"Value":                           "数值",
	"Successful sign-ins":             "登录成功",
	"Failed sign-ins":                 "登录失败",
	"Off-hours sign-ins":              "非工作时间登录",
	"Rejected by login rules":         "被登录规则拒绝",
	"Sign-ins held for review":        "登录需复核",
	"Asked for two-step verification": "要求两步验证",
	"By user":                         "按用户",
	"By client IP":                    "按来源 IP",
	"By asset":                        "按资产",
	"By protocol":                     "按协议",
	"By rule":                         "按规则",
	"By kind":                         "按类型",
	"User":                            "用户",
	"Client IP":                       "来源 IP",
	"Asset":                           "资产",
	"Protocol":                        "协议",
	"Rule":                            "规则",
	"Succeeded":                       "成功",
	"Failed":                          "失败",
	"Time":                            "时间",
	"Action":                          "动作",
	"Detail":                          "详情",
	"Events":                          "明细",
	"Total sessions":                  "会话总数",
	"Active sessions":                 "进行中的会话",
	"Users":                           "用户数",
	"Assets":                          "资产数",
	"Total duration":                  "总时长",
	"Count":                           "次数",
	"Duration":                        "时长",
	"Longest sessions":                "最长的会话",
	"Session":                         "会话",
	"Total commands":                  "命令总数",
	"Blocked":                         "拦截",
	"Alerted":                         "告警",
	"Commands held for review":        "命令需复核",
	"Hits":                            "命中次数",
	"Flagged commands":                "被拦截、告警、复核的命令",
	"Uploads":                         "上传",
	"Downloads":                       "下载",
	"Access requests":                 "访问申请",
	"Login reviews":                   "登录复核",
	"Command reviews":                 "命令复核",
	"Total":                           "总数",
	"Pending":                         "待审批",
	"Approved":                        "批准",
	"Rejected":                        "拒绝",
	"Cancelled":                       "撤回",
	"Expired":                         "过期",
	"Revoked":                         "撤销",
	"Requesters":                      "申请人",
	"Reviewers":                       "审批人",
	"Password rotations":              "改密成功",
	"Failed rotations":                "改密失败",
	"Password pushes":                 "推送成功",
	"Failed pushes":                   "推送失败",
	"New risky accounts":              "新增风险账号",
	"Resolved risky accounts":         "已处理风险账号",
	"Open risky accounts now":         "当前未处理风险账号",
	"Ignored risky accounts now":      "当前已忽略风险账号",
	"Rotations and pushes":            "改密和推送",
	"Assets now":                      "当前资产总数",
	"Added assets":                    "新增资产",
	"Deleted assets":                  "删除资产",
	"Permissions now":                 "当前授权总数",
	"Added permissions":               "新增授权",
	"Updated permissions":             "修改授权",
	"Removed permissions":             "删除或撤销授权",
	"Asset changes":                   "资产变更",
	"Permission changes":              "授权变更",
	"Permission":                      "授权",
	"Up to %d rows are listed.":       "最多列出 %d 行。",
	"Only the top %d are listed.":     "只列出前 %d 个。",
}

type reportWriter struct {
	language string
}

func (w *reportWriter) t(s string) string {
	if w.language == "zh" {
		if res, ok := reportLabelsZh[s]; ok {
			return res
		}
	}
	return s
}

func (w *reportWriter) heading(sheet *XlsxSheet, title string) {
	if len(sheet.rows) > 0 {
		sheet.Add()
	}
	sheet.Heading(w.t(title))
}

func (w *reportWriter) items(sheet *XlsxSheet, title string, nameTitle string, items []*ReportItem, columns ...string) {
	w.heading(sheet, title)
	header := []string{w.t(nameTitle)}
	for _, column := range columns {
		header = append(header, w.t(column))
	}
	sheet.Header(header...)
	for _, item := range items {
		cells := []interface{}{item.Name}
		for _, column := range columns {
			switch column {
			case "Count", "Succeeded", "Hits":
				cells = append(cells, item.Count)
			case "Failed":
				cells = append(cells, item.Failed)
			case "Blocked":
				cells = append(cells, item.Blocked)
			case "Uploads":
				cells = append(cells, item.Uploads)
			case "Downloads":
				cells = append(cells, item.Downloads)
			case "Duration":
				cells = append(cells, formatReportDuration(item.Seconds))
			}
		}
		sheet.Add(cells...)
	}
	if len(items) == reportTopCount {
		sheet.Add(fmt.Sprintf(w.t("Only the top %d are listed."), reportTopCount))
	}
}

func (w *reportWriter) events(sheet *XlsxSheet, title string, events []*ReportEvent, withDuration bool) {
	w.heading(sheet, title)
	header := []string{w.t("Time"), w.t("User"), w.t("Client IP"), w.t("Asset"), w.t("Action"), w.t("Detail")}
	if withDuration {
		header = append(header, w.t("Duration"))
	}
	sheet.Header(header...)
	for _, event := range events {
		cells := []interface{}{event.Time, event.User, event.ClientIp, event.Asset, event.Action, event.Detail}
		if withDuration {
			cells = append(cells, formatReportDuration(event.Seconds))
		}
		sheet.Add(cells...)
	}
	if len(events) == reportEventCount {
		sheet.Add(fmt.Sprintf(w.t("Up to %d rows are listed."), reportEventCount))
	}
}

func (w *reportWriter) values(sheet *XlsxSheet, values ...interface{}) {
	sheet.Header(w.t("Item"), w.t("Value"))
	for i := 0; i+1 < len(values); i += 2 {
		sheet.Add(w.t(values[i].(string)), values[i+1])
	}
}

func (w *reportWriter) reviews(sheet *XlsxSheet, title string, reviews *ReportReviews) {
	w.heading(sheet, title)
	w.values(sheet, "Total", reviews.Total, "Pending", reviews.Pending, "Approved", reviews.Approved, "Rejected", reviews.Rejected,
		"Cancelled", reviews.Cancelled, "Expired", reviews.Expired, "Revoked", reviews.Revoked)
}

func formatReportDuration(seconds int64) string {
	return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
}

func getReportPeriodText(data *ReportData) (string, string) {
	start, _ := time.Parse(time.RFC3339, data.StartTime)
	end, _ := time.Parse(time.RFC3339, data.EndTime)
	return start.Format("2006-01-02 15:04"), end.Add(-time.Second).Format("2006-01-02 15:04")
}

// BuildReportXlsx makes the workbook of the report: a summary sheet and a sheet for each part.
func BuildReportXlsx(data *ReportData, language string) ([]byte, error) {
	w := &reportWriter{language: language}
	from, to := getReportPeriodText(data)
	summary := &XlsxSheet{Name: w.t("Summary")}
	summary.Heading(w.t("Casvisor audit report"))
	summary.Add(w.t("Organization"), data.Owner)
	summary.Add(w.t("From"), from)
	summary.Add(w.t("To"), to)
	summary.Add(w.t("Time zone"), data.TimeZone)
	summary.Add(w.t("Working hours"), data.WorkingHours)
	generated, _ := time.Parse(time.RFC3339, data.GeneratedTime)
	summary.Add(w.t("Generated at"), generated.Format("2006-01-02 15:04"))
	summary.Add()
	summary.Header(w.t("Item"), w.t("Value"))
	sheets := []*XlsxSheet{summary}

	if res := data.Login; res != nil {
		summary.Add(w.t("Successful sign-ins"), res.Succeeded)
		summary.Add(w.t("Failed sign-ins"), res.Failed)
		summary.Add(w.t("Off-hours sign-ins"), res.OffHours)
		summary.Add(w.t("Rejected by login rules"), res.Rejected)

		sheet := &XlsxSheet{Name: w.t("Logins")}
		w.values(sheet, "Successful sign-ins", res.Succeeded, "Failed sign-ins", res.Failed, "Off-hours sign-ins", res.OffHours,
			"Rejected by login rules", res.Rejected, "Sign-ins held for review", res.Reviewed, "Asked for two-step verification", res.MfaRequired)
		w.items(sheet, "By user", "User", res.Users, "Succeeded", "Failed")
		w.items(sheet, "By client IP", "Client IP", res.ClientIps, "Succeeded", "Failed")
		w.events(sheet, "Events", res.Events, false)
		sheets = append(sheets, sheet)
	}
	if res := data.Session; res != nil {
		summary.Add(w.t("Total sessions"), res.Total)
		summary.Add(w.t("Total duration"), formatReportDuration(res.Seconds))

		sheet := &XlsxSheet{Name: w.t("Sessions")}
		w.values(sheet, "Total sessions", res.Total, "Active sessions", res.Active, "Users", res.Users, "Assets", res.Assets, "Total duration", formatReportDuration(res.Seconds))
		w.items(sheet, "By user", "User", res.ByUser, "Count", "Duration")
		w.items(sheet, "By asset", "Asset", res.ByAsset, "Count", "Duration")
		w.items(sheet, "By protocol", "Protocol", res.ByProtocol, "Count", "Duration")
		w.events(sheet, "Longest sessions", res.Longest, true)
		sheets = append(sheets, sheet)
	}
	if res := data.Command; res != nil {
		summary.Add(w.t("Total commands"), res.Total)
		summary.Add(w.t("Blocked"), res.Blocked)

		sheet := &XlsxSheet{Name: w.t("Commands")}
		w.values(sheet, "Total commands", res.Total, "Blocked", res.Blocked, "Alerted", res.Alerted, "Commands held for review", res.Reviewed)
		w.items(sheet, "By rule", "Rule", res.Rules, "Hits")
		w.items(sheet, "By user", "User", res.ByUser, "Count", "Blocked")
		w.events(sheet, "Flagged commands", res.Events, false)
		sheets = append(sheets, sheet)
	}
	if res := data.File; res != nil {
		summary.Add(w.t("Uploads"), res.Uploads)
		summary.Add(w.t("Downloads"), res.Downloads)

		sheet := &XlsxSheet{Name: w.t("File transfers")}
		w.values(sheet, "Uploads", res.Uploads, "Downloads", res.Downloads)
		w.items(sheet, "By user", "User", res.ByUser, "Uploads", "Downloads")
		w.items(sheet, "By asset", "Asset", res.ByAsset, "Uploads", "Downloads")
		w.events(sheet, "Events", res.Events, false)
		sheets = append(sheets, sheet)
	}
	if res := data.Approval; res != nil {
		summary.Add(w.t("Access requests"), res.AccessRequests.Total)
		summary.Add(w.t("Command reviews"), res.CommandReviews.Total)

		sheet := &XlsxSheet{Name: w.t("Approvals")}
		w.reviews(sheet, "Access requests", res.AccessRequests)
		w.reviews(sheet, "Login reviews", res.LoginReviews)
		w.reviews(sheet, "Command reviews", res.CommandReviews)
		w.items(sheet, "Requesters", "User", res.Requesters, "Count")
		w.items(sheet, "Reviewers", "User", res.Reviewers, "Count")
		w.events(sheet, "Access requests", res.Events, false)
		sheets = append(sheets, sheet)
	}
	if res := data.Account; res != nil {
		summary.Add(w.t("Password rotations"), res.Rotated)
		summary.Add(w.t("Failed rotations"), res.RotateFailed)
		summary.Add(w.t("New risky accounts"), res.NewRisks)

		sheet := &XlsxSheet{Name: w.t("Accounts")}
		w.values(sheet, "Password rotations", res.Rotated, "Failed rotations", res.RotateFailed, "Password pushes", res.Pushed, "Failed pushes", res.PushFailed,
			"New risky accounts", res.NewRisks, "Resolved risky accounts", res.ResolvedRisks, "Open risky accounts now", res.OpenRisks, "Ignored risky accounts now", res.IgnoredRisks)
		w.items(sheet, "By kind", "Item", res.RiskKinds, "Count")
		w.events(sheet, "Rotations and pushes", res.Events, false)
		sheets = append(sheets, sheet)
	}
	if res := data.Asset; res != nil {
		summary.Add(w.t("Assets now"), res.Total)
		summary.Add(w.t("Added assets"), res.Added)
		summary.Add(w.t("Deleted assets"), res.Deleted)
		summary.Add(w.t("Permission changes"), res.PermissionsAdded+res.PermissionsUpdated+res.PermissionsDeleted)

		sheet := &XlsxSheet{Name: w.t("Assets and permissions")}
		w.values(sheet, "Assets now", res.Total, "Added assets", res.Added, "Deleted assets", res.Deleted, "Permissions now", res.Permissions,
			"Added permissions", res.PermissionsAdded, "Updated permissions", res.PermissionsUpdated, "Removed permissions", res.PermissionsDeleted)
		w.items(sheet, "By protocol", "Protocol", res.ByProtocol, "Count")
		w.events(sheet, "Asset changes", res.Events, false)
		w.events(sheet, "Permission changes", res.PermissionEvents, false)
		sheets = append(sheets, sheet)
	}
	return WriteXlsx(sheets)
}

func getReportMail(report *Report, displayName string, data *ReportData, fileName string) (string, string) {
	w := &reportWriter{language: report.Language}
	from, to := getReportPeriodText(data)
	lines := []string{}
	add := func(label string, value interface{}) {
		lines = append(lines, fmt.Sprintf("%s: %v", w.t(label), value))
	}
	if res := data.Login; res != nil {
		add("Successful sign-ins", res.Succeeded)
		add("Failed sign-ins", res.Failed)
		add("Off-hours sign-ins", res.OffHours)
		add("Rejected by login rules", res.Rejected)
	}
	if res := data.Session; res != nil {
		add("Total sessions", res.Total)
		add("Total duration", formatReportDuration(res.Seconds))
	}
	if res := data.Command; res != nil {
		add("Total commands", res.Total)
		add("Blocked", res.Blocked)
		add("Alerted", res.Alerted)
	}
	if res := data.File; res != nil {
		add("Uploads", res.Uploads)
		add("Downloads", res.Downloads)
	}
	if res := data.Approval; res != nil {
		add("Access requests", res.AccessRequests.Total)
		add("Command reviews", res.CommandReviews.Total)
	}
	if res := data.Account; res != nil {
		add("Password rotations", res.Rotated)
		add("Failed rotations", res.RotateFailed)
		add("New risky accounts", res.NewRisks)
	}
	if res := data.Asset; res != nil {
		add("Assets now", res.Total)
		add("Added assets", res.Added)
		add("Deleted assets", res.Deleted)
	}

	if report.Language == "zh" {
		subject := fmt.Sprintf("[Casvisor] %s 审计报表：%s", report.Owner, displayName)
		body := fmt.Sprintf("组织 %s 的审计报表 %s，时间 %s 至 %s（%s），详见附件 %s。\n\n%s\n", report.Owner, displayName, from, to, data.TimeZone, fileName, strings.Join(lines, "\n"))
		return subject, body
	}
	subject := fmt.Sprintf("[Casvisor] Audit report of %s: %s", report.Owner, displayName)
	body := fmt.Sprintf("The audit report %s of the organization %s, from %s to %s (%s), is attached: %s.\n\n%s\n", displayName, report.Owner, from, to, data.TimeZone, fileName, strings.Join(lines, "\n"))
	return subject, body
}
