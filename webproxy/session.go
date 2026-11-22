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

// Package webproxy opens the websites kept as Web assets. Each asset is served on a subdomain of
// its own under webAssetDomain, so that the website keeps its paths and cookies as they are,
// and every request goes through the session, its permission, the command rules and the audit.
package webproxy

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
)

const (
	pathPrefix         = "/__casvisor__/"
	cookieName         = "casvisor_web_session"
	signinTokenTtl     = time.Minute
	closedSessionTtl   = time.Hour
	defaultIdleMinutes = 30
)

var labelPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// GetLabel returns the subdomain of a web asset: <name>--<organization> when both can be
// part of a host name, and a hash of them otherwise.
func GetLabel(owner string, name string) string {
	if labelPattern.MatchString(name) && labelPattern.MatchString(owner) && !strings.Contains(name, "--") && !strings.Contains(owner, "--") && len(name)+len(owner)+2 <= 63 {
		return name + "--" + owner
	}
	sum := sha256.Sum256([]byte(owner + "/" + name))
	return "w" + hex.EncodeToString(sum[:8])
}

type domainConfig struct {
	// scheme is how users reach the websites, empty to take it from the request
	scheme   string
	host     string
	hostname string
}

var (
	domainOnce  sync.Once
	domainValue *domainConfig
	domainErr   error
)

// getDomain reads webAssetDomain, like web.casvisor.example.com, https://web.casvisor.example.com
// or web.localhost:19000, whose subdomains all have to reach Casvisor.
func getDomain() (*domainConfig, error) {
	domainOnce.Do(func() {
		value := strings.TrimSpace(conf.GetConfigString("webAssetDomain"))
		if value == "" {
			return
		}

		scheme := ""
		if i := strings.Index(value, "://"); i >= 0 {
			scheme = strings.ToLower(value[:i])
			value = value[i+3:]
		}
		value = strings.TrimRight(value, "/")
		u, err := url.Parse("http://" + value)
		if err != nil || u.Hostname() == "" || u.Path != "" || u.User != nil || u.RawQuery != "" || (scheme != "" && scheme != "http" && scheme != "https") {
			domainErr = fmt.Errorf("webAssetDomain: %s is invalid, it should be a domain like web.casvisor.example.com", conf.GetConfigString("webAssetDomain"))
			return
		}
		domainValue = &domainConfig{scheme: scheme, host: strings.ToLower(u.Host), hostname: strings.ToLower(u.Hostname())}
	})
	return domainValue, domainErr
}

// matchHost tells whether a request is for a website, and the subdomain it is for.
func matchHost(host string) (string, bool) {
	d, _ := getDomain()
	if d == nil {
		return "", false
	}

	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	suffix := "." + d.hostname
	if !strings.HasSuffix(host, suffix) {
		return "", false
	}
	return strings.TrimSuffix(host, suffix), true
}

// Session is a web session, held by the cookie its browser was given on the website's subdomain.
type Session struct {
	id          string
	user        *casdoorsdk.User
	asset       *object.Asset
	label       string
	target      *url.URL
	scheme      string
	casvisorUrl string
	cookie      string
	transport   *object.WebTransport
	proxy       *httputil.ReverseProxy

	mu       sync.Mutex
	actions  []string
	lastSeen time.Time
	active   int
	closed   bool
	closedAt time.Time
	message  string
}

type signinToken struct {
	session *Session
	expires time.Time
}

var (
	mu               sync.Mutex
	sessionsByCookie = map[string]*Session{}
	sessionsById     = map[string]*Session{}
	signinTokens     = map[string]*signinToken{}
)

func newToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// SessionOptions is what a web session starts with: the asset carries the credentials of
// the account the session uses, and the request is the one to Casvisor that opens the website.
type SessionOptions struct {
	Id      string
	User    *casdoorsdk.User
	Asset   *object.Asset
	Actions []string
	Request *http.Request
}

