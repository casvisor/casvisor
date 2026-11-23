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

// Package dbproxy lets database clients on the user's machine, such as DBeaver, Navicat, DataGrip, mysql
// and psql, reach MySQL and PostgreSQL assets through Casvisor. Casvisor listens on a port of each protocol and
// speaks it to the client, which signs in with a database token from the web console as both the user and the
// password; Casvisor then signs in to the database with the asset's account, through the asset's network
// gateway, and passes the traffic through, checking every statement against the command rules and
// recording it as a command of the session, like the web console does.
package dbproxy

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"runtime/debug"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/beego/beego"
	"github.com/beego/beego/logs"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
)

const handshakeTimeout = 30 * time.Second

var (
	tlsConfig  *tls.Config
	nextConnId atomic.Uint32
	// conns are the connections of the proxy by their connection id, which a client cancels a query by
	conns sync.Map
)

func isEnabled() bool {
	return !conf.GetConfigBool("isDemoMode")
}

// IsMysqlEnabled tells whether the proxy listens for MySQL clients, on dbProxyMysqlPort.
func IsMysqlEnabled() bool {
	return isEnabled() && conf.GetConfigString("dbProxyMysqlPort") != ""
}

// IsPostgresqlEnabled tells whether the proxy listens for PostgreSQL clients, on dbProxyPostgresqlPort.
func IsPostgresqlEnabled() bool {
	return isEnabled() && conf.GetConfigString("dbProxyPostgresqlPort") != ""
}

// GetHost is the host name clients reach the proxy at, when it isn't the one of the web console.
func GetHost() string {
	if host := conf.GetConfigString("dbProxyHost"); host != "" {
		return host
	}
	return conf.GetConfigString("sshGatewayHost")
}

func Start() {
	if !IsMysqlEnabled() && !IsPostgresqlEnabled() {
		return
	}

	var err error
	tlsConfig, err = getTlsConfig()
	if err != nil {
		logs.Error(fmt.Sprintf("database proxy: TLS is off: %s", err.Error()))
	}

	b := make([]byte, 2)
	_, _ = rand.Read(b)
	nextConnId.Store(uint32(b[0])<<16 | uint32(b[1])<<8)

	if IsMysqlEnabled() {
		go listen("MySQL", conf.GetConfigString("dbProxyMysqlPort"), serveMysql)
	}
	if IsPostgresqlEnabled() {
		go listen("PostgreSQL", conf.GetConfigString("dbProxyPostgresqlPort"), servePostgresql)
	}
}

func listen(protocol string, port string, serve func(conn net.Conn)) {
	for {
		listener, err := net.Listen("tcp", net.JoinHostPort("", port))
		if err == nil {
			logs.Info(fmt.Sprintf("database proxy: listening for %s clients on port %s", protocol, port))
			for {
				var conn net.Conn
				conn, err = listener.Accept()
				if err != nil {
					break
				}
				go func() {
					defer recoverPanic(protocol)
					serve(conn)
				}()
			}
			_ = listener.Close()
		}
		logs.Error(fmt.Sprintf("database proxy: %s stopped, restarting in 10 seconds: %s", protocol, err.Error()))
		time.Sleep(10 * time.Second)
	}
}

func recoverPanic(where string) {
	if err := recover(); err != nil {
		logs.Error("database proxy: %s panicked: %v %s", where, err, debug.Stack())
	}
}

