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
	"fmt"
	"io"
	"mime/multipart"
	"net/url"
	"os"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/object"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// sessionSsh is the SSH connection of a web terminal session, which the file
// panel next to the terminal reuses through SFTP instead of signing in again.
type sessionSsh struct {
	client *ssh.Client

	mu   sync.Mutex
	sftp *sftp.Client
}

var sessionSshs sync.Map

func registerSessionSsh(sessionId string, client *ssh.Client) {
	sessionSshs.Store(sessionId, &sessionSsh{client: client})
}

func unregisterSessionSsh(sessionId string) {
	value, ok := sessionSshs.LoadAndDelete(sessionId)
	if !ok {
		return
	}

	s := value.(*sessionSsh)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sftp != nil {
		_ = s.sftp.Close()
	}
}

func (s *sessionSsh) getSftp() (*sftp.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sftp == nil {
		client, err := sftp.NewClient(s.client)
		if err != nil {
			return nil, err
		}
		s.sftp = client
	}
	return s.sftp, nil
}

type SessionFile struct {
	Name         string `json:"name"`
	Size         int64  `json:"size"`
	Mode         string `json:"mode"`
	IsDir        bool   `json:"isDir"`
	IsLink       bool   `json:"isLink"`
	ModifiedTime string `json:"modifiedTime"`
}

type SessionFiles struct {
	Path  string         `json:"path"`
	Files []*SessionFile `json:"files"`
}

type sessionFileContext struct {
	client  *sftp.Client
	user    *casdoorsdk.User
	session *object.Session
	asset   *object.Asset
}

// getSessionSftp checks that the session is the user's own, still connected, and
// that the user holds one of the given actions on its asset right now.
func (c *ApiController) getSessionSftp(actions ...string) (*sessionFileContext, error) {
	sessionId := c.Input().Get("id")
	session, err := object.GetConnSession(sessionId)
	if err != nil {
		return nil, err
	}

	user := c.GetSessionUser()
	if session == nil || user == nil || session.Creator != getUserId(user) {
		return nil, fmt.Errorf("you have no permission to use the session: %s", sessionId)
	}
	if session.Status != object.Connected {
		return nil, fmt.Errorf("the session: %s is not connected", sessionId)
	}

	value, ok := sessionSshs.Load(sessionId)
	if !ok {
		return nil, fmt.Errorf("files can only be transferred in SSH sessions")
	}

	asset, err := object.GetAsset(session.Asset)
	if err != nil {
		return nil, err
	}
	if asset == nil {
		return nil, fmt.Errorf("the asset: %s does not exist", session.Asset)
	}

	allowed, err := object.GetAssetActions(user, asset, c.getAuthzClientIp())
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(actions, func(action string) bool { return slices.Contains(allowed, action) }) {
		return nil, fmt.Errorf("you have no permission to %s files on the asset: %s", strings.ToLower(strings.Join(actions, " or ")), asset.Name)
	}

	client, err := value.(*sessionSsh).getSftp()
	if err != nil {
		return nil, err
	}

	return &sessionFileContext{client: client, user: user, session: session, asset: asset}, nil
}

func (c *ApiController) auditSessionFile(ctx *sessionFileContext, action string, detail string) {
	object.AddAuditRecord(&object.AuditEvent{
		Organization: ctx.asset.Owner,
		User:         ctx.user.Name,
		ClientIp:     c.getClientIp(),
		UserAgent:    c.getUserAgent(),
		Method:       "SFTP",
		Action:       action,
		Asset:        ctx.asset.GetId(),
		Detail:       detail,
	})
}

func getRemotePath(p string) (string, error) {
	if p == "" || !strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("the path: %s should be absolute", p)
	}
	return path.Clean(p), nil
}

// GetSessionFiles
// @Title GetSessionFiles
// @Tag Session File API
// @Description list a directory on the asset of an SSH session, the home directory when path is empty
// @Param   id      query    string  true        "The id of the session"
// @Param   path    query    string  false       "The absolute path of the directory"
// @Success 200 {object} controllers.SessionFiles The Response object
// @router /get-session-files [get]
func (c *ApiController) GetSessionFiles() {
	ctx, err := c.getSessionSftp(object.ActionUpload, object.ActionDownload)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	dir := c.Input().Get("path")
	if dir == "" {
		dir, err = ctx.client.Getwd()
	} else {
		dir, err = getRemotePath(dir)
	}
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	entries, err := ctx.client.ReadDir(dir)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	files := []*SessionFile{}
	for _, entry := range entries {
		isLink := entry.Mode()&os.ModeSymlink != 0
		isDir := entry.IsDir()
		if isLink {
			target, err := ctx.client.Stat(path.Join(dir, entry.Name()))
			if err == nil {
				isDir = target.IsDir()
			}
		}

		files = append(files, &SessionFile{
			Name:         entry.Name(),
			Size:         entry.Size(),
			Mode:         entry.Mode().String(),
			IsDir:        isDir,
			IsLink:       isLink,
			ModifiedTime: entry.ModTime().Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].IsDir != files[j].IsDir {
			return files[i].IsDir
		}
		return files[i].Name < files[j].Name
	})

	c.ResponseOk(SessionFiles{Path: dir, Files: files})
}

