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
	"slices"
	"strings"
	"sync"
	"sync/atomic"
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

	bracketedPasteOn  = []byte("\x1b[?2004h")
	bracketedPasteOff = []byte("\x1b[?2004l")
)

// maxTyped bounds the keystrokes kept for a line.
const maxTyped = 4096

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

// inputChunk is keystrokes and who typed them: the session's user, or an administrator who
// took part in the session from the monitor.
type inputChunk struct {
	data     []byte
	operator *casdoorsdk.User
}

type Recorder struct {
	opts  Options
	input chan inputChunk

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
	outTail    []byte

	// whether the program on the asset asked for bracketed paste, so that only then
	// a pasted Enter is taken as text rather than as running the line
	bracketedPaste atomic.Bool

	// reviewCancel is set while a command waits for review: the keystrokes are dropped
	// meanwhile, and Ctrl+C or the end of the session withdraws the command
	reviewCancel chan struct{}

	// only touched by the input goroutine
	operator    *casdoorsdk.User
	lineUser    *casdoorsdk.User
	lineStarted bool
	promptRow   int
	promptX     int
	promptText  string
	inPaste     bool
	inTail      []byte
	// typed is what was typed for the line, for when the screen doesn't show it
	typed         []byte
	typedReliable bool
	inEscape      bool
}

var recorders sync.Map

// maxCols and maxRows bound the virtual screen, whose memory grows with its size.
const (
	maxCols = 1000
	maxRows = 500
)

