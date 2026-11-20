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
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/beego/beego/utils/pagination"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/util"
)

const maxJobFileSize = 1024 * 1024 * 1024

type jobFile struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// GetJobs
// @Title GetJobs
// @Tag Job API
// @Description get the jobs, by page
// @Param   owner     query    string  true        "The owner of the jobs"
// @Param   pageSize  query    string  true        "The size of each page"
// @Param   p         query    string  true        "The number of the page"
// @Success 200 {object} object.Job The Response object
// @router /get-jobs [get]
func (c *ApiController) GetJobs() {
	owner, ok := c.resolveQueryOwner(c.Input().Get("owner"))
	if !ok {
		return
	}
	field := c.Input().Get("field")
	value := c.Input().Get("value")
	sortField := c.Input().Get("sortField")
	sortOrder := c.Input().Get("sortOrder")
	limit := util.ParseInt(c.Input().Get("pageSize"))
	if limit <= 0 {
		limit = 10
	}

	count, err := object.GetJobCount(owner, field, value)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	paginator := pagination.SetPaginator(c.Ctx, limit, count)
	jobs, err := object.GetPaginationJobs(owner, paginator.Offset(), limit, field, value, sortField, sortOrder)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk(jobs, paginator.Nums())
}

// GetJob
// @Title GetJob
// @Tag Job API
// @Description get a job with its result on each asset, the output live while it runs
// @Param   id     query    string  true        "The id ( owner/name ) of the job"
// @Success 200 {object} object.Job The Response object
// @router /get-job [get]
func (c *ApiController) GetJob() {
	owner, name, ok := c.resolveTenantId(c.Input().Get("id"))
	if !ok {
		return
	}

	job, err := object.GetJob(util.GetIdFromOwnerAndName(owner, name))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk(job)
}

// StartJob
// @Title StartJob
// @Tag Job API
// @Description run a command, a script or a file transfer on SSH assets, picked by name or by tag
// @Param   body    body   object.Job  true        "The owner, displayName, type (Command, Script or File), assets, assetTags, content, scriptType (Bash or PowerShell), fileKey, fileName and filePath, timeout in seconds and concurrency"
// @Success 200 {object} object.Job The Response object
// @router /start-job [post]
func (c *ApiController) StartJob() {
	var job object.Job
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &job)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	if !c.checkOwner(job.Owner) {
		return
	}

	res, err := object.StartJob(&job, c.GetSessionUser(), c.getClientIp())
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk(res)
}

// RerunJob
// @Title RerunJob
// @Tag Job API
// @Description run a finished job again on the assets where it did not succeed
// @Param   id     query    string  true        "The id ( owner/name ) of the job"
// @Success 200 {object} object.Job The Response object
// @router /rerun-job [post]
func (c *ApiController) RerunJob() {
	owner, name, ok := c.resolveTenantId(c.Input().Get("id"))
	if !ok {
		return
	}

	job, err := object.RerunJob(util.GetIdFromOwnerAndName(owner, name), c.GetSessionUser(), c.getClientIp())
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk(job)
}

// CancelJob
// @Title CancelJob
// @Tag Job API
// @Description stop a running job
// @Param   id     query    string  true        "The id ( owner/name ) of the job"
// @Success 200 {object} controllers.Response The Response object
// @router /cancel-job [post]
func (c *ApiController) CancelJob() {
	owner, name, ok := c.resolveTenantId(c.Input().Get("id"))
	if !ok {
		return
	}

	err := object.CancelJob(util.GetIdFromOwnerAndName(owner, name))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk()
}

// DeleteJob
// @Title DeleteJob
// @Tag Job API
// @Description delete a job that is not running, with its results and its uploaded file
// @Param   body    body   object.Job  true        "The owner and name of the job"
// @Success 200 {object} controllers.Response The Response object
// @router /delete-job [post]
func (c *ApiController) DeleteJob() {
	var job object.Job
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &job)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	if !c.checkOwner(job.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.DeleteJob(&job))
	c.ServeJSON()
}

// UploadJobFile
// @Title UploadJobFile
// @Tag Job API
// @Description upload the file a job sends to its assets, as the form field "file"; the key it returns goes in the fileKey of the job
// @Param   owner     query    string  true        "The owner of the job"
// @Success 200 {object} controllers.jobFile The Response object
// @router /upload-job-file [post]
func (c *ApiController) UploadJobFile() {
	owner := c.Input().Get("owner")
	if !c.checkOwner(owner) {
		return
	}

	file, header, err := c.GetFile("file")
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	defer file.Close()

	name := path.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
	if name == "." || name == ".." || name == "/" {
		c.ResponseError(fmt.Sprintf("invalid file name: %s", header.Filename))
		return
	}
	if header.Size > maxJobFileSize {
		c.ResponseError("the file is larger than 1 GB")
		return
	}

	key := util.GenerateId()
	filePath, err := object.GetJobFilePath(owner, key)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	err = os.MkdirAll(filepath.Dir(filePath), 0o700)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	dst, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	size, err := io.Copy(dst, file)
	closeErr := dst.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(filePath)
		c.ResponseError(err.Error())
		return
	}

	c.ResponseOk(jobFile{Key: key, Name: name, Size: size})
}
