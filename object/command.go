// Copyright 2024 The Casbin Authors. All Rights Reserved.
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

type Command struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	DisplayName string `xorm:"varchar(100)" json:"displayName"`

	Assets  []string `json:"assets"`
	Command string   `xorm:"mediumtext" json:"command"`
}

func (c *Command) GetId() string {
	return util.GetIdFromOwnerAndName(c.Owner, c.Name)
}

func GetCommandCount(owner, field, value string) (int64, error) {
	session := GetSession(owner, -1, -1, field, value, "", "")
	return session.Count(&Command{})
}

func GetCommands(owner string) ([]*Command, error) {
	commands := []*Command{}
	err := adapter.Engine.Desc("created_time").Find(&commands, &Command{Owner: owner})
	if err != nil {
		return commands, err
	}

	return commands, nil
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

func getCommand(owner string, name string) (*Command, error) {
	if owner == "" || name == "" {
		return nil, nil
	}

	command := Command{Owner: owner, Name: name}
	existed, err := adapter.Engine.Get(&command)
	if err != nil {
		return &command, err
	}

	if existed {
		return &command, nil
	} else {
		return nil, nil
	}
}

func GetCommand(id string) (*Command, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	return getCommand(owner, name)
}

func UpdateCommand(id string, command *Command) (bool, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	if p, err := getCommand(owner, name); err != nil {
		return false, err
	} else if p == nil {
		return false, nil
	}

	_, err := adapter.Engine.ID(core.PK{owner, name}).AllCols().Update(command)
	if err != nil {
		return false, err
	}

	return true, nil
}

func AddCommand(command *Command) (bool, error) {
	_, err := adapter.Engine.Insert(command)
	if err != nil {
		return false, err
	}

	return true, nil
}

func DeleteCommand(command *Command) (bool, error) {
	_, err := adapter.Engine.ID(core.PK{command.Owner, command.Name}).Delete(&Command{})
	if err != nil {
		return false, err
	}

	return true, nil
}
