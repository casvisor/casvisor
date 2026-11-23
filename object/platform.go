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
	"crypto/rand"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strings"

	"github.com/casvisor/casvisor/util"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"
	"xorm.io/core"
	"xorm.io/xorm"
)

const (
	PlatformMethodShell      = "Shell"
	PlatformMethodPowerShell = "PowerShell"
	PlatformMethodCli        = "CLI"

	ElevationSudo   = "sudo"
	ElevationSu     = "su"
	ElevationEnable = "enable"

	defaultPasswordSymbols = "!#%+-=_."
)

type PlatformProtocol struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}

// Platform is a kind of system, such as Linux, Cisco IOS or MySQL: its protocols and their ports,
// the charset of its terminal, how to become privileged, how passwords are changed and what they
// look like, and what Casvisor may do with its accounts. The built-in ones are in the code, and an
// organization keeps its own copies to change.
type Platform struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	UpdatedTime string `xorm:"varchar(100)" json:"updatedTime"`
	DisplayName string `xorm:"varchar(100)" json:"displayName"`
	Description string `xorm:"varchar(500)" json:"description"`
	IsBuiltIn   bool   `xorm:"-" json:"isBuiltIn"`

	Category  string              `xorm:"varchar(50)" json:"category"`
	Protocols []*PlatformProtocol `xorm:"mediumtext" json:"protocols"`
	Charset   string              `xorm:"varchar(50)" json:"charset"`

	Elevation        string `xorm:"varchar(20)" json:"elevation"`
	ElevationCommand string `xorm:"varchar(100)" json:"elevationCommand"`

	Method         string `xorm:"varchar(50)" json:"method"`
	ChangeCommands string `xorm:"mediumtext" json:"changeCommands"`
	PushCommands   string `xorm:"mediumtext" json:"pushCommands"`
	PromptPattern  string `xorm:"varchar(500)" json:"promptPattern"`
	ErrorPattern   string `xorm:"varchar(500)" json:"errorPattern"`

	PasswordLength    int    `json:"passwordLength"`
	PasswordUppercase bool   `json:"passwordUppercase"`
	PasswordLowercase bool   `json:"passwordLowercase"`
	PasswordDigits    bool   `json:"passwordDigits"`
	PasswordSymbols   string `xorm:"varchar(100)" json:"passwordSymbols"`
	PasswordExclude   string `xorm:"varchar(100)" json:"passwordExclude"`

	CanChangePassword bool `json:"canChangePassword"`
	CanPush           bool `json:"canPush"`
	CanGather         bool `json:"canGather"`
}

type platformMethod struct {
	change, push, gather, lock bool
}

var platformMethods = map[string]platformMethod{
	"":                       {},
	PlatformMethodShell:      {true, true, true, true},
	PlatformMethodPowerShell: {true, true, true, true},
	PlatformMethodCli:        {true, true, false, false},
	ProtocolMysql:            {true, true, true, true},
	ProtocolPostgresql:       {true, true, true, true},
	ProtocolSqlServer:        {true, false, true, true},
	ProtocolOracle:           {true, false, true, true},
	ProtocolRedis:            {true, false, true, true},
	ProtocolMongodb:          {true, false, true, false},
}

var PlatformCharsets = []string{"UTF-8", "GB18030", "GBK", "Big5", "Shift_JIS", "EUC-JP", "EUC-KR", "ISO-8859-1", "windows-1252"}

func (platform *Platform) GetId() string {
	return util.GetIdFromOwnerAndName(platform.Owner, platform.Name)
}

func (platform *Platform) getMethod() platformMethod {
	return platformMethods[platform.Method]
}

func (platform *Platform) canChange() bool {
	return platform.CanChangePassword && platform.getMethod().change
}

func (platform *Platform) canPush() bool {
	return platform.CanPush && platform.getMethod().push
}

func (platform *Platform) canGather() bool {
	return platform.CanGather && platform.getMethod().gather
}

// reaches tells whether the method gets to the asset: the ones that sign in to a shell or a CLI need SSH.
func (platform *Platform) reaches(asset *Asset) bool {
	if platform.Method == PlatformMethodShell || platform.Method == PlatformMethodPowerShell || platform.Method == PlatformMethodCli {
		return strings.EqualFold(asset.Protocol, "SSH")
	}
	return true
}

func (platform *Platform) canLock() bool {
	return platform.getMethod().lock
}

func (platform *Platform) getDisplayName() string {
	if platform.DisplayName != "" {
		return platform.DisplayName
	}
	return platform.Name
}

func (platform *Platform) supportsProtocol(protocol string) bool {
	return slices.ContainsFunc(platform.Protocols, func(p *PlatformProtocol) bool { return p.Name == protocol })
}