// getTlsConfig takes the certificate of dbProxyCertFile and dbProxyKeyFile, or else the one Casvisor serves
// HTTPS with, or else makes a self-signed one, which clients that don't check the certificate take.
func getTlsConfig() (*tls.Config, error) {
	certFile, keyFile := conf.GetConfigString("dbProxyCertFile"), conf.GetConfigString("dbProxyKeyFile")
	if certFile == "" && keyFile == "" {
		certFile, keyFile = beego.BConfig.Listen.HTTPSCertFile, beego.BConfig.Listen.HTTPSKeyFile
	}

	var cert tls.Certificate
	var err error
	if certFile != "" && keyFile != "" {
		cert, err = tls.LoadX509KeyPair(certFile, keyFile)
	} else {
		cert, err = makeSelfSignedCertificate()
	}
	if err != nil {
		return nil, err
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, nil
}

func makeSelfSignedCertificate() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		return tls.Certificate{}, err
	}

	host := GetHost()
	if host == "" {
		host = "casvisor"
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host, Organization: []string{"Casvisor database proxy"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

// bufferedConn reads what a bufio.Reader has already taken from the connection first, for a TLS
// handshake that starts while the client's first bytes are in the buffer.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

func getHost(addr net.Addr) string {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}

// proxyConn is what the MySQL and PostgreSQL connections share: the user and asset of the
// token, the session, the command records and the statement waiting for review.
type proxyConn struct {
	id        uint32
	protocol  string
	conn      net.Conn
	clientIp  string
	userAgent string

	token     *object.ClientToken
	user      *casdoorsdk.User
	asset     *object.Asset
	account   string
	client    *object.DatabaseClient
	sessionId string

	mu           sync.Mutex
	reviewCancel chan struct{}
	gone         chan struct{}
	goneOnce     sync.Once
	closeOnce    sync.Once
	expireTimer  *time.Timer

	// cancelQuery asks the database to cancel what the connection runs, for another connection's KILL or cancel request
	cancelQuery func()
}

func newProxyConn(protocol string, conn net.Conn) *proxyConn {
	return &proxyConn{
		id:       nextConnId.Add(1),
		protocol: protocol,
		conn:     conn,
		clientIp: getHost(conn.RemoteAddr()),
		gone:     make(chan struct{}),
	}
}

// authorize finds the token the client signs in with and checks again that its user may connect to the asset.
func (p *proxyConn) authorize(token *object.ClientToken) error {
	if !object.IsUserAllowedToSignin(token.UserId) {
		return fmt.Errorf("the user: %s is not allowed to sign in", token.UserId)
	}
	owner, name := util.GetOwnerAndNameFromId(token.UserId)
	user, err := object.GetCasdoorUser(owner, name)
	if err != nil {
		return err
	}

	asset, err := object.GetAsset(token.AssetId)
	if err != nil {
		return err
	}
	if asset == nil {
		return fmt.Errorf("the asset: %s has been deleted", token.AssetId)
	}
	if asset.Protocol != p.protocol {
		return fmt.Errorf("the asset: %s uses %s, connect to the %s port of the database proxy", asset.Name, asset.Protocol, asset.Protocol)
	}

	account := token.Account
	actions, err := object.GetAssetAccountActions(user, asset, &account, p.clientIp)
	if err != nil {
		return err
	}
	if !slices.Contains(actions, object.ActionConnect) {
		return fmt.Errorf("you have no permission to connect to the asset: %s", asset.Name)
	}
	err = object.CheckConnectMethod(user, asset, account, object.ConnectMethodDatabase)
	if err != nil {
		return err
	}
	asset, err = asset.WithAccount(account)
	if err != nil {
		return err
	}

	p.token = token
	p.user = user
	p.asset = asset
	p.account = account
	return nil
}

// checkLoginRules applies the login rules to the sign-in. A database client can't answer a verification code, so
// a rule asking for one rejects it; a rule holding it for review keeps the client waiting until an administrator
// approves it, and the approval lasts for the token and the client IP. notify tells the client it waits, if it can be told.
func (p *proxyConn) checkLoginRules(notify func(message string)) error {
	decision, err := object.CheckLoginRules(p.user, p.clientIp, object.LoginMethodDatabase, time.Now())
	if err != nil {
		return err
	}

	switch decision.Action {
	case object.LoginActionReject:
		p.addLoginAudit("login-reject", fmt.Sprintf("Database client sign-in rejected by the login rule: %s", decision.RuleName()))
		return fmt.Errorf("the sign-in is not allowed by the login rule: %s", decision.RuleName())
	case object.LoginActionMfa:
		p.addLoginAudit("login-reject", fmt.Sprintf("Database client sign-in rejected: the login rule %s requires two-step verification, which database clients can't do", decision.RuleName()))
		return fmt.Errorf("the login rule %s requires two-step verification, which database clients can't do, use the database console in the web instead", decision.RuleName())
	case object.LoginActionReview:
		if p.token.IsApproved(p.clientIp) {
			return nil
		}
		_ = p.conn.SetDeadline(time.Now().Add(object.LoginReviewTimeout + time.Minute))
		defer p.watchClient()()
		review, err := object.AddLoginReview(p.user, decision, p.clientIp, p.userAgent, object.LoginMethodDatabase)
		if err != nil {
			return err
		}
		p.addLoginAudit("login-review", fmt.Sprintf("Database client sign-in held for review %s by the login rule: %s", review.Name, decision.RuleName()))
		if notify != nil {
			notify(fmt.Sprintf("casvisor: the login rule %s holds this sign-in until an administrator approves it (request %s, waiting up to %d minutes)", decision.RuleName(), review.Name[:8], int(object.LoginReviewTimeout.Minutes())))
		}

		status, err := object.WaitLoginReview(review.GetId(), p.gone)
		if err != nil {
			return err
		}
		if status != object.LoginReviewApproved {
			return fmt.Errorf("the sign-in was not approved: %s", status)
		}
		used, err := object.ConsumeLoginReview(review.GetId())
		if err != nil || !used {
			return fmt.Errorf("the approval has already been used")
		}
		p.token.Approve(p.clientIp)
	}
	return nil
}

// watchClient tells when the client goes away while its sign-in waits, which a client waiting for the
// answer only does by closing the connection; the returned function stops watching.
func (p *proxyConn) watchClient() func() {
	done := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		_, _ = p.conn.Read(make([]byte, 1))
		select {
		case <-done:
		default:
			p.goneOnce.Do(func() { close(p.gone) })
		}
	}()
	return func() {
		close(done)
		_ = p.conn.SetReadDeadline(time.Now())
		<-exited
		_ = p.conn.SetDeadline(time.Now().Add(handshakeTimeout))
	}
}

func (p *proxyConn) addLoginAudit(action string, detail string) {
	object.AddAuditRecord(&object.AuditEvent{
		Organization: p.user.Owner,
		User:         p.user.Name,
		ClientIp:     p.clientIp,
		UserAgent:    p.userAgent,
		Method:       "Login",
		Action:       action,
		Detail:       detail,
	})
}

// openSession opens the session of the connection, which the web console lists, shows the statements of
// and can stop; it ends when the token is revoked or expires, or the permission is revoked.
func (p *proxyConn) openSession(backend net.Conn) error {
	userAgent := p.userAgent
	if len(userAgent) > 200 {
		userAgent = userAgent[:200]
	}
	session := &object.Session{
		Account:       p.account,
		Creator:       util.GetIdFromOwnerAndName(p.user.Owner, p.user.Name),
		ClientIp:      p.clientIp,
		UserAgent:     userAgent,
		ClientIpDesc:  util.GetDescFromIP(p.clientIp),
		UserAgentDesc: userAgent,
	}
	_, err := object.CreateSession(session, p.asset.GetId(), "gateway", []string{})
	if err != nil {
		return err
	}
	session.Status = object.Connected
	session.StartTime = util.GetCurrentTime()
	session.Reviewed = true
	_, err = object.UpdateSession(session.GetId(), session)
	if err != nil {
		return err
	}

	p.sessionId = session.GetId()
	p.token.Use(p.clientIp, p.sessionId)
	conns.Store(p.id, p)
	object.AddSessionCloser(p.sessionId, func(code int, msg string) {
		p.kill()
	})
	object.AddLiveSession(p.sessionId, &object.LiveSession{
		User:     p.user,
		AssetId:  p.asset.GetId(),
		Account:  p.account,
		ClientIp: p.clientIp,
		// only Connect matters to a database client, losing it ends the session
		OnActions: func(actions []string) {},
	})
	sessionId := p.sessionId
	p.expireTimer = time.AfterFunc(time.Until(p.token.ExpireTime), func() {
		_ = object.CloseSession(sessionId, object.PermissionRevokedCode, "The database token has expired")
	})

	object.AddAuditRecord(&object.AuditEvent{
		Organization: p.asset.Owner,
		User:         p.user.Name,
		ClientIp:     p.clientIp,
		UserAgent:    p.userAgent,
		Method:       p.protocol,
		Action:       "database-proxy",
		Asset:        p.asset.GetId(),
		Detail:       fmt.Sprintf("%s via %s", net.JoinHostPort(p.asset.Host, fmt.Sprintf("%d", p.asset.Port)), backend.RemoteAddr().String()),
	})
	return nil
}

// kill cuts the client and the database, which ends the connection's loops.
func (p *proxyConn) kill() {
	p.goneOnce.Do(func() { close(p.gone) })
	_ = p.conn.Close()
	if p.client != nil {
		p.client.Kill()
	}
	p.cancelReview()
}

// close ends the connection and its session.
func (p *proxyConn) close(message string) {
	p.closeOnce.Do(func() {
		p.kill()
		if p.client != nil {
			p.client.Close()
		}
		conns.Delete(p.id)
		if p.expireTimer != nil {
			p.expireTimer.Stop()
		}
		if p.sessionId != "" {
			p.token.EndSession(p.sessionId)
			object.RemoveSessionCloser(p.sessionId)
			err := object.CloseSession(p.sessionId, 0, message)
			if err != nil {
				logs.Error(fmt.Sprintf("database proxy: failed to close the session: %s: %s", p.sessionId, err.Error()))
			}
		}
	})
}

// startReview returns the channel that withdraws the statement about to wait for review.
func (p *proxyConn) startReview() chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()

	cancel := make(chan struct{})
	select {
	case <-p.gone:
		close(cancel)
	default:
		p.reviewCancel = cancel
	}
	return cancel
}

