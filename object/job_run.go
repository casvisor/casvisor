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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/casvisor/casvisor/util"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"xorm.io/core"
)

const maxJobOutput = 1024 * 1024

var (
	jobRuns     = map[string]*jobRun{}
	jobRunsLock sync.Mutex

	unixNameRegex     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
	windowsDriveRegex = regexp.MustCompile(`^[A-Za-z]:`)
)

type jobRun struct {
	job      *Job
	user     *casdoorsdk.User
	clientIp string
	ctx      context.Context
	cancel   context.CancelFunc

	lock    sync.Mutex
	results map[string]*jobResultRun
}

type jobResultRun struct {
	result *JobResult

	lock      sync.Mutex
	out       []byte
	truncated bool
	dirty     bool
}

func getJobRun(jobId string) *jobRun {
	jobRunsLock.Lock()
	defer jobRunsLock.Unlock()
	return jobRuns[jobId]
}

// StartJob saves the job and runs it on its assets in the background.
func StartJob(job *Job, user *casdoorsdk.User, clientIp string) (*Job, error) {
	job.Name = util.GenerateId()
	job.CreatedTime = util.GetCurrentTime()
	job.User = util.GetIdFromOwnerAndName(user.Owner, user.Name)
	job.ClientIp = clientIp

	err := job.check()
	if err != nil {
		return nil, err
	}
	assets, err := job.getJobAssets()
	if err != nil {
		return nil, err
	}

	job.Status = JobRunning
	job.StartTime = job.CreatedTime
	job.Total = len(assets)
	if job.Assets == nil {
		job.Assets = []string{}
	}
	if job.AssetTags == nil {
		job.AssetTags = []string{}
	}

	results := []*JobResult{}
	for _, asset := range assets {
		results = append(results, &JobResult{
			Owner:       job.Owner,
			Job:         job.Name,
			Asset:       asset.Name,
			DisplayName: asset.DisplayName,
			Host:        asset.Host,
			Status:      JobPending,
		})
	}

	_, err = adapter.engine.Insert(job)
	if err != nil {
		return nil, err
	}
	for _, result := range results {
		_, err = adapter.engine.Insert(result)
		if err != nil {
			return nil, err
		}
	}

	run, err := newJobRun(job, results, user, clientIp)
	if err != nil {
		return nil, err
	}
	go run.run()
	return job, nil
}

// RerunJob runs a finished job again on the assets where it did not succeed.
func RerunJob(id string, user *casdoorsdk.User, clientIp string) (*Job, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	job, err := getJob(owner, name)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, fmt.Errorf("the job: %s does not exist", id)
	}
	if job.Type == JobTypeFile {
		filePath, err := GetJobFilePath(job.Owner, job.FileKey)
		if err != nil {
			return nil, err
		}
		if _, err = os.Stat(filePath); err != nil {
			return nil, fmt.Errorf("the uploaded file of the job is gone")
		}
	}

	results, err := getJobResults(owner, name)
	if err != nil {
		return nil, err
	}
	failed := []*JobResult{}
	for _, result := range results {
		if result.Status != JobSucceeded {
			failed = append(failed, result)
		}
	}
	if len(failed) == 0 {
		return nil, fmt.Errorf("the job has no failed assets to run again")
	}

	jobRunsLock.Lock()
	running := jobRuns[job.GetId()] != nil
	jobRunsLock.Unlock()
	if running {
		return nil, fmt.Errorf("the job: %s is still running", id)
	}

	for _, result := range failed {
		*result = JobResult{Owner: result.Owner, Job: result.Job, Asset: result.Asset, DisplayName: result.DisplayName, Host: result.Host, Status: JobPending}
		_, err = adapter.engine.ID(core.PK{result.Owner, result.Job, result.Asset}).AllCols().Update(result)
		if err != nil {
			return nil, err
		}
	}

	job.Status = JobRunning
	job.StartTime = util.GetCurrentTime()
	job.EndTime = ""
	job.Message = ""
	job.Failed = 0
	_, err = adapter.engine.ID(core.PK{job.Owner, job.Name}).Cols("status", "start_time", "end_time", "message", "failed").Update(job)
	if err != nil {
		return nil, err
	}

	run, err := newJobRun(job, failed, user, clientIp)
	if err != nil {
		return nil, err
	}
	go run.run()
	return job, nil
}

