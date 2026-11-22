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

package webproxy

import (
	"context"
	"errors"
	"fmt"
	"html"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"time"

	beecontext "github.com/beego/beego/context"
	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
)

const (
	maxCommandText  = 4000
	maxCommandError = 1000
)

var errDownloadDenied = errors.New("downloading is not permitted")

type stateKey struct{}

// requestState is what a request through the proxy learns on its way.
type requestState struct {
	host    string
	scheme  string
	actions []string
	blocked string
	err     string
}

func getState(r *http.Request) *requestState {
	state, _ := r.Context().Value(stateKey{}).(*requestState)
	if state == nil {
		state = &requestState{}
	}
	return state
}

// Filter serves the requests for the subdomains of webAssetDomain, before beego reads their
// bodies or starts a beego session, and lets every other request through.
func Filter(ctx *beecontext.Context) {
	label, ok := matchHost(ctx.Request.Host)
	if !ok {
		return
	}

	serve(ctx.ResponseWriter.ResponseWriter, ctx.Request, label)
	ctx.ResponseWriter.Started = true
}

func serve(w http.ResponseWriter, r *http.Request, label string) {
	if !labelPattern.MatchString(label) {
		writePage(w, http.StatusNotFound, "Not found", "There is no website here.", "")
		return
	}
	if strings.HasPrefix(r.URL.Path, pathPrefix) {
		serveOwn(w, r, label)
		return
	}

	s := getSessionByCookie(r)
	if s == nil {
		writePage(w, http.StatusUnauthorized, "Open the website from Casvisor", "This website is reached through Casvisor. Open it from the asset list of Casvisor.", "")
		return
	}
	if s.label != label {
		writePage(w, http.StatusForbidden, "Open the website from Casvisor", "Your session is for another website. Open this one from the asset list of Casvisor.", s.casvisorUrl)
		return
	}
	if closed, message := s.getEnded(); closed {
		writePage(w, http.StatusGone, "The session has ended", message+". Open the website again from Casvisor.", s.casvisorUrl)
		return
	}

	s.serve(w, r)
}

// serveOwn serves the paths Casvisor keeps on every website's subdomain.
func serveOwn(w http.ResponseWriter, r *http.Request, label string) {
	switch strings.TrimPrefix(r.URL.Path, pathPrefix) {
	case "signin":
		s := takeSigninToken(r.URL.Query().Get("token"))
		if s == nil || s.label != label {
			writePage(w, http.StatusForbidden, "The link has expired", "This link can only be used once, within a minute. Open the website again from Casvisor.", "")
			return
		}
		if closed, message := s.getEnded(); closed {
			writePage(w, http.StatusGone, "The session has ended", message+". Open the website again from Casvisor.", s.casvisorUrl)
			return
		}

		// the browser leaves the session it had on this website for the new one
		if old := getSessionByCookie(r); old != nil && old != s && old.label == label {
			if closed, _ := old.getEnded(); !closed {
				err := object.CloseSession(old.id, 0, "Replaced by a new session in the same browser")
				if err != nil {
					logs.Error(fmt.Sprintf("webproxy.serveOwn(): failed to close the session: %s: %s", old.id, err.Error()))
				}
			}
		}

		http.SetCookie(w, &http.Cookie{
			Name:     cookieName,
			Value:    s.cookie,
			Path:     "/",
			HttpOnly: true,
			Secure:   s.scheme == "https",
			SameSite: http.SameSiteLaxMode,
		})
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		http.Redirect(w, r, s.target.RequestURI(), http.StatusFound)
	case "signout":
		s := getSessionByCookie(r)
		if s != nil && s.label == label {
			err := object.CloseSession(s.id, 0, "Signed out")
			if err != nil {
				logs.Error(fmt.Sprintf("webproxy.serveOwn(): failed to close the session: %s: %s", s.id, err.Error()))
			}
		}
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
		link := ""
		if s != nil {
			link = s.casvisorUrl
		}
		writePage(w, http.StatusOK, "Signed out", "The session has ended.", link)
	default:
		writePage(w, http.StatusNotFound, "Not found", "There is nothing here.", "")
	}
}

