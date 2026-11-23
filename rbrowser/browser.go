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

// Package rbrowser opens Web assets in a headless Chromium that Casvisor runs, one for each
// session, and streams its screen to the user's page. The browser reaches nothing but the
// website, through a proxy of the session; Casvisor signs in to the website for the user with
// the account's password, which the user never sees, and records the screen and every page opened.
package rbrowser

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/object"
)

const (
	minSize = 200
	maxSize = 4096

	frameKindBitmap = 1
	jpegQuality     = 75

	pointerInterval = 50 * time.Millisecond
	cursorInterval  = 150 * time.Millisecond

	maxUploadBytes = 64 << 20
)

var (
	dataDir  = filepath.Join("data", "browsers")
	browsers sync.Map
)

// Viewer is the page that shows a session's browser.
type Viewer interface {
	// WriteFrame sends a screen frame, which goes into the recording
	WriteFrame(data []byte) error
	// WriteRecorded sends a message that goes into the recording, like a new screen size
	WriteRecorded(msg interface{}) error
	// WriteMessage sends a message that is left out of the recording
	WriteMessage(msg interface{}) error
	// Record adds a message to the recording without sending it, like where the pointer is
	Record(msg interface{})
}

type Options struct {
	SessionId string
	User      *casdoorsdk.User
	// Asset carries the username and password of the account the session uses
	Asset    *object.Asset
	Actions  []string
	ClientIp string
	Width    int
	Height   int
	Viewer   Viewer
}

type tab struct {
	targetId  string
	sessionId string
	url       string
	title     string
}

type download struct {
	name  string
	path  string
	size  int64
	ready bool
}

type fileChooser struct {
	sessionId     string
	backendNodeId int64
}

// Browser is the browser of a session.
type Browser struct {
	id       string
	user     *casdoorsdk.User
	asset    *object.Asset
	clientIp string
	viewer   Viewer
	target   *url.URL
	login    *object.WebLogin
	dir      string
	cdp      *cdp
	proxy    *proxy

	mu          sync.Mutex
	actions     []string
	width       int
	height      int
	tabs        []*tab
	signingIn   bool
	downloads   map[string]*download
	chooser     *fileChooser
	dialog      string
	uploads     int
	lastPointer time.Time
	lastCursor  time.Time
	cursor      string
	firstTab    chan struct{}
	closed      bool
}

func clampSize(value int, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return max(minSize, min(maxSize, value))
}

// Start starts the browser of a session at the website's sign-in page, or at its address, and
// signs in to it when the asset says how.
func Start(o *Options) (*Browser, error) {
	path := GetPath()
	if path == "" {
		return nil, fmt.Errorf("no Chrome, Chromium or Edge is found on the server of Casvisor, install one or set remoteBrowserPath in conf/app.conf")
	}

	target, err := o.Asset.GetWebUrl()
	if err != nil {
		return nil, err
	}

	login := o.Asset.WebLogin
	if login != nil && (login.UsernameSelector == "" && login.PasswordSelector == "" || o.Asset.Password == "") {
		login = nil
	}

	_, name, _ := strings.Cut(o.SessionId, "/")
	dir, err := filepath.Abs(filepath.Join(dataDir, name))
	if err != nil {
		return nil, err
	}
	for _, sub := range []string{"profile", "downloads", "uploads"} {
		err = os.MkdirAll(filepath.Join(dir, sub), 0o700)
		if err != nil {
			return nil, err
		}
	}

	b := &Browser{
		id:        o.SessionId,
		user:      o.User,
		asset:     o.Asset,
		clientIp:  o.ClientIp,
		viewer:    o.Viewer,
		target:    target,
		login:     login,
		dir:       dir,
		actions:   o.Actions,
		width:     clampSize(o.Width, 1280),
		height:    clampSize(o.Height, 800),
		downloads: map[string]*download{},
		firstTab:  make(chan struct{}),
	}

	b.proxy, err = newProxy(o.Asset, login == nil, o.ClientIp)
	if err != nil {
		b.removeDir()
		return nil, err
	}

	b.cdp, err = startCdp(path, b.getArgs(), b.onEvent)
	if err != nil {
		b.proxy.close()
		b.removeDir()
		return nil, err
	}

	err = b.init()
	if err != nil {
		b.Close()
		return nil, err
	}
	browsers.Store(b.id, b)
	return b, nil
}

