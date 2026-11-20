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

package controllers

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
	"github.com/gorilla/websocket"
)

// the results a database session keeps for exporting
const databaseKeptResults = 20

type databaseRequest struct {
	Type     string `json:"type"`
	Id       int    `json:"id"`
	Sql      string `json:"sql,omitempty"`
	Database string `json:"database,omitempty"`
	Schema   string `json:"schema,omitempty"`
	Table    string `json:"table,omitempty"`
}

type databaseReply struct {
	Type    string            `json:"type"`
	Id      int               `json:"id,omitempty"`
	Code    int               `json:"code,omitempty"`
	Message string            `json:"message,omitempty"`
	Error   string            `json:"error,omitempty"`
	Data    interface{}       `json:"data,omitempty"`
	Result  *object.SqlResult `json:"result,omitempty"`
}

// databaseSession is a connected database session, kept for exporting its results.
type databaseSession struct {
	id       string
	user     *casdoorsdk.User
	asset    *object.Asset
	clientIp string
	client   *object.DatabaseClient

	mu      sync.Mutex
	results []*object.SqlResult
}

var databaseSessions sync.Map

func (s *databaseSession) keepResult(result *object.SqlResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.results = append(s.results, result)
	if len(s.results) > databaseKeptResults {
		s.results = s.results[len(s.results)-databaseKeptResults:]
	}
}

func (s *databaseSession) getResult(id string) *object.SqlResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, result := range s.results {
		if result.Id == id {
			return result
		}
	}
	return nil
}

// record applies the command rules to a statement and saves it as a command of
// the session; a blocked statement comes back with the rule in its result. A statement
// whose rules can't be read is blocked too.
func (s *databaseSession) record(statement string) (*object.Command, *object.SqlResult) {
	rule, ruleErr := object.MatchSqlRule(s.user, s.asset, statement)
	if ruleErr != nil {
		logs.Error(fmt.Sprintf("databaseSession.record(): failed to match the command rules: %s", ruleErr.Error()))
	}

	command := &object.Command{
		Owner: s.asset.Owner,
		// microseconds keep the statements of a batch in order
		CreatedTime: time.Now().Format("2006-01-02T15:04:05.000000Z07:00"),
		Session:     s.id,
		Asset:       s.asset.GetId(),
		User:        getUserId(s.user),
		ClientIp:    s.clientIp,
		Text:        statement,
		Protocol:    s.asset.Protocol,
	}
	var blocked *object.SqlResult
	if ruleErr != nil {
		command.Action = object.CommandActionBlock
		command.Error = "the statement is blocked because the command rules can't be read"
		blocked = &object.SqlResult{Statement: statement, Error: command.Error, Action: command.Action}
	} else if rule != nil {
		command.Action = rule.Action
		command.Rule = rule.GetId()
		if rule.Action == object.CommandActionBlock {
			ruleName := rule.DisplayName
			if ruleName == "" {
				ruleName = rule.Name
			}
			command.Error = fmt.Sprintf("the statement is blocked by the rule: %s", ruleName)
			blocked = &object.SqlResult{Statement: statement, Error: command.Error, Action: rule.Action, Rule: ruleName}
		}
	}

	err := object.AddCommand(command)
	if err != nil {
		logs.Error(fmt.Sprintf("databaseSession.record(): %s", err.Error()))
	}
	return command, blocked
}

func (s *databaseSession) execute(statement string) *object.SqlResult {
	command, blocked := s.record(statement)
	if blocked != nil {
		return blocked
	}

	result := s.client.Execute(statement)
	result.Action = command.Action
	if result.Columns != nil {
		result.Id = util.GenerateId()
		s.keepResult(result)
	}

	command.RowCount = result.RowCount
	command.Duration = result.Duration
	command.Error = result.Error
	err := object.UpdateCommandResult(command)
	if err != nil {
		logs.Error(fmt.Sprintf("databaseSession.execute(): %s", err.Error()))
	}
	return result
}

func (s *databaseSession) handle(stream *wsStream, request *databaseRequest) {
	reply := databaseReply{Type: "reply", Id: request.Id}
	var err error
	switch request.Type {
	case "query":
		statements := object.SplitSqlStatements(request.Sql, s.asset.Protocol)
		if len(statements) == 0 {
			err = fmt.Errorf("no statement to run")
			break
		}
		for _, statement := range statements {
			result := s.execute(statement)
			if stream.WriteJSONUnrecorded(databaseReply{Type: "result", Id: request.Id, Result: result}) != nil || result.Error != "" {
				break
			}
		}
	case "databases":
		var databases []string
		databases, err = s.client.GetDatabases()
		reply.Data = map[string]interface{}{"databases": databases, "current": s.client.Database}
	case "tables":
		reply.Data, err = s.client.GetTables(request.Database)
	case "columns":
		reply.Data, err = s.client.GetColumns(request.Database, request.Schema, request.Table)
	case "use":
		statement := s.client.GetUseStatement(request.Database)
		command, blocked := s.record(statement)
		if blocked != nil {
			err = fmt.Errorf("%s", blocked.Error)
			break
		}
		start := time.Now()
		err = s.client.UseDatabase(request.Database)
		command.Duration = time.Since(start).Milliseconds()
		if err != nil {
			command.Error = err.Error()
		}
		_ = object.UpdateCommandResult(command)
		reply.Data = map[string]interface{}{"current": s.client.Database}
	default:
		err = fmt.Errorf("unknown request: %s", request.Type)
	}

	if err != nil {
		reply.Error = err.Error()
	}
	_ = stream.WriteJSONUnrecorded(reply)
}