func (s *Session) serve(w http.ResponseWriter, r *http.Request) {
	s.begin()
	defer s.end()

	start := time.Now()
	text := r.Method + " " + r.URL.RequestURI()
	if runes := []rune(text); len(runes) > maxCommandText {
		text = string(runes[:maxCommandText])
	}
	command := &object.Command{
		Owner: s.asset.Owner,
		// microseconds keep the requests in order
		CreatedTime: start.Format("2006-01-02T15:04:05.000000Z07:00"),
		Session:     s.id,
		Asset:       s.asset.GetId(),
		User:        s.user.Owner + "/" + s.user.Name,
		ClientIp:    util.GetClientIp(r),
		Text:        text,
		Protocol:    object.ProtocolWeb,
	}

	actions := s.getActions()
	if isUpload(r) && !slices.Contains(actions, object.ActionUpload) {
		s.block(w, command, "uploading files is not permitted", "Uploading is not permitted", "You have no permission to upload files to this website.")
		return
	}

	rule, err := object.MatchCommandRule(s.user, s.asset, text)
	if err != nil {
		logs.Error(fmt.Sprintf("webproxy.serve(): failed to match the command rules: %s", err.Error()))
		s.block(w, command, "the request is blocked because the command rules can't be read", "The request is blocked", "The command rules can't be read.")
		return
	}
	if rule != nil {
		command.Rule = rule.GetId()
		command.Action = rule.Action
		if rule.Action == object.CommandActionBlock {
			ruleName := rule.DisplayName
			if ruleName == "" {
				ruleName = rule.Name
			}
			s.block(w, command, fmt.Sprintf("the request is blocked by the rule: %s", ruleName), "The request is blocked", fmt.Sprintf("The request is blocked by the rule: %s.", ruleName))
			return
		}
	}

	state := &requestState{host: r.Host, scheme: s.scheme, actions: actions}
	r = r.WithContext(context.WithValue(r.Context(), stateKey{}, state))
	recorder := &statusRecorder{ResponseWriter: w}
	if s.forward(recorder, r) {
		state.err = "the response was cut off: " + state.err
	}

	if state.blocked != "" {
		command.Action = object.CommandActionBlock
		command.Error = state.blocked
	} else if state.err != "" {
		command.Error = state.err
	}
	if command.Action == "" && !isNavigation(r) && !isUpgrade(r) && isSafeMethod(r.Method) {
		return
	}

	command.StatusCode = recorder.status
	if command.StatusCode == 0 && isUpgrade(r) && state.err == "" {
		command.StatusCode = http.StatusSwitchingProtocols
	}
	command.Duration = time.Since(start).Milliseconds()
	s.save(command)
}

// forward sends the request to the website. When the proxy has to give up on a response it
// already started, the connection is closed, so that the browser doesn't take the part it got
// for the whole, and the proxy's way of saying so is not left to beego to log as a crash.
func (s *Session) forward(w http.ResponseWriter, r *http.Request) (aborted bool) {
	defer func() {
		if err := recover(); err != nil {
			if err != http.ErrAbortHandler {
				panic(err)
			}
			aborted = true
			if conn, _, hijackErr := http.NewResponseController(w).Hijack(); hijackErr == nil {
				_ = conn.Close()
			}
		}
	}()

	s.proxy.ServeHTTP(w, r)
	return false
}

func (s *Session) block(w http.ResponseWriter, command *object.Command, reason string, title string, message string) {
	command.Action = object.CommandActionBlock
	command.Error = reason
	command.StatusCode = http.StatusForbidden
	s.save(command)
	writePage(w, http.StatusForbidden, title, message, "")
}

func (s *Session) save(command *object.Command) {
	if runes := []rune(command.Error); len(runes) > maxCommandError {
		command.Error = string(runes[:maxCommandError])
	}
	err := object.AddCommand(command)
	if err != nil {
		logs.Error(fmt.Sprintf("webproxy.save(): %s", err.Error()))
	}
}

// targetHost is the Host header the website expects, without a default port.
func (s *Session) targetHost() string {
	port := s.target.Port()
	if (s.target.Scheme == "http" && port == "80") || (s.target.Scheme == "https" && port == "443") {
		hostname := s.target.Hostname()
		if strings.Contains(hostname, ":") {
			return "[" + hostname + "]"
		}
		return hostname
	}
	return s.target.Host
}

func (s *Session) targetOrigin() string {
	return s.target.Scheme + "://" + s.targetHost()
}

func (s *Session) rewrite(pr *httputil.ProxyRequest) {
	state := getState(pr.In)
	out := pr.Out

	out.URL.Scheme = s.target.Scheme
	out.URL.Host = s.target.Host
	out.Host = s.targetHost()

	removeCookie(out.Header, cookieName)
	out.Header.Set("X-Forwarded-For", util.GetClientIp(pr.In))
	out.Header.Set("X-Forwarded-Host", pr.In.Host)
	out.Header.Set("X-Forwarded-Proto", state.scheme)

	// the website sees requests from its own pages, as if it were opened at its own address
	origin := state.scheme + "://" + pr.In.Host
	if out.Header.Get("Origin") == origin {
		out.Header.Set("Origin", s.targetOrigin())
	}
	if referer := out.Header.Get("Referer"); referer == origin || strings.HasPrefix(referer, origin+"/") {
		out.Header.Set("Referer", s.targetOrigin()+strings.TrimPrefix(referer, origin))
	}

	if s.asset.Username != "" || s.asset.Password != "" {
		out.SetBasicAuth(s.asset.Username, s.asset.Password)
	}
}

func (s *Session) modifyResponse(res *http.Response) error {
	state := getState(res.Request)

	if !slices.Contains(state.actions, object.ActionDownload) && isAttachment(res.Header) {
		_ = res.Body.Close()
		return errDownloadDenied
	}

	if location := res.Header.Get("Location"); location != "" {
		res.Header.Set("Location", s.rewriteLocation(location, state))
	}

	if cookies := res.Header.Values("Set-Cookie"); len(cookies) != 0 {
		res.Header.Del("Set-Cookie")
		for _, cookie := range cookies {
			if rewritten := rewriteCookie(cookie, state.scheme); rewritten != "" {
				res.Header.Add("Set-Cookie", rewritten)
			}
		}
	}

	// the policies of Casvisor's domain are not the website's to set
	res.Header.Del("Strict-Transport-Security")
	res.Header.Del("Alt-Svc")
	return nil
}

