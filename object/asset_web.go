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
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProtocolWeb is a website, opened in the browser through Casvisor's reverse proxy.
// The username and password of the asset, when set, are sent as HTTP basic authentication.
const ProtocolWeb = "Web"

const webProbeTimeout = 15 * time.Second

// WebModeBrowser opens a Web asset in a browser that Casvisor runs, whose screen the user sees
// and controls; the website is opened through the reverse proxy otherwise.
const WebModeBrowser = "Browser"

// ProtocolBrowser is the protocol of a session that opens a Web asset in Casvisor's browser.
const ProtocolBrowser = "Browser"

// WebLogin is how Casvisor signs in to a website in its browser: the page with the form, and the
// CSS selectors of the username, the password and the button that submits them. NextSelector is
// the button between the username and the password of a sign-in in two steps.
type WebLogin struct {
	LoginUrl         string `json:"loginUrl"`
	UsernameSelector string `json:"usernameSelector"`
	PasswordSelector string `json:"passwordSelector"`
	NextSelector     string `json:"nextSelector"`
	SubmitSelector   string `json:"submitSelector"`
}

// getWebUrl reads the host of a web asset, either an address like http://10.0.0.5:3000/grafana/,
// whose path and query are the page opened first, or a host name whose port is the port of the
// asset, served over https on 443 and over http on the other ports.
func getWebUrl(host string, port int) (*url.URL, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, fmt.Errorf("the address of the website is empty")
	}
	if !strings.Contains(host, "://") {
		scheme := "http"
		if port == 443 {
			scheme = "https"
		}
		if port <= 0 {
			port = 80
		}
		host = scheme + "://" + net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port))
	}

	u, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("the address of the website: %s is invalid: %s", host, err.Error())
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("the address of the website: %s must start with http:// or https://", host)
	}
	if u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return nil, fmt.Errorf("the address of the website: %s is invalid", host)
	}
	if u.Port() == "" {
		defaultPort := "80"
		if u.Scheme == "https" {
			defaultPort = "443"
		}
		u.Host = net.JoinHostPort(u.Hostname(), defaultPort)
	}
	if u.Path == "" {
		u.Path = "/"
	}
	return u, nil
}

// GetWebUrl returns the address of a web asset, with its port.
func (asset *Asset) GetWebUrl() (*url.URL, error) {
	if asset.Protocol != ProtocolWeb {
		return nil, fmt.Errorf("the asset: %s is not a website", asset.GetId())
	}
	return getWebUrl(asset.Host, asset.Port)
}

// normalizeWeb keeps the port of the asset the same as the port of its address,
// which the port forwarding of the SSH gateway checks, and the sign-in page on the website.
func (asset *Asset) normalizeWeb() error {
	if asset.Protocol != ProtocolWeb {
		asset.WebMode = ""
		asset.WebLogin = nil
		return nil
	}

	asset.Host = strings.TrimSpace(asset.Host)
	u, err := getWebUrl(asset.Host, asset.Port)
	if err != nil {
		return err
	}
	asset.Port, _ = strconv.Atoi(u.Port())

	if asset.WebMode != "" && asset.WebMode != WebModeBrowser {
		return fmt.Errorf("the way to open the website: %s is invalid", asset.WebMode)
	}
	login := asset.WebLogin
	if login == nil {
		return nil
	}
	login.LoginUrl = strings.TrimSpace(login.LoginUrl)
	login.UsernameSelector = strings.TrimSpace(login.UsernameSelector)
	login.PasswordSelector = strings.TrimSpace(login.PasswordSelector)
	login.NextSelector = strings.TrimSpace(login.NextSelector)
	login.SubmitSelector = strings.TrimSpace(login.SubmitSelector)
	if *login == (WebLogin{}) {
		asset.WebLogin = nil
		return nil
	}
	if login.LoginUrl != "" {
		loginUrl, err := u.Parse(login.LoginUrl)
		if err != nil || !strings.EqualFold(loginUrl.Scheme, u.Scheme) || !strings.EqualFold(getUrlHost(loginUrl), u.Host) {
			return fmt.Errorf("the sign-in page: %s must be on the website: %s://%s", login.LoginUrl, u.Scheme, u.Host)
		}
		login.LoginUrl = loginUrl.String()
	}
	if login.UsernameSelector == "" && login.PasswordSelector == "" {
		return fmt.Errorf("the sign-in of the website needs the selector of its username or password field")
	}
	return nil
}

func getUrlHost(u *url.URL) string {
	if u.Port() != "" {
		return u.Host
	}
	port := "80"
	if strings.EqualFold(u.Scheme, "https") {
		port = "443"
	}
	return net.JoinHostPort(u.Hostname(), port)
}

func (asset *Asset) normalizeAddress() error {
	err := asset.normalizeKubernetes()
	if err != nil {
		return err
	}
	return asset.normalizeWeb()
}

