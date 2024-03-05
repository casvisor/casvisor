// Copyright 2024 The casbin Authors. All Rights Reserved.
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

package dbgate

import "github.com/casbin/casvisor/util"

var driversMap = make(map[string]string)

func init() {
	initDriversMap()
}

func initDriversMap() {
	drivers := []struct {
		label  string
		engine string
	}{
		{"MySQL", "mysql@dbgate-plugin-mysql"},
		{"MongoDB", "mongo@dbgate-plugin-mongo"},
		{"PostgreSQL", "postgres@dbgate-plugin-postgres"},
		{"SQLite", "sqlite@dbgate-plugin-sqlite"},
		{"Microsoft SQL Server", "mssql@dbgate-plugin-mssql"},
		{"Oracle", "oracle@dbgate-plugin-oracle"},
		{"Redis", "redis@dbgate-plugin-redis"},
	}

	for _, db := range drivers {
		driversMap[db.label] = db.engine
	}
}

func (c *Connection) TransferToSave() *Connection {
	if c.Id == "" {
		c.Id = util.GenerateId()
	}
	c.Engine = driversMap[c.Engine]
	return c
}
