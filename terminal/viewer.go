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

package terminal

import "sync"

// Event is what a viewer receives: output, or the new size of the terminal.
type Event struct {
	Data []byte
	Cols int
	Rows int
}

// Viewer is an administrator watching a session.
type Viewer struct {
	Events chan Event

	once sync.Once
}

func (v *Viewer) send(event Event) bool {
	select {
	case v.Events <- event:
		return true
	default:
		// a viewer that falls behind is dropped instead of slowing the session down
		v.close()
		return false
	}
}

func (v *Viewer) close() {
	v.once.Do(func() {
		close(v.Events)
	})
}

// Watch returns the output kept so far, which redraws the screen, the terminal
// size, and a viewer that receives everything from now on.
func (r *Recorder) Watch() ([]byte, int, int, *Viewer) {
	r.mu.Lock()
	defer r.mu.Unlock()

	viewer := &Viewer{Events: make(chan Event, 1024)}
	if r.closed {
		viewer.close()
	} else {
		r.viewers[viewer] = struct{}{}
	}
	return append([]byte{}, r.history...), r.cols, r.rows, viewer
}

func (r *Recorder) Unwatch(viewer *Viewer) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.viewers, viewer)
	viewer.close()
}
