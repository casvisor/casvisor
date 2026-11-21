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
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ProtocolKubernetes is the API server of a Kubernetes cluster, whose containers are opened
// with a shell. The password of the asset is a bearer token, like a service account token.
const ProtocolKubernetes = "Kubernetes"

const (
	KubernetesDefaultPort     = 6443
	KubernetesExecSubprotocol = "v4.channel.k8s.io"

	kubernetesRequestTimeout = 30 * time.Second
)

var kubernetesNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)

func IsKubernetesName(name string) bool {
	return kubernetesNamePattern.MatchString(name)
}

type KubernetesPod struct {
	Name        string   `json:"name"`
	Phase       string   `json:"phase"`
	Node        string   `json:"node"`
	Containers  []string `json:"containers"`
	CreatedTime string   `json:"createdTime"`
}

type KubernetesPods struct {
	Namespaces []string         `json:"namespaces"`
	Namespace  string           `json:"namespace"`
	Pods       []*KubernetesPod `json:"pods"`
}

// getKubernetesUrl reads the host of the asset, either an address like
// https://10.0.0.1:6443, which may have a path for API servers behind a proxy,
// or a host name whose port is the port of the asset.
func getKubernetesUrl(host string, port int) (*url.URL, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, fmt.Errorf("the API server address is empty")
	}
	if !strings.Contains(host, "://") {
		if port <= 0 {
			port = KubernetesDefaultPort
		}
		host = "https://" + net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port))
	}

	u, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("the API server address: %s is invalid: %s", host, err.Error())
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("the API server address: %s must start with https://", host)
	}
	if u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("the API server address: %s is invalid", host)
	}
	if u.Port() == "" {
		u.Host = net.JoinHostPort(u.Hostname(), "443")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u, nil
}

// normalizeKubernetes keeps the port of the asset the same as the port of its
// address, which the port forwarding of the SSH gateway checks.
func (asset *Asset) normalizeKubernetes() error {
	if asset.Protocol != ProtocolKubernetes {
		return nil
	}

	asset.Host = strings.TrimRight(strings.TrimSpace(asset.Host), "/")
	u, err := getKubernetesUrl(asset.Host, asset.Port)
	if err != nil {
		return err
	}
	asset.Port, _ = strconv.Atoi(u.Port())
	return nil
}

func getCertificateFingerprint(cert []byte) string {
	sum := sha256.Sum256(cert)
	return hex.EncodeToString(sum[:])
}

// KubernetesClient talks to the API server of an asset through its network gateway. The
// public key of the API server is pinned in the fingerprint of the asset at the first
// connection, as certificates of API servers are renewed often but keep their key.
type KubernetesClient struct {
	asset   *Asset
	baseUrl *url.URL
	token   string
	// noSave keeps the pinned fingerprint in the asset only, for testing an asset before it is saved
	noSave bool

	mu          sync.Mutex
	fingerprint string
}

func NewKubernetesClient(asset *Asset) (*KubernetesClient, error) {
	if asset.Protocol != ProtocolKubernetes {
		return nil, fmt.Errorf("the asset: %s is not a Kubernetes cluster", asset.GetId())
	}
	u, err := getKubernetesUrl(asset.Host, asset.Port)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(asset.Password)
	if token == "" {
		return nil, fmt.Errorf("the asset: %s has no token", asset.GetId())
	}
	return &KubernetesClient{asset: asset, baseUrl: u, token: token}, nil
}

func (c *KubernetesClient) dial(ctx context.Context, network string, address string) (net.Conn, error) {
	return DialAsset(c.asset)
}

func (c *KubernetesClient) getTlsConfig() *tls.Config {
	serverName := c.baseUrl.Hostname()
	if net.ParseIP(serverName) != nil {
		serverName = ""
	}
	return &tls.Config{
		ServerName: serverName,
		// the certificate is checked by its pinned public key instead
		InsecureSkipVerify: true,
		VerifyConnection:   c.verifyConnection,
	}
}

func (c *KubernetesClient) verifyConnection(state tls.ConnectionState) error {
	if len(state.PeerCertificates) == 0 {
		return fmt.Errorf("the API server of the asset: %s sent no certificate", c.asset.GetId())
	}

	fingerprint := getCertificateFingerprint(state.PeerCertificates[0].RawSubjectPublicKeyInfo)
	pinned := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(c.asset.Fingerprint), ":", ""))
	if pinned == "" {
		c.mu.Lock()
		c.fingerprint = fingerprint
		c.mu.Unlock()
		return nil
	}
	if pinned != fingerprint {
		return fmt.Errorf("the public key of the API server of the asset: %s has changed from %s to %s, clear the fingerprint of the asset if the change is expected", c.asset.GetId(), pinned, fingerprint)
	}
	return nil
}

