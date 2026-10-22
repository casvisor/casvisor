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

// Package terminal follows an interactive SSH session the way the user sees it:
// it keeps a virtual screen to read each command off when Enter is pressed,
// applies the command rules, and lets administrators watch the session live.
package terminal

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/recording"
	"github.com/casvisor/casvisor/util"
	"github.com/hinshun/vt10x"
)

const (
	historySize = 512 * 1024

	echoWait    = 20 * time.Millisecond
	maxEchoWait = 500 * time.Millisecond
)

var (
	pasteStart = []byte("\x1b[200~")
	pasteEnd   = []byte("\x1b[201~")
)

type Options struct {
	SessionId string
	User      *casdoorsdk.User
	Asset     *object.Asset
	ClientIp  string
	Cols      int
	Rows      int
	// Send writes the user's keystrokes to the asset
	Send func(p []byte) error
	// Notify shows a message in the user's terminal
	Notify func(p []byte)
}

type Recorder struct {
	opts  Options
	input chan []byte

	mu         sync.Mutex
	vt         vt10x.Terminal
	partial    []byte
	lastOutput time.Time
	lastSend   time.Time
	cols, rows int
	history    []byte
	viewers    map[*Viewer]struct{}
	closed     bool
	rec        *recording.Recording
	recStarted bool

	// only touched by the input goroutine
	lineStarted bool
	promptRow   int
	promptX     int
	promptText  string
	inPaste     bool
}

var recorders sync.Map

func NewRecorder(opts Options) *Recorder {
	if opts.Cols <= 0 {
		opts.Cols = 80
	}
	if opts.Rows <= 0 {
		opts.Rows = 24
	}

	r := &Recorder{
		opts:    opts,
		input:   make(chan []byte, 256),
		vt:      vt10x.New(vt10x.WithSize(opts.Cols, opts.Rows)),
		cols:    opts.Cols,
		rows:    opts.Rows,
		viewers: map[*Viewer]struct{}{},
	}
	recorders.Store(opts.SessionId, r)
	go r.run()
	return r
}

func GetRecorder(sessionId string) *Recorder {
	value, ok := recorders.Load(sessionId)
	if !ok {
		return nil
	}
	return value.(*Recorder)
}

func (r *Recorder) Close() {
	recorders.CompareAndDelete(r.opts.SessionId, r)

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	r.rec.Close()
	close(r.input)
	for viewer := range r.viewers {
		viewer.close()
	}
	r.viewers = map[*Viewer]struct{}{}
}

// Output takes what the asset sent to the user's terminal.
func (r *Recorder) Output(p []byte) {
	if len(p) == 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	data := append(r.partial, p...)
	n, _ := r.vt.Write(data)
	if n < len(data) {
		r.partial = append([]byte{}, data[n:]...)
	} else {
		r.partial = nil
	}
	r.lastOutput = time.Now()

	r.startRecording()
	r.rec.Write(recording.KindData, p)

	r.history = append(r.history, p...)
	if len(r.history) > historySize {
		r.history = append([]byte{}, r.history[len(r.history)-historySize:]...)
	}

	for viewer := range r.viewers {
		if !viewer.send(Event{Data: append([]byte{}, p...)}) {
			delete(r.viewers, viewer)
		}
	}
}

// startRecording starts the recording at the first output, so a session that
// never showed anything leaves no file; the caller holds the lock.
func (r *Recorder) startRecording() {
	if r.recStarted || r.closed || r.opts.Asset == nil {
		return
	}
	r.recStarted = true

	user := ""
	if r.opts.User != nil {
		user = util.GetIdFromOwnerAndName(r.opts.User.Owner, r.opts.User.Name)
	}
	rec, err := recording.Start(recording.Header{
		Protocol: r.opts.Asset.Protocol,
		Session:  r.opts.SessionId,
		Asset:    r.opts.Asset.GetId(),
		User:     user,
		Cols:     r.cols,
		Rows:     r.rows,
	})
	if err != nil {
		logs.Error(fmt.Sprintf("Recorder.startRecording(): %s", err.Error()))
		return
	}
	if rec == nil {
		return
	}

	err = object.AddSessionRecording(r.opts.SessionId, rec.Name)
	if err != nil {
		logs.Error(fmt.Sprintf("Recorder.startRecording(): %s", err.Error()))
	}
	r.rec = rec
}

func (r *Recorder) Resize(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.vt.Resize(cols, rows)
	r.cols, r.rows = cols, rows
	r.rec.WriteJson(recording.KindResize, map[string]int{"cols": cols, "rows": rows})
	for viewer := range r.viewers {
		if !viewer.send(Event{Cols: cols, Rows: rows}) {
			delete(r.viewers, viewer)
		}
	}
}

// Input takes the user's keystrokes; they reach the asset through Send, in order,
// with Enter held back until the line can be read and checked.
func (r *Recorder) Input(p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || len(p) == 0 {
		return
	}

	select {
	case r.input <- append([]byte{}, p...):
	default:
		// the asset is not reading, drop the keystrokes rather than block the session
	}
}

func (r *Recorder) run() {
	for p := range r.input {
		r.process(p)
	}
}

func (r *Recorder) send(p []byte) {
	if len(p) == 0 {
		return
	}

	r.mu.Lock()
	r.lastSend = time.Now()
	r.mu.Unlock()

	err := r.opts.Send(p)
	if err != nil {
		logs.Debug(fmt.Sprintf("Recorder.send(): %s", err.Error()))
	}
}