// WebTransport sends the requests of a website through the asset's network gateway. It
// keeps the connections it opened, so that closing it ends every request and WebSocket of a session.
type WebTransport struct {
	*http.Transport

	asset      *Asset
	serverName string
	// noSave keeps a pinned fingerprint in the asset only, for testing an asset before it is saved
	noSave bool

	mu     sync.Mutex
	conns  map[*webConn]struct{}
	closed bool
}

type webConn struct {
	net.Conn
	transport *WebTransport
	once      sync.Once
}

func (c *webConn) Close() error {
	c.once.Do(func() {
		c.transport.mu.Lock()
		delete(c.transport.conns, c)
		c.transport.mu.Unlock()
	})
	return c.Conn.Close()
}

func NewWebTransport(asset *Asset) (*WebTransport, error) {
	u, err := asset.GetWebUrl()
	if err != nil {
		return nil, err
	}

	t := &WebTransport{asset: asset, serverName: u.Hostname(), conns: map[*webConn]struct{}{}}
	t.Transport = &http.Transport{
		Proxy:               nil,
		DialContext:         t.dial,
		DialTLSContext:      t.dialTls,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   false,
	}
	return t, nil
}

// dial ignores the address, which is the asset's own: the connection always goes to the asset.
func (t *WebTransport) dial(ctx context.Context, network string, address string) (net.Conn, error) {
	t.mu.Lock()
	closed := t.closed
	t.mu.Unlock()
	if closed {
		return nil, fmt.Errorf("the session has ended")
	}

	conn, err := DialAsset(t.asset)
	if err != nil {
		return nil, err
	}

	c := &webConn{Conn: conn, transport: t}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		_ = conn.Close()
		return nil, fmt.Errorf("the session has ended")
	}
	t.conns[c] = struct{}{}
	t.mu.Unlock()
	return c, nil
}

func (t *WebTransport) dialTls(ctx context.Context, network string, address string) (net.Conn, error) {
	conn, err := t.dial(ctx, network, address)
	if err != nil {
		return nil, err
	}

	serverName := t.serverName
	if net.ParseIP(serverName) != nil {
		serverName = ""
	}
	tlsConn := tls.Client(conn, &tls.Config{
		ServerName: serverName,
		// the certificate is checked by verifyConnection, against the system roots or the pinned public key
		InsecureSkipVerify: true,
		VerifyConnection:   t.verifyConnection,
		NextProtos:         []string{"http/1.1"},
	})

	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	err = tlsConn.HandshakeContext(ctx)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return tlsConn, nil
}

// verifyConnection accepts a certificate the system trusts for the website's host. Any other
// certificate, like a self-signed one, is pinned by its public key the first time it is seen, and
// has to keep that key afterwards.
func (t *WebTransport) verifyConnection(state tls.ConnectionState) error {
	if len(state.PeerCertificates) == 0 {
		return fmt.Errorf("the website of the asset: %s sent no certificate", t.asset.GetId())
	}

	intermediates := x509.NewCertPool()
	for _, cert := range state.PeerCertificates[1:] {
		intermediates.AddCert(cert)
	}
	_, err := state.PeerCertificates[0].Verify(x509.VerifyOptions{DNSName: t.serverName, Intermediates: intermediates})
	if err == nil {
		return nil
	}

	fingerprint := getCertificateFingerprint(state.PeerCertificates[0].RawSubjectPublicKeyInfo)
	t.mu.Lock()
	defer t.mu.Unlock()

	pinned := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(t.asset.Fingerprint), ":", ""))
	if pinned == "" {
		t.asset.Fingerprint = fingerprint
		if t.noSave {
			return nil
		}
		return UpdateAssetFingerprint(t.asset.GetId(), fingerprint)
	}
	if pinned != fingerprint {
		return fmt.Errorf("the certificate of the website of the asset: %s is not trusted (%s), and its public key has changed from the pinned %s to %s, clear the fingerprint of the asset if the change is expected", t.asset.GetId(), err.Error(), pinned, fingerprint)
	}
	return nil
}

// Close ends every connection of the transport, and no more can be opened.
func (t *WebTransport) Close() {
	t.mu.Lock()
	t.closed = true
	conns := make([]*webConn, 0, len(t.conns))
	for c := range t.conns {
		conns = append(conns, c)
	}
	t.mu.Unlock()

	for _, c := range conns {
		_ = c.Close()
	}
	t.CloseIdleConnections()
}

// probeWeb opens the first page of the website and returns its status line and server.
func probeWeb(asset *Asset) (string, error) {
	u, err := asset.GetWebUrl()
	if err != nil {
		return "", err
	}
	transport, err := NewWebTransport(asset)
	if err != nil {
		return "", err
	}
	transport.noSave = true
	defer transport.Close()

	client := &http.Client{
		Transport: transport,
		Timeout:   webProbeTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	if asset.Username != "" || asset.Password != "" {
		req.SetBasicAuth(asset.Username, asset.Password)
	}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	_ = res.Body.Close()

	version := fmt.Sprintf("HTTP %s", res.Status)
	if server := res.Header.Get("Server"); server != "" {
		version += ", " + server
	}
	return version, nil
}
