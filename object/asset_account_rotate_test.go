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
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"xorm.io/xorm"
)

type fakeDatabaseRotator struct {
	queryResults map[string][][]string
	queryErrs    map[string]error
	execErr      error
	queries      []string
	queryArgs    [][]interface{}
	executed     []string
}

func (f *fakeDatabaseRotator) queryStrings(query string, args ...interface{}) ([][]string, error) {
	f.queries = append(f.queries, query)
	f.queryArgs = append(f.queryArgs, args)
	if err := f.queryErrs[query]; err != nil {
		return nil, err
	}
	return f.queryResults[query], nil
}

func (f *fakeDatabaseRotator) exec(statement string) error {
	f.executed = append(f.executed, statement)
	return f.execErr
}

func newRotateTestDB(t *testing.T) *xorm.Engine {
	t.Helper()
	engine := newPermissionTestDB(t)
	if err := engine.Sync2(new(Asset), new(Record)); err != nil {
		t.Fatalf("create rotate test tables: %v", err)
	}
	return engine
}

func TestCanRotateAccountProtocol(t *testing.T) {
	tests := []struct {
		protocol string
		want     bool
	}{
		{"SSH", true},
		{"ssh", true},
		{"Ssh", true},
		{ProtocolMysql, true},
		{ProtocolPostgresql, true},
		{"RDP", false},
		{"VNC", false},
		{"Telnet", false},
		{ProtocolKubernetes, false},
		{ProtocolDesktop, false},
		{"", false},
		{"mysql", false},
		{"postgres", false},
	}
	for _, tt := range tests {
		if got := canRotateAccountProtocol(tt.protocol); got != tt.want {
			t.Errorf("canRotateAccountProtocol(%q) = %t, want %t", tt.protocol, got, tt.want)
		}
	}
}

func TestBuildMysqlAccountPasswordSql(t *testing.T) {
	tests := []struct {
		name               string
		username           string
		host               string
		password           string
		create             bool
		noBackslashEscapes bool
		want               string
		wantErr            bool
	}{
		{
			name:     "alter normal account",
			username: "app_ro",
			host:     "%",
			password: "Secret123!#%",
			want:     "ALTER USER 'app_ro'@'%' IDENTIFIED BY 'Secret123!#%'",
		},
		{
			name:     "create normal account",
			username: "app_ro",
			host:     "%",
			password: "Secret123!#%",
			create:   true,
			want:     "CREATE USER 'app_ro'@'%' IDENTIFIED BY 'Secret123!#%'",
		},
		{
			name:     "escapes single quotes and backslashes",
			username: `a'b\c`,
			host:     `10.'%\`,
			password: `p'w\end\`,
			want:     `ALTER USER 'a''b\\c'@'10.''%\\' IDENTIFIED BY 'p''w\\end\\'`,
		},
		{
			name:               "no backslash escapes mode preserves backslashes",
			username:           `a'b\c`,
			host:               `%`,
			password:           `p'w\end\`,
			noBackslashEscapes: true,
			want:               `ALTER USER 'a''b\c'@'%' IDENTIFIED BY 'p''w\end\'`,
		},
		{
			name:     "backticks and sql injection payload stay quoted",
			username: "u`name'; DROP USER 'root'@'localhost'; --",
			host:     "localhost",
			password: `x\' ; GRANT ALL ON *.* TO 'evil'@'%'; --`,
			want:     `ALTER USER 'u` + "`" + `name''; DROP USER ''root''@''localhost''; --'@'localhost' IDENTIFIED BY 'x\\'' ; GRANT ALL ON *.* TO ''evil''@''%''; --'`,
		},
		{
			name:     "empty host is allowed for legacy mysql rows",
			username: "legacy",
			host:     "",
			password: "Secret123!",
			want:     "ALTER USER 'legacy'@'' IDENTIFIED BY 'Secret123!'",
		},
		{name: "empty username", username: "", host: "%", password: "Secret123!", wantErr: true},
		{name: "empty password", username: "app", host: "%", password: "", wantErr: true},
		{name: "null byte in username", username: "app\x00", host: "%", password: "Secret123!", wantErr: true},
		{name: "newline in password", username: "app", host: "%", password: "a\nb", wantErr: true},
		{name: "carriage return in host", username: "app", host: "%\r", password: "Secret123!", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildMysqlAccountPasswordSql(tt.username, tt.host, tt.password, tt.create, tt.noBackslashEscapes)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("buildMysqlAccountPasswordSql = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("buildMysqlAccountPasswordSql error: %v", err)
			}
			if got != tt.want {
				t.Errorf("buildMysqlAccountPasswordSql =\n  %s\nwant:\n  %s", got, tt.want)
			}
		})
	}
}