// CancelJob stops a running job: the assets it runs on are cut off and the ones it has not reached are skipped.
func CancelJob(id string) error {
	run := getJobRun(id)
	if run == nil {
		return fmt.Errorf("the job: %s is not running", id)
	}
	run.cancel()
	return nil
}

func newJobRun(job *Job, results []*JobResult, user *casdoorsdk.User, clientIp string) (*jobRun, error) {
	ctx, cancel := context.WithCancel(context.Background())
	run := &jobRun{job: job, user: user, clientIp: clientIp, ctx: ctx, cancel: cancel, results: map[string]*jobResultRun{}}
	for _, result := range results {
		run.results[result.Asset] = &jobResultRun{result: result}
	}

	jobRunsLock.Lock()
	defer jobRunsLock.Unlock()
	if jobRuns[job.GetId()] != nil {
		cancel()
		return nil, fmt.Errorf("the job: %s is already running", job.GetId())
	}
	jobRuns[job.GetId()] = run
	return run, nil
}

func (r *jobResultRun) Write(p []byte) (int, error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.out = append(r.out, p...)
	if len(r.out) > maxJobOutput {
		r.out = r.out[len(r.out)-maxJobOutput:]
		r.truncated = true
	}
	r.dirty = true
	return len(p), nil
}

func (r *jobResultRun) logf(format string, args ...interface{}) {
	r.lock.Lock()
	prefix := ""
	if len(r.out) > 0 && r.out[len(r.out)-1] != '\n' {
		prefix = "\n"
	}
	r.lock.Unlock()
	_, _ = r.Write([]byte(prefix + "[casvisor] " + fmt.Sprintf(format, args...) + "\n"))
}

func (r *jobResultRun) output() string {
	r.lock.Lock()
	defer r.lock.Unlock()
	output := strings.ToValidUTF8(string(r.out), "\uFFFD")
	if r.truncated {
		output = "...\n" + output
	}
	return output
}

func (run *jobRun) fillLive(results []*JobResult) {
	for _, result := range results {
		rr := run.results[result.Asset]
		if rr != nil && result.Status == JobRunning {
			result.Output = rr.output()
		}
	}
}

func (run *jobRun) flush() {
	for _, rr := range run.results {
		rr.lock.Lock()
		dirty := rr.dirty
		rr.dirty = false
		rr.lock.Unlock()
		if !dirty {
			continue
		}

		result := &JobResult{Output: rr.output()}
		_, err := adapter.engine.ID(core.PK{rr.result.Owner, rr.result.Job, rr.result.Asset}).Cols("output").Update(result)
		if err != nil {
			logs.Error("failed to save the output of the job: %s on the asset: %s: %s", run.job.Name, rr.result.Asset, err.Error())
		}
	}
}

func (run *jobRun) run() {
	defer func() {
		jobRunsLock.Lock()
		delete(jobRuns, run.job.GetId())
		jobRunsLock.Unlock()
	}()

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				run.flush()
			case <-done:
				return
			}
		}
	}()

	names := []string{}
	for name := range run.results {
		names = append(names, name)
	}
	sort.Strings(names)
	queue := make(chan *jobResultRun, len(names))
	for _, name := range names {
		queue <- run.results[name]
	}
	close(queue)

	var wg sync.WaitGroup
	for i := 0; i < min(run.job.Concurrency, len(run.results)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for rr := range queue {
				run.runOnAsset(rr)
			}
		}()
	}
	wg.Wait()
	close(done)

	run.finish()
}

