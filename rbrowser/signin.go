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
	"fmt"
	"strings"
	"time"
)

const (
	signinStepTimeout = 20 * time.Second
	signinWait        = 8 * time.Second
)

const fillJs = `(async (selector, value, timeout) => {
  const deadline = Date.now() + timeout;
  const visible = (e) => !!(e.offsetWidth || e.offsetHeight || e.getClientRects().length);
  let e = null;
  while (!(e = document.querySelector(selector)) || !visible(e)) {
    if (Date.now() > deadline) throw new Error("no visible element matches " + selector);
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  e.focus();
  const proto = e instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
  Object.getOwnPropertyDescriptor(proto, "value").set.call(e, value);
  e.dispatchEvent(new Event("input", {bubbles: true}));
  e.dispatchEvent(new Event("change", {bubbles: true}));
  return true;
})(%s, %s, %d)`

const clickJs = `(async (selector, fallback, timeout) => {
  const deadline = Date.now() + timeout;
  let e = null;
  if (selector) {
    while (!(e = document.querySelector(selector))) {
      if (Date.now() > deadline) throw new Error("no element matches " + selector);
      await new Promise((resolve) => setTimeout(resolve, 200));
    }
    setTimeout(() => e.click(), 0);
    return true;
  }
  const field = document.querySelector(fallback);
  const form = field && field.form;
  if (!form) throw new Error("the form to submit is not found, set the selector of its button");
  setTimeout(() => (form.requestSubmit ? form.requestSubmit() : form.submit()), 0);
  return true;
})(%s, %s, %d)`

const clearJs = `((value) => {
  for (const e of document.querySelectorAll("input, textarea")) {
    if (e.value === value) {
      const proto = e instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
      Object.getOwnPropertyDescriptor(proto, "value").set.call(e, "");
      e.dispatchEvent(new Event("input", {bubbles: true}));
    }
  }
  return true;
})(%s)`

func (b *Browser) setSigningIn(signingIn bool) {
	b.mu.Lock()
	b.signingIn = signingIn
	b.mu.Unlock()
	_ = b.viewer.WriteMessage(map[string]interface{}{"t": "signin", "active": signingIn})
}

// signIn opens the sign-in page and fills in the account, while the screen is hidden from the
// user and the user's input is ignored, so that the password is never shown. A password left
// in the page, after a failed sign-in, is cleared before the screen is shown again.
func (b *Browser) signIn(start string) {
	b.setSigningIn(true)
	err := b.fill(start)

	if t := b.getActive(); t != nil {
		_, _ = b.evaluate(t.sessionId, fmt.Sprintf(clearJs, encodeJson(b.asset.Password)), false)
	}
	b.setSigningIn(false)
	b.restartScreencast()
	if err != nil {
		b.notice("Casvisor failed to sign in to the website: " + err.Error())
	}
}

func (b *Browser) fill(start string) error {
	err := b.navigate(start)
	if err != nil {
		return err
	}

	login := b.login
	timeout := signinStepTimeout.Milliseconds()
	if login.UsernameSelector != "" {
		err = b.run(fmt.Sprintf(fillJs, encodeJson(login.UsernameSelector), encodeJson(b.asset.Username), timeout))
		if err != nil {
			return err
		}
		if login.NextSelector != "" {
			err = b.run(fmt.Sprintf(clickJs, encodeJson(login.NextSelector), "null", timeout))
			if err != nil {
				return err
			}
		}
	}

	if login.PasswordSelector != "" {
		err = b.run(fmt.Sprintf(fillJs, encodeJson(login.PasswordSelector), encodeJson(b.asset.Password), timeout))
		if err != nil {
			return err
		}
	}

	fallback := login.PasswordSelector
	if fallback == "" {
		fallback = login.UsernameSelector
	}
	t := b.getActive()
	if t == nil {
		return errClosed
	}
	before := b.getUrl(t)
	err = b.run(fmt.Sprintf(clickJs, encodeJson(login.SubmitSelector), encodeJson(fallback), timeout))
	if err != nil {
		return err
	}

	deadline := time.Now().Add(signinWait)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		if b.getUrl(t) != before {
			time.Sleep(time.Second)
			break
		}
	}
	return nil
}

// run evaluates a step of the sign-in in the page shown, again in the next page when a click
// before it was still leaving the page.
func (b *Browser) run(expression string) error {
	deadline := time.Now().Add(signinStepTimeout)
	for {
		t := b.getActive()
		if t == nil {
			return errClosed
		}
		_, err := b.evaluate(t.sessionId, expression, true)
		if err == nil {
			return nil
		}
		if !isContextLost(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func isContextLost(err error) bool {
	text := err.Error()
	return strings.Contains(text, "context was destroyed") || strings.Contains(text, "Cannot find context") || strings.Contains(text, "Inspected target navigated")
}

func (b *Browser) getUrl(t *tab) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return t.url
}
