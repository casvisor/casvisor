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

// Package rdgateway is a Remote Desktop Gateway inside Casvisor, so that Remote Desktop
// Connection (mstsc) on the user's machine opens an RDP asset from a .rdp file that the web
// console hands out. mstsc reaches the gateway over HTTPS, at the same address as the web console,
// and presents a one-time token as its gateway access token; Casvisor checks it, connects to
// the asset through the asset's network gateway, and passes the RDP bytes through. The RDP
// session itself is encrypted between mstsc and the asset, so it is listed and can be stopped,
// but not recorded, and the user signs in to Windows with an account they know.
package rdgateway

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/beego/beego"
	beecontext "github.com/beego/beego/context"
	"github.com/beego/beego/logs"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
	"github.com/gorilla/websocket"
)

// Path is where mstsc sends the requests of the gateway.
const Path = "/remoteDesktopGateway/"

const (
	methodOut = "RDG_OUT_DATA"
	methodIn  = "RDG_IN_DATA"

	handshakeTimeout = time.Minute
)

func init() {
	// beego answers 405 to methods it doesn't know, before any filter sees them
	beego.HTTPMETHOD[methodOut] = true
	beego.HTTPMETHOD[methodIn] = true
}

func IsEnabled() bool {
	return !conf.GetConfigBool("isDemoMode") && !conf.GetConfigBool("disableRdGateway")
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  32 * 1024,
	WriteBufferSize: 32 * 1024,
	// mstsc sends no Origin; the one-time token is what is checked
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Filter takes the requests of mstsc before beego reads their bodies or starts a session.
func Filter(ctx *beecontext.Context) {
	if !strings.EqualFold(strings.TrimSuffix(ctx.Request.URL.Path, "/")+"/", Path) {
		return
	}

	serve(ctx.ResponseWriter.ResponseWriter, ctx.Request)
	ctx.ResponseWriter.Started = true
}

func serve(w http.ResponseWriter, r *http.Request) {
	if !IsEnabled() {
		http.Error(w, "the Remote Desktop Gateway is off", http.StatusNotFound)
		return
	}
	if r.Method != methodOut || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		// the older transport of two long HTTP requests, which mstsc falls back to when WebSockets fail
		logs.Info(fmt.Sprintf("Remote Desktop Gateway: refused a %s request without a WebSocket from %s (%s)", r.Method, util.GetClientIp(r), r.UserAgent()))
		http.Error(w, "use the WebSocket transport", http.StatusNotImplemented)
		return
	}

	// the upgrader only takes GET, which is what RDG_OUT_DATA is here
	r.Method = http.MethodGet
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	t := &tunnel{ws: ws, clientIp: util.GetClientIp(r), userAgent: r.UserAgent()}
	t.run()
}

// wsReader reads the binary messages of a WebSocket as one stream, as a packet may span messages.
type wsReader struct {
	ws  *websocket.Conn
	cur io.Reader
}