func (run *jobRun) finish() {
	results, err := getJobResults(run.job.Owner, run.job.Name)
	if err != nil {
		logs.Error("failed to get the results of the job: %s: %s", run.job.Name, err.Error())
	}

	job := run.job
	job.Succeeded, job.Failed = 0, 0
	for _, result := range results {
		if result.Status == JobSucceeded {
			job.Succeeded++
		} else if result.Status != JobPending && result.Status != JobRunning {
			job.Failed++
		}
	}

	if run.ctx.Err() != nil {
		job.Status = JobCanceled
	} else if job.Failed > 0 {
		job.Status = JobFailed
	} else {
		job.Status = JobSucceeded
	}
	job.EndTime = util.GetCurrentTime()
	_, err = adapter.engine.ID(core.PK{job.Owner, job.Name}).Cols("status", "end_time", "succeeded", "failed").Update(job)
	if err != nil {
		logs.Error("failed to save the job: %s: %s", job.Name, err.Error())
	}
}

func (run *jobRun) saveResult(rr *jobResultRun, cols ...string) {
	_, err := adapter.engine.ID(core.PK{rr.result.Owner, rr.result.Job, rr.result.Asset}).Cols(cols...).Update(rr.result)
	if err != nil {
		logs.Error("failed to save the result of the job: %s on the asset: %s: %s", run.job.Name, rr.result.Asset, err.Error())
	}
}

func (run *jobRun) endResult(rr *jobResultRun, status string, err error) {
	result := rr.result
	result.Status = status
	if err != nil {
		result.Error = truncate(err.Error(), 1000)
		rr.logf("%s", err.Error())
	}
	result.EndTime = util.GetCurrentTime()
	result.Output = rr.output()
	run.saveResult(rr, "status", "exit_code", "error", "rule", "session", "start_time", "end_time", "output")
}

func (run *jobRun) runOnAsset(rr *jobResultRun) {
	result := rr.result
	if run.ctx.Err() != nil {
		run.endResult(rr, JobCanceled, nil)
		return
	}

	result.Status = JobRunning
	result.ExitCode = -1
	result.StartTime = util.GetCurrentTime()
	run.saveResult(rr, "status", "exit_code", "start_time")

	asset, err := GetAsset(util.GetIdFromOwnerAndName(run.job.Owner, result.Asset))
	if err == nil && asset == nil {
		err = fmt.Errorf("the asset: %s does not exist", result.Asset)
	}
	if err != nil {
		run.endResult(rr, JobFailed, err)
		return
	}

	session := &Session{
		Creator:       run.job.User,
		ClientIp:      run.clientIp,
		ClientIpDesc:  util.GetDescFromIP(run.clientIp),
		UserAgent:     "Casvisor job",
		UserAgentDesc: "Job: " + run.job.getLabel(),
	}
	_, err = CreateSession(session, asset.GetId(), "job", []string{})
	if err != nil {
		run.endResult(rr, JobFailed, err)
		return
	}
	sessionId := session.GetId()
	session.Status = Connected
	session.StartTime = util.GetCurrentTime()
	session.Reviewed = true
	_, err = UpdateSession(sessionId, session)
	if err != nil {
		run.endResult(rr, JobFailed, err)
		return
	}
	result.Session = sessionId
	run.saveResult(rr, "session")

	timeout := time.Duration(run.job.Timeout) * time.Second
	ctx, cancel := context.WithTimeout(run.ctx, timeout+5*time.Second)
	defer cancel()
	AddSessionCloser(sessionId, func(code int, msg string) { cancel() })
	defer RemoveSessionCloser(sessionId)

	status, err := run.execute(ctx, rr, asset, sessionId)
	if status == "" {
		status = JobSucceeded
		if err != nil {
			status = JobFailed
		}
	}
	if ctx.Err() != nil && status != JobSucceeded && status != JobBlocked {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = JobTimeout
			err = fmt.Errorf("timed out after %d seconds", run.job.Timeout)
		} else {
			status = JobCanceled
			err = fmt.Errorf("canceled")
		}
	}
	run.endResult(rr, status, err)

	message := "The job has ended"
	if status != JobSucceeded {
		message = fmt.Sprintf("The job has ended: %s", status)
	}
	err = CloseDBSession(sessionId, 0, message)
	if err != nil {
		logs.Error("failed to close the session of the job: %s: %s", sessionId, err.Error())
	}
}

func (job *Job) getLabel() string {
	if job.DisplayName != "" {
		return job.DisplayName
	}
	return job.Name
}

