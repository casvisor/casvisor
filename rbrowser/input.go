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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/casvisor/casvisor/object"
)

// Message is what the page sends: the user's mouse, keyboard and clipboard, and the buttons around the screen.
type Message struct {
	Type string `json:"type"`

	Event      string  `json:"event"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Button     string  `json:"button"`
	Buttons    int     `json:"buttons"`
	ClickCount int     `json:"clickCount"`
	DeltaX     float64 `json:"deltaX"`
	DeltaY     float64 `json:"deltaY"`
	Modifiers  int     `json:"modifiers"`

	Key      string `json:"key"`
	Code     string `json:"code"`
	KeyCode  int    `json:"keyCode"`
	Location int    `json:"location"`
	Repeat   bool   `json:"repeat"`
	Text     string `json:"text"`

	Width  int    `json:"width"`
	Height int    `json:"height"`
	Url    string `json:"url"`
	Accept bool   `json:"accept"`

	Files []struct {
		Name string `json:"name"`
		Data string `json:"data"`
	} `json:"files"`
}

const (
	modifierCtrl = 2
	modifierMeta = 4
)

var mouseEvents = map[string]string{"down": "mousePressed", "up": "mouseReleased", "move": "mouseMoved", "wheel": "mouseWheel"}

// Handle acts on a message of the page.
func (b *Browser) Handle(msg *Message) {
	b.mu.Lock()
	signingIn := b.signingIn
	b.mu.Unlock()
	if signingIn && msg.Type != "resize" {
		return
	}

	t := b.getActive()
	if t == nil {
		return
	}

	switch msg.Type {
	case "mouse":
		b.dispatchMouse(t, msg)
	case "key":
		b.dispatchKey(t, msg)
	case "text":
		if msg.Text != "" {
			b.input(t, "Input.insertText", map[string]interface{}{"text": msg.Text})
		}
	case "paste":
		if !b.hasAction(object.ActionPaste) {
			b.notice("Pasting is not permitted")
			return
		}
		if msg.Text != "" {
			b.input(t, "Input.insertText", map[string]interface{}{"text": msg.Text})
		}
	case "resize":
		b.resize(msg.Width, msg.Height)
	case "navigate":
		go func() {
			err := b.navigate(msg.Url)
			if err != nil {
				b.notice(err.Error())
			}
		}()
	case "back":
		go b.goHistory(-1)
	case "forward":
		go b.goHistory(1)
	case "reload":
		b.input(t, "Page.reload", nil)
	case "close-tab":
		b.mu.Lock()
		count := len(b.tabs)
		b.mu.Unlock()
		if count > 1 {
			_, _ = b.cdp.send("", "Target.closeTarget", map[string]interface{}{"targetId": t.targetId})
		}
	case "dialog":
		b.mu.Lock()
		sessionId := b.dialog
		b.mu.Unlock()
		if sessionId != "" {
			_ = b.cdp.call(sessionId, "Page.handleJavaScriptDialog", map[string]interface{}{"accept": msg.Accept, "promptText": msg.Text}, nil)
		}
	case "files":
		b.uploadFiles(msg)
	case "signin":
		if b.login != nil {
			start := b.target.String()
			if b.login.LoginUrl != "" {
				start = b.login.LoginUrl
			}
			go b.signIn(start)
		}
	}
}

// input sends the user's input without waiting for it to be handled, which is not until the page
// closes a dialog it opened, in the order it came.
func (b *Browser) input(t *tab, method string, params interface{}) {
	_, _ = b.cdp.send(t.sessionId, method, params)
}

func (b *Browser) dispatchMouse(t *tab, msg *Message) {
	eventType, ok := mouseEvents[msg.Event]
	if !ok {
		return
	}
	button := msg.Button
	if button == "" {
		button = "none"
	}
	params := map[string]interface{}{
		"type":       eventType,
		"x":          msg.X,
		"y":          msg.Y,
		"button":     button,
		"buttons":    msg.Buttons,
		"clickCount": msg.ClickCount,
		"modifiers":  msg.Modifiers,
	}
	if eventType == "mouseWheel" {
		params["deltaX"] = msg.DeltaX
		params["deltaY"] = msg.DeltaY
	}
	b.input(t, "Input.dispatchMouseEvent", params)

	now := time.Now()
	b.mu.Lock()
	recordPointer := now.Sub(b.lastPointer) >= pointerInterval || eventType != "mouseMoved"
	if recordPointer {
		b.lastPointer = now
	}
	checkCursor := eventType == "mouseMoved" && now.Sub(b.lastCursor) >= cursorInterval
	if checkCursor {
		b.lastCursor = now
	}
	b.mu.Unlock()

	if recordPointer {
		b.viewer.Record(map[string]interface{}{"t": "pointer", "x": int(msg.X), "y": int(msg.Y)})
	}
	if checkCursor {
		go b.updateCursor(t, msg.X, msg.Y)
	}
}

const cursorJs = `(() => {
  const e = document.elementFromPoint(%s, %s);
  if (!e) return "default";
  const cursor = getComputedStyle(e).cursor;
  if (cursor && cursor !== "auto") return cursor;
  if (e.closest("a[href]")) return "pointer";
  if (e.isContentEditable || (/^(INPUT|TEXTAREA)$/).test(e.tagName)) return "text";
  return "default";
})()`

func (b *Browser) updateCursor(t *tab, x float64, y float64) {
	cursor, err := b.evaluate(t.sessionId, fmt.Sprintf(cursorJs, strconv.FormatFloat(x, 'f', 1, 64), strconv.FormatFloat(y, 'f', 1, 64)), false)
	if err != nil {
		return
	}
	value, _ := cursor.(string)

	b.mu.Lock()
	changed := value != "" && value != b.cursor
	if changed {
		b.cursor = value
	}
	b.mu.Unlock()
	if changed {
		_ = b.viewer.WriteMessage(map[string]interface{}{"t": "cursor", "cursor": value})
	}
}

func (b *Browser) dispatchKey(t *tab, msg *Message) {
	eventType := "keyUp"
	if msg.Event == "down" {
		eventType = "rawKeyDown"
		if msg.Text != "" {
			eventType = "keyDown"
		}
	}
	params := map[string]interface{}{
		"type":                  eventType,
		"modifiers":             msg.Modifiers,
		"key":                   msg.Key,
		"code":                  msg.Code,
		"windowsVirtualKeyCode": msg.KeyCode,
		"nativeVirtualKeyCode":  msg.KeyCode,
		"location":              msg.Location,
		"autoRepeat":            msg.Repeat,
	}
	if eventType == "keyDown" {
		params["text"] = msg.Text
		params["unmodifiedText"] = msg.Text
	}
	b.input(t, "Input.dispatchKeyEvent", params)

	key := strings.ToLower(msg.Key)
	if msg.Event == "down" && msg.Modifiers&(modifierCtrl|modifierMeta) != 0 && (key == "c" || key == "x") {
		go b.copySelection(t)
	}
}

const selectionJs = `(() => {
  let doc = document;
  let e = doc.activeElement;
  while (e && e.tagName === "IFRAME") {
    try { doc = e.contentDocument; } catch (err) { return ""; }
    if (!doc) return "";
    e = doc.activeElement;
  }
  if (e && (e.tagName === "INPUT" || e.tagName === "TEXTAREA")) {
    if (e.type === "password") return "";
    return (e.value || "").substring(e.selectionStart || 0, e.selectionEnd || 0);
  }
  return String(doc.getSelection() || "");
})()`

// copySelection hands what the user copied in the browser to the page, with Copy.
func (b *Browser) copySelection(t *tab) {
	if !b.hasAction(object.ActionCopy) {
		return
	}
	time.Sleep(50 * time.Millisecond)
	value, err := b.evaluate(t.sessionId, selectionJs, false)
	if err != nil {
		return
	}
	text, _ := value.(string)
	if text != "" {
		_ = b.viewer.WriteMessage(map[string]interface{}{"t": "clipboard", "text": text})
	}
}

func (b *Browser) resize(width int, height int) {
	width, height = clampSize(width, 1280), clampSize(height, 800)
	b.mu.Lock()
	changed := width != b.width || height != b.height
	b.width, b.height = width, height
	b.mu.Unlock()
	if !changed {
		return
	}

	_ = b.viewer.WriteRecorded(map[string]interface{}{"t": "size", "width": width, "height": height})
	b.restartScreencast()
}

type evaluateResult struct {
	Result struct {
		Type  string      `json:"type"`
		Value interface{} `json:"value"`
	} `json:"result"`
	ExceptionDetails *struct {
		Text      string `json:"text"`
		Exception *struct {
			Description string `json:"description"`
		} `json:"exception"`
	} `json:"exceptionDetails"`
}

func (b *Browser) evaluate(sessionId string, expression string, awaitPromise bool) (interface{}, error) {
	var res evaluateResult
	err := b.cdp.call(sessionId, "Runtime.evaluate", map[string]interface{}{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  awaitPromise,
		"silent":        true,
	}, &res)
	if err != nil {
		return nil, err
	}
	if res.ExceptionDetails != nil {
		text := res.ExceptionDetails.Text
		if res.ExceptionDetails.Exception != nil && res.ExceptionDetails.Exception.Description != "" {
			text = res.ExceptionDetails.Exception.Description
		}
		return nil, fmt.Errorf("%s", strings.SplitN(text, "\n", 2)[0])
	}
	return res.Result.Value, nil
}

func (b *Browser) onFileChooser(sessionId string, mode string, backendNodeId int64) {
	if !b.hasAction(object.ActionUpload) {
		command := b.newCommand("UPLOAD")
		command.Action = object.CommandActionBlock
		command.Error = "uploading files is not permitted"
		b.saveCommand(command)
		b.notice("Uploading is not permitted")
		return
	}

	b.mu.Lock()
	b.chooser = &fileChooser{sessionId: sessionId, backendNodeId: backendNodeId}
	b.mu.Unlock()
	_ = b.viewer.WriteMessage(map[string]interface{}{"t": "filechooser", "multiple": mode == "selectMultiple"})
}

// uploadFiles gives the files the user chose to the file input that asked for them.
func (b *Browser) uploadFiles(msg *Message) {
	b.mu.Lock()
	chooser := b.chooser
	b.chooser = nil
	b.uploads++
	index := b.uploads
	b.mu.Unlock()
	if chooser == nil || len(msg.Files) == 0 {
		return
	}
	if !b.hasAction(object.ActionUpload) {
		b.notice("Uploading is not permitted")
		return
	}

	dir := filepath.Join(b.dir, "uploads", strconv.Itoa(index))
	err := os.MkdirAll(dir, 0o700)
	if err != nil {
		b.notice(err.Error())
		return
	}

	paths := []string{}
	total := 0
	for _, file := range msg.Files {
		data, err := base64.StdEncoding.DecodeString(file.Data)
		if err != nil {
			continue
		}
		total += len(data)
		if total > maxUploadBytes {
			b.notice(fmt.Sprintf("The files can be at most %d MB together", maxUploadBytes>>20))
			return
		}

		name := filepath.Base(strings.ReplaceAll(file.Name, "\\", "/"))
		if name == "." || name == "/" || name == ".." || strings.ContainsAny(name, ":") {
			name = "file"
		}
		path := filepath.Join(dir, name)
		err = os.WriteFile(path, data, 0o600)
		if err != nil {
			b.notice(err.Error())
			return
		}
		paths = append(paths, path)
		b.saveCommand(b.newCommand(fmt.Sprintf("UPLOAD %s (%d bytes)", name, len(data))))
	}

	err = b.cdp.call(chooser.sessionId, "DOM.setFileInputFiles", map[string]interface{}{"files": paths, "backendNodeId": chooser.backendNodeId}, nil)
	if err != nil {
		b.notice(err.Error())
	}
}

func (b *Browser) onDownloadBegin(guid string, rawUrl string, name string) {
	text := "DOWNLOAD " + name
	if u, err := url.Parse(rawUrl); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		text = fmt.Sprintf("DOWNLOAD %s (%s)", u.RequestURI(), name)
	}

	if !b.hasAction(object.ActionDownload) {
		_ = b.cdp.call("", "Browser.cancelDownload", map[string]interface{}{"guid": guid}, nil)
		command := b.newCommand(text)
		command.Action = object.CommandActionBlock
		command.Error = "downloading files is not permitted"
		b.saveCommand(command)
		b.notice("Downloading is not permitted")
		return
	}

	b.mu.Lock()
	b.downloads[guid] = &download{name: name, path: filepath.Join(b.dir, "downloads", guid)}
	b.mu.Unlock()
	b.saveCommand(b.newCommand(text))
}

func (b *Browser) onDownloadProgress(guid string, state string, size int64) {
	b.mu.Lock()
	d := b.downloads[guid]
	if d != nil && state == "completed" {
		d.ready = true
		d.size = size
	}
	if d != nil && state == "canceled" {
		delete(b.downloads, guid)
	}
	b.mu.Unlock()

	if d != nil && state == "completed" {
		_ = b.viewer.WriteMessage(map[string]interface{}{"t": "download", "id": guid, "name": d.name, "size": size})
	}
}

// GetDownload returns the file of a download that has completed, while the user may download.
func (b *Browser) GetDownload(id string) (string, string, error) {
	if !b.hasAction(object.ActionDownload) {
		return "", "", fmt.Errorf("you have no permission to download files from the asset: %s", b.asset.GetId())
	}

	b.mu.Lock()
	d := b.downloads[id]
	b.mu.Unlock()
	if d == nil || !d.ready {
		return "", "", fmt.Errorf("the download: %s does not exist", id)
	}
	return d.path, d.name, nil
}

func encodeJson(v interface{}) string {
	data, _ := json.Marshal(v)
	return string(data)
}