// Get returns the browser of a connected session.
func Get(sessionId string) *Browser {
	value, ok := browsers.Load(sessionId)
	if !ok {
		return nil
	}
	return value.(*Browser)
}

// Done is closed once the browser has exited.
func (b *Browser) Done() <-chan struct{} {
	return b.cdp.done
}

func (b *Browser) getArgs() []string {
	args := []string{
		"--headless=new",
		"--user-data-dir=" + filepath.Join(b.dir, "profile"),
		"--proxy-server=http://" + b.proxy.getAddress(),
		"--proxy-bypass-list=<-loopback>",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
		"--disable-component-update",
		"--disable-default-apps",
		"--disable-extensions",
		"--disable-component-extensions-with-background-pages",
		"--disable-sync",
		"--disable-breakpad",
		"--disable-crash-reporter",
		"--disable-domain-reliability",
		"--disable-client-side-phishing-detection",
		"--disable-quic",
		"--disable-features=Translate,MediaRouter,OptimizationHints,AutofillServerCommunication,msPasswordReveal,msEdgeSidebarV2",
		"--force-webrtc-ip-handling-policy=disable_non_proxied_udp",
		"--password-store=basic",
		"--use-mock-keychain",
		"--metrics-recording-only",
		"--no-pings",
		"--mute-audio",
		"--window-size=" + strconv.Itoa(b.width) + "," + strconv.Itoa(b.height),
	}
	if b.proxy.spki != "" {
		args = append(args, "--ignore-certificate-errors-spki-list="+b.proxy.spki)
	}
	if runtime.GOOS == "linux" {
		args = append(args, "--disable-dev-shm-usage")
		if os.Geteuid() == 0 {
			args = append(args, "--no-sandbox")
		}
	}
	return append(args, "about:blank")
}

func (b *Browser) init() error {
	err := b.cdp.call("", "Browser.setDownloadBehavior", map[string]interface{}{
		"behavior":      "allowAndName",
		"downloadPath":  filepath.Join(b.dir, "downloads"),
		"eventsEnabled": true,
	}, nil)
	if err != nil {
		return err
	}

	err = b.cdp.call("", "Target.setAutoAttach", map[string]interface{}{
		"autoAttach":             true,
		"waitForDebuggerOnStart": true,
		"flatten":                true,
	}, nil)
	if err != nil {
		return err
	}
	err = b.cdp.call("", "Target.setDiscoverTargets", map[string]interface{}{"discover": true}, nil)
	if err != nil {
		return err
	}

	select {
	case <-b.firstTab:
	case <-b.cdp.done:
		return errClosed
	case <-time.After(commandTimeout):
		return fmt.Errorf("the browser has opened no page")
	}

	err = b.viewer.WriteRecorded(map[string]interface{}{"t": "size", "width": b.width, "height": b.height})
	if err != nil {
		return err
	}

	start := b.target.String()
	if b.login != nil && b.login.LoginUrl != "" {
		start = b.login.LoginUrl
	}
	if b.login != nil {
		go b.signIn(start)
		return nil
	}
	return b.navigate(start)
}

func (b *Browser) getActive() *tab {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.tabs) == 0 {
		return nil
	}
	return b.tabs[len(b.tabs)-1]
}

func (b *Browser) hasAction(action string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Contains(b.actions, action)
}

// SetActions passes the actions the user holds on the asset now.
func (b *Browser) SetActions(actions []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.actions = actions
}