func TestBuildPostgresqlAccountPasswordSql(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		create   bool
		want     string
		wantErr  bool
	}{
		{
			name:     "alter normal role",
			username: "app_ro",
			password: "Secret123!#%",
			want:     `ALTER ROLE "app_ro" WITH PASSWORD 'Secret123!#%'`,
		},
		{
			name:     "create normal role with login",
			username: "app_ro",
			password: "Secret123!#%",
			create:   true,
			want:     `CREATE ROLE "app_ro" WITH LOGIN PASSWORD 'Secret123!#%'`,
		},
		{
			name:     "uppercase keyword and punctuation role",
			username: "Select-User.1",
			password: "p'ss''word",
			want:     `ALTER ROLE "Select-User.1" WITH PASSWORD 'p''ss''''word'`,
		},
		{
			name:     "backslashes use escape string literal",
			username: "app",
			password: `a\b'c\`,
			want:     `ALTER ROLE "app" WITH PASSWORD E'a\\b''c\\'`,
		},
		{
			name:     "double quotes and sql injection payload stay quoted",
			username: `admin" WITH SUPERUSER; --`,
			password: `x\'; ALTER ROLE postgres SUPERUSER; --`,
			want:     `ALTER ROLE "admin"" WITH SUPERUSER; --" WITH PASSWORD E'x\\''; ALTER ROLE postgres SUPERUSER; --'`,
		},
		{name: "empty username", username: "", password: "Secret123!", wantErr: true},
		{name: "empty password", username: "app", password: "", wantErr: true},
		{name: "null byte in username rejected before sanitize", username: "admin\x00", password: "Secret123!", wantErr: true},
		{name: "null byte in password", username: "app", password: "a\x00b", wantErr: true},
		{name: "newline in username", username: "app\n", password: "Secret123!", wantErr: true},
		{name: "newline in password", username: "app", password: "a\rb", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildPostgresqlAccountPasswordSql(tt.username, tt.password, tt.create)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("buildPostgresqlAccountPasswordSql = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("buildPostgresqlAccountPasswordSql error: %v", err)
			}
			if got != tt.want {
				t.Errorf("buildPostgresqlAccountPasswordSql =\n  %s\nwant:\n  %s", got, tt.want)
			}
		})
	}
}