// cancelReview withdraws the statement waiting for review, if any; it tells whether there was one.
func (p *proxyConn) cancelReview() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.reviewCancel == nil {
		return false
	}
	close(p.reviewCancel)
	p.reviewCancel = nil
	return true
}

func (p *proxyConn) endReview(cancel chan struct{}) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.reviewCancel == cancel {
		p.reviewCancel = nil
	}
}

// record checks SQL text the client sent against the command rules and saves it as a command of the session;
// false means it may not run, and command.Error tells why. onHeld is told when the text waits for review.
func (p *proxyConn) record(text string, onHeld func(message string)) (*object.Command, bool) {
	command := &object.Command{
		Owner: p.asset.Owner,
		// microseconds keep the statements in order
		CreatedTime: time.Now().Format("2006-01-02T15:04:05.000000Z07:00"),
		Session:     p.sessionId,
		Asset:       p.asset.GetId(),
		User:        util.GetIdFromOwnerAndName(p.user.Owner, p.user.Name),
		ClientIp:    p.clientIp,
		Text:        text,
		Protocol:    p.protocol,
	}

	cancel := p.startReview()
	_, allowed, err := object.CheckDatabaseStatement(p.user, p.asset, command, cancel, func(message string, review *object.CommandReview) {
		if onHeld != nil {
			onHeld(message)
		}
	})
	p.endReview(cancel)
	if err != nil {
		logs.Error(fmt.Sprintf("database proxy: %s", err.Error()))
	}
	if !allowed && command.Error == "" {
		command.Error = "the statement is blocked"
	}

	err = object.AddCommand(command)
	if err != nil {
		logs.Error(fmt.Sprintf("database proxy: %s", err.Error()))
	}
	return command, allowed
}

// finish saves how a command ran; a statement prepared once and run many times adds up its runs.
func finish(command *object.Command, start time.Time, rows int64, message string) {
	command.Duration += time.Since(start).Milliseconds()
	command.RowCount += rows
	if message != "" {
		command.Error = message
	}
	err := object.UpdateCommandResult(command)
	if err != nil {
		logs.Error(fmt.Sprintf("database proxy: %s", err.Error()))
	}
}

// getProxyConn returns another connection of the proxy that the user holds, for a client cancelling what it runs.
func getProxyConn(id uint32, user *casdoorsdk.User) *proxyConn {
	value, ok := conns.Load(id)
	if !ok {
		return nil
	}
	p := value.(*proxyConn)
	if user != nil && (p.user.Owner != user.Owner || p.user.Name != user.Name) {
		return nil
	}
	return p
}
