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
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// KubernetesRequest is what a request to the API server does, read from its path the way the
// API server reads it for its own authorization.
type KubernetesRequest struct {
	IsResource  bool
	Verb        string
	Path        string
	Group       string
	Namespace   string
	Resource    string
	Name        string
	Subresource string
}

var kubernetesReadVerbs = []string{"get", "list", "watch"}

// subresources that reach into a running container, which are always created, even when
// they are opened with a GET that upgrades to a WebSocket
var kubernetesConnectSubresources = []string{"exec", "attach", "portforward"}

func ParseKubernetesRequest(method string, path string, query url.Values) *KubernetesRequest {
	req := &KubernetesRequest{Path: path, Verb: strings.ToLower(method)}
	if method == http.MethodHead {
		req.Verb = "get"
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case len(parts) >= 3 && parts[0] == "api":
		parts = parts[2:]
	case len(parts) >= 4 && parts[0] == "apis":
		req.Group = parts[1]
		parts = parts[3:]
	default:
		return req
	}
	req.IsResource = true

	isWatch := false
	if parts[0] == "watch" {
		isWatch = true
		parts = parts[1:]
	}
	// a namespace is in its own namespace, and so are its status and finalize
	if len(parts) >= 2 && parts[0] == "namespaces" {
		req.Namespace = parts[1]
		if len(parts) > 2 && parts[2] != "status" && parts[2] != "finalize" {
			parts = parts[2:]
		}
	}
	if len(parts) > 0 {
		req.Resource = parts[0]
	}
	if len(parts) > 1 {
		req.Name = parts[1]
	}
	if len(parts) > 2 {
		req.Subresource = strings.Join(parts[2:], "/")
	}
	if req.Resource == "" {
		req.IsResource = false
		return req
	}

	switch method {
	case http.MethodGet, http.MethodHead:
		value := strings.ToLower(query.Get("watch"))
		if isWatch || value == "true" || value == "1" {
			req.Verb = "watch"
		} else if req.Name == "" {
			req.Verb = "list"
		} else {
			req.Verb = "get"
		}
	case http.MethodPost:
		req.Verb = "create"
	case http.MethodPut:
		req.Verb = "update"
	case http.MethodPatch:
		req.Verb = "patch"
	case http.MethodDelete:
		if req.Name == "" {
			req.Verb = "deletecollection"
		} else {
			req.Verb = "delete"
		}
	}
	if slices.Contains(kubernetesConnectSubresources, req.Subresource) {
		req.Verb = "create"
	}
	return req
}

func (req *KubernetesRequest) IsReadOnly() bool {
	if !req.IsResource {
		return req.Verb == "get"
	}
	return slices.Contains(kubernetesReadVerbs, req.Verb)
}

// IsConnect tells whether the request opens a stream into a container: exec, attach or portforward.
func (req *KubernetesRequest) IsConnect() bool {
	return req.Resource == "pods" && slices.Contains(kubernetesConnectSubresources, req.Subresource)
}

// IsAllowed tells whether the scope lets the request through: changes need a namespace that may
// be changed, and what has no namespace, like nodes or a list across namespaces, needs "*".
// A namespace itself may be read in its own scope, but only changed with "*".
func (req *KubernetesRequest) IsAllowed(scope *KubernetesScope) bool {
	if !req.IsResource {
		return req.IsReadOnly() && len(scope.Namespaces) > 0
	}

	namespace := req.Namespace
	if namespace == "" || (req.Resource == "namespaces" && !req.IsReadOnly()) {
		namespace = "*"
	}
	if req.IsReadOnly() {
		return scope.CanRead(namespace)
	}
	return scope.CanWrite(namespace)
}

// GetText is how the request is matched against the command rules and listed in the commands
// of its session: the verb, the resource with its name and subresource, and the namespace,
// like "delete namespaces/dev" or "create pods/web/exec -n dev"; a path without a resource,
// like "get /version".
func (req *KubernetesRequest) GetText() string {
	if !req.IsResource {
		return req.Verb + " " + req.Path
	}

	text := req.Verb + " " + req.Resource
	if req.Group != "" {
		text += "." + req.Group
	}
	if req.Name != "" {
		text += "/" + req.Name
	}
	if req.Subresource != "" {
		text += "/" + req.Subresource
	}
	if req.Namespace != "" && req.Resource != "namespaces" {
		text += " -n " + req.Namespace
	}
	return text
}
