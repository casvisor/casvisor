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
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/beego/beego/utils/pagination"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
)

const maxBackupUploadSize = 64 << 20

// GetAccountBackups
// @Title GetAccountBackups
// @Tag Account API
// @Description get the account backups, by page, with their passwords masked
// @Param   owner     query    string  true        "The owner of the account backups"
// @Success 200 {array} object.AccountBackup The Response object
// @router /get-account-backups [get]
func (c *ApiController) GetAccountBackups() {
	owner := c.Input().Get("owner")
	if !c.checkOwner(owner) {
		return
	}
	limit := util.ParseInt(c.Input().Get("pageSize"))
	if limit <= 0 {
		limit = 100
	}
	field, value := c.Input().Get("field"), c.Input().Get("value")

	count, err := object.GetAccountBackupCount(owner, field, value)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	paginator := pagination.SetPaginator(c.Ctx, limit, count)
	backups, err := object.GetPaginationAccountBackups(owner, paginator.Offset(), limit, field, value, c.Input().Get("sortField"), c.Input().Get("sortOrder"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(object.GetMaskedAccountBackups(backups), paginator.Nums())
}

// GetAccountBackup
// @Title GetAccountBackup
// @Tag Account API
// @Description get an account backup, with its password masked
// @Param   id     query    string  true        "The id ( owner/name ) of the account backup"
// @Success 200 {object} object.AccountBackup The Response object
// @router /get-account-backup [get]
func (c *ApiController) GetAccountBackup() {
	id := c.Input().Get("id")
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
	if !c.checkOwner(owner) {
		return
	}

	backup, err := object.GetAccountBackup(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(object.GetMaskedAccountBackups([]*object.AccountBackup{backup})[0])
}

// UpdateAccountBackup
// @Title UpdateAccountBackup
// @Tag Account API
// @Description update an account backup; a masked password keeps the saved one
// @Param   id     query    string  true        "The id ( owner/name ) of the account backup"
// @Param   body    body   object.AccountBackup  true        "The details of the account backup"
// @Success 200 {object} controllers.Response The Response object
// @router /update-account-backup [post]
func (c *ApiController) UpdateAccountBackup() {
	var backup object.AccountBackup
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &backup)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	id := c.Input().Get("id")
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
	if !c.checkOwner(owner) || !c.checkOwner(backup.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.UpdateAccountBackup(id, &backup))
	c.ServeJSON()
}

// AddAccountBackup
// @Title AddAccountBackup
// @Tag Account API
// @Description add an account backup
// @Param   body    body   object.AccountBackup  true        "The details of the account backup"
// @Success 200 {object} controllers.Response The Response object
// @router /add-account-backup [post]
func (c *ApiController) AddAccountBackup() {
	var backup object.AccountBackup
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &backup)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(backup.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.AddAccountBackup(&backup))
	c.ServeJSON()
}

// DeleteAccountBackup
// @Title DeleteAccountBackup
// @Tag Account API
// @Description delete an account backup
// @Param   body    body   object.AccountBackup  true        "The account backup"
// @Success 200 {object} controllers.Response The Response object
// @router /delete-account-backup [post]
func (c *ApiController) DeleteAccountBackup() {
	var backup object.AccountBackup
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &backup)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(backup.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.DeleteAccountBackup(&backup))
	c.ServeJSON()
}

// RunAccountBackup
// @Title RunAccountBackup
// @Tag Account API
// @Description make the backup now and email it with its webhooks of the Email type
// @Param   id     query    string  true        "The id ( owner/name ) of the account backup"
// @Success 200 {object} controllers.Response The Response object
// @router /run-account-backup [post]
func (c *ApiController) RunAccountBackup() {
	id := c.Input().Get("id")
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
	if !c.checkOwner(owner) {
		return
	}

	err := object.RunAccountBackup(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk()
}

// DownloadAccountBackup
// @Title DownloadAccountBackup
// @Tag Account API
// @Description download the backup made now: a zip file encrypted with AES-256 and the password of the backup
// @Param   id     query    string  true        "The id ( owner/name ) of the account backup"
// @Success 200 {file} file The zip file
// @router /download-account-backup [post]
func (c *ApiController) DownloadAccountBackup() {
	id := c.Input().Get("id")
	owner, _ := util.GetOwnerAndNameFromIdNoCheck(id)
	if !c.checkOwner(owner) {
		return
	}

	data, fileName, count, err := object.GetAccountBackupFile(id)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.EnableRender = false
	c.Ctx.Output.Header("Content-Type", "application/zip")
	c.Ctx.Output.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
	c.Ctx.Output.Header("Content-Length", strconv.Itoa(len(data)))
	c.Ctx.Output.Header("X-Account-Count", strconv.Itoa(count))
	c.Ctx.Output.Header("Cache-Control", "no-store")
	c.Ctx.ResponseWriter.WriteHeader(200)
	_, _ = c.Ctx.ResponseWriter.Write(data)
}

// RestoreAccountBackup
// @Title RestoreAccountBackup
// @Tag Account API
// @Description restore the assets and the accounts of a backup, from its zip file in the form field "file"
// @Param   owner     formData    string  true        "The organization to restore into"
// @Param   password  formData    string  true        "The password of the zip file"
// @Param   overwrite formData    bool    false       "Whether to update the existing assets and accounts as they are in the backup"
// @Param   dryRun    formData    bool    false       "Whether to only tell what the restore would do"
// @Success 200 {object} object.AccountRestoreReport The Response object
// @router /restore-account-backup [post]
func (c *ApiController) RestoreAccountBackup() {
	owner := c.GetString("owner")
	if !c.checkOwner(owner) {
		return
	}

	file, header, err := c.GetFile("file")
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	defer file.Close()
	if header.Size > maxBackupUploadSize {
		c.ResponseError("the file is larger than 64 MB")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBackupUploadSize+1))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	report, err := object.RestoreAccountBackup(owner, data, c.GetString("password"), c.GetString("overwrite") == "true", c.GetString("dryRun") == "true")
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(report)
}