func (b *Browser) notice(message string) {
	_ = b.viewer.WriteMessage(map[string]interface{}{"t": "notice", "message": message})
}

func (b *Browser) onEvent(method string, params json.RawMessage, sessionId string) {
	switch method {
	case "Target.attachedToTarget":
		var ev struct {
			SessionId  string `json:"sessionId"`
			TargetInfo struct {
				TargetId string `json:"targetId"`
				Type     string `json:"type"`
				Url      string `json:"url"`
				Title    string `json:"title"`
			} `json:"targetInfo"`
			WaitingForDebugger bool `json:"waitingForDebugger"`
		}
		if json.Unmarshal(params, &ev) != nil {
			return
		}
		if ev.TargetInfo.Type != "page" {
			if ev.WaitingForDebugger {
				go func() {
					_ = b.cdp.call(ev.SessionId, "Runtime.runIfWaitingForDebugger", nil, nil)
				}()
			}
			return
		}
		go b.addTab(&tab{targetId: ev.TargetInfo.TargetId, sessionId: ev.SessionId, url: ev.TargetInfo.Url, title: ev.TargetInfo.Title})
	case "Target.detachedFromTarget":
		var ev struct {
			SessionId string `json:"sessionId"`
		}
		if json.Unmarshal(params, &ev) == nil {
			go b.removeTab(ev.SessionId)
		}
	case "Target.targetInfoChanged":
		var ev struct {
			TargetInfo struct {
				TargetId string `json:"targetId"`
				Title    string `json:"title"`
			} `json:"targetInfo"`
		}
		if json.Unmarshal(params, &ev) == nil {
			b.updateTab(ev.TargetInfo.TargetId, "", ev.TargetInfo.Title)
		}
	case "Page.frameNavigated":
		var ev struct {
			Frame struct {
				Id       string `json:"id"`
				ParentId string `json:"parentId"`
				Url      string `json:"url"`
			} `json:"frame"`
		}
		if json.Unmarshal(params, &ev) == nil && ev.Frame.ParentId == "" {
			b.onNavigated(sessionId, ev.Frame.Id, ev.Frame.Url)
		}
	case "Page.navigatedWithinDocument":
		var ev struct {
			FrameId string `json:"frameId"`
			Url     string `json:"url"`
		}
		if json.Unmarshal(params, &ev) == nil {
			b.onNavigated(sessionId, ev.FrameId, ev.Url)
		}
	case "Page.loadEventFired":
		go b.refreshTitle(sessionId)
	case "Page.screencastFrame":
		var ev struct {
			Data      string `json:"data"`
			SessionId int64  `json:"sessionId"`
		}
		if json.Unmarshal(params, &ev) == nil {
			b.onFrame(sessionId, ev.Data)
			_ = b.cdp.call(sessionId, "Page.screencastFrameAck", map[string]interface{}{"sessionId": ev.SessionId}, nil)
		}
	case "Page.javascriptDialogOpening":
		var ev struct {
			Message       string `json:"message"`
			Type          string `json:"type"`
			DefaultPrompt string `json:"defaultPrompt"`
		}
		if json.Unmarshal(params, &ev) != nil {
			return
		}
		if ev.Type == "beforeunload" {
			go func() {
				_ = b.cdp.call(sessionId, "Page.handleJavaScriptDialog", map[string]interface{}{"accept": true}, nil)
			}()
			return
		}
		b.mu.Lock()
		b.dialog = sessionId
		b.mu.Unlock()
		_ = b.viewer.WriteMessage(map[string]interface{}{"t": "dialog", "type": ev.Type, "message": ev.Message, "defaultPrompt": ev.DefaultPrompt})
	case "Page.javascriptDialogClosed":
		b.mu.Lock()
		b.dialog = ""
		b.mu.Unlock()
		_ = b.viewer.WriteMessage(map[string]interface{}{"t": "dialog-closed"})
	case "Page.fileChooserOpened":
		var ev struct {
			Mode          string `json:"mode"`
			BackendNodeId int64  `json:"backendNodeId"`
		}
		if json.Unmarshal(params, &ev) == nil {
			b.onFileChooser(sessionId, ev.Mode, ev.BackendNodeId)
		}
	case "Fetch.requestPaused":
		var ev pausedRequest
		if json.Unmarshal(params, &ev) == nil {
			go b.onRequest(sessionId, &ev)
		}
	case "Browser.downloadWillBegin":
		var ev struct {
			Guid              string `json:"guid"`
			Url               string `json:"url"`
			SuggestedFilename string `json:"suggestedFilename"`
		}
		if json.Unmarshal(params, &ev) == nil {
			go b.onDownloadBegin(ev.Guid, ev.Url, ev.SuggestedFilename)
		}
	case "Browser.downloadProgress":
		var ev struct {
			Guid          string  `json:"guid"`
			State         string  `json:"state"`
			ReceivedBytes float64 `json:"receivedBytes"`
		}
		if json.Unmarshal(params, &ev) == nil {
			b.onDownloadProgress(ev.Guid, ev.State, int64(ev.ReceivedBytes))
		}
	}
}

