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

import "testing"

func TestGetKubernetesUrl(t *testing.T) {
	tests := []struct {
		host string
		port int
		want string
	}{
		{"10.0.0.1", 6443, "https://10.0.0.1:6443"},
		{"10.0.0.1", 0, "https://10.0.0.1:6443"},
		{"fd00::1", 8443, "https://[fd00::1]:8443"},
		{"https://k8s.example.com:6443/", 22, "https://k8s.example.com:6443"},
		{"https://rancher.example.com/k8s/clusters/c-abc/", 6443, "https://rancher.example.com:443/k8s/clusters/c-abc"},
		{"http://10.0.0.1:8080", 6443, ""},
		{"https://user@10.0.0.1:6443", 6443, ""},
		{"https://10.0.0.1:6443?x=1", 6443, ""},
		{"", 6443, ""},
	}
	for _, test := range tests {
		u, err := getKubernetesUrl(test.host, test.port)
		if test.want == "" {
			if err == nil {
				t.Errorf("getKubernetesUrl(%q, %d) = %s, want an error", test.host, test.port, u)
			}
			continue
		}
		if err != nil || u.String() != test.want {
			t.Errorf("getKubernetesUrl(%q, %d) = %v, %v, want %s", test.host, test.port, u, err, test.want)
		}
	}
}

func TestIsKubernetesName(t *testing.T) {
	for _, name := range []string{"default", "kube-system", "coredns-697968c856-kzwf5", "my.pod.1"} {
		if !IsKubernetesName(name) {
			t.Errorf("IsKubernetesName(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "..", "a/b", "../etc", "Upper", "-a", "a-", "a?b"} {
		if IsKubernetesName(name) {
			t.Errorf("IsKubernetesName(%q) = true, want false", name)
		}
	}
}