func NewRecorder(opts Options) *Recorder {
	if opts.Cols <= 0 {
		opts.Cols = 80
	}
	if opts.Rows <= 0 {
		opts.Rows = 24
	}
	opts.Cols, opts.Rows = min(opts.Cols, maxCols), min(opts.Rows, maxRows)

	r := &Recorder{
		opts:    opts,
		input:   make(chan inputChunk, 256),
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
	r.cancelReview()
	r.rec.Close()
	close(r.input)
	for viewer := range r.viewers {
		viewer.close()
	}
	r.viewers = map[*Viewer]struct{}{}
}

// writeVt feeds the virtual screen. vt10x panics on some escape sequences, such as a
// negative count; the screen then starts over instead of the whole server going down,
// and the commands are read from the keystrokes until a prompt shows again. The caller holds r.mu.
func (r *Recorder) writeVt(data []byte) (n int) {
	defer func() {
		if err := recover(); err != nil {
			logs.Warning("terminal: the virtual screen of session: %s failed and is reset: %v", r.opts.SessionId, err)
			r.vt = vt10x.New(vt10x.WithSize(r.cols, r.rows))
			n = len(data)
		}
	}()

	n, _ = r.vt.Write(data)
	return n
}

func (r *Recorder) resizeVt(cols int, rows int) {
	defer func() {
		if err := recover(); err != nil {
			logs.Warning("terminal: the virtual screen of session: %s failed and is reset: %v", r.opts.SessionId, err)
			r.vt = vt10x.New(vt10x.WithSize(cols, rows))
		}
	}()

	r.vt.Resize(cols, rows)
}

// Output takes what the asset sent to the user's terminal.
func (r *Recorder) Output(p []byte) {
	if len(p) == 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	data := append(r.partial, p...)
	n := r.writeVt(data)
	if n < len(data) {
		r.partial = append([]byte{}, data[n:]...)
	} else {
		r.partial = nil
	}
	r.lastOutput = time.Now()
	r.trackBracketedPaste(p)

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

// trackBracketedPaste follows the asset turning bracketed paste on and off, across
// writes that split the sequence; the caller holds the lock.
func (r *Recorder) trackBracketedPaste(p []byte) {
	scan := append(append([]byte{}, r.outTail...), p...)
	on := bytes.LastIndex(scan, bracketedPasteOn)
	off := bytes.LastIndex(scan, bracketedPasteOff)
	if on > off {
		r.bracketedPaste.Store(true)
	} else if off > on {
		r.bracketedPaste.Store(false)
	}

	tail := len(bracketedPasteOn) - 1
	if len(scan) > tail {
		scan = scan[len(scan)-tail:]
	}
	r.outTail = append([]byte{}, scan...)
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
	cols, rows = min(cols, maxCols), min(rows, maxRows)

	r.mu.Lock()
	defer r.mu.Unlock()

	r.resizeVt(cols, rows)
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
	r.InputAs(nil, p)
}

// InputAs takes keystrokes typed by an administrator taking part in the session; the commands
// they run are checked against the rules for them and recorded with them as the operator.
func (r *Recorder) InputAs(operator *casdoorsdk.User, p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || len(p) == 0 {
		return
	}
	if r.reviewCancel != nil {
		if bytes.IndexByte(p, 3) >= 0 {
			r.cancelReview()
		}
		return
	}

	select {
	case r.input <- inputChunk{data: append([]byte{}, p...), operator: operator}:
	default:
		// the asset is not reading, drop the keystrokes rather than block the session
	}
}

// cancelReview withdraws the command waiting for review, if any; the caller holds the lock.
func (r *Recorder) cancelReview() {
	if r.reviewCancel != nil {
		close(r.reviewCancel)
		r.reviewCancel = nil
	}
}

// Announce shows a message from Casvisor to the user, to the viewers and in the recording.
func (r *Recorder) Announce(message string) {
	r.notify(fmt.Sprintf("\r\n\x1b[1;33m[Casvisor] %s\x1b[0m\r\n", message))
}

func (r *Recorder) run() {
	for chunk := range r.input {
		r.operator = chunk.operator
		r.process(chunk.data)
	}
}

// getLineUser returns who typed the line being run, for the command rules.
func (r *Recorder) getLineUser() *casdoorsdk.User {
	if r.lineUser != nil {
		return r.lineUser
	}
	return r.opts.User
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

// isLineEnd tells the keys that make a shell run the line: Enter, Ctrl+J and,
// in bash, Ctrl+O.
func isLineEnd(b byte) bool {
	return b == '\r' || b == '\n' || b == 0x0f
}

func (r *Recorder) process(p []byte) {
	// the paste markers are looked for across chunks, as a client can split them
	scan := append(append([]byte{}, r.inTail...), p...)
	offset := len(r.inTail)

	start := 0
	for i := 0; i < len(p); i++ {
		end := scan[:offset+i+1]
		if bytes.HasSuffix(end, pasteStart) {
			r.inPaste = true
		} else if bytes.HasSuffix(end, pasteEnd) {
			r.inPaste = false
		}

		switch b := p[i]; {
		case isLineEnd(b) && !(r.inPaste && r.bracketedPaste.Load()):
			r.send(p[start:i])
			start = i + 1
			r.enter(b)
		case b == 3 || b == 4:
			// Ctrl+C or Ctrl+D drops the line
			r.lineStarted = false
			r.inPaste = false
		default:
			if !r.lineStarted {
				r.startLine()
			}
			r.addTyped(b)
		}
	}
	r.send(p[start:])

	tail := len(pasteStart) - 1
	if len(scan) > tail {
		scan = scan[len(scan)-tail:]
	}
	r.inTail = append([]byte{}, scan...)
}

// addTyped keeps the printable keystrokes of the line; a line edited with cursor
// keys or other sequences can't be followed, so it is no longer used.
func (r *Recorder) addTyped(b byte) {
	switch {
	case r.inEscape:
		// a CSI sequence ends with a byte from @ to ~
		if b >= 0x40 && b <= 0x7e && b != '[' {
			r.inEscape = false
		}
	case b == 0x1b:
		r.inEscape = true
		if !r.inPaste {
			r.typedReliable = false
		}
	case b == 0x7f || b == 0x08:
		if len(r.typed) > 0 {
			r.typed = r.typed[:len(r.typed)-1]
		}
	case b == '\n' || b == '\r':
		r.typed = append(r.typed, ' ')
	case b == '\t' || b < 0x20:
		r.typedReliable = false
	default:
		if len(r.typed) < maxTyped {
			r.typed = append(r.typed, b)
		}
	}
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
	r.lineUser = r.operator
	r.typed = r.typed[:0]
	r.typedReliable = true
	r.inEscape = false
}

func (r *Recorder) isFullScreen() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.vt.Lock()
	defer r.vt.Unlock()
	return r.vt.Mode()&vt10x.ModeAltScreen != 0
}

func (r *Recorder) enter(key byte) {
	if !r.lineStarted {
		r.send([]byte{key})
		return
	}

	r.waitForEcho()
	fullScreen := r.isFullScreen()
	command := r.readCommand()
	typed := ""
	if r.typedReliable {
		typed = strings.TrimSpace(string(r.typed))
	}
	r.lineStarted = false

	// In a full-screen program, or on a line the screen doesn't show such as with echo
	// turned off, what is on the line and what was typed are still checked, but only
	// recorded when a rule stops or holds them, as they are often text or passwords.
	visible := !fullScreen && command != ""
	texts := []string{}
	for _, text := range []string{command, typed} {
		if text != "" && !slices.Contains(texts, text) {
			texts = append(texts, text)
		}
	}

	var rule *object.CommandRule
	matchedText := ""
	for _, text := range texts {
		matched, err := object.MatchCommandRule(r.getLineUser(), r.opts.Asset, text)
		if err != nil {
			// a command whose rules can't be read is blocked rather than run unchecked
			logs.Error(fmt.Sprintf("Recorder.enter(): failed to match the command rules: %s", err.Error()))
			r.notify("\r\n\x1b[1;31m[Casvisor] The command is blocked because the command rules can't be read\x1b[0m\r\n")
			r.send([]byte{3})
			c := r.newCommand(text, nil)
			c.Action, c.Error = object.CommandActionBlock, "the command is blocked because the command rules can't be read"
			r.record(c)
			return
		}
		if more := object.MoreSevereRule(rule, matched); more != rule {
			rule, matchedText = more, text
		}
	}

	if rule != nil && rule.Action == object.CommandActionBlock {
		r.notify(fmt.Sprintf("\r\n\x1b[1;31m[Casvisor] The command is blocked by the rule: %s\x1b[0m\r\n", rule.GetDisplayName()))
		// Ctrl+C clears the line instead of running it
		r.send([]byte{3})
		r.record(r.newCommand(matchedText, rule))
		return
	}

	if rule != nil && rule.Action == object.CommandActionReview {
		// the reviewer sees the line as the screen shows it, which is what runs
		if visible {
			matchedText = command
		}
		if r.hold(matchedText, rule) {
			r.send([]byte{key})
		} else {
			r.send([]byte{3})
		}
		return
	}

	if visible {
		r.record(r.newCommand(command, rule))
	}
	r.send([]byte{key})
}

// hold keeps the line from running until an administrator approves it, and tells whether
// it was approved. Keystrokes are dropped while it waits, and Ctrl+C withdraws the command.
func (r *Recorder) hold(text string, rule *object.CommandRule) bool {
	cancel := make(chan struct{})
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return false
	}
	r.reviewCancel = cancel
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		if r.reviewCancel == cancel {
			r.reviewCancel = nil
		}
		r.mu.Unlock()
	}()

	command := r.newCommand(text, nil)
	approved, err := object.HoldCommand(command, rule, cancel, func(review *object.CommandReview) {
		r.notifyOpen(fmt.Sprintf("\r\n\x1b[1;33m[Casvisor] %s, press Ctrl+C to withdraw it\x1b[0m\r\n", capitalize(object.GetHeldMessage(rule, review))))
	})
	if err != nil {
		logs.Error(fmt.Sprintf("Recorder.hold(): %s", err.Error()))
	}
	r.record(command)

	if approved {
		_, reviewer := util.GetOwnerAndNameFromIdNoCheck(command.Reviewer)
		r.notifyOpen(fmt.Sprintf("\x1b[1;32m[Casvisor] The command is approved by %s\x1b[0m\r\n", reviewer))
	} else {
		r.notifyOpen(fmt.Sprintf("\x1b[1;31m[Casvisor] %s\x1b[0m\r\n", capitalize(command.Error)))
	}
	return approved
}