// NewSession starts a web session and returns the address that signs the browser in to the
// website's subdomain; the address can be used once, within a minute.
func NewSession(o *SessionOptions) (*Session, string, error) {
	d, err := getDomain()
	if err != nil {
		return nil, "", err
	}
	if d == nil {
		return nil, "", fmt.Errorf("websites can't be opened until webAssetDomain is set in conf/app.conf, as a domain whose subdomains all reach Casvisor, like web.casvisor.example.com")
	}

	target, err := o.Asset.GetWebUrl()
	if err != nil {
		return nil, "", err
	}
	transport, err := object.NewWebTransport(o.Asset)
	if err != nil {
		return nil, "", err
	}

	scheme := d.scheme
	if scheme == "" {
		scheme = util.GetRequestScheme(o.Request)
	}
	s := &Session{
		id:          o.Id,
		user:        o.User,
		asset:       o.Asset,
		label:       GetLabel(o.Asset.Owner, o.Asset.Name),
		target:      target,
		scheme:      scheme,
		casvisorUrl: util.GetRequestScheme(o.Request) + "://" + o.Request.Host + "/",
		cookie:      newToken(),
		transport:   transport,
		actions:     o.Actions,
		lastSeen:    time.Now(),
	}
	s.proxy = &httputil.ReverseProxy{
		Rewrite:        s.rewrite,
		Transport:      transport,
		ModifyResponse: s.modifyResponse,
		ErrorHandler:   s.handleError,
		FlushInterval:  -1,
		ErrorLog:       log.New(io.Discard, "", 0),
	}

	token := newToken()
	mu.Lock()
	sessionsByCookie[s.cookie] = s
	sessionsById[s.id] = s
	signinTokens[token] = &signinToken{session: s, expires: time.Now().Add(signinTokenTtl)}
	mu.Unlock()

	object.AddSessionCloser(s.id, func(code int, msg string) {
		s.close(msg)
	})
	return s, fmt.Sprintf("%s://%s.%s%ssignin?token=%s", scheme, s.label, d.host, pathPrefix, token), nil
}

// SetActions passes the actions the user holds on the asset now, which decide uploading and downloading.
func (s *Session) SetActions(actions []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actions = actions
}

func (s *Session) getActions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.actions
}

func (s *Session) begin() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active++
	s.lastSeen = time.Now()
}

func (s *Session) end() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active--
	s.lastSeen = time.Now()
}

func (s *Session) getEnded() (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed, s.message
}

func (s *Session) idleFor() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active > 0 || s.closed {
		return 0
	}
	return time.Since(s.lastSeen)
}

// close ends the requests and WebSockets of the session. The cookie is kept for a while, to
// tell the browser why the session ended.
func (s *Session) close(message string) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.closedAt = time.Now()
	s.message = message
	s.mu.Unlock()

	mu.Lock()
	delete(sessionsById, s.id)
	for token, t := range signinTokens {
		if t.session == s {
			delete(signinTokens, token)
		}
	}
	mu.Unlock()

	s.transport.Close()
}

func getSessionByCookie(r *http.Request) *Session {
	cookie, err := r.Cookie(cookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}

	mu.Lock()
	defer mu.Unlock()
	return sessionsByCookie[cookie.Value]
}

// takeSigninToken returns the session of a sign-in address, only once.
func takeSigninToken(token string) *Session {
	mu.Lock()
	defer mu.Unlock()

	t, ok := signinTokens[token]
	delete(signinTokens, token)
	if !ok || time.Now().After(t.expires) {
		return nil
	}
	return t.session
}

func getIdleTimeout() time.Duration {
	minutes, err := strconv.Atoi(conf.GetConfigString("webSessionIdleMinutes"))
	if err != nil || minutes <= 0 {
		minutes = defaultIdleMinutes
	}
	return time.Duration(minutes) * time.Minute
}

// sweep ends the sessions nobody has used for the idle timeout, and forgets ended sessions
// and unused sign-in addresses.
func sweep() {
	idleTimeout := getIdleTimeout()
	now := time.Now()

	mu.Lock()
	live := []*Session{}
	for _, s := range sessionsById {
		live = append(live, s)
	}
	for cookie, s := range sessionsByCookie {
		s.mu.Lock()
		expired := s.closed && now.Sub(s.closedAt) > closedSessionTtl
		s.mu.Unlock()
		if expired {
			delete(sessionsByCookie, cookie)
		}
	}
	for token, t := range signinTokens {
		if now.After(t.expires) {
			delete(signinTokens, token)
		}
	}
	mu.Unlock()

	for _, s := range live {
		if s.idleFor() < idleTimeout {
			continue
		}
		err := object.CloseSession(s.id, 0, fmt.Sprintf("Idle for %d minutes", int(idleTimeout.Minutes())))
		if err != nil {
			logs.Error(fmt.Sprintf("webproxy.sweep(): failed to close the session: %s: %s", s.id, err.Error()))
		}
	}
}

// Start ends the web sessions left connected by an earlier run, whose cookies are gone with
// it, and then ends idle sessions every half a minute.
func Start() {
	if _, err := getDomain(); err != nil {
		logs.Error(err.Error())
	}

	sessions, err := object.GetSessionsByStatus([]string{object.Connected})
	if err != nil {
		logs.Error(fmt.Sprintf("webproxy.Start(): %s", err.Error()))
	}
	for _, session := range sessions {
		if session.Protocol != object.ProtocolWeb {
			continue
		}
		err = object.CloseDBSession(session.GetId(), 0, "Ended, Casvisor was restarted")
		if err != nil {
			logs.Error(fmt.Sprintf("webproxy.Start(): %s", err.Error()))
		}
	}

	go func() {
		for range time.Tick(30 * time.Second) {
			sweep()
		}
	}()
}