func (b *Browser) addTab(t *tab) {
	calls := []struct {
		method string
		params interface{}
	}{
		{"Page.enable", nil},
		{"Page.setInterceptFileChooserDialog", map[string]interface{}{"enabled": true}},
		{"Fetch.enable", map[string]interface{}{"patterns": []map[string]interface{}{{"urlPattern": "*", "resourceType": "Document", "requestStage": "Request"}}}},
		{"Runtime.runIfWaitingForDebugger", nil},
	}
	pending := []*pendingCall{}
	var err error
	for _, call := range calls {
		var p *pendingCall
		p, err = b.cdp.send(t.sessionId, call.method, call.params)
		if err != nil {
			break
		}
		pending = append(pending, p)
	}
	for _, p := range pending {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		waitErr := p.wait(ctx, nil)
		cancel()
		if err == nil {
			err = waitErr
		}
	}
	if err != nil {
		logs.Warning(fmt.Sprintf("rbrowser.addTab(): %s: %s", b.id, err.Error()))
		_ = b.cdp.call("", "Target.closeTarget", map[string]interface{}{"targetId": t.targetId}, nil)
		return
	}

	b.mu.Lock()
	previous := (*tab)(nil)
	if len(b.tabs) != 0 {
		previous = b.tabs[len(b.tabs)-1]
	}
	b.tabs = append(b.tabs, t)
	first := previous == nil && len(b.tabs) == 1
	b.mu.Unlock()

	if previous != nil {
		_ = b.cdp.call(previous.sessionId, "Page.stopScreencast", nil, nil)
	}
	b.startScreencast(t)
	b.sendLocation()

	if first {
		select {
		case <-b.firstTab:
		default:
			close(b.firstTab)
		}
	}
}

func (b *Browser) removeTab(sessionId string) {
	b.mu.Lock()
	index := slices.IndexFunc(b.tabs, func(t *tab) bool { return t.sessionId == sessionId })
	if index < 0 || b.closed {
		b.mu.Unlock()
		return
	}
	wasActive := index == len(b.tabs)-1
	b.tabs = slices.Delete(b.tabs, index, index+1)
	var active *tab
	if len(b.tabs) != 0 {
		active = b.tabs[len(b.tabs)-1]
	}
	b.mu.Unlock()

	if active == nil {
		_ = b.cdp.call("", "Target.createTarget", map[string]interface{}{"url": b.target.String()}, nil)
		return
	}
	if wasActive {
		b.startScreencast(active)
		b.sendLocation()
	}
}