func (r *Recorder) process(p []byte) {
	start := 0
	for i := 0; i < len(p); i++ {
		if bytes.HasPrefix(p[i:], pasteStart) {
			r.inPaste = true
		} else if bytes.HasPrefix(p[i:], pasteEnd) {
			r.inPaste = false
		}

		switch b := p[i]; {
		case b == '\r' && !r.inPaste:
			r.send(p[start:i])
			start = i + 1
			r.enter()
		case b == 3 || b == 4:
			// Ctrl+C or Ctrl+D drops the line
			r.lineStarted = false
		default:
			if !r.lineStarted {
				r.startLine()
			}
		}
	}
	r.send(p[start:])
}

// startLine notes where the prompt ends, before the first keystroke of a line.
func (r *Recorder) startLine() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.vt.Lock()
	defer r.vt.Unlock()

	cursor := r.vt.Cursor()
	cols, _ := r.vt.Size()
	r.promptRow = cursor.Y
	r.promptX = min(cursor.X, cols)
	r.promptText = string(r.rowCells(cursor.Y)[:r.promptX])
	r.lineStarted = true
}

func (r *Recorder) isFullScreen() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.vt.Lock()
	defer r.vt.Unlock()
	return r.vt.Mode()&vt10x.ModeAltScreen != 0
}

func (r *Recorder) enter() {
	if !r.lineStarted || r.isFullScreen() {
		r.lineStarted = false
		r.send([]byte{'\r'})
		return
	}

	r.waitForEcho()
	command := r.readCommand()
	r.lineStarted = false
	if command == "" {
		r.send([]byte{'\r'})
		return
	}

	rule, err := object.MatchCommandRule(r.opts.User, r.opts.Asset, command)
	if err != nil {
		logs.Error(fmt.Sprintf("Recorder.enter(): failed to match the command rules: %s", err.Error()))
	}

	if rule != nil && rule.Action == object.CommandActionBlock {
		r.notify(fmt.Sprintf("\r\n\x1b[1;31m[Casvisor] The command is blocked by the rule: %s\x1b[0m\r\n", getRuleName(rule)))
		// Ctrl+C clears the line instead of running it
		r.send([]byte{3})
	} else {
		r.send([]byte{'\r'})
	}

	r.record(command, rule)
}

// waitForEcho waits for the asset to echo what was typed last, so that the screen
// shows the whole line, Tab completions and recalled history included.
func (r *Recorder) waitForEcho() {
	deadline := time.Now().Add(maxEchoWait)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		echoed := !r.lastOutput.Before(r.lastSend)
		quiet := time.Since(r.lastOutput) >= echoWait
		r.mu.Unlock()

		if echoed && quiet {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// readCommand reads the line from the end of the prompt to the cursor, following
// it upwards when the screen has scrolled or the line has wrapped.
func (r *Recorder) readCommand() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.vt.Lock()
	defer r.vt.Unlock()

	cursor := r.vt.Cursor()
	row := r.promptRow
	if row > cursor.Y || !strings.HasPrefix(string(r.rowCells(row)), r.promptText) {
		row = -1
		for y := cursor.Y; y >= 0 && y >= cursor.Y-50; y-- {
			if strings.HasPrefix(string(r.rowCells(y)), r.promptText) {
				row = y
				break
			}
		}
		if row < 0 {
			return ""
		}
	}

	var sb strings.Builder
	for y := row; y <= cursor.Y; y++ {
		cells := r.rowCells(y)
		if y == row {
			cells = cells[min(r.promptX, len(cells)):]
		}
		if y == cursor.Y {
			sb.WriteString(strings.TrimRight(string(cells), " "))
		} else {
			sb.WriteString(string(cells))
		}
	}
	return strings.TrimSpace(sb.String())
}

// rowCells returns the characters of a screen row; the caller holds the vt lock.
func (r *Recorder) rowCells(y int) []rune {
	cols, rows := r.vt.Size()
	if y < 0 || y >= rows {
		return []rune{}
	}

	res := make([]rune, cols)
	for x := 0; x < cols; x++ {
		c := r.vt.Cell(x, y).Char
		if c == 0 {
			c = ' '
		}
		res[x] = c
	}
	return res
}

func (r *Recorder) notify(message string) {
	p := []byte(message)
	r.opts.Notify(p)
	r.Output(p)
}

func (r *Recorder) record(text string, rule *object.CommandRule) {
	asset := r.opts.Asset
	command := &object.Command{
		Owner:    asset.Owner,
		Session:  r.opts.SessionId,
		Asset:    asset.GetId(),
		User:     util.GetIdFromOwnerAndName(r.opts.User.Owner, r.opts.User.Name),
		ClientIp: r.opts.ClientIp,
		Text:     text,
	}
	if rule != nil {
		command.Action = rule.Action
		command.Rule = rule.GetId()
	}

	err := object.AddCommand(command)
	if err != nil {
		logs.Error(fmt.Sprintf("Recorder.record(): %s", err.Error()))
	}
}

func getRuleName(rule *object.CommandRule) string {
	if rule.DisplayName != "" {
		return rule.DisplayName
	}
	return rule.Name
}

// CheckCommand applies the command rules to a command run without a terminal, like
// "ssh <asset> <command>", records it, and returns the rule that blocks it.
func CheckCommand(sessionId string, user *casdoorsdk.User, asset *object.Asset, clientIp string, text string) (*object.CommandRule, error) {
	rule, err := object.MatchCommandRule(user, asset, text)
	if err != nil {
		return nil, err
	}

	r := &Recorder{opts: Options{SessionId: sessionId, User: user, Asset: asset, ClientIp: clientIp}}
	r.record(text, rule)

	if rule != nil && rule.Action == object.CommandActionBlock {
		return rule, nil
	}
	return nil, nil
}

func GetBlockedMessage(rule *object.CommandRule) string {
	return fmt.Sprintf("the command is blocked by the rule: %s", getRuleName(rule))
}
