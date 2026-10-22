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

package sshgateway

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/beego/beego/logs"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
	"golang.org/x/crypto/ssh"
)

// gatewayConn is one client connection: a Casvisor user on one asset, which
// is either named in the SSH username or picked from a menu after signing in.
type gatewayConn struct {
	conn     *ssh.ServerConn
	clientIp string
	user     *casdoorsdk.User

	mu        sync.Mutex
	asset     *object.Asset
	actions   []string
	client    *ssh.Client
	sessionId string
	forwards  map[string]bool
}

func (g *gatewayConn) signIn() error {
	sshKey, err := object.GetSshKey(g.conn.Permissions.Extensions[sshKeyExtension])
	if err != nil {
		return err
	}
	if sshKey == nil {
		return fmt.Errorf("the public key has been removed")
	}

	if !object.IsUserAllowedToSignin(sshKey.GetUserId()) {
		return fmt.Errorf("the user: %s is not allowed to sign in", sshKey.GetUserId())
	}

	g.user, err = object.GetCasdoorUser(sshKey.Owner, sshKey.User)
	if err != nil {
		return err
	}

	err = object.UpdateSshKeyLastUsedTime(sshKey)
	if err != nil {
		logs.Error(fmt.Sprintf("SSH gateway: failed to update the last used time of the key: %s: %s", sshKey.GetId(), err.Error()))
	}

	userName, assetName := parseUsername(g.conn.User())
	if userName != "" && userName != g.user.Name && userName != sshKey.GetUserId() {
		return fmt.Errorf("the public key belongs to the user: %s, not %s", sshKey.GetUserId(), userName)
	}

	asset, err := g.findAsset(assetName)
	if err != nil {
		return err
	}
	if asset == nil {
		if userName == "" && assetName == g.user.Name {
			// "ssh <user>@<casvisor host>" opens the asset menu
			return nil
		}
		return fmt.Errorf("the asset: %s does not exist, connect with \"ssh -p <port> <asset>@<host>\"", assetName)
	}

	return g.selectAsset(asset)
}

// parseUsername splits "<user>@<asset>" (or just "<asset>"); ssh itself splits the
// destination at its last "@", so "ssh alice@web1@casvisor" arrives as "alice@web1".
func parseUsername(username string) (string, string) {
	i := strings.LastIndex(username, "@")
	if i < 0 {
		return "", username
	}
	return username[:i], username[i+1:]
}

func (g *gatewayConn) findAsset(name string) (*object.Asset, error) {
	if name == "" {
		return nil, nil
	}
	if strings.Contains(name, "/") {
		owner, assetName := util.GetOwnerAndNameFromIdNoCheck(name)
		if owner == "" || assetName == "" {
			return nil, nil
		}
		return object.GetAsset(name)
	}
	return object.GetAsset(util.GetIdFromOwnerAndName(g.user.Owner, name))
}

func (g *gatewayConn) getAllowedAssets() ([]*object.Asset, error) {
	owner := g.user.Owner
	if owner == "built-in" {
		owner = ""
	}
	assets, err := object.GetAssets(owner)
	if err != nil {
		return nil, err
	}

	if !object.IsAdminUser(g.user) {
		assets, err = object.FilterAllowedAssets(g.user, assets, g.clientIp)
		if err != nil {
			return nil, err
		}
	}

	res := []*object.Asset{}
	for _, asset := range assets {
		if isSshAsset(asset) {
			res = append(res, asset)
		}
	}
	return res, nil
}

func isSshAsset(asset *object.Asset) bool {
	return strings.EqualFold(asset.Protocol, "SSH")
}

func (g *gatewayConn) selectAsset(asset *object.Asset) error {
	// the asset list comes with the credentials still encrypted
	asset, err := object.GetAsset(asset.GetId())
	if err != nil {
		return err
	}
	if asset == nil {
		return fmt.Errorf("the asset has been deleted")
	}

	if !isSshAsset(asset) {
		return fmt.Errorf("the asset: %s uses %s, only SSH assets can be reached through the SSH gateway", asset.Name, asset.Protocol)
	}

	actions, err := object.GetAssetActions(g.user, asset, g.clientIp)
	if err != nil {
		return err
	}
	if !slices.Contains(actions, object.ActionConnect) {
		return fmt.Errorf("you have no permission to connect to the asset: %s", asset.Name)
	}

	if asset.Username == "" {
		return fmt.Errorf("the asset: %s has no username, ask the administrator to set its account", asset.Name)
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	g.asset = asset
	g.actions = actions
	return nil
}

func (g *gatewayConn) getAsset() *object.Asset {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.asset
}

func (g *gatewayConn) can(action string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Contains(g.actions, action)
}

// getClient connects to the asset the first time a channel needs it, and opens
// the session that the web console lists and an administrator can stop.
func (g *gatewayConn) getClient() (*ssh.Client, error) {
	client, isNew, err := g.connect()
	if err != nil {
		return nil, err
	}
	if isNew {
		g.audit("ssh-connect", "")
	}
	return client, nil
}

func (g *gatewayConn) connect() (*ssh.Client, bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.client != nil {
		return g.client, false, nil
	}
	if g.asset == nil {
		return nil, false, fmt.Errorf("no asset is selected, connect with \"ssh -p <port> <asset>@<host>\"")
	}

	client, err := object.DialSsh(g.asset)
	if err != nil {
		return nil, false, err
	}

	userAgent := string(g.conn.ClientVersion())
	session := &object.Session{
		Creator:       util.GetIdFromOwnerAndName(g.user.Owner, g.user.Name),
		ClientIp:      g.clientIp,
		UserAgent:     userAgent,
		ClientIpDesc:  util.GetDescFromIP(g.clientIp),
		UserAgentDesc: userAgent,
	}
	_, err = object.CreateSession(session, g.asset.GetId(), "gateway", object.GetSessionOperations(g.actions))
	if err != nil {
		_ = client.Close()
		return nil, false, err
	}

	session.Status = object.Connected
	session.StartTime = util.GetCurrentTime()
	session.Reviewed = true
	_, err = object.UpdateSession(session.GetId(), session)
	if err != nil {
		_ = client.Close()
		return nil, false, err
	}

	g.client = client
	g.sessionId = session.GetId()
	object.AddSessionCloser(g.sessionId, func(code int, msg string) {
		_ = g.conn.Close()
	})
	go func() {
		_ = client.Wait()
		_ = g.conn.Close()
	}()

	return client, true, nil
}

func (g *gatewayConn) close() {
	g.mu.Lock()
	client := g.client
	sessionId := g.sessionId
	g.mu.Unlock()

	if client != nil {
		_ = client.Close()
	}
	if sessionId != "" {
		object.RemoveSessionCloser(sessionId)
		err := object.CloseSession(sessionId, 0, "Normal user exit")
		if err != nil {
			logs.Error(fmt.Sprintf("SSH gateway: failed to close the session: %s: %s", sessionId, err.Error()))
		}
	}
}

func (g *gatewayConn) audit(action string, detail string) {
	asset := g.getAsset()
	assetId := ""
	organization := g.user.Owner
	if asset != nil {
		assetId = asset.GetId()
		organization = asset.Owner
	}

	method := "SSH"
	if strings.HasPrefix(action, "sftp-") {
		method = "SFTP"
	}

	object.AddAuditRecord(&object.AuditEvent{
		Organization: organization,
		User:         g.user.Name,
		ClientIp:     g.clientIp,
		UserAgent:    string(g.conn.ClientVersion()),
		Method:       method,
		Action:       action,
		Asset:        assetId,
		Detail:       detail,
	})
}