func (b *Browser) updateTab(targetId string, rawUrl string, title string) {
	b.mu.Lock()
	var found *tab
	for _, t := range b.tabs {
		if t.targetId == targetId {
			found = t
		}
	}
	if found != nil {
		if rawUrl != "" {
			found.url = rawUrl
		}
		if title != "" {
			found.title = title
		}
	}
	b.mu.Unlock()

	if found != nil && found == b.getActive() {
		b.sendLocation()
	}
}

// refreshTitle reads the title of a page once it has loaded, which is often set after it is shown.
func (b *Browser) refreshTitle(sessionId string) {
	b.mu.Lock()
	targetId := ""
	for _, t := range b.tabs {
		if t.sessionId == sessionId {
			targetId = t.targetId
		}
	}
	b.mu.Unlock()
	if targetId == "" {
		return
	}

	var res struct {
		TargetInfo struct {
			Title string `json:"title"`
		} `json:"targetInfo"`
	}
	err := b.cdp.call("", "Target.getTargetInfo", map[string]interface{}{"targetId": targetId}, &res)
	if err == nil {
		b.updateTab(targetId, "", res.TargetInfo.Title)
	}
}

func (b *Browser) startScreencast(t *tab) {
	b.mu.Lock()
	width, height := b.width, b.height
	b.mu.Unlock()

	_ = b.cdp.call(t.sessionId, "Emulation.setDeviceMetricsOverride", map[string]interface{}{
		"width":             width,
		"height":            height,
		"deviceScaleFactor": 1,
		"mobile":            false,
	}, nil)
	_ = b.cdp.call(t.sessionId, "Page.startScreencast", map[string]interface{}{
		"format":        "jpeg",
		"quality":       jpegQuality,
		"maxWidth":      width,
		"maxHeight":     height,
		"everyNthFrame": 1,
	}, nil)
}

// restartScreencast sends the screen as it is, even when nothing changes on it.
func (b *Browser) restartScreencast() {
	t := b.getActive()
	if t == nil {
		return
	}
	_ = b.cdp.call(t.sessionId, "Page.stopScreencast", nil, nil)
	b.startScreencast(t)
}

func (b *Browser) sendLocation() {
	t := b.getActive()
	if t == nil {
		return
	}

	var history struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			Id int64 `json:"id"`
		} `json:"entries"`
	}
	_ = b.cdp.call(t.sessionId, "Page.getNavigationHistory", nil, &history)

	b.mu.Lock()
	msg := map[string]interface{}{
		"t":            "location",
		"url":          t.url,
		"title":        t.title,
		"tabs":         len(b.tabs),
		"canGoBack":    history.CurrentIndex > 0,
		"canGoForward": history.CurrentIndex < len(history.Entries)-1,
	}
	b.mu.Unlock()
	_ = b.viewer.WriteMessage(msg)
}

// onNavigated keeps the browser on the website: a page of the browser itself or a file
// on the server is left for the website at once.
func (b *Browser) onNavigated(sessionId string, frameId string, rawUrl string) {
	b.mu.Lock()
	var found *tab
	for _, t := range b.tabs {
		if t.sessionId == sessionId && t.targetId == frameId {
			found = t
		}
	}
	b.mu.Unlock()
	if found == nil {
		return
	}

	u, err := url.Parse(rawUrl)
	if err != nil || !isAllowedScheme(u.Scheme) {
		go func() {
			_ = b.cdp.call(sessionId, "Page.navigate", map[string]interface{}{"url": b.target.String()}, nil)
		}()
		b.notice(fmt.Sprintf("%s can't be opened in this browser", rawUrl))
		return
	}
	b.updateTab(found.targetId, rawUrl, "")
}

func isAllowedScheme(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "http", "https", "about", "data", "blob", "chrome-error":
		return true
	}
	return false
}