// getJobCommandText is what the job runs, as recorded in the commands.
func (job *Job) getJobCommandText() string {
	if job.Type == JobTypeFile {
		return fmt.Sprintf("sftp put %s %s", job.FileName, job.FilePath)
	}
	return job.Content
}

// matchJobRule checks each line of a command or script against the command rules, a Block rule before an Alert rule.
func matchJobRule(user *casdoorsdk.User, asset *Asset, text string) (*CommandRule, error) {
	var res *CommandRule
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		rule, err := MatchCommandRule(user, asset, line)
		if err != nil {
			return nil, err
		}
		if rule != nil && rule.Action == CommandActionBlock {
			return rule, nil
		}
		if res == nil {
			res = rule
		}
	}
	return res, nil
}

// execute runs the job on one asset and returns the status when it is not just success or failure.
func (run *jobRun) execute(ctx context.Context, rr *jobResultRun, asset *Asset, sessionId string) (string, error) {
	job := run.job
	result := rr.result
	text := job.getJobCommandText()

	command := &Command{
		Owner:    asset.Owner,
		Session:  sessionId,
		Asset:    asset.GetId(),
		User:     job.User,
		ClientIp: run.clientIp,
		Text:     text,
	}
	var rule *CommandRule
	var err error
	if job.Type != JobTypeFile {
		rule, err = matchJobRule(run.user, asset, text)
		if err != nil {
			return "", err
		}
	}
	if rule != nil {
		command.Action = rule.Action
		command.Rule = rule.GetId()
		result.Rule = rule.GetId()
	}
	err = AddCommand(command)
	if err != nil {
		return "", err
	}
	if rule != nil && rule.Action == CommandActionBlock {
		ruleName := rule.DisplayName
		if ruleName == "" {
			ruleName = rule.Name
		}
		return JobBlocked, fmt.Errorf("the command is blocked by the rule: %s", ruleName)
	}

	rr.logf("Connecting to %s (%s:%d)", asset.GetId(), asset.Host, asset.Port)
	client, err := DialSsh(asset)
	if err != nil {
		return "", err
	}
	defer client.Close()

	// the running command is signalled first and gets a moment to end before the connection is cut
	stop := context.AfterFunc(ctx, func() {
		time.Sleep(3 * time.Second)
		_ = client.Close()
	})
	defer stop()

	if job.Type == JobTypeFile {
		return "", run.sendFile(rr, client)
	}

	host, err := detectJobHost(client)
	if err != nil {
		return "", err
	}

	if !host.timeout {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(job.Timeout)*time.Second)
		defer cancel()
	}
	code, err := host.run(ctx, rr, job)
	result.ExitCode = code
	if errors.Is(err, context.DeadlineExceeded) {
		return JobTimeout, fmt.Errorf("timed out after %d seconds", job.Timeout)
	}
	if err != nil {
		return "", err
	}
	if host.unix && host.timeout && code == 124 {
		return JobTimeout, fmt.Errorf("timed out after %d seconds", job.Timeout)
	}
	if code != 0 {
		return JobFailed, fmt.Errorf("exited with code %d", code)
	}
	return JobSucceeded, nil
}

type jobHost struct {
	client *ssh.Client
	unix   bool
	// GNU timeout puts the command in its own process group and kills the whole group, children included
	timeout      bool
	inPowerShell bool
}

func detectJobHost(client *ssh.Client) (*jobHost, error) {
	var out bytes.Buffer
	_, err := runSshCommand(client, "uname -s; command -v timeout", "", &out, io.Discard)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(out.String(), "\r", "")), "\n")
	if unixNameRegex.MatchString(strings.TrimSpace(lines[0])) {
		return &jobHost{client: client, unix: true, timeout: len(lines) > 1 && strings.HasPrefix(lines[1], "/")}, nil
	}

	windows := newWindowsHost(client)
	return &jobHost{client: client, inPowerShell: windows.inPowerShell}, nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (h *jobHost) wrap(job *Job, command string) string {
	if !h.timeout {
		return command
	}
	return fmt.Sprintf("timeout -k 5 %d %s", job.Timeout, command)
}

