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

const MaxExportRows = 100000

// IterateSessions calls fn on each session that matches the filter of the session list, MaxExportRows at most.
func IterateSessions(owner, status, field, value, sortField, sortOrder string, fn func(session *Session) error) error {
	session := GetSession(owner, 0, MaxExportRows, field, value, sortField, sortOrder)
	return session.Iterate(&Session{Status: status}, func(i int, bean interface{}) error {
		return fn(bean.(*Session))
	})
}

func IterateCommands(owner, field, value, sortField, sortOrder string, fn func(command *Command) error) error {
	session := GetSession(owner, 0, MaxExportRows, field, value, sortField, sortOrder)
	return session.Iterate(&Command{}, func(i int, bean interface{}) error {
		return fn(bean.(*Command))
	})
}

func IterateRecords(owner, field, value, sortField, sortOrder string, fn func(record *Record) error) error {
	session := GetSession(owner, 0, MaxExportRows, field, value, sortField, sortOrder)
	return session.Iterate(&Record{Owner: owner}, func(i int, bean interface{}) error {
		return fn(bean.(*Record))
	})
}
