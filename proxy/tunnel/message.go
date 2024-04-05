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

package tunnel

import "encoding/json"

type MessageType string

const (
	TypeServerHeartbeat MessageType = "1001"
	TypeClientHeartbeat MessageType = "1002"

	TypeInitApp MessageType = "1003" // client request to init app
	TypeAppMsg  MessageType = "1004" // server notify client has proxy app

	TypeAppWaitBind MessageType = "1005" // server notifies client to connect to app port
	TypeClientBind  MessageType = "1006" // client connects to the app port
)

type Message struct {
	Type    MessageType `json:"type"`
	Content string      `json:"content"`
	Name    string      `json:"name"`
	Meta    interface{} `json:"mate"`
}

func NewMessage(typ MessageType, msg string, name string, meta interface{}) *Message {
	return &Message{Type: typ, Content: msg, Name: name, Meta: meta}
}

func UnmarshalMsg(msgBytes []byte) (msg *Message, err error) {
	msg = &Message{}
	if err = json.Unmarshal(msgBytes, msg); err != nil {
		return
	}
	return
}