// notifyOpen shows a message unless the session has ended meanwhile.
func (r *Recorder) notifyOpen(message string) {
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if !closed {
		r.notify(message)
	}
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
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

// newCommand is a command of the session, with the rule it broke.
func (r *Recorder) newCommand(text string, rule *object.CommandRule) *object.Command {
	asset := r.opts.Asset
	command := &object.Command{
		Owner:    asset.Owner,
		Session:  r.opts.SessionId,
		Asset:    asset.GetId(),
		User:     util.GetIdFromOwnerAndName(r.opts.User.Owner, r.opts.User.Name),
		ClientIp: r.opts.ClientIp,
		Text:     text,
		Protocol: asset.Protocol,
	}
	if r.lineUser != nil {
		command.Operator = util.GetIdFromOwnerAndName(r.lineUser.Owner, r.lineUser.Name)
	}
	if rule != nil {
		command.Action = rule.Action
		command.Rule = rule.GetId()
	}
	return command
}

func (r *Recorder) record(command *object.Command) {
	err := object.AddCommand(command)
	if err != nil {
		logs.Error(fmt.Sprintf("Recorder.record(): %s", err.Error()))
	}
}

// CheckCommand applies the command rules to a command run without a terminal, like
// "ssh <asset> <command>", holds it while a Review rule has it reviewed, records it, and
// tells why it may not run, or "" when it may. onHeld is told while the command waits;
// closing cancel withdraws it.
func CheckCommand(sessionId string, user *casdoorsdk.User, asset *object.Asset, clientIp string, text string, cancel <-chan struct{}, onHeld func(message string)) (string, error) {
	rule, err := object.MatchCommandRule(user, asset, text)
	if err != nil {
		return "", err
	}

	r := &Recorder{opts: Options{SessionId: sessionId, User: user, Asset: asset, ClientIp: clientIp}}
	command := r.newCommand(text, rule)
	if rule != nil && rule.Action == object.CommandActionReview {
		approved, err := object.HoldCommand(command, rule, cancel, func(review *object.CommandReview) {
			if onHeld != nil {
				onHeld(object.GetHeldMessage(rule, review))
			}
		})
		r.record(command)
		if err != nil {
			return "", err
		}
		if !approved {
			return command.Error, nil
		}
		return "", nil
	}

	r.record(command)
	if rule != nil && rule.Action == object.CommandActionBlock {
		return GetBlockedMessage(rule), nil
	}
	return "", nil
}

func GetBlockedMessage(rule *object.CommandRule) string {
	return fmt.Sprintf("the command is blocked by the rule: %s", rule.GetDisplayName())
}