func TestSetMysqlAccountPassword(t *testing.T) {
	const userQuery = "SELECT user, host FROM mysql.user WHERE user = ? ORDER BY host"
	const modeQuery = "SELECT @@sql_mode"

	t.Run("rotates all matching hosts", func(t *testing.T) {
		db := &fakeDatabaseRotator{
			queryResults: map[string][][]string{
				userQuery: {{"app", "%"}, {"app", "localhost"}},
				modeQuery: {{"STRICT_TRANS_TABLES"}},
			},
		}
		if err := setMysqlAccountPassword(db, "app", `new'pass\`, false); err != nil {
			t.Fatalf("setMysqlAccountPassword: %v", err)
		}
		want := []string{
			`ALTER USER 'app'@'%' IDENTIFIED BY 'new''pass\\'`,
			`ALTER USER 'app'@'localhost' IDENTIFIED BY 'new''pass\\'`,
		}
		if !slices.Equal(db.executed, want) {
			t.Errorf("executed = %v, want %v", db.executed, want)
		}
	})

	t.Run("honors NO_BACKSLASH_ESCAPES sql_mode", func(t *testing.T) {
		db := &fakeDatabaseRotator{
			queryResults: map[string][][]string{
				userQuery: {{"app", "%"}},
				modeQuery: {{"STRICT_TRANS_TABLES,NO_BACKSLASH_ESCAPES"}},
			},
		}
		if err := setMysqlAccountPassword(db, "app", `p'w\`, false); err != nil {
			t.Fatalf("setMysqlAccountPassword: %v", err)
		}
		want := []string{`ALTER USER 'app'@'%' IDENTIFIED BY 'p''w\'`}
		if !slices.Equal(db.executed, want) {
			t.Errorf("executed = %v, want %v", db.executed, want)
		}
	})

	t.Run("rotate fails when account missing", func(t *testing.T) {
		db := &fakeDatabaseRotator{queryResults: map[string][][]string{}}
		err := setMysqlAccountPassword(db, "ghost", "Secret123!", false)
		if err == nil || !strings.Contains(err.Error(), "does not exist") {
			t.Fatalf("err = %v, want does not exist", err)
		}
		if len(db.executed) != 0 {
			t.Errorf("executed %v statements for missing user", db.executed)
		}
	})

	t.Run("push creates missing account and alters existing account", func(t *testing.T) {
		dbMissing := &fakeDatabaseRotator{queryResults: map[string][][]string{}}
		if err := setMysqlAccountPassword(dbMissing, "newuser", "Secret123!", true); err != nil {
			t.Fatalf("push missing: %v", err)
		}
		if want := []string{"CREATE USER 'newuser'@'%' IDENTIFIED BY 'Secret123!'"}; !slices.Equal(dbMissing.executed, want) {
			t.Errorf("push missing executed = %v, want %v", dbMissing.executed, want)
		}

		dbExisting := &fakeDatabaseRotator{
			queryResults: map[string][][]string{
				userQuery: {{"existing", "10.0.%"}},
			},
		}
		if err := setMysqlAccountPassword(dbExisting, "existing", "Secret123!", true); err != nil {
			t.Fatalf("push existing: %v", err)
		}
		if want := []string{"ALTER USER 'existing'@'10.0.%' IDENTIFIED BY 'Secret123!'"}; !slices.Equal(dbExisting.executed, want) {
			t.Errorf("push existing executed = %v, want %v", dbExisting.executed, want)
		}
	})

	t.Run("redacts password from database errors", func(t *testing.T) {
		db := &fakeDatabaseRotator{
			queryResults: map[string][][]string{userQuery: {{"app", "%"}}},
			execErr:      errors.New("syntax error near 'TopSecret99!'"),
		}
		err := setMysqlAccountPassword(db, "app", "TopSecret99!", false)
		if err == nil || strings.Contains(err.Error(), "TopSecret99!") || !strings.Contains(err.Error(), "***") {
			t.Fatalf("err = %v, want password redacted", err)
		}
	})

	t.Run("propagates mysql.user lookup errors", func(t *testing.T) {
		db := &fakeDatabaseRotator{
			queryErrs: map[string]error{userQuery: errors.New("SELECT command denied")},
		}
		err := setMysqlAccountPassword(db, "app", "Secret123!", false)
		if err == nil || !strings.Contains(err.Error(), "SELECT command denied") {
			t.Fatalf("err = %v, want lookup error", err)
		}
	})
}

func TestSetPostgresqlAccountPassword(t *testing.T) {
	const roleQuery = "SELECT 1 FROM pg_roles WHERE rolname = $1"

	t.Run("rotates existing role", func(t *testing.T) {
		db := &fakeDatabaseRotator{
			queryResults: map[string][][]string{roleQuery: {{"1"}}},
		}
		if err := setPostgresqlAccountPassword(db, "app_ro", "Secret123!", false); err != nil {
			t.Fatalf("setPostgresqlAccountPassword: %v", err)
		}
		want := []string{`ALTER ROLE "app_ro" WITH PASSWORD 'Secret123!'`}
		if !slices.Equal(db.executed, want) {
			t.Errorf("executed = %v, want %v", db.executed, want)
		}
	})

	t.Run("rotate fails when role missing", func(t *testing.T) {
		db := &fakeDatabaseRotator{queryResults: map[string][][]string{}}
		err := setPostgresqlAccountPassword(db, "ghost", "Secret123!", false)
		if err == nil || !strings.Contains(err.Error(), "does not exist") {
			t.Fatalf("err = %v, want does not exist", err)
		}
		if len(db.executed) != 0 {
			t.Errorf("executed %v statements for missing role", db.executed)
		}
	})

	t.Run("push creates missing role and alters existing role", func(t *testing.T) {
		dbMissing := &fakeDatabaseRotator{queryResults: map[string][][]string{}}
		if err := setPostgresqlAccountPassword(dbMissing, "new_role", `p'w\1`, true); err != nil {
			t.Fatalf("push missing: %v", err)
		}
		wantCreate := []string{`CREATE ROLE "new_role" WITH LOGIN PASSWORD E'p''w\\1'`}
		if !slices.Equal(dbMissing.executed, wantCreate) {
			t.Errorf("push missing executed = %v, want %v", dbMissing.executed, wantCreate)
		}

		dbExisting := &fakeDatabaseRotator{
			queryResults: map[string][][]string{roleQuery: {{"1"}}},
		}
		if err := setPostgresqlAccountPassword(dbExisting, "existing_role", "Secret123!", true); err != nil {
			t.Fatalf("push existing: %v", err)
		}
		wantAlter := []string{`ALTER ROLE "existing_role" WITH PASSWORD 'Secret123!'`}
		if !slices.Equal(dbExisting.executed, wantAlter) {
			t.Errorf("push existing executed = %v, want %v", dbExisting.executed, wantAlter)
		}
	})

	t.Run("redacts password from database errors", func(t *testing.T) {
		db := &fakeDatabaseRotator{
			queryResults: map[string][][]string{roleQuery: {{"1"}}},
			execErr:      errors.New("failed to set TopSecret99!"),
		}
		err := setPostgresqlAccountPassword(db, "app", "TopSecret99!", false)
		if err == nil || strings.Contains(err.Error(), "TopSecret99!") || !strings.Contains(err.Error(), "***") {
			t.Fatalf("err = %v, want password redacted", err)
		}
	})

	t.Run("propagates pg_roles lookup errors", func(t *testing.T) {
		db := &fakeDatabaseRotator{
			queryErrs: map[string]error{roleQuery: errors.New("permission denied")},
		}
		err := setPostgresqlAccountPassword(db, "app", "Secret123!", false)
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("err = %v, want lookup error", err)
		}
	})
}