func isBuiltInPlatform(name string) bool {
	return slices.ContainsFunc(builtInPlatforms, func(p *Platform) bool { return p.Name == name })
}

func getBuiltInPlatform(owner string, name string) *Platform {
	for _, p := range builtInPlatforms {
		if p.Name == name {
			res := *p
			res.Owner = owner
			res.IsBuiltIn = true
			res.Protocols = []*PlatformProtocol{}
			for _, protocol := range p.Protocols {
				copied := *protocol
				res.Protocols = append(res.Protocols, &copied)
			}
			return &res
		}
	}
	return nil
}

// getDefaultPlatformName is the built-in platform an asset of the protocol gets when none is chosen.
func getDefaultPlatformName(protocol string, os string) string {
	isWindows := strings.EqualFold(os, "Windows")
	switch protocol {
	case "SSH", "VNC":
		if isWindows {
			return "windows"
		}
		return "linux"
	case "RDP", ProtocolDesktop:
		return "windows"
	case "Telnet":
		return "linux"
	case ProtocolMysql:
		return "mysql"
	case ProtocolPostgresql:
		return "postgresql"
	case ProtocolSqlServer:
		return "sqlserver"
	case ProtocolOracle:
		return "oracle"
	case ProtocolRedis:
		return "redis"
	case ProtocolMongodb:
		return "mongodb"
	case ProtocolKubernetes:
		return "kubernetes"
	case ProtocolWeb:
		return "website"
	}
	return "linux"
}

// GetPlatforms returns the built-in platforms followed by the organization's own.
func GetPlatforms(owner string) ([]*Platform, error) {
	res := []*Platform{}
	for _, p := range builtInPlatforms {
		res = append(res, getBuiltInPlatform(owner, p.Name))
	}
	platforms := []*Platform{}
	err := adapter.engine.Desc("created_time").Find(&platforms, &Platform{Owner: owner})
	if err != nil {
		return nil, err
	}
	return append(res, platforms...), nil
}

// GetFilteredPlatforms is a page of the platforms whose name, display name, category or method contains value.
func GetFilteredPlatforms(owner string, value string, offset int, limit int) ([]*Platform, int, error) {
	platforms, err := GetPlatforms(owner)
	if err != nil {
		return nil, 0, err
	}
	value = strings.ToLower(strings.TrimSpace(value))
	res := []*Platform{}
	for _, p := range platforms {
		text := strings.ToLower(strings.Join([]string{p.Name, p.DisplayName, p.Category, p.Method}, " "))
		if value == "" || strings.Contains(text, value) {
			res = append(res, p)
		}
	}
	total := len(res)
	if offset >= 0 && limit > 0 {
		if offset > total {
			offset = total
		}
		res = res[offset:min(offset+limit, total)]
	}
	return res, total, nil
}

func getPlatform(owner string, name string) (*Platform, error) {
	if owner == "" || name == "" {
		return nil, nil
	}
	if builtIn := getBuiltInPlatform(owner, name); builtIn != nil {
		return builtIn, nil
	}
	platform := Platform{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&platform)
	if err != nil || !existed {
		return nil, err
	}
	return &platform, nil
}

func GetPlatform(id string) (*Platform, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	return getPlatform(owner, name)
}

// getAssetPlatform is the platform of the asset, or the default one of its protocol when it has none.
func getAssetPlatform(asset *Asset) (*Platform, error) {
	name := asset.Platform
	if name == "" {
		name = getDefaultPlatformName(asset.Protocol, asset.Os)
	}
	platform, err := getPlatform(asset.Owner, name)
	if err != nil {
		return nil, err
	}
	if platform == nil {
		return nil, fmt.Errorf("the platform: %s of the asset: %s does not exist", name, asset.Name)
	}
	return platform, nil
}

// GetAssetCharset is how the terminal of the asset encodes text, or nil for UTF-8.
func GetAssetCharset(asset *Asset) encoding.Encoding {
	platform, err := getAssetPlatform(asset)
	if err != nil || platform == nil {
		return nil
	}
	return getCharsetEncoding(platform.Charset)
}

func getCharsetEncoding(charset string) encoding.Encoding {
	if charset == "" || strings.EqualFold(charset, "UTF-8") {
		return nil
	}
	enc, err := htmlindex.Get(charset)
	if err != nil || enc == encoding.Nop {
		return nil
	}
	return enc
}