// pinFingerprint saves the public key of the API server, once the token has been accepted.
func (c *KubernetesClient) pinFingerprint() error {
	c.mu.Lock()
	fingerprint := c.fingerprint
	c.fingerprint = ""
	c.mu.Unlock()

	if fingerprint == "" || c.asset.Fingerprint != "" {
		return nil
	}
	c.asset.Fingerprint = fingerprint
	if c.noSave {
		return nil
	}
	return UpdateAssetFingerprint(c.asset.GetId(), fingerprint)
}

func (c *KubernetesClient) getUrl(scheme string, path string, query url.Values) string {
	u := *c.baseUrl
	u.Scheme = scheme
	u.Path = c.baseUrl.Path + path
	u.RawQuery = query.Encode()
	return u.String()
}

type kubernetesStatus struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Reason  string `json:"reason"`
	Code    int    `json:"code"`
}

func getKubernetesError(statusCode int, body []byte) error {
	var status kubernetesStatus
	message := strings.TrimSpace(string(body))
	if json.Unmarshal(body, &status) == nil && status.Message != "" {
		message = status.Message
	}
	if len(message) > 500 {
		message = message[:500]
	}
	if statusCode == http.StatusUnauthorized {
		return fmt.Errorf("the API server rejected the token (401 Unauthorized)")
	}
	if message == "" {
		message = http.StatusText(statusCode)
	}
	return fmt.Errorf("the API server returned %d: %s", statusCode, message)
}

func (c *KubernetesClient) get(path string, v interface{}) (int, error) {
	client := &http.Client{
		Timeout: kubernetesRequestTimeout,
		Transport: &http.Transport{
			DialContext:       c.dial,
			TLSClientConfig:   c.getTlsConfig(),
			DisableKeepAlives: true,
		},
	}

	req, err := http.NewRequest(http.MethodGet, c.getUrl("https", path, url.Values{}), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024*1024))
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, getKubernetesError(resp.StatusCode, body)
	}

	err = c.pinFingerprint()
	if err != nil {
		return resp.StatusCode, err
	}
	return resp.StatusCode, json.Unmarshal(body, v)
}

// GetNamespaces returns the namespaces, with the status code of the API server.
func (c *KubernetesClient) GetNamespaces() ([]string, int, error) {
	var list struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	code, err := c.get("/api/v1/namespaces", &list)
	if err != nil {
		return nil, code, err
	}

	res := []string{}
	for _, item := range list.Items {
		res = append(res, item.Metadata.Name)
	}
	sort.Strings(res)
	return res, code, nil
}

func (c *KubernetesClient) GetPods(namespace string) ([]*KubernetesPod, error) {
	if !IsKubernetesName(namespace) {
		return nil, fmt.Errorf("the namespace: %s is invalid", namespace)
	}

	var list struct {
		Items []struct {
			Metadata struct {
				Name              string `json:"name"`
				CreationTimestamp string `json:"creationTimestamp"`
			} `json:"metadata"`
			Spec struct {
				NodeName   string `json:"nodeName"`
				Containers []struct {
					Name string `json:"name"`
				} `json:"containers"`
			} `json:"spec"`
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		} `json:"items"`
	}
	_, err := c.get(fmt.Sprintf("/api/v1/namespaces/%s/pods", namespace), &list)
	if err != nil {
		return nil, err
	}

	res := []*KubernetesPod{}
	for _, item := range list.Items {
		pod := &KubernetesPod{
			Name:        item.Metadata.Name,
			Phase:       item.Status.Phase,
			Node:        item.Spec.NodeName,
			Containers:  []string{},
			CreatedTime: item.Metadata.CreationTimestamp,
		}
		for _, container := range item.Spec.Containers {
			pod.Containers = append(pod.Containers, container.Name)
		}
		res = append(res, pod)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Name < res[j].Name })
	return res, nil
}

