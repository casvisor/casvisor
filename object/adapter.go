// Copyright 2023 The casbin Authors. All Rights Reserved.
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
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/beego/beego"
	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/util"
	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

var adapter *Adapter

func InitConfig() {
	err := beego.LoadAppConfig("ini", "../conf/app.conf")
	if err != nil {
		panic(err)
	}

	InitAdapter()
}

func InitAdapter() {
	adapter = NewAdapter(conf.GetConfigString("driverName"), conf.GetConfigDataSourceName())
}

// Adapter represents the MySQL adapter for policy storage.
type Adapter struct {
	driverName     string
	dataSourceName string
	engine         *xorm.Engine
}

// finalizer is the destructor for Adapter.
func finalizer(a *Adapter) {
	err := a.engine.Close()
	if err != nil {
		panic(err)
	}
}

// NewAdapter is the constructor for Adapter.
func NewAdapter(driverName string, dataSourceName string) *Adapter {
	a := &Adapter{}
	a.driverName = driverName
	a.dataSourceName = dataSourceName

	// Open the DB, create it if not existed.
	a.open()

	// Call the destructor when the object is released.
	runtime.SetFinalizer(a, finalizer)

	return a
}

func (a *Adapter) createDatabase() error {
	engine, err := xorm.NewEngine(a.driverName, a.dataSourceName)
	if err != nil {
		return err
	}
	defer engine.Close()

	_, err = engine.Exec(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s default charset utf8 COLLATE utf8_general_ci", conf.GetConfigString("dbName")))
	return err
}

func (a *Adapter) open() {
	if IsSqlite(a.driverName) {
		a.openSqlite()
		return
	}

	if err := a.createDatabase(); err != nil {
		panic(err)
	}

	engine, err := xorm.NewEngine(a.driverName, a.dataSourceName+conf.GetConfigString("dbName"))
	if err != nil {
		panic(err)
	}

	a.engine = engine
	a.createTable()
}

func IsSqlite(driverName string) bool {
	return driverName == "sqlite" || driverName == "sqlite3"
}

// GetSqliteDataSourceName waits for a locked database instead of failing at once,
// and lets readers go on while a write is in progress.
func GetSqliteDataSourceName(dataSourceName string) string {
	if strings.Contains(dataSourceName, "_pragma") {
		return dataSourceName
	}

	separator := "?"
	if strings.Contains(dataSourceName, "?") {
		separator = "&"
	}
	return dataSourceName + separator + "_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"
}

func (a *Adapter) openSqlite() {
	path := strings.TrimPrefix(strings.SplitN(a.dataSourceName, "?", 2)[0], "file:")
	if dir := filepath.Dir(path); dir != "." {
		err := os.MkdirAll(dir, 0o755)
		if err != nil {
			panic(err)
		}
	}

	engine, err := xorm.NewEngine("sqlite", GetSqliteDataSourceName(a.dataSourceName))
	if err != nil {
		panic(err)
	}

	a.engine = engine
	a.createTable()
}

func (a *Adapter) close() {
	a.engine.Close()
	a.engine = nil
}

func (a *Adapter) createTable() {
	err := a.engine.Sync2(new(Asset))
	if err != nil {
		panic(err)
	}

	err = a.engine.Sync2(new(Permission))
	if err != nil {
		panic(err)
	}

	err = a.engine.Sync2(new(Record))
	if err != nil {
		panic(err)
	}

	err = a.engine.Sync2(new(Session))
	if err != nil {
		panic(err)
	}

	err = a.engine.Sync2(new(SshKey))
	if err != nil {
		panic(err)
	}

	err = a.engine.Sync2(new(SshHostKey))
	if err != nil {
		panic(err)
	}

	err = a.engine.Sync2(new(CommandRule))
	if err != nil {
		panic(err)
	}

	err = a.engine.Sync2(new(Command))
	if err != nil {
		panic(err)
	}

	err = a.engine.Sync2(new(LocalUser))
	if err != nil {
		panic(err)
	}

	err = a.engine.Sync2(new(PatchStatus))
	if err != nil {
		panic(err)
	}

	err = a.engine.Sync2(new(PatchJob))
	if err != nil {
		panic(err)
	}
}

func GetSession(owner string, offset, limit int, field, value, sortField, sortOrder string) *xorm.Session {
	session := adapter.engine.Prepare()
	if offset != -1 && limit != -1 {
		session.Limit(limit, offset)
	}
	if owner != "" {
		session = session.And("owner=?", owner)
	}
	if field != "" && value != "" {
		if util.FilterField(field) {
			session = session.And(fmt.Sprintf("%s like ?", util.SnakeString(field)), fmt.Sprintf("%%%s%%", value))
		}
	}
	if sortField == "" || sortOrder == "" {
		sortField = "created_time"
	}
	if sortOrder == "ascend" {
		session = session.Asc(util.SnakeString(sortField))
	} else {
		session = session.Desc(util.SnakeString(sortField))
	}
	return session
}
