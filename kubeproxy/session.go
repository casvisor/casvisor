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

package kubeproxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
)

// tokenSession is a kubeconfig token in use: the requests it is serving, and the sessions of
// the containers opened with it, which all end with it.
type tokenSession struct {
	token *object.KubernetesToken

	mu        sync.Mutex
	user      *casdoorsdk.User
	clientIp  string
	transport *http.Transport
	transKey  string
	requests  map[*request]struct{}
	children  map[string]struct{}
	closed    bool
}

// request is a request in progress, ended when the scope no longer covers it.
type request struct {
	req    *object.KubernetesRequest
	cancel context.CancelCauseFunc
}

var (
	errRevoked    = errors.New("the kubeconfig has been revoked")
	errNotCovered = errors.New("the permission no longer covers this request")
)

var (
	sessionsMu sync.Mutex
	sessions   = map[string]*tokenSession{}
)

// getTokenSession returns the session of a kubeconfig token, nil when the token can't be used.
func getTokenSession(value string, assetId string) (*tokenSession, error) {
	sessionsMu.Lock()
	ts := sessions[value]
	sessionsMu.Unlock()
	if ts != nil {
		if ts.isClosed() || !ts.token.IsActive(time.Now()) || ts.token.Asset != assetId {
			return nil, nil
		}
		return ts, nil
	}

	token, err := object.GetKubernetesTokenByValue(value)
	if err != nil || token == nil || token.Asset != assetId {
		return nil, err
	}
	session, err := object.GetConnSession(token.Session)
	if err != nil {
		return nil, err
	}
	if session == nil || session.Status != object.Connected {
		return nil, object.RevokeKubernetesTokens(token.Session)
	}

	owner, name := util.GetOwnerAndNameFromId(token.User)
	user, err := object.GetCasdoorUser(owner, name)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, nil
	}

	ts = &tokenSession{token: token, user: user, clientIp: token.ClientIp, requests: map[*request]struct{}{}, children: map[string]struct{}{}}
	sessionsMu.Lock()
	if existing := sessions[value]; existing != nil {
		sessionsMu.Unlock()
		return existing, nil
	}
	sessions[value] = ts
	sessionsMu.Unlock()

	object.AddSessionCloser(token.Session, func(code int, msg string) {
		ts.close(value)
	})
	liveSession := &object.LiveSession{User: user, AssetId: token.Asset, Account: token.Account, ClientIp: token.ClientIp}
	liveSession.OnActions = func(actions []string) {
		ts.recheck(liveSession.User)
	}
	object.AddLiveSession(token.Session, liveSession)
	return ts, nil
}

func (ts *tokenSession) isClosed() bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.closed
}

func (ts *tokenSession) getUser() *casdoorsdk.User {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.user
}

func (ts *tokenSession) setClientIp(clientIp string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.clientIp = clientIp
}

// getTransport keeps the connections to the API server of the asset, made again when the address
// or the token of the asset changes.
func (ts *tokenSession) getTransport(client *object.KubernetesClient, key string) *http.Transport {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.transport == nil || ts.transKey != key {
		if ts.transport != nil {
			ts.transport.CloseIdleConnections()
		}
		ts.transport = client.NewTransport()
		ts.transKey = key
	}
	return ts.transport
}

// begin adds a request in progress, whose context is cancelled, with the reason as its cause,
// when the token is revoked or the permission no longer covers the request.
func (ts *tokenSession) begin(ctx context.Context, req *object.KubernetesRequest) (context.Context, func(), bool) {
	ctx, cancel := context.WithCancelCause(ctx)
	r := &request{req: req, cancel: cancel}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.closed {
		cancel(errRevoked)
		return ctx, func() {}, false
	}
	ts.requests[r] = struct{}{}
	return ctx, func() {
		cancel(nil)
		ts.mu.Lock()
		delete(ts.requests, r)
		ts.mu.Unlock()
	}, true
}

func (ts *tokenSession) addChild(sessionId string) bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.closed {
		return false
	}
	ts.children[sessionId] = struct{}{}
	return true
}

func (ts *tokenSession) removeChild(sessionId string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	delete(ts.children, sessionId)
}

// recheck ends the requests in progress that the permissions of the user no longer cover, like
// a watch in a namespace taken out of the permission.
func (ts *tokenSession) recheck(user *casdoorsdk.User) {
	ts.mu.Lock()
	if user != nil {
		ts.user = user
	}
	user, clientIp := ts.user, ts.clientIp
	ts.mu.Unlock()

	asset, err := object.GetAsset(ts.token.Asset)
	if err != nil || asset == nil {
		return
	}
	_, scope, err := object.GetKubernetesAccess(user, asset, ts.token.Account, clientIp)
	if err != nil {
		logs.Error(fmt.Sprintf("kubeproxy.recheck(): %s", err.Error()))
		return
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	for r := range ts.requests {
		if !r.req.IsAllowed(scope) {
			r.cancel(errNotCovered)
		}
	}
}

// close ends the requests in progress and the sessions of the containers opened with the token.
func (ts *tokenSession) close(value string) {
	ts.mu.Lock()
	if ts.closed {
		ts.mu.Unlock()
		return
	}
	ts.closed = true
	children := []string{}
	for sessionId := range ts.children {
		children = append(children, sessionId)
	}
	for r := range ts.requests {
		r.cancel(errRevoked)
	}
	transport := ts.transport
	ts.mu.Unlock()

	if transport != nil {
		transport.CloseIdleConnections()
	}
	sessionsMu.Lock()
	delete(sessions, value)
	sessionsMu.Unlock()

	// the session of the token is being closed and holds the lock of closing sessions
	go func() {
		for _, sessionId := range children {
			err := object.CloseSession(sessionId, object.PermissionRevokedCode, "The kubeconfig it was opened with has been revoked")
			if err != nil {
				logs.Error(fmt.Sprintf("kubeproxy.close(): failed to close the session: %s: %s", sessionId, err.Error()))
			}
		}
	}()
}

// expireTokens ends the sessions of the kubeconfig tokens that have expired.
func expireTokens() {
	tokens, err := object.GetExpiredKubernetesTokens()
	if err != nil {
		logs.Error(fmt.Sprintf("kubeproxy.expireTokens(): %s", err.Error()))
		return
	}

	for _, token := range tokens {
		err = object.CloseSession(token.Session, 0, "The kubeconfig has expired")
		if err == nil {
			err = object.RevokeKubernetesTokens(token.Session)
		}
		if err != nil {
			logs.Error(fmt.Sprintf("kubeproxy.expireTokens(): %s", err.Error()))
		}
	}
}

// Start ends the sessions of kubeconfig tokens once they expire. The tokens that are still valid
// go on across restarts of Casvisor.
func Start() {
	go func() {
		for {
			expireTokens()
			time.Sleep(30 * time.Second)
		}
	}()
}