func (r *wsReader) Read(p []byte) (int, error) {
	for {
		if r.cur == nil {
			_, reader, err := r.ws.NextReader()
			if err != nil {
				return 0, err
			}
			r.cur = reader
		}
		n, err := r.cur.Read(p)
		if err == io.EOF {
			r.cur = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

// tunnel is one connection of mstsc: a handshake, the token, then one channel to the asset.
type tunnel struct {
	ws        *websocket.Conn
	clientIp  string
	userAgent string

	writeMu sync.Mutex

	user      *casdoorsdk.User
	asset     *object.Asset
	account   string
	actions   []string
	conn      net.Conn
	sessionId string

	closeOnce sync.Once
}

func (t *tunnel) write(b []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	_ = t.ws.SetWriteDeadline(time.Now().Add(time.Minute))
	return t.ws.WriteMessage(websocket.BinaryMessage, b)
}

func (t *tunnel) close(message string) {
	t.closeOnce.Do(func() {
		_ = t.ws.Close()
		if t.conn != nil {
			_ = t.conn.Close()
		}
		if t.sessionId != "" {
			object.RemoveSessionCloser(t.sessionId)
			err := object.CloseSession(t.sessionId, 0, message)
			if err != nil {
				logs.Error(fmt.Sprintf("Remote Desktop Gateway: failed to close the session: %s: %s", t.sessionId, err.Error()))
			}
		}
	})
}

func (t *tunnel) run() {
	defer t.close("Normal user exit")
	_ = t.ws.SetReadDeadline(time.Now().Add(handshakeTimeout))

	r := &wsReader{ws: t.ws}
	for {
		p, err := readPacket(r)
		if err != nil {
			return
		}

		switch p.kind {
		case pktHandshakeRequest:
			req, err := parseHandshakeRequest(p.body)
			if err != nil {
				return
			}
			if req.extendedAuth&extendedAuthPaa == 0 {
				_ = t.write(handshakeResponse(errorCookieAccessDenied, req.major, req.minor, 0))
				return
			}
			err = t.write(handshakeResponse(0, req.major, req.minor, extendedAuthPaa))
		case pktTunnelCreate:
			cookie, err := parseTunnelCreate(p.body)
			if err != nil {
				return
			}
			err = t.authenticate(cookie)
			if err != nil {
				logs.Info(fmt.Sprintf("Remote Desktop Gateway: refused %s: %s", t.clientIp, err.Error()))
				_ = t.write(tunnelResponse(errorCookieAccessDenied))
				return
			}
			err = t.write(tunnelResponse(0))
		case pktTunnelAuth:
			if t.asset == nil {
				return
			}
			_, err = parseTunnelAuth(p.body)
			if err != nil {
				return
			}
			err = t.write(tunnelAuthResponse(0, t.getRedirectionFlags()))
		case pktChannelCreate:
			if t.asset == nil || t.conn != nil {
				return
			}
			_, err = parseChannelCreate(p.body)
			if err != nil {
				return
			}
			err = t.openChannel()
			if err != nil {
				logs.Info(fmt.Sprintf("Remote Desktop Gateway: failed to connect %s to the asset: %s: %s", t.clientIp, t.asset.GetId(), err.Error()))
				_ = t.write(channelResponse(errorConnectFailed))
				return
			}
			err = t.write(channelResponse(0))
			if err == nil {
				// the RDP session may sit idle for a long time, mstsc sends keepalives
				_ = t.ws.SetReadDeadline(time.Time{})
				go t.pump()
			}
		case pktData:
			if t.conn == nil {
				return
			}
			data, err := parseData(p.body)
			if err != nil {
				return
			}
			_, err = t.conn.Write(data)
			if err != nil {
				return
			}
		case pktKeepAlive:
		case pktCloseChannel:
			_ = t.write(closeChannelResponse())
			return
		default:
			logs.Info(fmt.Sprintf("Remote Desktop Gateway: unexpected packet type 0x%x from %s", p.kind, t.clientIp))
		}
		if err != nil {
			return
		}
	}
}

// authenticate takes the one-time token and checks the permission again.
func (t *tunnel) authenticate(cookie string) error {
	token := object.ConsumeClientToken(strings.TrimSpace(cookie), object.ClientTokenRdp)
	if token == nil {
		return fmt.Errorf("the gateway access token is unknown, used or expired, open the asset again from Casvisor")
	}

	owner, name := util.GetOwnerAndNameFromId(token.UserId)
	user, err := object.GetCasdoorUser(owner, name)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("the user: %s does not exist", token.UserId)
	}

	asset, err := object.GetAsset(token.AssetId)
	if err != nil {
		return err
	}
	if asset == nil {
		return fmt.Errorf("the asset: %s does not exist", token.AssetId)
	}

	account := token.Account
	actions, err := object.GetAssetAccountActions(user, asset, &account, t.clientIp)
	if err != nil {
		return err
	}
	if !slices.Contains(actions, object.ActionConnect) || !slices.Contains(actions, object.ActionForward) {
		return fmt.Errorf("the user: %s may no longer connect to the asset: %s from Remote Desktop Connection", token.UserId, asset.GetId())
	}

	t.user = user
	t.asset = asset
	t.account = account
	t.actions = actions
	return nil
}

// getRedirectionFlags turns off what mstsc would otherwise bring into the session: the clipboard
// without Copy and Paste, the drives without Upload and Download, and always printers, ports and devices.
func (t *tunnel) getRedirectionFlags() uint32 {
	flags := uint32(redirDisablePrinter | redirDisablePort | redirDisablePnp)
	if !slices.Contains(t.actions, object.ActionCopy) || !slices.Contains(t.actions, object.ActionPaste) {
		flags |= redirDisableClipboard
	}
	if !slices.Contains(t.actions, object.ActionUpload) || !slices.Contains(t.actions, object.ActionDownload) {
		flags |= redirDisableDrive
	}
	return flags
}

// openChannel connects to the asset, whichever resource mstsc names, and opens the session.
func (t *tunnel) openChannel() error {
	conn, err := object.DialAsset(t.asset)
	if err != nil {
		return err
	}

	session := &object.Session{
		Account:       t.account,
		Creator:       util.GetIdFromOwnerAndName(t.user.Owner, t.user.Name),
		ClientIp:      t.clientIp,
		UserAgent:     t.userAgent,
		ClientIpDesc:  util.GetDescFromIP(t.clientIp),
		UserAgentDesc: t.userAgent,
	}
	_, err = object.CreateSession(session, t.asset.GetId(), "tunnel", []string{})
	if err != nil {
		_ = conn.Close()
		return err
	}
	session.Status = object.Connected
	session.StartTime = util.GetCurrentTime()
	session.Reviewed = true
	_, err = object.UpdateSession(session.GetId(), session)
	if err != nil {
		_ = conn.Close()
		return err
	}

	t.conn = conn
	t.sessionId = session.GetId()
	object.AddSessionCloser(t.sessionId, func(code int, msg string) {
		_ = t.ws.Close()
		_ = conn.Close()
	})
	object.AddLiveSession(t.sessionId, &object.LiveSession{
		User:     t.user,
		AssetId:  t.asset.GetId(),
		Account:  t.account,
		ClientIp: t.clientIp,
		OnActions: func(actions []string) {
			if !slices.Contains(actions, object.ActionForward) {
				go func() {
					_ = object.CloseSession(t.sessionId, object.PermissionRevokedCode, fmt.Sprintf("The permission to reach the asset: %s from Remote Desktop Connection has been revoked", t.asset.Name))
				}()
			}
		},
	})

	object.AddAuditRecord(&object.AuditEvent{
		Organization: t.asset.Owner,
		User:         t.user.Name,
		ClientIp:     t.clientIp,
		UserAgent:    t.userAgent,
		Method:       "RDP",
		Action:       "rdp-gateway",
		Asset:        t.asset.GetId(),
		Detail:       net.JoinHostPort(t.asset.Host, fmt.Sprintf("%d", t.asset.Port)),
	})
	return nil
}

// pump passes what the asset sends to mstsc, in DATA packets.
func (t *tunnel) pump() {
	defer t.close("Normal user exit")
	buf := make([]byte, maxDataSize)
	for {
		n, err := t.conn.Read(buf)
		if n > 0 {
			if t.write(dataPacket(buf[:n])) != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}