// DownloadSessionFile
// @Title DownloadSessionFile
// @Tag Session File API
// @Description download a file from the asset of an SSH session
// @Param   id      query    string  true        "The id of the session"
// @Param   path    query    string  true        "The absolute path of the file"
// @Success 200 {file} file The file
// @router /download-session-file [get]
func (c *ApiController) DownloadSessionFile() {
	ctx, err := c.getSessionSftp(object.ActionDownload)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	filePath, err := getRemotePath(c.Input().Get("path"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	file, err := ctx.client.Open(filePath)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if info.IsDir() {
		c.ResponseError(fmt.Sprintf("the path: %s is a directory", filePath))
		return
	}

	c.auditSessionFile(ctx, "sftp-download", filePath)

	c.EnableRender = false
	header := c.Ctx.ResponseWriter.Header()
	header.Set("Content-Type", "application/octet-stream")
	header.Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	header.Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(path.Base(filePath))))
	c.Ctx.ResponseWriter.WriteHeader(200)
	_, _ = io.Copy(c.Ctx.ResponseWriter, file)
}

// UploadSessionFile
// @Title UploadSessionFile
// @Tag Session File API
// @Description upload files (form field "files") into a directory on the asset of an SSH session
// @Param   id      query    string  true        "The id of the session"
// @Param   path    query    string  true        "The absolute path of the directory"
// @Success 200 {object} controllers.Response The Response object
// @router /upload-session-file [post]
func (c *ApiController) UploadSessionFile() {
	ctx, err := c.getSessionSftp(object.ActionUpload)
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

	for _, header := range headers {
		name := path.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
		if name == "." || name == ".." || name == "/" {
			c.ResponseError(fmt.Sprintf("invalid file name: %s", header.Filename))
			return
		}

		err = uploadSessionFile(ctx.client, header, path.Join(dir, name))
		if err != nil {
			c.ResponseError(err.Error())
			return
		}
		c.auditSessionFile(ctx, "sftp-upload", path.Join(dir, name))
	}

	c.ResponseOk()
}

func uploadSessionFile(client *sftp.Client, header *multipart.FileHeader, filePath string) error {
	src, err := header.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := client.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

// CreateSessionDir
// @Title CreateSessionDir
// @Tag Session File API
// @Description create a directory on the asset of an SSH session
// @Param   id      query    string  true        "The id of the session"
// @Param   path    query    string  true        "The absolute path of the new directory"
// @Success 200 {object} controllers.Response The Response object
// @router /create-session-dir [post]
func (c *ApiController) CreateSessionDir() {
	ctx, err := c.getSessionSftp(object.ActionUpload)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	dir, err := getRemotePath(c.Input().Get("path"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	err = ctx.client.Mkdir(dir)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk()
}

// RenameSessionFile
// @Title RenameSessionFile
// @Tag Session File API
// @Description rename or move a file or directory on the asset of an SSH session
// @Param   id       query    string  true        "The id of the session"
// @Param   path     query    string  true        "The absolute path of the file"
// @Param   newPath  query    string  true        "The new absolute path"
// @Success 200 {object} controllers.Response The Response object
// @router /rename-session-file [post]
func (c *ApiController) RenameSessionFile() {
	ctx, err := c.getSessionSftp(object.ActionUpload)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	oldPath, err := getRemotePath(c.Input().Get("path"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	newPath, err := getRemotePath(c.Input().Get("newPath"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	err = ctx.client.Rename(oldPath, newPath)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk()
}

// DeleteSessionFile
// @Title DeleteSessionFile
// @Tag Session File API
// @Description delete a file, or a directory with everything in it, on the asset of an SSH session
// @Param   id      query    string  true        "The id of the session"
// @Param   path    query    string  true        "The absolute path of the file or directory"
// @Success 200 {object} controllers.Response The Response object
// @router /delete-session-file [post]
func (c *ApiController) DeleteSessionFile() {
	ctx, err := c.getSessionSftp(object.ActionUpload)
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
		c.ResponseError("the root directory cannot be deleted")
		return
	}

	info, err := ctx.client.Lstat(filePath)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	if info.IsDir() {
		err = ctx.client.RemoveAll(filePath)
	} else {
		err = ctx.client.Remove(filePath)
	}
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk()
}
