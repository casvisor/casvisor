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

// Package recording saves what a session showed its user, frame by frame with
// the time it was shown, so that the browser can replay it with the same code
// that drew it live: terminal output for SSH and Telnet, the RFB stream for
// VNC, and the screen frames for RDP.
//
// A recording file is the magic line "CASVREC1\n", a JSON header line, then
// frames of a little-endian uint32 offset in milliseconds, a kind byte, a
// uint32 length and the data.
package recording

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/conf"
)

const (
	Magic = "CASVREC1\n"

	// KindData is binary data the viewer received: terminal output, the RFB
	// stream or an RDP screen frame.
	KindData = 1
	// KindText is a JSON message the viewer received, e.g. a new RDP desktop size.
	KindText = 2
	// KindResize is a JSON {"cols", "rows"} for a terminal that changed its size.
	KindResize = 3

	flushInterval = 2 * time.Second
)

type Header struct {
	Version   int    `json:"version"`
	Protocol  string `json:"protocol"`
	Session   string `json:"session"`
	Asset     string `json:"asset"`
	User      string `json:"user"`
	StartTime string `json:"startTime"`
	Cols      int    `json:"cols,omitempty"`
	Rows      int    `json:"rows,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
}

type Frame struct {
	Time uint32
	Kind byte
	Data []byte
}

// Recording is a recording being written.
type Recording struct {
	Name   string
	Header Header

	mu        sync.Mutex
	file      *os.File
	w         *bufio.Writer
	start     time.Time
	lastFlush time.Time
	size      int64
	viewers   map[*Viewer]struct{}
	closed    bool
}

var recordings sync.Map

func IsEnabled() bool {
	return !conf.GetConfigBool("disableRecording")
}

func getRoot() string {
	root := conf.GetConfigString("recordingPath")
	if root == "" {
		root = "recordings"
	}
	return root
}

// GetPath returns the file of a recording name, refusing names that leave the recording directory.
func GetPath(name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if name == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid recording name: %s", name)
	}
	return filepath.Join(getRoot(), clean), nil
}

// Start creates the recording file of a session, named by its organization,
// the day and the session, with a suffix when the session records more than one screen.
func Start(header Header) (*Recording, error) {
	if !IsEnabled() {
		return nil, nil
	}

	now := time.Now()
	owner, name, _ := strings.Cut(header.Session, "/")
	dir := filepath.Join(getRoot(), owner, now.Format("2006-01-02"))
	err := os.MkdirAll(dir, 0o700)
	if err != nil {
		return nil, err
	}

	var file *os.File
	var fileName string
	for i := 1; ; i++ {
		fileName = name + ".cvr"
		if i > 1 {
			fileName = fmt.Sprintf("%s-%d.cvr", name, i)
		}
		file, err = os.OpenFile(filepath.Join(dir, fileName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			break
		}
		if !os.IsExist(err) || i >= 100 {
			return nil, err
		}
	}

	header.Version = 1
	header.StartTime = now.Format(time.RFC3339Nano)
	r := &Recording{
		Name:      filepath.ToSlash(filepath.Join(owner, now.Format("2006-01-02"), fileName)),
		Header:    header,
		file:      file,
		w:         bufio.NewWriterSize(file, 64*1024),
		start:     now,
		lastFlush: now,
		viewers:   map[*Viewer]struct{}{},
	}

	headerJson, err := json.Marshal(header)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	n1, _ := r.w.WriteString(Magic)
	n2, _ := r.w.Write(append(headerJson, '\n'))
	r.size = int64(n1 + n2)

	recordings.Store(header.Session, r)
	return r, nil
}

// Get returns the recording being written for a session.
func Get(sessionId string) *Recording {
	value, ok := recordings.Load(sessionId)
	if !ok {
		return nil
	}
	return value.(*Recording)
}

func (r *Recording) Write(kind byte, data []byte) {
	if r == nil || len(data) == 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}

	frame := Frame{Time: uint32(time.Since(r.start).Milliseconds()), Kind: kind, Data: data}
	var head [9]byte
	binary.LittleEndian.PutUint32(head[0:], frame.Time)
	head[4] = kind
	binary.LittleEndian.PutUint32(head[5:], uint32(len(data)))
	_, err := r.w.Write(head[:])
	if err == nil {
		_, err = r.w.Write(data)
	}
	if err != nil {
		logs.Error(fmt.Sprintf("Recording.Write(): %s: %s", r.Name, err.Error()))
		r.closeLocked()
		return
	}
	r.size += int64(len(head) + len(data))

	if time.Since(r.lastFlush) >= flushInterval {
		_ = r.w.Flush()
		r.lastFlush = time.Now()
	}

	for viewer := range r.viewers {
		if !viewer.send(Frame{Time: frame.Time, Kind: kind, Data: append([]byte{}, data...)}) {
			delete(r.viewers, viewer)
		}
	}
}

func (r *Recording) WriteJson(kind byte, v interface{}) {
	if r == nil {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	r.Write(kind, data)
}

// Size returns the bytes written so far.
func (r *Recording) Size() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.size
}

func (r *Recording) Close() {
	if r == nil {
		return
	}

	recordings.CompareAndDelete(r.Header.Session, r)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeLocked()
}

func (r *Recording) closeLocked() {
	if r.closed {
		return
	}
	r.closed = true
	_ = r.w.Flush()
	_ = r.file.Close()
	for viewer := range r.viewers {
		viewer.close()
	}
	r.viewers = map[*Viewer]struct{}{}
}

// Delete removes the recording files of a session.
func Delete(names []string) {
	for _, name := range names {
		path, err := GetPath(name)
		if err != nil {
			continue
		}
		err = os.Remove(path)
		if err != nil && !os.IsNotExist(err) {
			logs.Error(fmt.Sprintf("recording.Delete(): %s", err.Error()))
		}
	}
}

// ReadHeader reads the header of a recording file and leaves the reader at its first frame.
func ReadHeader(r *bufio.Reader) (*Header, error) {
	magic := make([]byte, len(Magic))
	_, err := io.ReadFull(r, magic)
	if err != nil || string(magic) != Magic {
		return nil, fmt.Errorf("not a Casvisor recording")
	}

	line, err := r.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	header := &Header{}
	err = json.Unmarshal(line, header)
	if err != nil {
		return nil, err
	}
	return header, nil
}

// ReadFrame reads the next frame of a recording file.
func ReadFrame(r *bufio.Reader) (*Frame, error) {
	var head [9]byte
	_, err := io.ReadFull(r, head[:])
	if err != nil {
		return nil, err
	}

	frame := &Frame{
		Time: binary.LittleEndian.Uint32(head[0:]),
		Kind: head[4],
		Data: make([]byte, binary.LittleEndian.Uint32(head[5:])),
	}
	_, err = io.ReadFull(r, frame.Data)
	if err != nil {
		return nil, err
	}
	return frame, nil
}
