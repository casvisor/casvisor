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
	"sort"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/util"
	"golang.org/x/crypto/bcrypt"
	"xorm.io/core"
)

const (
	maxSigninFailures = 5
	signinLockTime    = 15 * time.Minute
	minPasswordLength = 8
)

// LocalUser is a user kept by Casvisor itself, for deployments without Casdoor.
type LocalUser struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	UpdatedTime string `xorm:"varchar(100)" json:"updatedTime"`
	DisplayName string `xorm:"varchar(100)" json:"displayName"`
	Email       string `xorm:"varchar(100)" json:"email"`

	Groups      []string `xorm:"mediumtext" json:"groups"`
	IsAdmin     bool     `json:"isAdmin"`
	IsForbidden bool     `json:"isForbidden"`

	// Password is only set by requests that change it; the hash never leaves the server
	Password       string `xorm:"-" json:"password"`
	PasswordHash   string `xorm:"varchar(100)" json:"-"`
	LastSigninTime string `xorm:"varchar(100)" json:"lastSigninTime"`
}

var (
	userNameRegex        = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)
	dummyPasswordHash, _ = bcrypt.GenerateFromPassword([]byte("casvisor"), bcrypt.DefaultCost)
)

// IsLocalAuth tells whether users sign in to Casvisor itself, which is the case
// when no Casdoor is configured.
func IsLocalAuth() bool {
	return conf.GetConfigString("casdoorEndpoint") == ""
}

func GetLocalOrganization() string {
	res := conf.GetConfigString("casdoorOrganization")
	if res == "" {
		res = "casvisor"
	}
	return res
}

func (user *LocalUser) GetId() string {
	return util.GetIdFromOwnerAndName(user.Owner, user.Name)
}

// ToCasdoorUser gives the user the shape the rest of Casvisor works with.
func (user *LocalUser) ToCasdoorUser() *casdoorsdk.User {
	groups := []string{}
	for _, group := range user.Groups {
		groups = append(groups, fmt.Sprintf("%s/%s", user.Owner, group))
	}

	return &casdoorsdk.User{
		Owner:       user.Owner,
		Name:        user.Name,
		DisplayName: user.DisplayName,
		Email:       user.Email,
		Groups:      groups,
		IsAdmin:     user.IsAdmin,
		IsForbidden: user.IsForbidden,
		CreatedTime: user.CreatedTime,
	}
}

func GetLocalUserCount(owner, field, value string) (int64, error) {
	session := GetSession(owner, -1, -1, field, value, "", "")
	return session.Count(&LocalUser{})
}

func GetLocalUsers(owner string) ([]*LocalUser, error) {
	users := []*LocalUser{}
	err := adapter.engine.Asc("name").Find(&users, &LocalUser{Owner: owner})
	if err != nil {
		return users, err
	}

	return users, nil
}

func GetPaginationLocalUsers(owner string, offset, limit int, field, value, sortField, sortOrder string) ([]*LocalUser, error) {
	users := []*LocalUser{}
	session := GetSession(owner, offset, limit, field, value, sortField, sortOrder)
	err := session.Find(&users)
	if err != nil {
		return users, err
	}

	return users, nil
}

func getLocalUser(owner string, name string) (*LocalUser, error) {
	if owner == "" || name == "" {
		return nil, nil
	}

	user := LocalUser{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&user)
	if err != nil {
		return nil, err
	}

	if existed {
		return &user, nil
	}
	return nil, nil
}

func GetLocalUser(id string) (*LocalUser, error) {
	owner, name := util.GetOwnerAndNameFromIdNoCheck(id)
	return getLocalUser(owner, name)
}

// GetLocalGroups returns the groups the users of the organization are in.
func GetLocalGroups(owner string) ([]string, error) {
	users, err := GetLocalUsers(owner)
	if err != nil {
		return nil, err
	}

	set := map[string]bool{}
	for _, user := range users {
		for _, group := range user.Groups {
			set[group] = true
		}
	}

	res := []string{}
	for group := range set {
		res = append(res, group)
	}
	sort.Strings(res)
	return res, nil
}

func checkPassword(password string) error {
	if len(password) < minPasswordLength {
		return fmt.Errorf("the password should have at least %d characters", minPasswordLength)
	}
	return nil
}

func (user *LocalUser) setPassword(password string) error {
	err := checkPassword(password)
	if err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user.PasswordHash = string(hash)
	return nil
}

func AddLocalUser(user *LocalUser) (bool, error) {
	if !userNameRegex.MatchString(user.Name) {
		return false, fmt.Errorf("invalid user name: %s, use letters, digits, \".\", \"_\" and \"-\"", user.Name)
	}

	err := user.setPassword(user.Password)
	if err != nil {
		return false, err
	}

	user.CreatedTime = util.GetCurrentTime()
	user.UpdatedTime = user.CreatedTime
	affected, err := adapter.engine.Insert(user)
	if err != nil {
		return false, err
	}

	return affected != 0, nil
}