func (platform *Platform) check() error {
	platform.Name = strings.TrimSpace(platform.Name)
	if platform.Name == "" || strings.ContainsAny(platform.Name, "/@: \t") {
		return fmt.Errorf("the name of the platform can not be empty or contain \"/\", \"@\", \":\" or spaces")
	}
	if isBuiltInPlatform(platform.Name) {
		return fmt.Errorf("the name: %s is taken by a built-in platform", platform.Name)
	}

	if len(platform.Protocols) == 0 {
		return fmt.Errorf("the platform needs at least one protocol")
	}
	seen := map[string]bool{}
	for _, protocol := range platform.Protocols {
		name, err := normalizeAssetProtocol(protocol.Name)
		if err != nil {
			return err
		}
		if seen[name] {
			return fmt.Errorf("the protocol: %s appears twice", name)
		}
		seen[name] = true
		protocol.Name = name
		if protocol.Port < 0 || protocol.Port > 65535 {
			return fmt.Errorf("invalid port: %d of the protocol: %s", protocol.Port, name)
		}
	}

	if platform.Charset == "" {
		platform.Charset = "UTF-8"
	}
	if _, err := htmlindex.Get(platform.Charset); err != nil {
		return fmt.Errorf("unknown charset: %s", platform.Charset)
	}

	if !slices.Contains([]string{"", ElevationSudo, ElevationSu, ElevationEnable}, platform.Elevation) {
		return fmt.Errorf("unknown elevation: %s, it should be sudo, su or enable", platform.Elevation)
	}
	if platform.Elevation == ElevationSu || platform.Elevation == ElevationEnable {
		if strings.TrimSpace(platform.ElevationCommand) == "" {
			return fmt.Errorf("the elevation: %s needs a command, such as su - root or enable", platform.Elevation)
		}
	}

	method, ok := platformMethods[platform.Method]
	if !ok {
		return fmt.Errorf("unknown method: %s", platform.Method)
	}
	if platform.CanChangePassword && !method.change || platform.CanPush && !method.push || platform.CanGather && !method.gather {
		return fmt.Errorf("the method: %s can't do all the things the platform turns on", platform.Method)
	}
	if platform.Method == PlatformMethodCli {
		if strings.TrimSpace(platform.ChangeCommands) == "" {
			return fmt.Errorf("the CLI method needs the commands that change a password")
		}
		for _, pattern := range []string{platform.PromptPattern, platform.ErrorPattern} {
			if _, err := regexp.Compile(pattern); err != nil {
				return fmt.Errorf("invalid pattern: %s: %s", pattern, err.Error())
			}
		}
		if strings.TrimSpace(platform.PromptPattern) == "" {
			return fmt.Errorf("the CLI method needs the pattern of the prompt")
		}
	}

	if platform.PasswordLength < 8 || platform.PasswordLength > 128 {
		return fmt.Errorf("the length of the passwords should be between 8 and 128")
	}
	if len(platform.getPasswordGroups()) == 0 {
		return fmt.Errorf("the passwords need at least one kind of characters that is not all excluded")
	}
	return nil
}

func (platform *Platform) getPasswordGroups() []string {
	res := []string{}
	add := func(enabled bool, chars string) {
		if !enabled {
			return
		}
		group := strings.Map(func(r rune) rune {
			if r > 126 || r <= 32 || strings.ContainsRune(platform.PasswordExclude, r) {
				return -1
			}
			return r
		}, chars)
		if group != "" {
			res = append(res, group)
		}
	}
	add(platform.PasswordUppercase, "ABCDEFGHJKLMNPQRSTUVWXYZ")
	add(platform.PasswordLowercase, "abcdefghijkmnopqrstuvwxyz")
	add(platform.PasswordDigits, "23456789")
	add(true, platform.PasswordSymbols)
	return res
}

func randomIndex(n int) (int, error) {
	i, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(i.Int64()), nil
}

// generatePassword makes a random password by the rules of the platform, with at least one
// character of each kind it uses.
func (platform *Platform) generatePassword() (string, error) {
	groups := platform.getPasswordGroups()
	length := max(platform.PasswordLength, len(groups), 8)
	if len(groups) == 0 {
		return "", fmt.Errorf("the platform: %s has no characters for passwords", platform.Name)
	}

	res := []byte{}
	for _, group := range groups {
		i, err := randomIndex(len(group))
		if err != nil {
			return "", err
		}
		res = append(res, group[i])
	}
	all := strings.Join(groups, "")
	for len(res) < length {
		i, err := randomIndex(len(all))
		if err != nil {
			return "", err
		}
		res = append(res, all[i])
	}
	for i := len(res) - 1; i > 0; i-- {
		j, err := randomIndex(i + 1)
		if err != nil {
			return "", err
		}
		res[i], res[j] = res[j], res[i]
	}
	return string(res), nil
}

