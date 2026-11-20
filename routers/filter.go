// Copyright 2023 The casbin Authors. All Rights Reserved.
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
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/beego/beego/context"
	"github.com/casvisor/casvisor/conf"
)

var (
	oldCasdoorEndpoint     = "https://door.casdoor.com"
	newCasdoorEndpoint     = conf.GetConfigString("casdoorEndpoint")
	oldClientId            = "b108dacba027db36ec26"
	newClientId            = conf.GetConfigString("clientId")
	oldCasdoorOrganization = "casbin"
	newCasdoorOrganization = conf.GetConfigString("casdoorOrganization")
	oldCasdoorApplication  = "app-casvisor"
	newCasdoorApplication  = conf.GetConfigString("casdoorApplication")
)

var errInvalidStaticPath = errors.New("invalid static path")

func TransparentStatic(ctx *context.Context) {
	if ctx == nil || ctx.Request == nil || ctx.Request.URL == nil || ctx.ResponseWriter == nil {
		return
	}

	urlPath := ctx.Request.URL.Path
	if strings.HasPrefix(urlPath, "/api/") {
		return
	}

	name, err := resolveStaticFile("web/build", urlPath)
	if err != nil {
		writeStaticFileError(ctx.ResponseWriter, ctx.Request, err)
		return
	}

	serveFileWithReplace(ctx.ResponseWriter, ctx.Request, name)
}

func resolveStaticFile(staticRoot, urlPath string) (string, error) {
	if strings.HasPrefix(urlPath, "//") || strings.ContainsAny(urlPath, "\x00\\") {
		return "", errInvalidStaticPath
	}

	path := filepath.FromSlash(strings.TrimPrefix(urlPath, "/"))
	// filepath.VolumeName does not recognize Windows drive letters on Unix.
	drivePrefix := len(path) >= 2 && path[1] == ':' &&
		(path[0] >= 'A' && path[0] <= 'Z' || path[0] >= 'a' && path[0] <= 'z')
	if filepath.VolumeName(path) != "" || drivePrefix {
		return "", errInvalidStaticPath
	}

	absRoot, err := filepath.Abs(staticRoot)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(absRoot, path)
	if !isWithinStaticRoot(absRoot, candidate) {
		return "", errInvalidStaticPath
	}

	info, err := os.Stat(candidate)
	if errors.Is(err, os.ErrNotExist) || (err == nil && info.IsDir()) {
		candidate = filepath.Join(absRoot, "index.html")
		info, err = os.Stat(candidate)
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errInvalidStaticPath
	}

	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", err
	}
	realCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	if !isWithinStaticRoot(realRoot, realCandidate) {
		return "", errInvalidStaticPath
	}
	return realCandidate, nil
}

func isWithinStaticRoot(root, name string) bool {
	rel, err := filepath.Rel(root, name)
	// IsLocal also rules out Windows device names and alternate streams.
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(rel) && filepath.IsLocal(rel)
}

func writeStaticFileError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errInvalidStaticPath):
		http.Error(w, "Forbidden", http.StatusForbidden)
	case errors.Is(err, os.ErrNotExist):
		http.NotFound(w, r)
	default:
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func serveFileWithReplace(w http.ResponseWriter, r *http.Request, name string) {
	f, err := os.Open(name)
	if err != nil {
		writeStaticFileError(w, r, err)
		return
	}
	defer f.Close()

	d, err := f.Stat()
	if err != nil {
		writeStaticFileError(w, r, err)
		return
	}
	if !d.Mode().IsRegular() {
		writeStaticFileError(w, r, errInvalidStaticPath)
		return
	}

	data, err := io.ReadAll(f)
	if err != nil {
		writeStaticFileError(w, r, err)
		return
	}
	content := string(data)
	if oldCasdoorEndpoint != newCasdoorEndpoint {
		content = strings.ReplaceAll(content, fmt.Sprintf("\"%s\"", oldCasdoorEndpoint), fmt.Sprintf("\"%s\"", newCasdoorEndpoint))
	}
	if oldClientId != newClientId {
		content = strings.ReplaceAll(content, fmt.Sprintf("\"%s\"", oldClientId), fmt.Sprintf("\"%s\"", newClientId))
	}
	if oldCasdoorOrganization != newCasdoorOrganization {
		content = strings.ReplaceAll(content, fmt.Sprintf("\"%s\"", oldCasdoorOrganization), fmt.Sprintf("\"%s\"", newCasdoorOrganization))
	}
	if oldCasdoorApplication != newCasdoorApplication {
		content = strings.ReplaceAll(content, fmt.Sprintf("\"%s\"", oldCasdoorApplication), fmt.Sprintf("\"%s\"", newCasdoorApplication))
	}

	http.ServeContent(w, r, d.Name(), d.ModTime(), strings.NewReader(content))
}
