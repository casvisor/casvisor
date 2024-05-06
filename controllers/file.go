// Copyright 2024 The casbin Authors. All Rights Reserved.
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
	"bytes"
	"fmt"
	"io"
	"path"

	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/storage"
	"github.com/casvisor/casvisor/util"
)

// UpdateFile
// @Title UpdateFile
// @Tag File API
// @Description update file
// @Param sessionId query string true "The id of the session"
// @Param filename query string true "The name of the file"
// @Success 200 {object} controllers.Response The Response object
// @router /update-file [post]
func (c *ApiController) UpdateFile() {
	c.ResponseOk()
}

// UploadFile
// @Title UploadFile
// @Tag File API
// @Description add file
// @Param key query string true "The directory of the file"
// @Param sessionId query string true "The id of the session"
// @Success 200 {object} controllers.Response The Response object
// @router /upload-file [post]
func (c *ApiController) UploadFile() {
	userName := c.GetSessionUsername()
	sessionId := c.Input().Get("id")
	key := c.Input().Get("key")
	_, file, err := c.GetFile("file")
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	srcFile, err := file.Open()
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	session, err := object.GetConnSession(sessionId)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if session == nil {
		c.ResponseError("session not found")
		return
	}

	provider, err := storage.GetStorageProvider(session.Protocol, sessionId, "")
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	var fileBuffer *bytes.Buffer
	fileBuffer = bytes.NewBuffer(nil)
	_, err = io.Copy(fileBuffer, srcFile)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	_, err = provider.PutObject(userName, key, file.Filename, fileBuffer)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	addRecordForFile(c, userName, "Upload", sessionId, key, file.Filename, false)
	c.ResponseOk()
}

// DownloadFile
// @Title DownloadFile
// @Tag File API
// @Description download file
// @Param sessionId query string true "The id of the session"
// @Param key query string true "The direction of the file"
// @Success 200 {object} controllers.Response The Response object
// @router /download-file [get]
func (c *ApiController) DownloadFile() {
	userName := c.GetSessionUsername()
	sessionId := c.Input().Get("id")
	key := c.Input().Get("key")

	session, err := object.GetConnSession(sessionId)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if session == nil {
		c.ResponseError("session not found")
		return
	}

	sftpClient, err := storage.GetSftpClient(sessionId)
	dstFile, err := sftpClient.Open(key)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	defer dstFile.Close()
	var buff bytes.Buffer
	if _, err := dstFile.WriteTo(&buff); err != nil {
		c.ResponseError(err.Error())
		return
	}
	addRecordForFile(c, userName, "Download", sessionId, key, "", false)
	filenameWithSuffix := path.Base(key)
	c.Ctx.ResponseWriter.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filenameWithSuffix))
	c.Ctx.ResponseWriter.Header().Set("Content-Type", "application/octet-stream")
	c.Ctx.ResponseWriter.Write(buff.Bytes())
}

// DeleteFile
// @Title DeleteFile
// @Tag File API
// @Description delete file
// @Param sessionId query string true "The id of the session"
// @Param key query string true "The direction of the file"
// @Success 200 {object} controllers.Response The Response object
// @router /delete-file [post]
func (c *ApiController) DeleteFile() {
	userName := c.GetSessionUsername()
	sessionId := c.Input().Get("id")
	key := c.Input().Get("key")

	session, err := object.GetConnSession(sessionId)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if session == nil {
		c.ResponseError("session not found")
		return
	}

	provider, err := storage.GetStorageProvider(session.Protocol, sessionId, "")
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	err = provider.DeleteObject(key)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	addRecordForFile(c, userName, "Delete", sessionId, key, "", false)
	c.ResponseOk()
}

// LsFiles
// @Title LsFiles
// @Tag File API
// @Description list files
// @Param sessionId query string true "The id of the session"
// @Param key query string true "The direction of the file"
// @Success 200 {object} controllers.Response The Response object
// @router /ls-files [get]
func (c *ApiController) LsFiles() {
	userName := c.GetSessionUsername()
	sessionId := c.Input().Get("id")
	key := c.Input().Get("key")
	mode := c.Input().Get("mode")

	session, err := object.GetConnSession(sessionId)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if session == nil {
		c.ResponseError("session not found")
		return
	}

	provider, err := storage.GetStorageProvider(session.Protocol, sessionId, "")
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	files, err := provider.ListObjects(key)

	if mode == "store" {
		store := object.Store{
			Owner:       session.Owner,
			Name:        session.Name,
			DisplayName: session.Asset,
			CreatedTime: util.GetCurrentTime(),
		}

		host := c.Ctx.Request.Host
		origin := getOriginFromHost(host)
		err := store.Populate(origin, files)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}
		addRecordForFile(c, userName, "Ls", sessionId, key, "", false)
		c.ResponseOk(store)
	} else {
		addRecordForFile(c, userName, "Ls", sessionId, key, "", false)
		c.ResponseOk(files)
	}
}

// MkdirFile
// @Title MkdirFile
// @Tag File API
// @Description make directory
// @Param key query string true "The direction of the file"
// @Success 200 {object} controllers.Response The Response object
// @router /mkdir-file [post]
func (c *ApiController) MkdirFile() {
	userName := c.GetSessionUsername()
	sessionId := c.Input().Get("id")
	key := c.Input().Get("key")

	session, err := object.GetConnSession(sessionId)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if session == nil {
		c.ResponseError("session not found")
		return
	}

	provider, err := storage.GetStorageProvider(session.Protocol, sessionId, "")
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	err = provider.Mkdir(key)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	addRecordForFile(c, userName, "MkDir", sessionId, key, "", false)
	c.ResponseOk()
}
