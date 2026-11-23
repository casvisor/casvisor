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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

const commandTimeout = 30 * time.Second

var errClosed = errors.New("the browser has closed")

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cdpMessage struct {
	Id        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	SessionId string          `json:"sessionId,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *cdpError       `json:"error,omitempty"`
}

// cdp talks to a browser over the pipes of --remote-debugging-pipe, which no other
// process on the server can reach, unlike a debugging port.
type cdp struct {
	cmd *exec.Cmd
	r   io.ReadCloser
	w   io.WriteCloser

	writeMu sync.Mutex
	mu      sync.Mutex
	nextId  int64
	pending map[int64]chan *cdpMessage
	closed  bool
	done    chan struct{}
	exited  chan struct{}

	onEvent func(method string, params json.RawMessage, sessionId string)
}

// startCdp starts the browser with args and reads its events with onEvent, one at a time.
func startCdp(path string, args []string, onEvent func(method string, params json.RawMessage, sessionId string)) (*cdp, error) {
	childR, parentW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	parentR, childW, err := os.Pipe()
	if err != nil {
		_ = childR.Close()
		_ = parentW.Close()
		return nil, err
	}

	cmd := exec.Command(path)
	err = setChildPipes(cmd, childR, childW, parentR, parentW)
	if err == nil {
		cmd.Args = append(cmd.Args, args...)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		err = cmd.Start()
	}
	_ = childR.Close()
	_ = childW.Close()
	if err != nil {
		_ = parentR.Close()
		_ = parentW.Close()
		return nil, fmt.Errorf("failed to start the browser: %s: %s", path, err.Error())
	}

	c := &cdp{
		cmd:     cmd,
		r:       parentR,
		w:       parentW,
		pending: map[int64]chan *cdpMessage{},
		done:    make(chan struct{}),
		exited:  make(chan struct{}),
		onEvent: onEvent,
	}
	go func() {
		_ = cmd.Wait()
		close(c.exited)
		c.close()
	}()
	go c.read()
	return c, nil
}

func (c *cdp) read() {
	events := make(chan *cdpMessage, 1024)
	go func() {
		for msg := range events {
			c.onEvent(msg.Method, msg.Params, msg.SessionId)
		}
	}()
	defer close(events)

	reader := bufio.NewReaderSize(c.r, 1<<20)
	for {
		data, err := reader.ReadBytes(0)
		if err != nil {
			c.close()
			return
		}

		msg := &cdpMessage{}
		if json.Unmarshal(data[:len(data)-1], msg) != nil {
			continue
		}
		if msg.Id != 0 {
			c.mu.Lock()
			ch := c.pending[msg.Id]
			delete(c.pending, msg.Id)
			c.mu.Unlock()
			if ch != nil {
				ch <- msg
			}
			continue
		}

		select {
		case events <- msg:
		case <-c.done:
			return
		}
	}
}

// call sends a command to the browser, or to one of its pages when sessionId is set, and
// decodes its result into res, if not nil.
func (c *cdp) call(sessionId string, method string, params interface{}, res interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	return c.callContext(ctx, sessionId, method, params, res)
}

func (c *cdp) callContext(ctx context.Context, sessionId string, method string, params interface{}, res interface{}) error {
	pending, err := c.send(sessionId, method, params)
	if err != nil {
		return err
	}
	return pending.wait(ctx, res)
}

// pendingCall is a command sent to the browser, whose result has yet to be read.
type pendingCall struct {
	c      *cdp
	id     int64
	method string
	ch     chan *cdpMessage
}

// send sends a command without waiting for its result, so that the commands sent one after
// another run in that order, like the setup of a page before it is let run.
func (c *cdp) send(sessionId string, method string, params interface{}) (*pendingCall, error) {
	if params == nil {
		params = struct{}{}
	}
	rawParams, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	p := &pendingCall{c: c, method: method, ch: make(chan *cdpMessage, 1)}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errClosed
	}
	c.nextId++
	p.id = c.nextId
	c.pending[p.id] = p.ch
	c.mu.Unlock()

	data, err := json.Marshal(&cdpMessage{Id: p.id, Method: method, Params: rawParams, SessionId: sessionId})
	if err != nil {
		return nil, err
	}
	c.writeMu.Lock()
	_, err = c.w.Write(append(data, 0))
	c.writeMu.Unlock()
	if err != nil {
		c.close()
		return nil, errClosed
	}
	return p, nil
}

func (p *pendingCall) wait(ctx context.Context, res interface{}) error {
	select {
	case msg := <-p.ch:
		if msg.Error != nil {
			return fmt.Errorf("%s: %s", p.method, msg.Error.Message)
		}
		if res != nil && len(msg.Result) != 0 {
			return json.Unmarshal(msg.Result, res)
		}
		return nil
	case <-p.c.done:
		return errClosed
	case <-ctx.Done():
		p.c.mu.Lock()
		delete(p.c.pending, p.id)
		p.c.mu.Unlock()
		return fmt.Errorf("%s: %s", p.method, ctx.Err().Error())
	}
}

func (c *cdp) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	close(c.done)
	c.mu.Unlock()

	_ = c.w.Close()
	_ = c.r.Close()
}

// stop asks the browser to close, and kills it when it doesn't in a few seconds.
func (c *cdp) stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_ = c.callContext(ctx, "", "Browser.close", nil, nil)
	cancel()
	c.close()

	select {
	case <-c.exited:
	case <-time.After(5 * time.Second):
		_ = c.cmd.Process.Kill()
		<-c.exited
	}
}
