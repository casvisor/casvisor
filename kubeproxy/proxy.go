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

// Package kubeproxy lets kubectl and other Kubernetes clients reach the API server of a
// Kubernetes asset through Casvisor, from a kubeconfig the web console hands out. The kubeconfig
// points to /kubernetes/<organization>/<asset> on Casvisor's own address, with a token of
// Casvisor; Casvisor checks every request against the user's permission, its namespaces and the
// command rules, records it, and sends it on with the asset's own token through its network
// gateway. Shells opened with kubectl exec are recorded like the web terminal.
package kubeproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	beecontext "github.com/beego/beego/context"
	"github.com/beego/beego/logs"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
)

// Path is where the kubeconfigs of Casvisor point kubectl to.
const Path = "/kubernetes/"

const (
	maxCommandText  = 4000
	maxCommandError = 1000
)

// GetServerUrl returns the address of the proxy of the asset, as the server of a kubeconfig.
func GetServerUrl(r *http.Request, asset *object.Asset) string {
	return fmt.Sprintf("%s://%s%s%s/%s", util.GetRequestScheme(r), r.Host, Path, url.PathEscape(asset.Owner), url.PathEscape(asset.Name))
}

// Filter takes the requests of kubectl before beego reads their bodies or starts a session.
func Filter(ctx *beecontext.Context) {
	if !strings.HasPrefix(ctx.Request.URL.Path, Path) {
		return
	}

	serve(ctx.ResponseWriter.ResponseWriter, ctx.Request)
	ctx.ResponseWriter.Started = true
}

// writeStatus answers with a Status of Kubernetes, which kubectl shows as it is.
func writeStatus(w http.ResponseWriter, code int, reason string, message string) {
	data, _ := json.Marshal(map[string]interface{}{
		"kind":       "Status",
		"apiVersion": "v1",
		"metadata":   map[string]interface{}{},
		"status":     "Failure",
		"message":    message,
		"reason":     reason,
		"code":       code,
	})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_, _ = w.Write(data)
}

func writeForbidden(w http.ResponseWriter, message string) {
	writeStatus(w, http.StatusForbidden, "Forbidden", message)
}