func TestAssetAccountAutoRotateCheck(t *testing.T) {
	engine := newRotateTestDB(t)
	insertPermissionTestRows(t, engine,
		&Asset{Owner: "org1", Name: "ssh1", Protocol: "SSH", Username: "root"},
		&Asset{Owner: "org1", Name: "mysql1", Protocol: ProtocolMysql, Username: "root"},
		&Asset{Owner: "org1", Name: "pg1", Protocol: ProtocolPostgresql, Username: "postgres"},
		&Asset{Owner: "org1", Name: "rdp1", Protocol: "RDP", Username: "Administrator"},
		&Asset{Owner: "org1", Name: "k8s1", Protocol: ProtocolKubernetes, Username: "cluster"},
	)

	for _, assetName := range []string{"ssh1", "mysql1", "pg1"} {
		acc := &AssetAccount{Owner: "org1", Name: "acc-" + assetName, Asset: assetName, Username: "app", Password: "secret", AutoRotate: true}
		if err := acc.check(); err != nil {
			t.Errorf("AutoRotate check for %s failed: %v", assetName, err)
		}
		if acc.RotateIntervalDays != 30 {
			t.Errorf("AutoRotate check for %s set RotateIntervalDays = %d, want 30", assetName, acc.RotateIntervalDays)
		}
	}
	for _, assetName := range []string{"rdp1", "k8s1"} {
		acc := &AssetAccount{Owner: "org1", Name: "acc-" + assetName, Asset: assetName, Username: "app", Password: "secret", AutoRotate: true}
		if err := acc.check(); err == nil {
			t.Errorf("AutoRotate check for %s succeeded, want error", assetName)
		}
	}
}

