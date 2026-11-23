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

package controllers

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/kubeproxy"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
)

// KubernetesToken is a kubeconfig for kubectl, with the token it holds, shown once.
type KubernetesToken struct {
	Name       string `json:"name"`
	Kubeconfig string `json:"kubeconfig"`
	Server     string `json:"server"`
	ExpireTime string `json:"expireTime"`
}

var kubeconfigNamePattern = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func getKubeconfigName(name string) string {
	return "casvisor-" + strings.Trim(kubeconfigNamePattern.ReplaceAllString(name, "-"), "-")
}

func quoteYaml(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

// AddKubernetesToken
// @Title AddKubernetesToken
// @Tag Session API
// @Description issue a kubeconfig that lets kubectl reach a Kubernetes asset through Casvisor for eight hours, with a token of Casvisor in it; every request is checked against the user's permission, its namespaces and the command rules, and recorded in a session of the token, which can be stopped to revoke it. The asset, or the account, has to keep its own token
// @Param   assetId    query    string  true     "The id of the asset"
// @Param   account    query    string  false    "The system account of the asset to connect as"
// @Success 200 {object} controllers.KubernetesToken The Response object
// @router /add-kubernetes-token [post]
func (c *ApiController) AddKubernetesToken() {
	if conf.GetConfigBool("isDemoMode") {
		c.ResponseError("kubectl is not available in demo mode")
		return
	}

	assetId := c.Input().Get("assetId")
	account := c.Input().Get("account")
	asset, _, scope, err := c.getKubernetesAsset(assetId, account, "", false)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if strings.TrimSpace(asset.Password) == "" {
		c.ResponseError(fmt.Sprintf("the asset: %s keeps no token for the account, which kubectl needs", asset.Name))
		return
	}

	user := c.GetSessionUser()
	err = object.CheckConnectMethod(user, asset, account, object.ConnectMethodKubectl)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	origin, err := object.GetAsset(assetId)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	token, value, err := object.AddKubernetesToken(getUserId(user), origin, account, c.getClientIp(), c.getUserAgent())
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	namespace := strings.TrimSpace(origin.Database)
	if names := scope.GetNames(); !scope.IsAll() && !slices.Contains(names, namespace) && len(names) > 0 {
		namespace = names[0]
	}
	if namespace == "" {
		namespace = "default"
	}

	server := kubeproxy.GetServerUrl(c.Ctx.Request, origin)
	cluster := getKubeconfigName(origin.Name)
	userName := getKubeconfigName(user.Name)
	kubeconfig := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
  - name: %s
    cluster:
      server: %s
users:
  - name: %s
    user:
      token: %s
contexts:
  - name: %s
    context:
      cluster: %s
      user: %s
      namespace: %s
current-context: %s
`, cluster, quoteYaml(server), userName, quoteYaml(value), cluster, cluster, userName, quoteYaml(namespace), cluster)

	c.ResponseOk(KubernetesToken{Name: token.GetId(), Kubeconfig: kubeconfig, Server: server, ExpireTime: token.ExpireTime})
}

// GetKubernetesTokens
// @Title GetKubernetesTokens
// @Tag Session API
// @Description list the kubeconfigs of a Kubernetes asset that are still valid: the user's own, and everyone's for administrators
// @Param   assetId    query    string  true     "The id of the asset"
// @Success 200 {array} object.KubernetesToken The Response object
// @router /get-kubernetes-tokens [get]
func (c *ApiController) GetKubernetesTokens() {
	user := c.GetSessionUser()
	if user == nil {
		c.ResponseError("please sign in first")
		return
	}

	assetId := c.Input().Get("assetId")
	userId := getUserId(user)
	owner, _ := util.GetOwnerAndNameFromId(assetId)
	if object.IsAdminUser(user) && (user.Owner == "built-in" || user.Owner == owner) {
		userId = ""
	}

	tokens, err := object.GetKubernetesTokens(assetId, userId)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(tokens)
}

// RevokeKubernetesToken
// @Title RevokeKubernetesToken
// @Tag Session API
// @Description revoke a kubeconfig, which ends its session, the requests in progress like watches, and the shells opened with it
// @Param   id    query    string  true     "The id of the kubeconfig"
// @Success 200 {object} controllers.Response The Response object
// @router /revoke-kubernetes-token [post]
func (c *ApiController) RevokeKubernetesToken() {
	user := c.GetSessionUser()
	if user == nil {
		c.ResponseError("please sign in first")
		return
	}

	token, err := object.GetKubernetesToken(c.Input().Get("id"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	isAdmin := object.IsAdminUser(user) && (user.Owner == "built-in" || user.Owner == token.Owner)
	if token == nil || (token.User != getUserId(user) && !isAdmin) {
		c.ResponseError("the kubeconfig does not exist")
		return
	}

	err = object.CloseSession(token.Session, ForcedDisconnect, fmt.Sprintf("The kubeconfig has been revoked by %s", getUserId(user)))
	if err == nil {
		err = object.RevokeKubernetesTokens(token.Session)
	}
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk()
}

// kubernetesContainer is a container named in a request of the web console, with the asset it is in.
type kubernetesContainer struct {
	asset     *object.Asset
	actions   []string
	scope     *object.KubernetesScope
	namespace string
	pod       string
	container string
}

func (k *kubernetesContainer) String() string {
	return fmt.Sprintf("%s/%s/%s", k.namespace, k.pod, k.container)
}

// getKubernetesContainer reads the asset and the container of a request; the token typed for an
// asset that keeps none is in the "password" of the JSON body, or of the form.
func (c *ApiController) getKubernetesContainer(password string) (*kubernetesContainer, error) {
	k := &kubernetesContainer{namespace: c.Input().Get("namespace"), pod: c.Input().Get("pod"), container: c.Input().Get("container")}
	for _, name := range []string{k.namespace, k.pod, k.container} {
		if !object.IsKubernetesName(name) {
			return nil, fmt.Errorf("the namespace, pod or container: %q is invalid", name)
		}
	}

	var err error
	k.asset, k.actions, k.scope, err = c.getKubernetesAsset(c.Input().Get("assetId"), c.Input().Get("account"), password, true)
	if err != nil {
		return nil, err
	}
	return k, nil
}

func (c *ApiController) getBodyPassword() (string, error) {
	var credential object.TunnelCredential
	if len(c.Ctx.Input.RequestBody) > 0 {
		err := json.Unmarshal(c.Ctx.Input.RequestBody, &credential)
		if err != nil {
			return "", err
		}
	}
	return credential.Password, nil
}

func (c *ApiController) auditKubernetesFile(k *kubernetesContainer, action string, detail string) {
	user := c.GetSessionUser()
	object.AddAuditRecord(&object.AuditEvent{
		Organization: k.asset.Owner,
		User:         user.Name,
		ClientIp:     c.getClientIp(),
		UserAgent:    c.getUserAgent(),
		Method:       "Kubernetes",
		Action:       action,
		Asset:        k.asset.GetId(),
		Detail:       fmt.Sprintf("%s:%s", k.String(), detail),
	})
}

// GetKubernetesLogs
// @Title GetKubernetesLogs
// @Tag Session API
// @Description stream the log of a container of a Kubernetes asset as plain text, with the Connect permission on a namespace the user may read; a log that is followed goes on until the request is cancelled or the permission is revoked; the token typed for an asset that keeps none goes in the body
// @Param   assetId      query    string  true     "The id of the asset"
// @Param   account      query    string  false    "The system account of the asset to connect as"
// @Param   namespace    query    string  true     "The namespace of the pod"
// @Param   pod          query    string  true     "The pod"
// @Param   container    query    string  true     "The container of the pod"
// @Param   tailLines    query    int     false    "The lines from the end to start with, all of them when empty or 0"
// @Param   follow       query    bool    false    "true to follow the log"
// @Param   timestamps   query    bool    false    "true to start each line with its time"
// @Param   previous     query    bool    false    "true for the log of the container's previous run"
// @Param   body         body     object.TunnelCredential  false  "The token in password, when the asset keeps none"
// @Success 200 {string} string The log
// @router /get-kubernetes-logs [post]
func (c *ApiController) GetKubernetesLogs() {
	password, err := c.getBodyPassword()
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	k, err := c.getKubernetesContainer(password)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !k.scope.CanRead(k.namespace) {
		c.ResponseError(fmt.Sprintf("you have no permission to the namespace: %s", k.namespace))
		return
	}

	tailLines, _ := strconv.Atoi(c.Input().Get("tailLines"))
	options := object.KubernetesLogOptions{
		TailLines:  tailLines,
		Follow:     c.Input().Get("follow") == "true",
		Timestamps: c.Input().Get("timestamps") == "true",
		Previous:   c.Input().Get("previous") == "true",
	}

	client, err := object.NewKubernetesClient(k.asset)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	ctx, cancel := context.WithCancel(c.Ctx.Request.Context())
	defer cancel()
	body, err := client.GetLogs(ctx, k.namespace, k.pod, k.container, options)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	defer body.Close()

	if options.Follow {
		user := c.GetSessionUser()
		account := c.Input().Get("account")
		clientIp := c.getAuthzClientIp()
		assetId := c.Input().Get("assetId")
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					asset, err := object.GetAsset(assetId)
					if err != nil {
						continue
					}
					_, scope, err := object.GetKubernetesAccess(user, asset, account, clientIp)
					if err == nil && !scope.CanRead(k.namespace) {
						cancel()
						return
					}
				}
			}
		}()
	}

	c.EnableRender = false
	header := c.Ctx.ResponseWriter.Header()
	header.Set("Content-Type", "text/plain; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	c.Ctx.ResponseWriter.WriteHeader(200)
	c.Ctx.ResponseWriter.Flush()

	buf := make([]byte, 32*1024)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			_, werr := c.Ctx.ResponseWriter.Write(buf[:n])
			if werr != nil {
				return
			}
			c.Ctx.ResponseWriter.Flush()
		}
		if err != nil {
			return
		}
	}
}

// kubernetesCommand is a command run in a container without a terminal, with its stdout as a
// stream, and its stderr and the status it ended with kept for the error.
type kubernetesCommand struct {
	exec   *object.KubernetesExec
	stdout *io.PipeReader
	done   chan struct{}

	mu     sync.Mutex
	stderr bytes.Buffer
	err    error
}

func runKubernetesCommand(k *kubernetesContainer, command []string, stdin bool) (*kubernetesCommand, error) {
	client, err := object.NewKubernetesClient(k.asset)
	if err != nil {
		return nil, err
	}
	exec, err := client.ExecWith(k.namespace, k.pod, k.container, command, object.KubernetesExecOptions{Stdin: stdin})
	if err != nil {
		return nil, err
	}

	reader, writer := io.Pipe()
	cmd := &kubernetesCommand{exec: exec, stdout: reader, done: make(chan struct{})}
	go func() {
		defer close(cmd.done)
		for {
			channel, data, err := exec.Read()
			if err != nil {
				_ = writer.CloseWithError(cmd.getError(fmt.Errorf("the command ended without a status: %s", err.Error())))
				return
			}
			switch channel {
			case object.KubernetesChannelStdout:
				_, _ = writer.Write(data)
			case object.KubernetesChannelStderr:
				cmd.mu.Lock()
				if cmd.stderr.Len() < 4096 {
					cmd.stderr.Write(data)
				}
				cmd.mu.Unlock()
			case object.KubernetesChannelError:
				err = object.GetKubernetesExecError(data)
				if err != nil {
					err = cmd.getError(err)
				}
				cmd.mu.Lock()
				cmd.err = err
				cmd.mu.Unlock()
				_ = writer.CloseWithError(err)
				return
			}
		}
	}()
	return cmd, nil
}

// getError tells why the command failed, by what it wrote to stderr when it wrote anything.
func (cmd *kubernetesCommand) getError(err error) error {
	cmd.mu.Lock()
	defer cmd.mu.Unlock()
	if message := strings.TrimSpace(cmd.stderr.String()); message != "" {
		return errors.New(message)
	}
	return err
}

// wait waits for the status of the command, nil when it succeeded.
func (cmd *kubernetesCommand) wait() error {
	select {
	case <-cmd.done:
	case <-time.After(time.Minute):
		return fmt.Errorf("the command did not end in a minute")
	}
	cmd.mu.Lock()
	defer cmd.mu.Unlock()
	return cmd.err
}

func (cmd *kubernetesCommand) Write(p []byte) (int, error) {
	err := cmd.exec.Write(p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (cmd *kubernetesCommand) Close() error {
	_ = cmd.stdout.Close()
	return cmd.exec.Close()
}

// checkKubernetesFiles checks that the user may transfer files with the action, which runs tar
// in the container, like kubectl cp, and so needs a namespace the user may change.
func checkKubernetesFiles(k *kubernetesContainer, action string) error {
	if !slices.Contains(k.actions, action) {
		return fmt.Errorf("you have no permission to %s files on the asset: %s", strings.ToLower(action), k.asset.Name)
	}
	if !k.scope.CanWrite(k.namespace) {
		return fmt.Errorf("you have no permission to run commands in the namespace: %s, which transferring files needs", k.namespace)
	}
	return nil
}

// DownloadKubernetesFile
// @Title DownloadKubernetesFile
// @Tag Session File API
// @Description download a file from a container of a Kubernetes asset, or a directory as a tar file, with the Download permission, the way kubectl cp does, by running tar in the container; the token typed for an asset that keeps none goes in the body
// @Param   assetId      query    string  true     "The id of the asset"
// @Param   account      query    string  false    "The system account of the asset to connect as"
// @Param   namespace    query    string  true     "The namespace of the pod"
// @Param   pod          query    string  true     "The pod"
// @Param   container    query    string  true     "The container of the pod"
// @Param   path         query    string  true     "The absolute path of the file or directory"
// @Param   body         body     object.TunnelCredential  false  "The token in password, when the asset keeps none"
// @Success 200 {file} file The file
// @router /download-kubernetes-file [post]
func (c *ApiController) DownloadKubernetesFile() {
	password, err := c.getBodyPassword()
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	k, err := c.getKubernetesContainer(password)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	err = checkKubernetesFiles(k, object.ActionDownload)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	filePath, err := getRemotePath(c.Input().Get("path"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if filePath == "/" {
		c.ResponseError("the root directory can't be downloaded")
		return
	}

	cmd, err := runKubernetesCommand(k, []string{"tar", "cf", "-", "-C", path.Dir(filePath), "./" + path.Base(filePath)}, false)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	defer cmd.Close()

	reader := tar.NewReader(cmd.stdout)
	first, err := reader.Next()
	if err != nil {
		// tar writes an empty archive for a path it can't read, and fails after it
		if errors.Is(err, io.EOF) {
			_, err = io.Copy(io.Discard, cmd.stdout)
			if err == nil {
				err = fmt.Errorf("the path: %s is empty", filePath)
			}
		}
		c.ResponseError(err.Error())
		return
	}

	name := path.Base(filePath)
	isDir := first.Typeflag == tar.TypeDir
	switch {
	case isDir:
		name += ".tar"
	case first.Typeflag == tar.TypeSymlink:
		c.ResponseError(fmt.Sprintf("the path: %s is a link to: %s", filePath, first.Linkname))
		return
	case first.Typeflag != tar.TypeReg:
		c.ResponseError(fmt.Sprintf("the path: %s is not a file or a directory", filePath))
		return
	}

	c.auditKubernetesFile(k, "kubernetes-download", filePath)

	c.EnableRender = false
	header := c.Ctx.ResponseWriter.Header()
	header.Set("Content-Type", "application/octet-stream")
	header.Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(name)))
	if !isDir {
		header.Set("Content-Length", strconv.FormatInt(first.Size, 10))
	}
	c.Ctx.ResponseWriter.WriteHeader(200)

	if !isDir {
		_, _ = io.Copy(c.Ctx.ResponseWriter, reader)
		return
	}

	// the directory goes as a tar of it, under its own name
	writer := tar.NewWriter(c.Ctx.ResponseWriter)
	for entry := first; ; {
		entry.Name = path.Clean(entry.Name)
		if entry.Typeflag == tar.TypeDir {
			entry.Name += "/"
		}
		if writer.WriteHeader(entry) != nil {
			return
		}
		if _, err = io.Copy(writer, reader); err != nil {
			return
		}
		entry, err = reader.Next()
		if err != nil {
			break
		}
	}
	_ = writer.Close()
}

// UploadKubernetesFile
// @Title UploadKubernetesFile
// @Tag Session File API
// @Description upload files (form field "files") into a directory of a container of a Kubernetes asset, with the Upload permission, the way kubectl cp does, by running tar in the container; the token typed for an asset that keeps none goes in the form field "password"
// @Param   assetId      query    string  true     "The id of the asset"
// @Param   account      query    string  false    "The system account of the asset to connect as"
// @Param   namespace    query    string  true     "The namespace of the pod"
// @Param   pod          query    string  true     "The pod"
// @Param   container    query    string  true     "The container of the pod"
// @Param   path         query    string  true     "The absolute path of the directory"
// @Success 200 {object} controllers.Response The Response object
// @router /upload-kubernetes-file [post]
func (c *ApiController) UploadKubernetesFile() {
	k, err := c.getKubernetesContainer(c.GetString("password"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	err = checkKubernetesFiles(k, object.ActionUpload)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	dir, err := getRemotePath(c.Input().Get("path"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	headers, err := c.GetFiles("files")
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	names := []string{}
	for _, header := range headers {
		name := path.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
		if name == "." || name == ".." || name == "/" {
			c.ResponseError(fmt.Sprintf("invalid file name: %s", header.Filename))
			return
		}
		names = append(names, name)
	}

	cmd, err := runKubernetesCommand(k, []string{"tar", "-xmf", "-", "-C", dir}, true)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	defer cmd.Close()

	err = func() error {
		buffered := bufio.NewWriterSize(cmd, 32*1024)
		writer := tar.NewWriter(buffered)
		for i, header := range headers {
			src, err := header.Open()
			if err != nil {
				return err
			}
			err = writer.WriteHeader(&tar.Header{Name: names[i], Mode: 0o644, Size: header.Size, ModTime: time.Now(), Typeflag: tar.TypeReg})
			if err == nil {
				_, err = io.Copy(writer, src)
			}
			_ = src.Close()
			if err != nil {
				return err
			}
		}
		err = writer.Close()
		if err == nil {
			err = buffered.Flush()
		}
		if err == nil {
			err = cmd.exec.CloseStdin()
		}
		return err
	}()
	if err == nil {
		err = cmd.wait()
	}
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	for _, name := range names {
		c.auditKubernetesFile(k, "kubernetes-upload", path.Join(dir, name))
	}
	c.ResponseOk()
}