// UpdateLocalUser changes the profile, and the password when one is given.
func UpdateLocalUser(id string, user *LocalUser) (bool, error) {
	old, err := GetLocalUser(id)
	if err != nil {
		return false, err
	}
	if old == nil {
		return false, nil
	}

	if old.IsAdmin && (!user.IsAdmin || user.IsForbidden) {
		err = checkOtherAdmin(old)
		if err != nil {
			return false, err
		}
	}

	columns := []string{"display_name", "email", "groups", "is_admin", "is_forbidden", "updated_time"}
	if user.Password != "" {
		err = user.setPassword(user.Password)
		if err != nil {
			return false, err
		}
		columns = append(columns, "password_hash")
		defer clearSigninFailures(old.GetId())
	}

	user.UpdatedTime = util.GetCurrentTime()
	affected, err := adapter.engine.ID(core.PK{old.Owner, old.Name}).Cols(columns...).Update(user)
	if err != nil {
		return false, err
	}

	return affected != 0, nil
}

func DeleteLocalUser(user *LocalUser) (bool, error) {
	old, err := getLocalUser(user.Owner, user.Name)
	if err != nil {
		return false, err
	}
	if old == nil {
		return false, nil
	}

	if old.IsAdmin {
		err = checkOtherAdmin(old)
		if err != nil {
			return false, err
		}
	}

	affected, err := adapter.engine.ID(core.PK{old.Owner, old.Name}).Delete(&LocalUser{})
	if err != nil {
		return false, err
	}

	return affected != 0, nil
}

// checkOtherAdmin keeps at least one administrator who can sign in.
func checkOtherAdmin(user *LocalUser) error {
	count, err := adapter.engine.Where("owner = ? and name <> ? and is_admin = ? and is_forbidden = ?", user.Owner, user.Name, true, false).Count(&LocalUser{})
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("the user: %s is the last administrator", user.Name)
	}
	return nil
}

// SigninLocalUser checks the password. Failures are counted for the user from each
// client IP, so that nobody can lock an administrator out from elsewhere.
func SigninLocalUser(owner string, name string, password string, clientIp string) (*LocalUser, error) {
	userId := util.GetIdFromOwnerAndName(owner, name)
	lockedUntil := getSigninLock(userId, clientIp)
	if !lockedUntil.IsZero() {
		return nil, fmt.Errorf("too many failed attempts, try again after %s", lockedUntil.Format("15:04:05"))
	}

	user, err := getLocalUser(owner, name)
	if err != nil {
		return nil, err
	}

	wrongPassword := fmt.Errorf("wrong user name or password")
	if user == nil {
		// spend the same time as for a real user, so that user names can't be told apart by timing
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(password))
		addSigninFailure(userId, clientIp)
		return nil, wrongPassword
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		addSigninFailure(userId, clientIp)
		return nil, wrongPassword
	}

	if user.IsForbidden {
		return nil, fmt.Errorf("the user: %s is forbidden to sign in", user.Name)
	}

	clearSigninFailures(userId)
	user.LastSigninTime = util.GetCurrentTime()
	_, err = adapter.engine.ID(core.PK{user.Owner, user.Name}).Cols("last_signin_time").Update(user)
	if err != nil {
		return nil, err
	}
	return user, nil
}

// ChangeLocalUserPassword lets users change their own password.
func ChangeLocalUserPassword(owner string, name string, oldPassword string, newPassword string) error {
	user, err := getLocalUser(owner, name)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("the user: %s/%s does not exist", owner, name)
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword)) != nil {
		return fmt.Errorf("the old password is wrong")
	}

	err = user.setPassword(newPassword)
	if err != nil {
		return err
	}

	user.UpdatedTime = util.GetCurrentTime()
	_, err = adapter.engine.ID(core.PK{owner, name}).Cols("password_hash", "updated_time").Update(user)
	return err
}

func generatePassword() (string, error) {
	const letters = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	res := make([]byte, 16)
	for i := range res {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			return "", err
		}
		res[i] = letters[n.Int64()]
	}
	return string(res), nil
}

// InitLocalAdmin creates the first administrator, "admin", when users sign in to
// Casvisor itself and there is no user yet. The password is adminPassword in the
// config or environment, or a random one that is printed once in the log.
func InitLocalAdmin() error {
	if !IsLocalAuth() {
		return nil
	}

	owner := GetLocalOrganization()
	count, err := adapter.engine.Count(&LocalUser{Owner: owner})
	if err != nil {
		return err
	}
	if count != 0 {
		return nil
	}

	password := conf.GetConfigString("adminPassword")
	generated := password == ""
	if generated {
		password, err = generatePassword()
		if err != nil {
			return err
		}
	}

	_, err = AddLocalUser(&LocalUser{
		Owner:       owner,
		Name:        "admin",
		DisplayName: "Admin",
		IsAdmin:     true,
		Password:    password,
	})
	if err != nil {
		return err
	}

	if generated {
		message := fmt.Sprintf("Casvisor created the administrator \"admin\" with the password: %s , sign in and change it in My Account", password)
		logs.Warning(message)
		fmt.Printf("\n%s\n%s\n%s\n\n", "========================================================================", message, "========================================================================")
	} else {
		logs.Info("Casvisor created the administrator \"admin\" with the password from adminPassword")
	}
	return nil
}