// GetAssetDatabase
// @Title GetAssetDatabase
// @Tag Session API
// @Description connect to the MySQL or PostgreSQL database of an asset over a websocket, to browse it and run SQL; every statement is checked against the command rules and recorded as a command of the session
// @Param   sessionId    query    string  true     "The id of the session, from add-asset-tunnel"
// @Param   username     query    string  false    "The username, when the asset keeps none"
// @Param   password     query    string  false    "The password, when the asset keeps none"
// @router /get-asset-database [get]
func (c *ApiController) GetAssetDatabase() {
	c.EnableRender = false
	ws, err := nativeUpgrader.Upgrade(c.Ctx.ResponseWriter, c.Ctx.Request, nil)
	if err != nil {
		c.ResponseError("WebSocket upgrade failed:", err)
		return
	}
	stream := newWsStream(ws)

	fail := func(code int, err error) {
		_ = stream.WriteJSONUnrecorded(databaseReply{Type: "error", Code: code, Message: err.Error()})
		stream.Close(code, err.Error())
	}

	sessionId := c.Input().Get("sessionId")
	session, asset, code, err := c.getTunnelSessionAndAsset(sessionId, c.Input().Get("username"), c.Input().Get("password"))
	if err != nil {
		fail(code, err)
		return
	}
	if !object.IsDatabaseProtocol(asset.Protocol) {
		fail(ParametersError, fmt.Errorf("the asset: %s is not a database", asset.GetId()))
		return
	}

	client, err := object.DialDatabase(asset)
	if err != nil {
		fail(NewTunnelError, err)
		return
	}
	defer client.Close()

	err = setSessionConnected(sessionId, session, 0, 0)
	if err != nil {
		fail(SessionUpdateError, err)
		return
	}

	s := &databaseSession{id: sessionId, user: c.GetSessionUser(), asset: asset, clientIp: c.getAuthzClientIp(), client: client}
	databaseSessions.Store(sessionId, s)
	defer databaseSessions.Delete(sessionId)
	c.addLiveSession(sessionId, asset)
	object.AddSessionCloser(sessionId, func(code int, msg string) {
		_ = stream.WriteJSONUnrecorded(databaseReply{Type: "error", Code: code, Message: msg})
		stream.Close(code, msg)
		client.Kill()
	})
	_ = stream.WriteJSONUnrecorded(databaseReply{Type: "connected", Data: map[string]interface{}{
		"protocol": asset.Protocol,
		"version":  client.Version,
		"database": client.Database,
		"maxRows":  object.SqlMaxRows,
	}})

	requests := make(chan *databaseRequest, 64)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for request := range requests {
			s.handle(stream, request)
		}
	}()

	for {
		messageType, data, err := ws.ReadMessage()
		if err != nil {
			break
		}
		if messageType != websocket.TextMessage {
			continue
		}

		var request databaseRequest
		err = json.Unmarshal(data, &request)
		if err != nil {
			continue
		}
		requests <- &request
	}

	close(requests)
	client.Kill()
	<-done
	closeTunnelSession(sessionId, "Normal user exit")
}

// ExportDatabaseResult
// @Title ExportDatabaseResult
// @Tag Session API
// @Description download a result of a connected database session as CSV, with the Download permission
// @Param   id           query    string  true     "The id of the session"
// @Param   result       query    string  true     "The id of the result"
// @Success 200 {file} file The CSV file
// @router /export-database-result [get]
func (c *ApiController) ExportDatabaseResult() {
	sessionId := c.Input().Get("id")
	user := c.GetSessionUser()
	value, ok := databaseSessions.Load(sessionId)
	if !ok {
		c.ResponseError(fmt.Sprintf("the session: %s is not connected", sessionId))
		return
	}
	s := value.(*databaseSession)
	if user == nil || getUserId(s.user) != getUserId(user) {
		c.ResponseError(fmt.Sprintf("you have no permission to use the session: %s", sessionId))
		return
	}

	asset, err := object.GetAsset(s.asset.GetId())
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	session, err := object.GetConnSession(sessionId)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	account := ""
	if session != nil {
		account = session.Account
	}
	actions, err := object.GetAssetAccountActions(user, asset, &account, c.getAuthzClientIp())
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !slices.Contains(actions, object.ActionConnect) || !slices.Contains(actions, object.ActionDownload) {
		c.ResponseError(fmt.Sprintf("you have no permission to download from the asset: %s", s.asset.Name))
		return
	}

	result := s.getResult(c.Input().Get("result"))
	if result == nil {
		c.ResponseError("the result does not exist, run the statement again")
		return
	}

	object.AddAuditRecord(&object.AuditEvent{
		Organization: s.asset.Owner,
		User:         user.Name,
		ClientIp:     c.getClientIp(),
		UserAgent:    c.getUserAgent(),
		Method:       s.asset.Protocol,
		Action:       "sql-export",
		Asset:        s.asset.GetId(),
		Detail:       result.Statement,
	})

	c.EnableRender = false
	fileName := fmt.Sprintf("%s-%s.csv", s.asset.Name, time.Now().Format("20060102-150405"))
	header := c.Ctx.ResponseWriter.Header()
	header.Set("Content-Type", "text/csv; charset=utf-8")
	header.Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(fileName)))
	c.Ctx.ResponseWriter.WriteHeader(200)

	// the byte order mark lets Excel read the file as UTF-8
	_, _ = c.Ctx.ResponseWriter.Write([]byte("\xEF\xBB\xBF"))
	writer := csv.NewWriter(c.Ctx.ResponseWriter)
	names := []string{}
	for _, column := range result.Columns {
		names = append(names, csvCell(column.Name))
	}
	_ = writer.Write(names)
	for _, row := range result.Rows {
		record := make([]string, len(row))
		for i, value := range row {
			if value != nil {
				record[i] = csvCell(*value)
			}
		}
		_ = writer.Write(record)
	}
	writer.Flush()
}
