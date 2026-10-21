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

import "sync"

type SessionCloser func(code int, msg string)

var sessionClosers sync.Map

func AddSessionCloser(id string, closer SessionCloser) {
	sessionClosers.Store(id, closer)
}

func RemoveSessionCloser(id string) {
	sessionClosers.Delete(id)
}

func closeNativeSession(id string, code int, msg string) {
	value, ok := sessionClosers.LoadAndDelete(id)
	if ok {
		value.(SessionCloser)(code, msg)
	}
}