// GetKubernetesPods lists the namespaces the token can see and the pods of one of them. A
// token that may not list namespaces, like one bound to a single namespace by a Role, is
// shown the namespace of the asset, or "default".
func GetKubernetesPods(asset *Asset, namespace string) (*KubernetesPods, error) {
	client, err := NewKubernetesClient(asset)
	if err != nil {
		return nil, err
	}

	defaultNamespace := strings.TrimSpace(asset.Database)
	if defaultNamespace == "" {
		defaultNamespace = "default"
	}

	namespaces, code, err := client.GetNamespaces()
	if err != nil {
		if code != http.StatusForbidden {
			return nil, err
		}
		namespaces = []string{defaultNamespace}
		if namespace != "" && namespace != defaultNamespace {
			namespaces = append(namespaces, namespace)
		}
	}

	if namespace == "" {
		namespace = defaultNamespace
		found := false
		for _, name := range namespaces {
			found = found || name == namespace
		}
		if !found && len(namespaces) > 0 {
			namespace = namespaces[0]
		}
	}

	pods, err := client.GetPods(namespace)
	if err != nil {
		return nil, err
	}
	return &KubernetesPods{Namespaces: namespaces, Namespace: namespace, Pods: pods}, nil
}

// KubernetesExec is a command run in a container with a terminal, over the WebSocket
// protocol of the API server: each message starts with its channel, 0 for stdin,
// 1 for stdout, 2 for stderr, 3 for the status when the command ends and 4 to resize.
type KubernetesExec struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
}

const (
	KubernetesChannelStdin  = 0
	KubernetesChannelStdout = 1
	KubernetesChannelStderr = 2
	KubernetesChannelError  = 3
	KubernetesChannelResize = 4
)

func (c *KubernetesClient) Exec(namespace string, pod string, container string, command []string) (*KubernetesExec, error) {
	for _, name := range []string{namespace, pod, container} {
		if !IsKubernetesName(name) {
			return nil, fmt.Errorf("the name: %s is invalid", name)
		}
	}

	query := url.Values{}
	query.Set("container", container)
	for _, arg := range command {
		query.Add("command", arg)
	}
	for _, name := range []string{"stdin", "stdout", "stderr", "tty"} {
		query.Set(name, "true")
	}

	dialer := websocket.Dialer{
		NetDialContext:   c.dial,
		TLSClientConfig:  c.getTlsConfig(),
		HandshakeTimeout: kubernetesRequestTimeout,
		Subprotocols:     []string{KubernetesExecSubprotocol},
		ReadBufferSize:   32 * 1024,
		WriteBufferSize:  32 * 1024,
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.token)

	conn, resp, err := dialer.Dial(c.getUrl("wss", fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/exec", namespace, pod), query), header)
	if err != nil {
		if resp != nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusSwitchingProtocols {
				return nil, getKubernetesError(resp.StatusCode, body)
			}
		}
		return nil, err
	}
	if conn.Subprotocol() != KubernetesExecSubprotocol {
		_ = conn.Close()
		return nil, fmt.Errorf("the API server does not support the protocol: %s", KubernetesExecSubprotocol)
	}

	err = c.pinFingerprint()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &KubernetesExec{conn: conn}, nil
}

func (e *KubernetesExec) write(channel byte, p []byte) error {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	_ = e.conn.SetWriteDeadline(time.Now().Add(time.Minute))
	return e.conn.WriteMessage(websocket.BinaryMessage, append([]byte{channel}, p...))
}

func (e *KubernetesExec) Write(p []byte) error {
	return e.write(KubernetesChannelStdin, p)
}

func (e *KubernetesExec) Resize(cols int, rows int) error {
	data, _ := json.Marshal(map[string]int{"Width": cols, "Height": rows})
	return e.write(KubernetesChannelResize, data)
}

func (e *KubernetesExec) Ping() error {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	return e.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second))
}

// Read returns the next message with its channel, skipping the empty message that opens each channel.
func (e *KubernetesExec) Read() (byte, []byte, error) {
	for {
		messageType, data, err := e.conn.ReadMessage()
		if err != nil {
			return 0, nil, err
		}
		if messageType != websocket.BinaryMessage || len(data) <= 1 {
			continue
		}
		return data[0], data[1:], nil
	}
}

func (e *KubernetesExec) Close() error {
	return e.conn.Close()
}

// GetKubernetesExecError returns the error in the status a command ended with, nil when it succeeded.
func GetKubernetesExecError(data []byte) error {
	var status kubernetesStatus
	err := json.Unmarshal(data, &status)
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(data)))
	}
	if status.Status == "Success" {
		return nil
	}
	if status.Message == "" {
		status.Message = status.Reason
	}
	return fmt.Errorf("%s", status.Message)
}
