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
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

const (
	RiskWeakPassword        = "weak-password"
	RiskStalePassword       = "stale-password"
	RiskReusedPassword      = "reused-password"
	RiskEmptyPassword       = "empty-password"
	RiskUnmanagedAccount    = "unmanaged-account"
	RiskUnmanagedPrivileged = "unmanaged-privileged"

	RiskLevelHigh   = "High"
	RiskLevelMedium = "Medium"
	RiskLevelLow    = "Low"

	RiskOpen     = "Open"
	RiskIgnored  = "Ignored"
	RiskResolved = "Resolved"

	stalePasswordDays   = 90
	reusedPasswordLimit = 3
	riskScanConcurrency = 5
)

// AccountRisk is something wrong with an account of an asset: a weak, old or reused password,
// an empty password, or an account on the asset that Casvisor does not manage.
type AccountRisk struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`

	Asset    string `xorm:"varchar(100) index" json:"asset"`
	Username string `xorm:"varchar(100)" json:"username"`
	Kind     string `xorm:"varchar(50) index" json:"kind"`
	Level    string `xorm:"varchar(20)" json:"level"`
	Detail   string `xorm:"varchar(500)" json:"detail"`
	// Managed tells whether Casvisor keeps the account, as the asset's own account or a system account
	Managed bool `json:"managed"`

	Status        string `xorm:"varchar(20) index" json:"status"`
	FirstSeenTime string `xorm:"varchar(100)" json:"firstSeenTime"`
	LastSeenTime  string `xorm:"varchar(100)" json:"lastSeenTime"`
	ResolvedTime  string `xorm:"varchar(100)" json:"resolvedTime"`
	Comment       string `xorm:"varchar(500)" json:"comment"`
}

// AccountRiskScan is how the last scan of an organization went.
type AccountRiskScan struct {
	Running   bool     `json:"running"`
	StartTime string   `json:"startTime"`
	EndTime   string   `json:"endTime"`
	Total     int      `json:"total"`
	Done      int      `json:"done"`
	Found     int      `json:"found"`
	Errors    []string `json:"errors"`
}

func (risk *AccountRisk) GetId() string {
	return util.GetIdFromOwnerAndName(risk.Owner, risk.Name)
}

func getRiskName(asset string, username string, kind string) string {
	sum := sha256.Sum256([]byte(asset + "\x00" + username + "\x00" + kind))
	return hex.EncodeToString(sum[:12])
}

func GetAccountRiskCount(owner, status, field, value string) (int64, error) {
	session := GetSession(owner, -1, -1, field, value, "", "")
	return session.Count(&AccountRisk{Status: status})
}

func GetPaginationAccountRisks(owner string, status string, offset, limit int, field, value, sortField, sortOrder string) ([]*AccountRisk, error) {
	risks := []*AccountRisk{}
	session := GetSession(owner, offset, limit, field, value, sortField, sortOrder)
	err := session.Find(&risks, &AccountRisk{Status: status})
	return risks, err
}

// SetAccountRiskStatus ignores a risk, with a comment, or opens it again.
func SetAccountRiskStatus(id string, status string, comment string) (bool, error) {
	if status != RiskOpen && status != RiskIgnored {
		return false, fmt.Errorf("a risk can only be set to %s or %s", RiskOpen, RiskIgnored)
	}
	if runes := []rune(comment); len(runes) > 500 {
		comment = string(runes[:500])
	}
	owner, name := util.GetOwnerAndNameFromId(id)
	affected, err := adapter.engine.ID(core.PK{owner, name}).Cols("status", "comment").Update(&AccountRisk{Status: status, Comment: comment})
	return affected != 0, err
}

var commonPasswords = []string{
	"123456", "1234567", "12345678", "123456789", "1234567890", "111111", "000000", "666666", "888888", "123123",
	"654321", "112233", "abc123", "abcd1234", "a123456", "a12345678", "qwerty", "qwerty123", "qwe123", "1qaz2wsx",
	"1q2w3e4r", "zxcvbnm", "asdfgh", "password", "password1", "password123", "passw0rd", "p@ssw0rd", "p@ssword", "admin",
	"admin123", "admin@123", "admin888", "administrator", "root", "root123", "root@123", "toor", "test", "test123",
	"guest", "changeme", "welcome", "letmein", "iloveyou", "default", "123qwe", "qweasd", "huawei", "huawei@123",
	"server", "oracle", "mysql", "ubuntu", "centos", "user", "user123", "pass", "secret", "abc@123",
}

// getWeakPasswordReason tells why a password is weak, or "" when it is not.
func getWeakPasswordReason(username string, password string) string {
	lower := strings.ToLower(password)
	if slices.Contains(commonPasswords, lower) {
		return "it is one of the most common passwords"
	}
	if lower == strings.ToLower(username) || strings.Contains(lower, strings.ToLower(username)) && len(username) >= 3 {
		return "it contains the username"
	}
	if len([]rune(password)) < 8 {
		return "it is shorter than 8 characters"
	}

	classes := 0
	for _, has := range []func(rune) bool{unicode.IsUpper, unicode.IsLower, unicode.IsDigit, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }} {
		if strings.IndexFunc(password, has) >= 0 {
			classes++
		}
	}
	if classes < 2 || (classes < 3 && len([]rune(password)) < 12) {
		return "it uses too few kinds of characters"
	}
	return ""
}

type riskCollector struct {
	owner    string
	mu       sync.Mutex
	findings map[string]*AccountRisk
}

func (c *riskCollector) add(asset string, username string, kind string, level string, detail string, managed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	name := getRiskName(asset, username, kind)
	c.findings[name] = &AccountRisk{Owner: c.owner, Name: name, Asset: asset, Username: username, Kind: kind, Level: level, Detail: truncate(detail, 500), Managed: managed}
}

var (
	riskScansLock sync.Mutex
	riskScans     = map[string]*AccountRiskScan{}
)

func GetAccountRiskScan(owner string) *AccountRiskScan {
	riskScansLock.Lock()
	defer riskScansLock.Unlock()
	if scan := riskScans[owner]; scan != nil {
		copied := *scan
		copied.Errors = slices.Clone(scan.Errors)
		return &copied
	}
	return &AccountRiskScan{}
}

// StartAccountRiskScan checks the accounts of the organization in the background.
func StartAccountRiskScan(owner string) error {
	riskScansLock.Lock()
	if scan := riskScans[owner]; scan != nil && scan.Running {
		riskScansLock.Unlock()
		return fmt.Errorf("a scan of the accounts is already running")
	}
	scan := &AccountRiskScan{Running: true, StartTime: util.GetCurrentTime(), Errors: []string{}}
	riskScans[owner] = scan
	riskScansLock.Unlock()

	go func() {
		err := scanAccountRisks(owner, scan)
		riskScansLock.Lock()
		defer riskScansLock.Unlock()
		if err != nil {
			scan.Errors = append(scan.Errors, err.Error())
			logs.Error(fmt.Sprintf("scanAccountRisks(): %s", err.Error()))
		}
		scan.Running = false
		scan.EndTime = util.GetCurrentTime()
	}()
	return nil
}

func scanAccountRisks(owner string, scan *AccountRiskScan) error {
	assets, err := GetAssets(owner)
	if err != nil {
		return err
	}
	for _, asset := range assets {
		err = asset.decryptCredentials()
		if err != nil {
			return err
		}
	}
	accounts, err := getAllAssetAccounts(owner)
	if err != nil {
		return err
	}
	managed := map[string][]string{}
	for _, account := range accounts {
		err = account.decryptCredentials()
		if err != nil {
			return err
		}
		managed[account.Asset] = append(managed[account.Asset], account.Username)
	}

	c := &riskCollector{owner: owner, findings: map[string]*AccountRisk{}}
	checkPasswords(c, assets, accounts, time.Now())

	gatheredAssets := []*Asset{}
	platforms := map[string]*Platform{}
	for _, asset := range assets {
		platform, err := getAssetPlatform(asset)
		if err != nil {
			scan.Errors = append(scan.Errors, fmt.Sprintf("%s: %s", asset.Name, err.Error()))
			continue
		}
		if platform.canGather() && platform.reaches(asset) {
			gatheredAssets = append(gatheredAssets, asset)
			platforms[asset.Name] = platform
		}
	}
	riskScansLock.Lock()
	scan.Total = len(gatheredAssets)
	riskScansLock.Unlock()

	scanned := map[string]bool{}
	var scannedLock sync.Mutex
	limit := make(chan struct{}, riskScanConcurrency)
	var wg sync.WaitGroup
	for _, asset := range gatheredAssets {
		wg.Add(1)
		limit <- struct{}{}
		go func(asset *Asset) {
			defer wg.Done()
			defer func() { <-limit }()

			known := append([]string{asset.Username}, managed[asset.Name]...)
			err := gatherAssetAccounts(c, asset, platforms[asset.Name], known)
			riskScansLock.Lock()
			scan.Done++
			if err != nil {
				scan.Errors = append(scan.Errors, fmt.Sprintf("%s: %s", asset.Name, truncate(err.Error(), 300)))
			}
			riskScansLock.Unlock()
			if err == nil {
				scannedLock.Lock()
				scanned[asset.Name] = true
				scannedLock.Unlock()
			}
		}(asset)
	}
	wg.Wait()

	found, opened, err := saveAccountRisks(owner, c.findings, scanned)
	riskScansLock.Lock()
	scan.Found = found
	riskScansLock.Unlock()
	if len(opened) > 0 {
		notifyAccountRisks(owner, opened, found)
	}
	return err
}

// checkPasswords finds the weak, old and reused passwords among the accounts Casvisor keeps.
func checkPasswords(c *riskCollector, assets []*Asset, accounts []*AssetAccount, now time.Time) {
	type owned struct{ asset, username string }
	byPassword := map[string][]owned{}

	for _, asset := range assets {
		username := asset.Username
		if username == "" && asset.Protocol == ProtocolRedis {
			username = "default"
		}
		if asset.Password == "" || username == "" {
			continue
		}
		if reason := getWeakPasswordReason(username, asset.Password); reason != "" {
			c.add(asset.Name, username, RiskWeakPassword, RiskLevelHigh, "the password of the asset's own account is weak: "+reason, true)
		}
		byPassword[asset.Password] = append(byPassword[asset.Password], owned{asset.Name, username})
	}

	for _, account := range accounts {
		if !account.IsEnabled || account.Password == "" {
			continue
		}
		if reason := getWeakPasswordReason(account.Username, account.Password); reason != "" {
			c.add(account.Asset, account.Username, RiskWeakPassword, RiskLevelHigh, "the password is weak: "+reason, true)
		}

		changed := account.LastRotatedTime
		if changed == "" {
			changed = account.CreatedTime
		}
		if t, err := time.Parse(time.RFC3339, changed); err == nil {
			days := int(now.Sub(t).Hours() / 24)
			if days >= stalePasswordDays {
				level := RiskLevelLow
				if days >= 2*stalePasswordDays {
					level = RiskLevelMedium
				}
				detail := fmt.Sprintf("the password has not been changed for %d days", days)
				if !account.AutoRotate {
					detail += ", and it is not rotated automatically"
				}
				c.add(account.Asset, account.Username, RiskStalePassword, level, detail, true)
			}
		}
		byPassword[account.Password] = append(byPassword[account.Password], owned{account.Asset, account.Username})
	}

	for _, uses := range byPassword {
		assetNames := []string{}
		for _, use := range uses {
			if !slices.Contains(assetNames, use.asset) {
				assetNames = append(assetNames, use.asset)
			}
		}
		if len(assetNames) < reusedPasswordLimit {
			continue
		}
		for _, use := range uses {
			c.add(use.asset, use.username, RiskReusedPassword, RiskLevelMedium, fmt.Sprintf("the same password is used on %d assets", len(assetNames)), true)
		}
	}
}

const linuxAccountScript = `getent passwd 2>/dev/null || cat /etc/passwd
echo CASVISOR_GROUPS
getent group 2>/dev/null || cat /etc/group
echo CASVISOR_SHADOW
cat /etc/shadow 2>/dev/null
exit 0
`

var noLoginShells = []string{"nologin", "false", "sync", "shutdown", "halt", "true"}

var privilegedGroups = []string{"sudo", "wheel", "admin", "root"}

// gatherAssetAccounts reads the accounts on the asset and reports the ones Casvisor does not
// manage, the privileged ones above all, and the ones with no password.
func gatherAssetAccounts(c *riskCollector, asset *Asset, platform *Platform, known []string) error {
	if IsDatabaseProtocol(platform.Method) {
		return gatherDatabaseAccounts(c, asset, known)
	}
	host, err := connectAccountHost(asset, platform)
	if err != nil {
		return err
	}
	defer host.close()

	switch h := host.(type) {
	case *linuxHost:
		var stdout, stderr bytes.Buffer
		code, err := h.runScript(linuxAccountScript, &stdout, &stderr)
		if err != nil {
			return err
		}
		if code != 0 {
			return fmt.Errorf("failed to read the accounts (exit code %d): %s", code, truncate(strings.TrimSpace(stderr.String()), 300))
		}
		reportLinuxAccounts(c, asset, known, stdout.String(), h.root)
		return nil
	case *windowsHost:
		var res struct {
			Users []struct {
				Name             string `json:"name"`
				PasswordRequired bool   `json:"passwordRequired"`
				Admin            bool   `json:"admin"`
			} `json:"users"`
		}
		err = h.runScript(windowsAccountScript, &res)
		if err != nil {
			return err
		}
		for _, user := range res.Users {
			isKnown := slices.ContainsFunc(known, func(name string) bool { return strings.EqualFold(name, user.Name) })
			if !user.PasswordRequired {
				c.add(asset.Name, user.Name, RiskEmptyPassword, RiskLevelHigh, "the account may sign in without a password", isKnown)
			}
			if isKnown {
				continue
			}
			if user.Admin {
				c.add(asset.Name, user.Name, RiskUnmanagedPrivileged, RiskLevelHigh, "an administrator of the asset that Casvisor does not manage", false)
			} else {
				c.add(asset.Name, user.Name, RiskUnmanagedAccount, RiskLevelLow, "an enabled account of the asset that Casvisor does not manage", false)
			}
		}
		return nil
	}
	return fmt.Errorf("the asset is neither Linux nor Windows")
}

func gatherDatabaseAccounts(c *riskCollector, asset *Asset, known []string) error {
	accounts, err := getDatabaseAccounts(asset)
	if err != nil {
		return err
	}
	if asset.Protocol == ProtocolRedis && asset.Username == "" {
		known = append(known, "default")
	}
	for _, account := range accounts {
		isKnown := slices.ContainsFunc(known, func(name string) bool {
			// the user names of Oracle and SQL Server are not case-sensitive
			if asset.Protocol == ProtocolOracle || asset.Protocol == ProtocolSqlServer {
				return strings.EqualFold(name, account.username)
			}
			return name == account.username
		})
		detail := ""
		if account.detail != "" {
			detail = " (" + account.detail + ")"
		}
		if account.noPassword {
			c.add(asset.Name, account.username, RiskEmptyPassword, RiskLevelHigh, "the account may sign in without a password"+detail, isKnown)
		}
		if isKnown {
			continue
		}
		if account.privileged {
			c.add(asset.Name, account.username, RiskUnmanagedPrivileged, RiskLevelHigh, "a privileged database account that Casvisor does not manage"+detail, false)
		} else {
			c.add(asset.Name, account.username, RiskUnmanagedAccount, RiskLevelLow, "a database account that Casvisor does not manage"+detail, false)
		}
	}
	return nil
}

const windowsAccountScript = `$admins = @(Get-LocalGroupMember -SID 'S-1-5-32-544' -ErrorAction SilentlyContinue | ForEach-Object { ($_.Name -split '\\')[-1] })
$users = @(Get-LocalUser | Where-Object { $_.Enabled } | ForEach-Object { @{ name = $_.Name; passwordRequired = $_.PasswordRequired; admin = ($admins -contains $_.Name) } })
Out-Result @{ users = $users }
`

func reportLinuxAccounts(c *riskCollector, asset *Asset, known []string, output string, canReadShadow bool) {
	sections := strings.SplitN(strings.ReplaceAll(output, "\r", ""), "CASVISOR_GROUPS\n", 2)
	passwd := sections[0]
	groupText, shadowText := "", ""
	if len(sections) == 2 {
		parts := strings.SplitN(sections[1], "CASVISOR_SHADOW\n", 2)
		groupText = parts[0]
		if len(parts) == 2 {
			shadowText = parts[1]
		}
	}

	// the members of the groups that may become root, by name and by primary group id
	privileged := map[string]bool{}
	privilegedGids := map[string]bool{}
	for _, line := range strings.Split(groupText, "\n") {
		fields := strings.Split(line, ":")
		if len(fields) < 4 || !slices.Contains(privilegedGroups, fields[0]) {
			continue
		}
		// the root group's own members are listed, its gid alone means nothing more than uid 0 does
		if fields[0] != "root" {
			privilegedGids[fields[2]] = true
		}
		for _, member := range strings.Split(fields[3], ",") {
			if member != "" {
				privileged[member] = true
			}
		}
	}

	emptyPassword := map[string]bool{}
	// an account past its expiry date can't sign in at all, as one locked from Casvisor is
	expired := map[string]bool{}
	today := int(time.Now().Unix() / 86400)
	if canReadShadow {
		for _, line := range strings.Split(shadowText, "\n") {
			fields := strings.Split(line, ":")
			if len(fields) >= 2 && fields[0] != "" && fields[1] == "" {
				emptyPassword[fields[0]] = true
			}
			if len(fields) >= 8 && fields[7] != "" {
				if day, err := strconv.Atoi(fields[7]); err == nil && day <= today {
					expired[fields[0]] = true
				}
			}
		}
	}

	for _, line := range strings.Split(passwd, "\n") {
		fields := strings.Split(strings.TrimSpace(line), ":")
		if len(fields) < 7 {
			continue
		}
		name, uid, gid, shell := fields[0], fields[2], fields[3], fields[6]
		base := shell[strings.LastIndex(shell, "/")+1:]
		if shell == "" || slices.Contains(noLoginShells, base) || expired[name] {
			continue
		}

		isKnown := slices.Contains(known, name)
		if emptyPassword[name] {
			c.add(asset.Name, name, RiskEmptyPassword, RiskLevelHigh, "the account may sign in without a password", isKnown)
		}
		if isKnown {
			continue
		}

		reasons := []string{}
		if uid == "0" {
			reasons = append(reasons, "its uid is 0")
		}
		if privileged[name] || privilegedGids[gid] {
			reasons = append(reasons, "it is in a group that may use sudo")
		}
		if len(reasons) > 0 {
			c.add(asset.Name, name, RiskUnmanagedPrivileged, RiskLevelHigh, "a privileged account that Casvisor does not manage: "+strings.Join(reasons, " and "), false)
			continue
		}
		if n, err := strconv.Atoi(uid); err == nil && n < 1000 && name != "root" {
			// a service account with a shell, such as postgres
			c.add(asset.Name, name, RiskUnmanagedAccount, RiskLevelLow, fmt.Sprintf("a system account with the login shell %s that Casvisor does not manage", shell), false)
			continue
		}
		c.add(asset.Name, name, RiskUnmanagedAccount, RiskLevelLow, fmt.Sprintf("an account with the login shell %s that Casvisor does not manage", shell), false)
	}
}

var scanOnlyRisks = []string{RiskEmptyPassword, RiskUnmanagedAccount, RiskUnmanagedPrivileged}

// saveAccountRisks keeps what the scan found and resolves the open risks it no longer finds:
// the ones that only a scan of the asset finds are resolved only when the asset was read. It returns
// the count of the open risks and the risks it opened: the new ones and the ones that came back.
func saveAccountRisks(owner string, findings map[string]*AccountRisk, scanned map[string]bool) (int, []*AccountRisk, error) {
	opened := []*AccountRisk{}
	old := []*AccountRisk{}
	err := adapter.engine.Find(&old, &AccountRisk{Owner: owner})
	if err != nil {
		return 0, nil, err
	}
	now := util.GetCurrentTime()

	for _, risk := range old {
		finding := findings[risk.Name]
		if finding == nil {
			if risk.Status != RiskOpen || (slices.Contains(scanOnlyRisks, risk.Kind) && !scanned[risk.Asset]) {
				continue
			}
			_, err = adapter.engine.ID(core.PK{risk.Owner, risk.Name}).Cols("status", "resolved_time").Update(&AccountRisk{Status: RiskResolved, ResolvedTime: now})
			if err != nil {
				return 0, nil, err
			}
			continue
		}

		finding.Status = risk.Status
		if risk.Status == RiskResolved {
			finding.Status = RiskOpen
			opened = append(opened, finding)
		}
		finding.FirstSeenTime, finding.LastSeenTime, finding.Comment = risk.FirstSeenTime, now, risk.Comment
		_, err = adapter.engine.ID(core.PK{risk.Owner, risk.Name}).Cols("level", "detail", "managed", "status", "last_seen_time", "resolved_time").Update(finding)
		if err != nil {
			return 0, nil, err
		}
		delete(findings, risk.Name)
	}

	for _, finding := range findings {
		finding.Status = RiskOpen
		finding.CreatedTime, finding.FirstSeenTime, finding.LastSeenTime = now, now, now
		_, err = adapter.engine.Insert(finding)
		if err != nil {
			return 0, nil, err
		}
		opened = append(opened, finding)
	}

	count, err := adapter.engine.Where("owner = ? and status = ?", owner, RiskOpen).Count(&AccountRisk{})
	return int(count), opened, err
}

// StartAccountRiskScanner scans the accounts of every organization once a day.
func StartAccountRiskScanner() {
	go func() {
		time.Sleep(10 * time.Minute)
		for {
			owners := []string{}
			err := adapter.engine.Table(&Asset{}).Distinct("owner").Cols("owner").Find(&owners)
			if err != nil {
				logs.Error(fmt.Sprintf("StartAccountRiskScanner(): %s", err.Error()))
			}
			for _, owner := range owners {
				err = StartAccountRiskScan(owner)
				if err != nil {
					logs.Warning(fmt.Sprintf("StartAccountRiskScanner(): %s: %s", owner, err.Error()))
				}
			}
			time.Sleep(24 * time.Hour)
		}
	}()
}