func getBearerToken(r *http.Request) string {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

// splitPath reads the asset and the path of the API server from a path of the proxy.
func splitPath(p string) (string, string, bool) {
	rest := strings.TrimPrefix(p, Path)
	owner, rest, ok1 := strings.Cut(rest, "/")
	name, apiPath, _ := strings.Cut(rest, "/")
	apiPath = "/" + apiPath
	if !ok1 || owner == "" || name == "" || path.Clean(apiPath) != strings.TrimSuffix(apiPath, "/") && apiPath != "/" {
		return "", "", false
	}
	return util.GetIdFromOwnerAndName(owner, name), apiPath, true
}

// call is one request through the proxy.
type call struct {
	ts       *tokenSession
	user     *casdoorsdk.User
	asset    *object.Asset
	client   *object.KubernetesClient
	actions  []string
	req      *object.KubernetesRequest
	clientIp string
	command  *object.Command
	start    time.Time
	// isSpdy is set for exec, attach and portforward over SPDY, which kubectl falls back to when
	// the WebSocket is refused: it is refused again, with the same message, and not recorded twice
	isSpdy bool
}

func serve(w http.ResponseWriter, r *http.Request) {
	if conf.GetConfigBool("isDemoMode") {
		writeStatus(w, http.StatusNotFound, "NotFound", "kubectl is not available in demo mode")
		return
	}

	assetId, apiPath, ok := splitPath(r.URL.Path)
	if !ok {
		writeStatus(w, http.StatusNotFound, "NotFound", "the path is not one of an asset, use the kubeconfig downloaded from Casvisor")
		return
	}

	value := getBearerToken(r)
	if value == "" {
		writeStatus(w, http.StatusUnauthorized, "Unauthorized", "use the kubeconfig downloaded from Casvisor")
		return
	}
	ts, err := getTokenSession(value, assetId)
	if err != nil {
		logs.Error(fmt.Sprintf("kubeproxy.serve(): %s", err.Error()))
		writeStatus(w, http.StatusInternalServerError, "InternalError", "the kubeconfig can't be checked")
		return
	}
	if ts == nil {
		writeStatus(w, http.StatusUnauthorized, "Unauthorized", "the kubeconfig has expired or been revoked, download a new one from Casvisor")
		return
	}

	clientIp := util.GetClientIp(r)
	ts.setClientIp(clientIp)
	c := &call{ts: ts, user: ts.getUser(), clientIp: clientIp, start: time.Now()}
	c.req = object.ParseKubernetesRequest(r.Method, apiPath, r.URL.Query())
	c.isSpdy = c.req.IsConnect() && !strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
	text := c.req.GetText() + " (" + r.Method + " " + apiPath
	if r.URL.RawQuery != "" {
		text += "?" + r.URL.RawQuery
	}
	text += ")"
	if runes := []rune(text); len(runes) > maxCommandText {
		text = string(runes[:maxCommandText])
	}
	c.command = &object.Command{
		Session:  ts.token.Session,
		User:     ts.token.User,
		ClientIp: clientIp,
		Text:     text,
		Protocol: object.ProtocolKubernetes,
		// microseconds keep the requests in order
		CreatedTime: c.start.Format("2006-01-02T15:04:05.000000Z07:00"),
	}

	message, err := c.check()
	if err != nil {
		logs.Error(fmt.Sprintf("kubeproxy.serve(): %s", err.Error()))
		writeStatus(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if message != "" {
		c.block(w, message)
		return
	}

	if !c.applyRules(w, r) {
		return
	}

	if c.req.IsConnect() {
		c.serveConnect(w, r)
		return
	}
	c.forward(w, r)
}

// check tells why the user may not send the request, or "" when it may go on.
func (c *call) check() (string, error) {
	asset, err := object.GetAsset(c.ts.token.Asset)
	if err != nil {
		return "", err
	}
	if asset == nil {
		return fmt.Sprintf("the asset: %s has been deleted", c.ts.token.Asset), nil
	}
	c.command.Owner = asset.Owner
	c.command.Asset = asset.GetId()

	actions, scope, err := object.GetKubernetesAccess(c.user, asset, c.ts.token.Account, c.clientIp)
	if err != nil {
		return "", err
	}
	if !slices.Contains(actions, object.ActionConnect) {
		return fmt.Sprintf("you no longer have the permission to connect to the asset: %s", asset.Name), nil
	}
	err = object.CheckConnectMethod(c.user, asset, c.ts.token.Account, object.ConnectMethodKubectl)
	if err != nil {
		return err.Error(), nil
	}
	if !c.req.IsAllowed(scope) {
		if c.req.IsResource && c.req.Namespace == "" && !scope.IsAll() {
			return fmt.Sprintf("you have no permission to %s, your permission only covers the namespaces: %s", c.req.GetText(), strings.Join(scope.GetNames(), ", ")), nil
		}
		return fmt.Sprintf("you have no permission to %s", c.req.GetText()), nil
	}
	if c.req.Resource == "pods" && c.req.Subresource == "portforward" && !slices.Contains(actions, object.ActionForward) {
		return fmt.Sprintf("you have no permission to forward ports to the asset: %s", asset.Name), nil
	}

	if c.ts.token.Account != "" && c.ts.token.Account != asset.Username {
		asset, err = asset.WithAccount(c.ts.token.Account)
		if err != nil {
			return "", err
		}
	}
	client, err := object.NewKubernetesClient(asset)
	if err != nil {
		return err.Error(), nil
	}

	c.asset = asset
	c.client = client
	c.actions = actions
	return "", nil
}

// applyRules applies the command rules to the request, and tells whether it may go on.
func (c *call) applyRules(w http.ResponseWriter, r *http.Request) bool {
	rule, err := object.MatchCommandRule(c.user, c.asset, c.req.GetText())
	if err != nil {
		logs.Error(fmt.Sprintf("kubeproxy.applyRules(): %s", err.Error()))
		c.block(w, "the request is blocked because the command rules can't be read")
		return false
	}
	if rule == nil {
		return true
	}

	c.command.Rule = rule.GetId()
	c.command.Action = rule.Action
	switch rule.Action {
	case object.CommandActionBlock:
		c.block(w, fmt.Sprintf("the request is blocked by the rule: %s", rule.GetDisplayName()))
		return false
	case object.CommandActionReview:
		return c.review(w, rule)
	}
	return true
}

// review lets a request that a Review rule matched through once an administrator has approved
// it. kubectl can't wait for the review, so the request is refused while it waits, and the same
// request sent again after the approval goes through, once.
func (c *call) review(w http.ResponseWriter, rule *object.CommandRule) bool {
	review, err := object.FindCommandReview(c.asset.Owner, c.command.Session, c.command.Text)
	if err != nil {
		logs.Error(fmt.Sprintf("kubeproxy.review(): %s", err.Error()))
		c.block(w, "the request is blocked because its review can't be read")
		return false
	}

	if review == nil {
		review, err = object.AddCommandReview(c.command, rule)
		if err != nil {
			logs.Error(fmt.Sprintf("kubeproxy.review(): %s", err.Error()))
			c.block(w, "the request is blocked because it can't be held for review")
			return false
		}
		c.command.Review = review.Name
		c.command.Error = "the request waits for review"
		c.command.StatusCode = http.StatusForbidden
		c.save()
	}

	switch review.Status {
	case object.CommandReviewPending:
		writeForbidden(w, fmt.Sprintf("the request needs the approval of an administrator by the rule: %s, it waits for review (request %s) for up to %d minutes, run it again once it is approved", rule.GetDisplayName(), review.Name, int(object.CommandReviewTimeout.Minutes())))
		return false
	case object.CommandReviewApproved:
		used, err := object.ConsumeCommandReview(review.GetId())
		if err != nil {
			logs.Error(fmt.Sprintf("kubeproxy.review(): %s", err.Error()))
		}
		if !used {
			writeForbidden(w, "the approval of the request has just been used, run it again to ask for a new review")
			return false
		}
		c.command.Review, c.command.Reviewer = review.Name, review.Reviewer
		return true
	default:
		writeForbidden(w, "the request is "+review.GetReviewOutcome())
		return false
	}
}

func (c *call) block(w http.ResponseWriter, message string) {
	c.command.Action = object.CommandActionBlock
	c.command.Error = message
	c.command.StatusCode = http.StatusForbidden
	c.save()
	writeForbidden(w, message)
}

func (c *call) save() {
	if c.isSpdy {
		return
	}
	if c.command.Owner == "" {
		c.command.Owner, _ = util.GetOwnerAndNameFromId(c.ts.token.Asset)
		c.command.Asset = c.ts.token.Asset
	}
	if runes := []rune(c.command.Error); len(runes) > maxCommandError {
		c.command.Error = string(runes[:maxCommandError])
	}
	c.command.Duration = time.Since(c.start).Milliseconds()
	err := object.AddCommand(c.command)
	if err != nil {
		logs.Error(fmt.Sprintf("kubeproxy.save(): %s", err.Error()))
	}
}

// statusRecorder notes the status sent to kubectl.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
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

// forward sends the request on to the API server with the token of the asset, as long as the
// session goes on and the scope covers it, and records it; the discovery of the API is recorded
// only when it fails.
func (c *call) forward(w http.ResponseWriter, r *http.Request) {
	ctx, done, ok := c.ts.begin(r.Context(), c.req)
	defer done()
	if !ok {
		writeStatus(w, http.StatusUnauthorized, "Unauthorized", "the kubeconfig has been revoked")
		return
	}

	errMessage := ""
	proxy := &httputil.ReverseProxy{
		Rewrite:   c.rewrite,
		Transport: c.ts.getTransport(c.client, c.asset.Host+"\n"+c.client.GetToken()),
		ModifyResponse: func(resp *http.Response) error {
			if resp.StatusCode != http.StatusUnauthorized {
				return c.client.PinFingerprint()
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			errMessage = c.getEndMessage(ctx, r, err.Error())
			if errMessage != "" {
				writeStatus(w, http.StatusBadGateway, "ServiceUnavailable", errMessage)
			}
		},
		FlushInterval: -1,
		ErrorLog:      log.New(io.Discard, "", 0),
	}

	recorder := &statusRecorder{ResponseWriter: w}
	func() {
		defer func() {
			if err := recover(); err != nil {
				if err != http.ErrAbortHandler {
					panic(err)
				}
				errMessage = c.getEndMessage(ctx, r, "the response was cut off")
			}
		}()
		proxy.ServeHTTP(recorder, r.WithContext(ctx))
	}()

	if !c.req.IsResource && c.command.Action == "" && errMessage == "" && recorder.status < 400 {
		return
	}
	// a WebSocket is answered on the connection taken over from the server
	if recorder.status == 0 && errMessage == "" && r.Header.Get("Upgrade") != "" {
		recorder.status = http.StatusSwitchingProtocols
	}
	c.command.StatusCode = recorder.status
	c.command.Error = errMessage
	c.save()
}

// getEndMessage tells why a request ended early: "" when kubectl ended it, like a watch it stops,
// Casvisor's reason when Casvisor ended it, and the error otherwise.
func (c *call) getEndMessage(ctx context.Context, r *http.Request, message string) string {
	if r.Context().Err() != nil {
		return ""
	}
	if cause := context.Cause(ctx); cause != nil {
		return "ended, " + cause.Error()
	}
	return message
}

func (c *call) rewrite(pr *httputil.ProxyRequest) {
	target := c.client.GetUrl("https", c.req.Path, pr.In.URL.RawQuery)
	pr.Out.URL = target
	pr.Out.Host = target.Host

	header := pr.Out.Header
	for key := range header {
		if strings.HasPrefix(strings.ToLower(key), "impersonate-") {
			header.Del(key)
		}
	}
	header.Del("Cookie")
	header.Set("Authorization", "Bearer "+c.client.GetToken())
	header.Set("X-Forwarded-For", c.clientIp)
}
