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

package routers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/beego/beego/context"
)

func TestTransparentStatic(t *testing.T) {
	workspace := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(cwd); err != nil {
			t.Error(err)
		}
	})

	root := filepath.Join(workspace, "web", "build")
	indexFile := filepath.Join(root, "index.html")
	assetFile := filepath.Join(root, "static", "app.js")
	sibling := filepath.Join(workspace, "web", "build-secret", "x")
	outside := filepath.Join(workspace, "web", "outside.txt")
	const index, asset, forbidden = "<html>SPA</html>", "console.log('static');", "Forbidden\n"
	for name, content := range map[string]string{
		indexFile: index,
		assetFile: asset,
		sibling:   "outside the static root",
		outside:   "outside the static root",
	} {
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	check := func(t *testing.T, path string, code int, body string) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.URL.Path = path
		w := httptest.NewRecorder()
		ctx := context.NewContext()
		ctx.Reset(w, r)
		TransparentStatic(ctx)
		if code == 0 {
			if ctx.ResponseWriter.Started {
				t.Fatalf("%q unexpectedly wrote a response: %d %q", path, w.Code, w.Body.String())
			}
			return
		}
		if !ctx.ResponseWriter.Started || w.Code != code || w.Body.String() != body {
			t.Fatalf("%q: got %d %q, want %d %q", path, w.Code, w.Body.String(), code, body)
		}
	}

	tests := []struct {
		name string
		path string
		code int
		body string
	}{
		{"root", "/", http.StatusOK, index},
		{"static file", "/static/app.js", http.StatusOK, asset},
		{"client route", "/assets/123", http.StatusOK, index},
		{"directory", "/static", http.StatusOK, index},
		{"parent traversal", "/../outside.txt", http.StatusForbidden, forbidden},
		{"nested traversal", "/static/../../outside.txt", http.StatusForbidden, forbidden},
		{"sibling prefix", "/../build-secret/x", http.StatusForbidden, forbidden},
		{"backslashes", `/..\..\conf\app.conf`, http.StatusForbidden, forbidden},
		{"drive letter", "/C:/Windows/win.ini", http.StatusForbidden, forbidden},
		{"drive relative", "/z:relative/in/drive", http.StatusForbidden, forbidden},
		{"UNC path", "//server/share", http.StatusForbidden, forbidden},
		{"NUL byte", "/static/app.js\x00", http.StatusForbidden, forbidden},
		{"API bypass", "/api/test", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check(t, tt.path, tt.code, tt.body)
		})
	}

	t.Run("in-root symlink", func(t *testing.T) {
		makeStaticSymlink(t, assetFile, filepath.Join(root, "alias.js"))
		check(t, "/alias.js", http.StatusOK, asset)
	})
	t.Run("symlink escape", func(t *testing.T) {
		makeStaticSymlink(t, sibling, filepath.Join(root, "escape"))
		check(t, "/escape", http.StatusForbidden, forbidden)
	})
	t.Run("index symlink escape", func(t *testing.T) {
		if err := os.Remove(indexFile); err != nil {
			t.Fatal(err)
		}
		makeStaticSymlink(t, sibling, indexFile)
		check(t, "/missing", http.StatusForbidden, forbidden)
	})
}

func makeStaticSymlink(t *testing.T, target, link string) {
	t.Helper()
	const privilegeNotHeld = syscall.Errno(1314) // ERROR_PRIVILEGE_NOT_HELD on Windows.
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" && (errors.Is(err, os.ErrPermission) || errors.Is(err, privilegeNotHeld)) {
			t.Skipf("cannot create symlinks: %v", err)
		}
		t.Fatal(err)
	}
}

func TestServeFileWithReplaceErrors(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/missing", nil)
	w := httptest.NewRecorder()
	serveFileWithReplace(w, r, filepath.Join(t.TempDir(), "missing"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing file: got %d, want %d", w.Code, http.StatusNotFound)
	}

	w = httptest.NewRecorder()
	serveFileWithReplace(w, r, "invalid\x00path")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("open error: got %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestGetUsernameByClientIdSecret(t *testing.T) {
	for _, ctx := range []*context.Context{nil, context.NewContext()} {
		if user, err := getUsernameByClientIdSecret(ctx); user != "" || err != nil {
			t.Fatalf("missing request: got (%q, %v), want empty username and no error", user, err)
		}
	}

	t.Setenv("casdoorApplication", "test-app")
	tests := []struct {
		name                       string
		basic                      bool
		id, secret                 string
		expectedID, expectedSecret string
		want                       string
		wantErr                    bool
	}{
		{"query only", false, "", "", "client", "secret", "", false},
		{"matching Basic Auth", true, "client", "secret", "client", "secret", "app/test-app", false},
		{"wrong ID", true, "wrong!", "secret", "client", "secret", "", true},
		{"wrong secret", true, "client", "wrong!", "client", "secret", "", true},
		{"unconfigured ID", true, "client", "secret", "", "secret", "", true},
		{"unconfigured secret", true, "client", "secret", "client", "", "", true},
		{"empty Basic ID", true, "", "secret", "client", "secret", "", false},
		{"empty Basic secret", true, "client", "", "client", "secret", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("clientId", tt.expectedID)
			t.Setenv("clientSecret", tt.expectedSecret)
			r := httptest.NewRequest(http.MethodGet, "/api/test?clientId=client&clientSecret=secret", nil)
			if tt.basic {
				r.SetBasicAuth(tt.id, tt.secret)
			}
			ctx := context.NewContext()
			ctx.Reset(httptest.NewRecorder(), r)
			user, err := getUsernameByClientIdSecret(ctx)
			if user != tt.want || (err != nil) != tt.wantErr {
				t.Fatalf("got (%q, %v), want username %q and error=%v", user, err, tt.want, tt.wantErr)
			}
		})
	}
}