func TestRotateAssetAccountStateTransitions(t *testing.T) {
	engine := newRotateTestDB(t)
	insertPermissionTestRows(t, engine,
		&Asset{Owner: "org1", Name: "mysql1", Protocol: ProtocolMysql, Username: "root", Password: "adminpass"},
		&Asset{Owner: "org1", Name: "pg1", Protocol: ProtocolPostgresql, Username: "postgres", Password: "adminpass"},
		&Asset{Owner: "org1", Name: "rdp1", Protocol: "RDP", Username: "Administrator", Password: "adminpass"},
	)

	origSet := setAccountPasswordFunc
	origVerify := verifyAccountPasswordFunc
	t.Cleanup(func() {
		setAccountPasswordFunc = origSet
		verifyAccountPasswordFunc = origVerify
	})

	t.Run("mysql rotate succeeds and persists new password", func(t *testing.T) {
		acc := &AssetAccount{Owner: "org1", Name: "m1", Asset: "mysql1", Username: "app", Password: "old-password", IsEnabled: true}
		if _, err := AddAssetAccount(acc); err != nil {
			t.Fatalf("AddAssetAccount: %v", err)
		}

		var gotSetPassword, gotVerifyPassword string
		var gotCreate bool
		setAccountPasswordFunc = func(asset *Asset, username string, password string, create bool) error {
			gotSetPassword = password
			gotCreate = create
			return nil
		}
		verifyAccountPasswordFunc = func(asset *Asset, username string, password string) error {
			gotVerifyPassword = password
			return nil
		}

		masked, err := RotateAssetAccount("org1/m1", false)
		if err != nil {
			t.Fatalf("RotateAssetAccount: %v", err)
		}
		if gotCreate {
			t.Errorf("create = true on rotate, want false")
		}
		if gotSetPassword == "" || gotSetPassword == "old-password" || gotSetPassword != gotVerifyPassword {
			t.Errorf("rotated password mismatch: set=%q verify=%q", gotSetPassword, gotVerifyPassword)
		}
		if masked.Password != "***" || masked.RotateStatus != AccountRotateSucceeded || masked.LastRotatedTime == "" {
			t.Errorf("masked = %+v", masked)
		}
		stored, err := GetAssetAccount("org1/m1")
		if err != nil {
			t.Fatalf("GetAssetAccount: %v", err)
		}
		if stored.Password != gotSetPassword {
			t.Errorf("stored password = %q, want %q", stored.Password, gotSetPassword)
		}
	})

	t.Run("postgresql rotate preserves new password when verification fails", func(t *testing.T) {
		acc := &AssetAccount{Owner: "org1", Name: "p1", Asset: "pg1", Username: "app", Password: "old-password", IsEnabled: true}
		if _, err := AddAssetAccount(acc); err != nil {
			t.Fatalf("AddAssetAccount: %v", err)
		}

		var rotatedPassword string
		setAccountPasswordFunc = func(asset *Asset, username string, password string, create bool) error {
			rotatedPassword = password
			return nil
		}
		verifyAccountPasswordFunc = func(asset *Asset, username string, password string) error {
			return errors.New("pg_hba rejected password " + password)
		}

		_, err := RotateAssetAccount("org1/p1", false)
		if err == nil || !strings.Contains(err.Error(), "signing in with it failed") {
			t.Fatalf("err = %v, want verify failure", err)
		}
		if strings.Contains(err.Error(), rotatedPassword) {
			t.Errorf("error leaked rotated password: %v", err)
		}
		stored, err := GetAssetAccount("org1/p1")
		if err != nil {
			t.Fatalf("GetAssetAccount: %v", err)
		}
		if stored.Password != rotatedPassword {
			t.Errorf("stored password = %q, want rotated password %q preserved", stored.Password, rotatedPassword)
		}
		if stored.RotateStatus != AccountRotateFailed || stored.LastRotatedTime == "" {
			t.Errorf("stored status=%q lastRotated=%q", stored.RotateStatus, stored.LastRotatedTime)
		}
	})

	t.Run("rotate keeps old password when remote change fails", func(t *testing.T) {
		acc := &AssetAccount{Owner: "org1", Name: "m2", Asset: "mysql1", Username: "app2", Password: "keep-me", IsEnabled: true}
		if _, err := AddAssetAccount(acc); err != nil {
			t.Fatalf("AddAssetAccount: %v", err)
		}

		setAccountPasswordFunc = func(asset *Asset, username string, password string, create bool) error {
			return errors.New("access denied for password " + password)
		}
		verifyCalled := false
		verifyAccountPasswordFunc = func(asset *Asset, username string, password string) error {
			verifyCalled = true
			return nil
		}

		_, err := RotateAssetAccount("org1/m2", false)
		if err == nil || !strings.Contains(err.Error(), "***") {
			t.Fatalf("err = %v, want redacted error", err)
		}
		if verifyCalled {
			t.Errorf("verifyAccountPassword should not run when setAccountPassword fails")
		}
		stored, err := GetAssetAccount("org1/m2")
		if err != nil {
			t.Fatalf("GetAssetAccount: %v", err)
		}
		if stored.Password != "keep-me" || stored.RotateStatus != AccountRotateFailed || stored.LastRotatedTime != "" {
			t.Errorf("stored = %+v, want password preserved and status Failed", stored)
		}
	})

	t.Run("push passes create true and only stamps LastRotatedTime on success", func(t *testing.T) {
		acc := &AssetAccount{Owner: "org1", Name: "p2", Asset: "pg1", Username: "pushuser", Password: "saved-password", IsEnabled: true}
		if _, err := AddAssetAccount(acc); err != nil {
			t.Fatalf("AddAssetAccount: %v", err)
		}

		var gotCreate bool
		setAccountPasswordFunc = func(asset *Asset, username string, password string, create bool) error {
			gotCreate = create
			return nil
		}
		verifyAccountPasswordFunc = func(asset *Asset, username string, password string) error {
			return errors.New("connect failed")
		}

		if _, err := RotateAssetAccount("org1/p2", true); err == nil {
			t.Fatal("push with verify failure succeeded, want error")
		}
		if !gotCreate {
			t.Errorf("create = false on push, want true")
		}
		stored, err := GetAssetAccount("org1/p2")
		if err != nil {
			t.Fatalf("GetAssetAccount: %v", err)
		}
		if stored.Password != "saved-password" || stored.LastRotatedTime != "" || stored.RotateStatus != AccountRotateFailed {
			t.Errorf("stored after failed push = %+v", stored)
		}

		verifyAccountPasswordFunc = func(asset *Asset, username string, password string) error {
			return nil
		}
		if _, err := RotateAssetAccount("org1/p2", true); err != nil {
			t.Fatalf("push retry: %v", err)
		}
		stored, err = GetAssetAccount("org1/p2")
		if err != nil {
			t.Fatalf("GetAssetAccount: %v", err)
		}
		if stored.Password != "saved-password" || stored.LastRotatedTime == "" || stored.RotateStatus != AccountRotateSucceeded {
			t.Errorf("stored after successful push = %+v", stored)
		}
	})

	t.Run("unsupported protocol fails before mutating status", func(t *testing.T) {
		acc := &AssetAccount{Owner: "org1", Name: "r1", Asset: "rdp1", Username: "user1", Password: "secret", IsEnabled: true}
		if _, err := AddAssetAccount(acc); err != nil {
			t.Fatalf("AddAssetAccount: %v", err)
		}

		called := false
		setAccountPasswordFunc = func(asset *Asset, username string, password string, create bool) error {
			called = true
			return nil
		}

		_, err := RotateAssetAccount("org1/r1", false)
		if err == nil || !strings.Contains(err.Error(), "SSH, MySQL and PostgreSQL") {
			t.Fatalf("err = %v, want protocol error", err)
		}
		if called {
			t.Errorf("setAccountPassword should not run for unsupported protocol")
		}
		stored, err := GetAssetAccount("org1/r1")
		if err != nil {
			t.Fatalf("GetAssetAccount: %v", err)
		}
		if stored.RotateStatus != "" {
			t.Errorf("RotateStatus = %q, want empty", stored.RotateStatus)
		}
	})

	t.Run("rejects concurrent rotation of same account", func(t *testing.T) {
		rotatingAccounts.Store("org1/m1", true)
		t.Cleanup(func() {
			rotatingAccounts.Delete("org1/m1")
		})
		_, err := RotateAssetAccount("org1/m1", false)
		if err == nil || !strings.Contains(err.Error(), "is being changed") {
			t.Fatalf("err = %v, want concurrent rotation error", err)
		}
	})
}

