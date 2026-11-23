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

package rbrowser

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/webproxy"
)

// proxy is the only way out of a session's browser: an HTTP proxy on the loopback address that
// lets the browser reach its website and nothing else. The website's connections go through the
// asset's network gateway, with its certificate checked or pinned like the reverse proxy does;
// the browser is given a certificate of the session for it, which only this browser trusts.
type proxy struct {
	target    *url.URL
	authority string
	username  string
	password  string
	clientIp  string

	listener  net.Listener
	server    *http.Server
	inner     *http.Server
	conns     *connListener
	transport *object.WebTransport
	tlsConfig *tls.Config
	// spki is the hash of the public key of the session's certificate, for --ignore-certificate-errors-spki-list
	spki string
}

func newProxy(asset *object.Asset, basicAuth bool, clientIp string) (*proxy, error) {
	target, err := asset.GetWebUrl()
	if err != nil {
		return nil, err
	}
	transport, err := object.NewWebTransport(asset)
	if err != nil {
		return nil, err
	}

	p := &proxy{
		target:    target,
		authority: strings.ToLower(net.JoinHostPort(target.Hostname(), target.Port())),
		clientIp:  clientIp,
		transport: transport,
		conns:     &connListener{ch: make(chan net.Conn), done: make(chan struct{})},
	}
	if basicAuth {
		p.username, p.password = asset.Username, asset.Password
	}

	if target.Scheme == "https" {
		err = p.makeCertificate()
		if err != nil {
			transport.Close()
			return nil, err
		}
	}

	p.listener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		transport.Close()
		return nil, err
	}

	forward := &httputil.ReverseProxy{
		Rewrite:      p.rewrite,
		Transport:    transport,
		ErrorHandler: p.handleError,
		ErrorLog:     log.New(io.Discard, "", 0),
	}
	p.inner = &http.Server{Handler: forward, ErrorLog: log.New(io.Discard, "", 0), ReadHeaderTimeout: time.Minute}
	p.server = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.serve(w, r, forward)
	}), ErrorLog: log.New(io.Discard, "", 0), ReadHeaderTimeout: time.Minute}

	go func() {
		_ = p.server.Serve(p.listener)
	}()
	go func() {
		_ = p.inner.Serve(p.conns)
	}()
	return p, nil
}

func (p *proxy) getAddress() string {
	return p.listener.Addr().String()
}

func (p *proxy) makeCertificate() error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: p.target.Hostname(), Organization: []string{"Casvisor"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(7 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(p.target.Hostname()); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{p.target.Hostname()}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}

	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	p.spki = base64.StdEncoding.EncodeToString(sum[:])
	p.tlsConfig = &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key, Leaf: cert}},
		NextProtos:   []string{"http/1.1"},
		MinVersion:   tls.VersionTLS12,
	}
	return nil
}

// serve lets through the requests for the website: CONNECT to its address, for https and
// WebSockets, and plain requests to its address over http.
func (p *proxy) serve(w http.ResponseWriter, r *http.Request, forward http.Handler) {
	if r.Method == http.MethodConnect {
		if strings.ToLower(r.Host) != p.authority {
			writePage(w, http.StatusForbidden, "The address can't be reached", "Casvisor only lets this browser reach its website.")
			return
		}

		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		_, err = conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		if err != nil {
			_ = conn.Close()
			return
		}

		var tunnel net.Conn = &bufferedConn{Conn: conn, r: rw.Reader}
		if p.tlsConfig != nil {
			tunnel = tls.Server(tunnel, p.tlsConfig)
		}
		select {
		case p.conns.ch <- tunnel:
		case <-p.conns.done:
			_ = conn.Close()
		}
		return
	}

	if !strings.EqualFold(r.URL.Scheme, p.target.Scheme) || strings.ToLower(hostWithPort(r.URL)) != p.authority {
		writePage(w, http.StatusForbidden, "The address can't be reached", "Casvisor only lets this browser reach its website.")
		return
	}
	forward.ServeHTTP(w, r)
}

func (p *proxy) rewrite(pr *httputil.ProxyRequest) {
	out := pr.Out
	out.URL.Scheme = p.target.Scheme
	out.URL.Host = p.target.Host
	out.Host = pr.In.Host
	out.Header.Del("Proxy-Authorization")
	out.Header.Set("X-Forwarded-For", p.clientIp)
	if p.username != "" || p.password != "" {
		out.SetBasicAuth(p.username, p.password)
	}
}

func (p *proxy) handleError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, io.EOF) {
		return
	}
	writePage(w, http.StatusBadGateway, "The website can't be reached", err.Error())
}

func writePage(w http.ResponseWriter, status int, title string, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.WriteHeader(status)
	_, _ = w.Write(webproxy.RenderPage(title, message, ""))
}

func (p *proxy) close() {
	_ = p.server.Close()
	p.conns.close()
	_ = p.inner.Close()
	p.transport.Close()
}

func hostWithPort(u *url.URL) string {
	if u.Port() != "" {
		return u.Host
	}
	port := "80"
	if strings.EqualFold(u.Scheme, "https") {
		port = "443"
	}
	return net.JoinHostPort(u.Hostname(), port)
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) {
	if c.r.Buffered() > 0 {
		return c.r.Read(b)
	}
	return c.Conn.Read(b)
}

// connListener hands the connections tunneled by CONNECT to the server of the website's requests.
type connListener struct {
	ch   chan net.Conn
	done chan struct{}
	once sync.Once
}

func (l *connListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.ch:
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *connListener) close() {
	l.once.Do(func() {
		close(l.done)
	})
}

func (l *connListener) Close() error {
	l.close()
	return nil
}

func (l *connListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}
}