// checkPassword refuses the characters the method can't pass to the asset safely.
func (platform *Platform) checkPassword(password string) error {
	if password == "" {
		return fmt.Errorf("the password can not be empty")
	}
	if strings.ContainsFunc(password, func(r rune) bool { return r < 32 || r == 127 }) {
		return fmt.Errorf("the password can not contain line breaks or control characters")
	}
	switch {
	case IsDatabaseProtocol(platform.Method):
		if strings.ContainsAny(password, `'"\`) {
			return fmt.Errorf("a database password set by Casvisor can not contain quotes or backslashes")
		}
	case platform.Method == PlatformMethodCli:
		if strings.ContainsAny(password, ` ?"'`) {
			return fmt.Errorf("a network device password set by Casvisor can not contain spaces, quotes or \"?\"")
		}
	}
	return nil
}

func GetPlatformCount(owner string, value string) (int, error) {
	_, total, err := GetFilteredPlatforms(owner, value, -1, -1)
	return total, err
}

func AddPlatform(platform *Platform) (bool, error) {
	if platform.CreatedTime == "" {
		platform.CreatedTime = util.GetCurrentTime()
	}
	platform.UpdatedTime = util.GetCurrentTime()
	err := platform.check()
	if err != nil {
		return false, err
	}
	affected, err := adapter.engine.Insert(platform)
	return affected != 0, err
}

func UpdatePlatform(id string, platform *Platform) (bool, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	if isBuiltInPlatform(name) {
		return false, fmt.Errorf("the built-in platform: %s can't be changed, copy it and change the copy", name)
	}
	old, err := getPlatform(owner, name)
	if err != nil || old == nil {
		return false, err
	}

	platform.Owner = owner
	platform.CreatedTime = old.CreatedTime
	platform.UpdatedTime = util.GetCurrentTime()
	err = platform.check()
	if err != nil {
		return false, err
	}

	if platform.Name != name {
		existing, err := getPlatform(owner, platform.Name)
		if err != nil {
			return false, err
		}
		if existing != nil {
			return false, fmt.Errorf("the platform: %s already exists", platform.Name)
		}
	}

	_, err = adapter.engine.Transaction(func(session *xorm.Session) (interface{}, error) {
		_, err := session.ID(core.PK{owner, name}).AllCols().Update(platform)
		if err != nil || platform.Name == name {
			return nil, err
		}
		_, err = session.Where("owner = ? and platform = ?", owner, name).Cols("platform").Update(&Asset{Platform: platform.Name})
		return nil, err
	})
	return err == nil, err
}

// DeletePlatform deletes a platform that no asset uses.
func DeletePlatform(platform *Platform) (bool, error) {
	if isBuiltInPlatform(platform.Name) {
		return false, fmt.Errorf("the built-in platform: %s can't be deleted", platform.Name)
	}
	count, err := adapter.engine.Where("owner = ? and platform = ?", platform.Owner, platform.Name).Count(&Asset{})
	if err != nil {
		return false, err
	}
	if count != 0 {
		return false, fmt.Errorf("%d assets use the platform: %s, choose another platform for them first", count, platform.Name)
	}
	affected, err := adapter.engine.ID(core.PK{platform.Owner, platform.Name}).Delete(&Platform{})
	return affected != 0, err
}

// checkPlatform gives the asset the default platform of its protocol when it has none, and makes
// sure its platform exists and has its protocol.
func (asset *Asset) checkPlatform() error {
	if asset.Platform == "" {
		asset.Platform = getDefaultPlatformName(asset.Protocol, asset.Os)
	}
	platform, err := getPlatform(asset.Owner, asset.Platform)
	if err != nil {
		return err
	}
	if platform == nil {
		return fmt.Errorf("the platform: %s does not exist", asset.Platform)
	}
	if !platform.supportsProtocol(asset.Protocol) {
		return fmt.Errorf("the platform: %s does not have the protocol: %s", platform.getDisplayName(), asset.Protocol)
	}
	return nil
}

// migrateAssetPlatforms gives the assets saved before there were platforms the default platform of their protocol.
func migrateAssetPlatforms(engine *xorm.Engine) error {
	assets := []*Asset{}
	err := engine.Where("platform is null or platform = ''").Cols("owner", "name", "protocol", "os").Find(&assets)
	if err != nil {
		return err
	}
	for _, asset := range assets {
		_, err = engine.ID(core.PK{asset.Owner, asset.Name}).Cols("platform").Update(&Asset{Platform: getDefaultPlatformName(asset.Protocol, asset.Os)})
		if err != nil {
			return err
		}
	}
	return nil
}