func (b *Browser) onFrame(sessionId string, data string) {
	b.mu.Lock()
	active := len(b.tabs) != 0 && b.tabs[len(b.tabs)-1].sessionId == sessionId
	hidden := b.signingIn || b.closed
	b.mu.Unlock()
	if !active || hidden {
		return
	}

	image, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(image))
	if err != nil {
		return
	}

	// the frame is a single tile of the whole screen, which the RDP screen of the page draws
	frame := make([]byte, 0, 15+len(image))
	frame = append(frame, frameKindBitmap)
	frame = binary.LittleEndian.AppendUint16(frame, 1)
	frame = binary.LittleEndian.AppendUint16(frame, 0)
	frame = binary.LittleEndian.AppendUint16(frame, 0)
	frame = binary.LittleEndian.AppendUint16(frame, uint16(config.Width))
	frame = binary.LittleEndian.AppendUint16(frame, uint16(config.Height))
	frame = binary.LittleEndian.AppendUint32(frame, uint32(len(image)))
	frame = append(frame, image...)
	_ = b.viewer.WriteFrame(frame)
}

// navigate opens an address of the website in the page shown; a path is taken on the website.
func (b *Browser) navigate(rawUrl string) error {
	u, err := b.resolve(rawUrl)
	if err != nil {
		return err
	}
	t := b.getActive()
	if t == nil {
		return fmt.Errorf("the browser has no page")
	}
	return b.cdp.call(t.sessionId, "Page.navigate", map[string]interface{}{"url": u.String()}, nil)
}

func (b *Browser) resolve(rawUrl string) (*url.URL, error) {
	rawUrl = strings.TrimSpace(rawUrl)
	if rawUrl == "" {
		return b.target, nil
	}
	u, err := b.target.Parse(rawUrl)
	if err != nil || !b.isSameOrigin(u) {
		return nil, fmt.Errorf("only %s can be opened in this browser", b.getOrigin())
	}
	return u, nil
}

func (b *Browser) getOrigin() string {
	return b.target.Scheme + "://" + b.target.Host
}

func (b *Browser) isSameOrigin(u *url.URL) bool {
	return strings.EqualFold(u.Scheme, b.target.Scheme) && strings.EqualFold(hostWithPort(u), b.target.Host)
}

func (b *Browser) goHistory(delta int) {
	t := b.getActive()
	if t == nil {
		return
	}
	var history struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			Id int64 `json:"id"`
		} `json:"entries"`
	}
	err := b.cdp.call(t.sessionId, "Page.getNavigationHistory", nil, &history)
	if err != nil {
		return
	}
	index := history.CurrentIndex + delta
	if index < 0 || index >= len(history.Entries) {
		return
	}
	_ = b.cdp.call(t.sessionId, "Page.navigateToHistoryEntry", map[string]interface{}{"entryId": history.Entries[index].Id}, nil)
}

// Close ends the browser and removes its profile, downloads and uploads.
func (b *Browser) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	b.mu.Unlock()

	browsers.CompareAndDelete(b.id, b)
	b.cdp.stop()
	b.proxy.close()
	b.removeDir()
}

func (b *Browser) removeDir() {
	for i := 0; i < 20; i++ {
		err := os.RemoveAll(b.dir)
		if err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	logs.Warning(fmt.Sprintf("rbrowser.removeDir(): failed to remove: %s", b.dir))
}

// Init ends the sessions an earlier run left connected, whose browsers are gone with it, and
// removes their profiles and files.
func Init() {
	err := os.RemoveAll(dataDir)
	if err != nil {
		logs.Warning(fmt.Sprintf("rbrowser.Init(): failed to remove the browsers of an earlier run: %s", err.Error()))
	}

	sessions, err := object.GetSessionsByStatus([]string{object.Connected})
	if err != nil {
		logs.Error(fmt.Sprintf("rbrowser.Init(): %s", err.Error()))
		return
	}
	for _, session := range sessions {
		if session.Protocol != object.ProtocolBrowser {
			continue
		}
		err = object.CloseDBSession(session.GetId(), 0, "Ended, Casvisor was restarted")
		if err != nil {
			logs.Error(fmt.Sprintf("rbrowser.Init(): %s", err.Error()))
		}
	}
}