func TestIsAccountRotationDue(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-10 * 24 * time.Hour).Format(time.RFC3339)
	expired := now.Add(-31 * 24 * time.Hour).Format(time.RFC3339)

	tests := []struct {
		name    string
		account AssetAccount
		want    bool
	}{
		{"disabled auto rotate", AssetAccount{AutoRotate: false, IsEnabled: true, Password: "p"}, false},
		{"disabled account", AssetAccount{AutoRotate: true, IsEnabled: false, Password: "p"}, false},
		{"empty password", AssetAccount{AutoRotate: true, IsEnabled: true}, false},
		{"has private key", AssetAccount{AutoRotate: true, IsEnabled: true, Password: "p", PrivateKey: "k"}, false},
		{"never rotated", AssetAccount{AutoRotate: true, IsEnabled: true, Password: "p", RotateIntervalDays: 30}, true},
		{"malformed timestamp", AssetAccount{AutoRotate: true, IsEnabled: true, Password: "p", RotateIntervalDays: 30, LastRotatedTime: "bad"}, true},
		{"not yet due", AssetAccount{AutoRotate: true, IsEnabled: true, Password: "p", RotateIntervalDays: 30, LastRotatedTime: recent}, false},
		{"due", AssetAccount{AutoRotate: true, IsEnabled: true, Password: "p", RotateIntervalDays: 30, LastRotatedTime: expired}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAccountRotationDue(&tt.account, now); got != tt.want {
				t.Errorf("isAccountRotationDue = %t, want %t", got, tt.want)
			}
		})
	}
}
