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

package object

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

var passwordPromptPattern = regexp.MustCompile(`(?i)(password|passwd|口令|密码)[^\n:：]*[:：]\s*$`)

var passwordPromptLine = regexp.MustCompile(`(?i)(password|passwd|口令|密码)[^\n:：]*[:：][ \t]*`)

// elevate makes the host root by the elevation of the platform when the asset's own account is
// not root and can't sudo without a password: sudo with the account's own password, or su with
// the elevation password of the asset.
func (h *linuxHost) elevate(asset *Asset, platform *Platform) error {
	if h.root {
		return nil
	}
	switch platform.Elevation {
	case ElevationSudo:
		if asset.Password == "" || strings.ContainsAny(asset.Password, "\r\n") {
			return nil
		}
		code, err := runSshCommand(h.client, "sudo -k -S -p '' true", asset.Password+"\n", io.Discard, io.Discard)
		if err == nil && code == 0 {
			h.sudoPassword = asset.Password
			h.root = true
		}
		return nil
	case ElevationSu:
		if asset.ElevationPassword == "" {
			return fmt.Errorf("the platform: %s becomes root by su, fill in the elevation password of the asset: %s", platform.getDisplayName(), asset.Name)
		}
		if strings.ContainsAny(asset.ElevationPassword, "\r\n") {
			return fmt.Errorf("the elevation password can not contain line breaks")
		}
		h.suCommand = strings.TrimSpace(platform.ElevationCommand)
		h.suPassword = asset.ElevationPassword
		var out, stderr bytes.Buffer
		code, err := h.runSuScript("id -u\n", &out, &stderr)
		if err != nil {
			return err
		}
		if code != 0 {
			return fmt.Errorf("%s failed (exit code %d): %s", h.suCommand, code, truncate(strings.TrimSpace(maskSecret(stderr.String(), h.suPassword)), 300))
		}
		lines := strings.Fields(out.String())
		h.root = len(lines) > 0 && lines[len(lines)-1] == "0"
		if !h.root {
			return fmt.Errorf("%s did not become root: %s", h.suCommand, truncate(strings.TrimSpace(out.String()), 300))
		}
	}
	return nil
}

// lockedBuffer collects the output of a session while it is read from another goroutine.
type lockedBuffer struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	changed chan struct{}
}

func newLockedBuffer() *lockedBuffer {
	return &lockedBuffer{changed: make(chan struct{}, 1)}
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	n, err := b.buf.Write(p)
	b.mu.Unlock()
	select {
	case b.changed <- struct{}{}:
	default:
	}
	return n, err
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitFor waits until match is true of the output, stop is closed or the timeout passes.
func (b *lockedBuffer) waitFor(timeout time.Duration, stop <-chan struct{}, match func(text string) bool) bool {
	deadline := time.After(timeout)
	for {
		if match(b.String()) {
			return true
		}
		select {
		case <-b.changed:
		case <-stop:
			return match(b.String())
		case <-deadline:
			return match(b.String())
		}
	}
}

const suReadyMarker = "CASVISOR_SU_READY"

// runSuScript runs the script as root through su on a terminal, as su reads the password only
// from a terminal. Once the shell of su is up, the script goes in as a line of base64 that a
// shell decodes and runs, with echo off, so that it is neither shown nor read as typed commands.
func (h *linuxHost) runSuScript(script string, stdout io.Writer, stderr io.Writer) (int, error) {
	encoded := base64.StdEncoding.EncodeToString([]byte(script))
	if len(encoded) > 4000 {
		return -1, fmt.Errorf("the script is too long to run through su")
	}

	session, err := h.client.NewSession()
	if err != nil {
		return -1, err
	}
	defer session.Close()

	err = session.RequestPty("dumb", 40, 500, ssh.TerminalModes{ssh.ECHO: 0, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400})
	if err != nil {
		return -1, err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		return -1, err
	}
	output := newLockedBuffer()
	session.Stdout = output
	session.Stderr = output

	command := fmt.Sprintf(`echo %s; IFS= read -r s; printf '%%s' "$s" | base64 -d | sh`, suReadyMarker)
	err = session.Start(h.suCommand + " -c " + shellQuote(command))
	if err != nil {
		return -1, err
	}
	var waitErr error
	exited := make(chan struct{})
	go func() {
		waitErr = session.Wait()
		close(exited)
	}()

	finish := func() (int, error) {
		text := output.String()
		if i := strings.Index(text, suReadyMarker); i >= 0 {
			text = text[i+len(suReadyMarker):]
		} else if i := passwordPromptLine.FindStringIndex(text); i != nil {
			text = text[i[1]:]
		}
		text = strings.TrimLeft(text, "\r\n")
		_, _ = io.WriteString(stdout, text)
		_, _ = io.WriteString(stderr, text)
		if exitErr, ok := waitErr.(*ssh.ExitError); ok {
			return exitErr.ExitStatus(), nil
		}
		if waitErr != nil {
			return -1, waitErr
		}
		return 0, nil
	}
	fail := func(message string) (int, error) {
		select {
		case <-exited:
			return finish()
		default:
		}
		_ = session.Close()
		return -1, fmt.Errorf("%s %s: %s", h.suCommand, message, truncate(strings.TrimSpace(maskSecret(output.String(), h.suPassword)), 300))
	}

	if !output.waitFor(10*time.Second, exited, passwordPromptPattern.MatchString) {
		return fail("did not ask for the password")
	}
	// su may clear what was typed before it turns the echo off, so the password waits a moment
	time.Sleep(300 * time.Millisecond)
	_, err = io.WriteString(stdin, h.suPassword+"\n")
	if err != nil {
		return fail(err.Error())
	}
	ready := output.waitFor(15*time.Second, exited, func(text string) bool {
		return strings.Contains(text, suReadyMarker+"\r\n") || strings.Contains(text, suReadyMarker+"\n")
	})
	if !ready {
		return fail("did not start a shell")
	}
	_, err = io.WriteString(stdin, encoded+"\n")
	if err != nil {
		return fail(err.Error())
	}

	select {
	case <-exited:
		return finish()
	case <-time.After(5 * time.Minute):
		_ = session.Close()
		return -1, fmt.Errorf("%s did not finish in 5 minutes", h.suCommand)
	}
}
