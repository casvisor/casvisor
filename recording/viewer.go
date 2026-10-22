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

package recording

import (
	"bufio"
	"io"
	"os"
	"sync"
)

// Viewer is an administrator watching a session live. A screen can only be
// drawn from its first frame on, so a viewer gets the recording so far and
// then every new frame.
type Viewer struct {
	Frames chan Frame

	once sync.Once
}

func (v *Viewer) send(frame Frame) bool {
	select {
	case v.Frames <- frame:
		return true
	default:
		// a viewer that falls behind is dropped instead of slowing the session down
		v.close()
		return false
	}
}

func (v *Viewer) close() {
	v.once.Do(func() {
		close(v.Frames)
	})
}

// Watch returns the header, a function that reads the frames recorded so far
// and a viewer that receives every frame from now on.
func (r *Recording) Watch() (*Header, func(fn func(*Frame) error) error, *Viewer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	viewer := &Viewer{Frames: make(chan Frame, 4096)}
	if r.closed {
		viewer.close()
		return &r.Header, func(fn func(*Frame) error) error { return nil }, viewer, nil
	}

	err := r.w.Flush()
	if err != nil {
		return nil, nil, nil, err
	}
	path, size := r.file.Name(), r.size
	readSoFar := func(fn func(*Frame) error) error {
		return readFrames(path, size, fn)
	}

	r.viewers[viewer] = struct{}{}
	return &r.Header, readSoFar, viewer, nil
}

func (r *Recording) Unwatch(viewer *Viewer) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.viewers, viewer)
	viewer.close()
}

// readFrames calls fn with each frame in the first size bytes of a recording file.
func readFrames(path string, size int64, fn func(*Frame) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := bufio.NewReaderSize(io.LimitReader(file, size), 64*1024)
	_, err = ReadHeader(reader)
	if err != nil {
		return err
	}

	for {
		frame, err := ReadFrame(reader)
		if err != nil {
			return nil
		}
		err = fn(frame)
		if err != nil {
			return err
		}
	}
}
