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
	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

// Command is a command run, or stopped by a rule, in an SSH session.
type Command struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100) index" json:"createdTime"`

	Session  string `xorm:"varchar(200) index" json:"session"`
	Asset    string `xorm:"varchar(200) index" json:"asset"`
	User     string `xorm:"varchar(200) index" json:"user"`
	ClientIp string `xorm:"varchar(100)" json:"clientIp"`
	Text     string `xorm:"mediumtext" json:"text"`
	Action   string `xorm:"varchar(20)" json:"action"`
	Rule     string `xorm:"varchar(200)" json:"rule"`
}

func GetCommandCount(owner, field, value string) (int64, error) {
	session := GetSession(owner, -1, -1, field, value, "", "")
	return session.Count(&Command{})
}

func GetPaginationCommands(owner string, offset, limit int, field, value, sortField, sortOrder string) ([]*Command, error) {
	commands := []*Command{}
	session := GetSession(owner, offset, limit, field, value, sortField, sortOrder)
	err := session.Find(&commands)
	if err != nil {
		return commands, err
	}

	return commands, nil
}

func GetSessionCommands(sessionId string) ([]*Command, error) {
	commands := []*Command{}
	err := adapter.engine.Asc("created_time").Find(&commands, &Command{Session: sessionId})
	if err != nil {
		return commands, err
	}

	return commands, nil
}

// AddCommand saves the command and counts it on its session.
func AddCommand(command *Command) error {
	command.Name = util.GenerateId()
	command.CreatedTime = util.GetCurrentTime()

	_, err := adapter.engine.Insert(command)
	if err != nil {
		return err
	}

	owner, name := util.GetOwnerAndNameFromIdNoCheck(command.Session)
	_, err = adapter.engine.ID(core.PK{owner, name}).Incr("command_count").Update(&Session{})
	return err
}