// rewriteLocation points a redirect to the website's own address back to its subdomain.
func (s *Session) rewriteLocation(location string, state *requestState) string {
	u, err := url.Parse(location)
	if err != nil || !u.IsAbs() || !strings.EqualFold(u.Scheme, s.target.Scheme) {
		return location
	}

	port := u.Port()
	if port == "" {
		port = "80"
		if strings.EqualFold(u.Scheme, "https") {
			port = "443"
		}
	}
	if !strings.EqualFold(net.JoinHostPort(u.Hostname(), port), s.target.Host) {
		return location
	}

	u.Scheme = state.scheme
	u.Host = state.host
	return u.String()
}

// rewriteCookie keeps a cookie of the website on its subdomain, and, when Casvisor is reached
// over http, drops what a browser would refuse without https.
func rewriteCookie(raw string, scheme string) string {
	cookie, err := http.ParseSetCookie(raw)
	if err != nil {
		return raw
	}
	if cookie.Name == cookieName {
		return ""
	}

	cookie.Domain = ""
	if scheme != "https" {
		cookie.Secure = false
		cookie.Partitioned = false
		if cookie.SameSite == http.SameSiteNoneMode {
			cookie.SameSite = http.SameSiteLaxMode
		}
	}
	if res := cookie.String(); res != "" {
		return res
	}
	return raw
}

func (s *Session) handleError(w http.ResponseWriter, r *http.Request, err error) {
	state := getState(r)
	if errors.Is(err, errDownloadDenied) {
		state.blocked = "downloading files is not permitted"
		writePage(w, http.StatusForbidden, "Downloading is not permitted", "You have no permission to download files from this website.", "")
		return
	}
	if errors.Is(err, context.Canceled) {
		state.err = "the browser cancelled the request"
		return
	}

	state.err = err.Error()
	if closed, message := s.getEnded(); closed {
		writePage(w, http.StatusGone, "The session has ended", message+". Open the website again from Casvisor.", s.casvisorUrl)
		return
	}
	writePage(w, http.StatusBadGateway, "The website can't be reached", err.Error(), "")
}

// removeCookie takes a cookie out of the Cookie header, leaving the others as they were sent.
func removeCookie(header http.Header, name string) {
	values := header.Values("Cookie")
	if len(values) == 0 {
		return
	}

	kept := []string{}
	for _, value := range values {
		for _, part := range strings.Split(value, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			key, _, _ := strings.Cut(part, "=")
			if strings.TrimSpace(key) == name {
				continue
			}
			kept = append(kept, part)
		}
	}

	header.Del("Cookie")
	if len(kept) != 0 {
		header.Set("Cookie", strings.Join(kept, "; "))
	}
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func isUpload(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mediaType == "multipart/form-data"
}

func isAttachment(header http.Header) bool {
	disposition, _, err := mime.ParseMediaType(header.Get("Content-Disposition"))
	return err == nil && disposition == "attachment"
}

func isUpgrade(r *http.Request) bool {
	return r.Header.Get("Upgrade") != ""
}

// isNavigation tells a page the browser opens from a request a page makes.
func isNavigation(r *http.Request) bool {
	if mode := r.Header.Get("Sec-Fetch-Mode"); mode != "" {
		dest := r.Header.Get("Sec-Fetch-Dest")
		return mode == "navigate" || dest == "document" || dest == "iframe" || dest == "frame"
	}
	return r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html")
}

// statusRecorder notes the status sent to the browser. The informational statuses other than
// switching protocols are not passed on, for their headers to come with the final response.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		return
	}
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

const pageTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<style>
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;font-family:system-ui,-apple-system,"Segoe UI",sans-serif;background:#f6f7f9;color:#1f2328}
main{max-width:520px;margin:16px;padding:32px;background:#fff;border:1px solid #e3e5e8;border-radius:8px}
h1{margin:0 0 12px;font-size:20px}
p{margin:0 0 16px;line-height:1.6;color:#4b5159;word-break:break-word}
a{color:#2563eb}
small{color:#8a9099}
@media (prefers-color-scheme:dark){body{background:#111317;color:#e6e8eb}main{background:#1a1d22;border-color:#2b3038}p{color:#b3b8bf}a{color:#7aa7ff}}
</style>
</head>
<body><main><h1>%s</h1><p>%s</p>%s<small>Casvisor</small></main></body>
</html>
`

func writePage(w http.ResponseWriter, status int, title string, message string, link string) {
	linkHtml := ""
	if link != "" {
		linkHtml = fmt.Sprintf(`<p><a href="%s">Go to Casvisor</a></p>`, html.EscapeString(link))
	}

	header := w.Header()
	for key := range header {
		header.Del(key)
	}
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, pageTemplate, html.EscapeString(title), html.EscapeString(title), html.EscapeString(message), linkHtml)
}