func (h *jobHost) run(ctx context.Context, rr *jobResultRun, job *Job) (int, error) {
	if job.Type == JobTypeCommand {
		if h.unix {
			return h.runCommand(ctx, rr, h.wrap(job, "sh -c "+shellQuote(job.Content)), "")
		}
		return h.runCommand(ctx, rr, job.Content, "")
	}

	if job.ScriptType == JobScriptBash {
		if !h.unix {
			return -1, fmt.Errorf("Bash scripts run on Linux and Unix assets, pick PowerShell for a Windows asset")
		}
		return h.runCommand(ctx, rr, h.wrap(job, "bash -s"), job.Content)
	}

	scriptDir := "/tmp"
	if !h.unix {
		scriptDir = "/" + windowsScriptDir
	}
	script := "$ProgressPreference = 'SilentlyContinue'\n" + job.Content
	if !h.unix {
		script = "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8\n" + script
	}
	scriptPath, err := uploadJobScript(h.client, scriptDir, script)
	if err != nil {
		return -1, err
	}
	defer removeJobScript(h.client, scriptPath)

	if h.unix {
		return h.runCommand(ctx, rr, h.wrap(job, "pwsh -NoProfile -NonInteractive -File "+shellQuote(scriptPath)), "")
	}

	windowsPath := strings.TrimPrefix(scriptPath, "/")
	command := "powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -File " + strings.ReplaceAll(windowsPath, "/", "\\")
	if h.inPowerShell {
		command = fmt.Sprintf("& '%s'; if ($LASTEXITCODE) { exit $LASTEXITCODE } elseif (-not $?) { exit 1 }", windowsPath)
	}
	return h.runCommand(ctx, rr, command, "")
}

func (h *jobHost) runCommand(ctx context.Context, rr *jobResultRun, command string, stdin string) (int, error) {
	session, err := h.client.NewSession()
	if err != nil {
		return -1, err
	}
	defer session.Close()

	stop := context.AfterFunc(ctx, func() {
		_ = session.Signal(ssh.SIGTERM)
		time.Sleep(2 * time.Second)
		_ = session.Signal(ssh.SIGKILL)
	})
	defer stop()

	session.Stdin = strings.NewReader(stdin)
	session.Stdout = rr
	session.Stderr = rr
	err = session.Run(command)
	if exitErr, ok := err.(*ssh.ExitError); ok {
		return exitErr.ExitStatus(), nil
	}
	if _, ok := err.(*ssh.ExitMissingError); ok && ctx.Err() != nil {
		return -1, ctx.Err()
	}
	if err != nil {
		if ctx.Err() != nil {
			return -1, ctx.Err()
		}
		return -1, err
	}
	return 0, nil
}

func uploadJobScript(client *ssh.Client, dir string, script string) (string, error) {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return "", fmt.Errorf("failed to open SFTP to upload the script: %s", err.Error())
	}
	defer sftpClient.Close()

	err = sftpClient.MkdirAll(dir)
	if err != nil {
		return "", fmt.Errorf("failed to create %s: %s", dir, err.Error())
	}
	scriptPath := dir + "/casvisor-job-" + util.GenerateId() + ".ps1"
	file, err := sftpClient.Create(scriptPath)
	if err != nil {
		return "", fmt.Errorf("failed to upload the script: %s", err.Error())
	}
	// a BOM makes Windows PowerShell 5.1 read the script as UTF-8
	_, err = file.Write(append([]byte("\uFEFF"), []byte(script)...))
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = sftpClient.Remove(scriptPath)
		return "", fmt.Errorf("failed to upload the script: %s", err.Error())
	}
	return scriptPath, nil
}

func removeJobScript(client *ssh.Client, scriptPath string) {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return
	}
	defer sftpClient.Close()
	_ = sftpClient.Remove(scriptPath)
}

// getSftpPath turns a Windows path like C:\temp\a.txt into the /C:/temp/a.txt that the SFTP server of Windows OpenSSH takes.
func getSftpPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if windowsDriveRegex.MatchString(p) {
		p = "/" + p
	}
	return p
}

func (run *jobRun) sendFile(rr *jobResultRun, client *ssh.Client) error {
	job := run.job
	localPath, err := GetJobFilePath(job.Owner, job.FileKey)
	if err != nil {
		return err
	}
	src, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("the uploaded file of the job is gone")
	}
	defer src.Close()

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("failed to open SFTP: %s", err.Error())
	}
	defer sftpClient.Close()

	target := getSftpPath(strings.TrimSpace(job.FilePath))
	if strings.HasSuffix(target, "/") {
		target = path.Join(target, job.FileName)
	} else if info, err := sftpClient.Stat(target); err == nil && info.IsDir() {
		target = path.Join(target, job.FileName)
	}

	err = sftpClient.MkdirAll(path.Dir(target))
	if err != nil {
		return fmt.Errorf("failed to create %s: %s", path.Dir(target), err.Error())
	}

	rr.logf("Uploading %s (%d bytes) to %s", job.FileName, job.FileSize, target)
	dst, err := sftpClient.Create(target)
	if err != nil {
		return fmt.Errorf("failed to create %s: %s", target, err.Error())
	}
	written, err := io.Copy(dst, src)
	closeErr := dst.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("failed to write %s: %s", target, err.Error())
	}

	rr.logf("Uploaded %d bytes", written)
	return nil
}

// InitJobs marks the jobs left running by a previous run of Casvisor as interrupted,
// and removes the files uploaded a day ago or earlier for jobs that were never started.
func InitJobs() {
	removeUnusedJobFiles()

	jobs := []*Job{}
	err := adapter.engine.Where("status = ?", JobRunning).Find(&jobs)
	if err != nil {
		logs.Error("failed to get the running jobs: %s", err.Error())
		return
	}

	sessions := []*Session{}
	err = adapter.engine.Where("mode = ? and status = ?", "job", Connected).Find(&sessions)
	if err != nil {
		logs.Error("failed to get the sessions of the running jobs: %s", err.Error())
	}
	for _, session := range sessions {
		err = CloseDBSession(session.GetId(), 0, "Interrupted, Casvisor was restarted")
		if err != nil {
			logs.Error("failed to close the session: %s: %s", session.GetId(), err.Error())
		}
	}

	now := util.GetCurrentTime()
	for _, job := range jobs {
		results, err := getJobResults(job.Owner, job.Name)
		if err != nil {
			logs.Error("failed to get the results of the job: %s: %s", job.Name, err.Error())
			continue
		}

		job.Succeeded, job.Failed = 0, 0
		for _, result := range results {
			if result.Status == JobPending || result.Status == JobRunning {
				result.Status = JobInterrupted
				result.Error = "Interrupted, Casvisor was restarted"
				result.EndTime = now
				_, err = adapter.engine.ID(core.PK{result.Owner, result.Job, result.Asset}).Cols("status", "error", "end_time").Update(result)
				if err != nil {
					logs.Error("failed to save the result of the job: %s: %s", job.Name, err.Error())
				}
			}
			if result.Status == JobSucceeded {
				job.Succeeded++
			} else {
				job.Failed++
			}
		}

		job.Status = JobInterrupted
		job.Message = "Interrupted, Casvisor was restarted"
		job.EndTime = now
		_, err = adapter.engine.ID(core.PK{job.Owner, job.Name}).Cols("status", "message", "end_time", "succeeded", "failed").Update(job)
		if err != nil {
			logs.Error("failed to save the job: %s: %s", job.Name, err.Error())
		}
	}
}

func removeUnusedJobFiles() {
	root := filepath.Join("data", "jobs")
	owners, err := os.ReadDir(root)
	if err != nil {
		return
	}

	for _, owner := range owners {
		files, err := os.ReadDir(filepath.Join(root, owner.Name()))
		if err != nil {
			continue
		}
		for _, file := range files {
			info, err := file.Info()
			if err != nil || time.Since(info.ModTime()) < 24*time.Hour {
				continue
			}
			existed, err := adapter.engine.Exist(&Job{Owner: owner.Name(), FileKey: file.Name()})
			if err != nil || existed {
				continue
			}
			_ = os.Remove(filepath.Join(root, owner.Name(), file.Name()))
		}
	}
}
