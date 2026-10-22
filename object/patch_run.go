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
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/util"
	"golang.org/x/crypto/ssh"
	"xorm.io/core"
)

const maxPatchJobOutput = 512 * 1024

var (
	patchRuns     = map[string]*patchRun{}
	patchRunsLock sync.Mutex
	patchSlots    = make(chan struct{}, 8)

	patchIdRegex = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+_:~-]*$`)
)

// patchHost runs the scan, install and reboot of one platform over an SSH connection.
type patchHost interface {
	scan(r *patchRun) (*PatchStatus, error)
	install(r *patchRun, ids []string) error
	reboot(r *patchRun) error
	bootId() (string, error)
	close()
}

type patchRun struct {
	asset *Asset
	job   *PatchJob

	lock  sync.Mutex
	out   strings.Builder
	dirty bool
}

func getPatchRun(assetId string) *patchRun {
	patchRunsLock.Lock()
	defer patchRunsLock.Unlock()
	return patchRuns[assetId]
}

// StartPatchJob starts a scan, install or reboot on an SSH asset in the background.
// For an install, empty ids means all the updates found.
func StartPatchJob(assetId string, jobType string, ids []string, user string) (*PatchJob, error) {
	asset, err := GetAsset(assetId)
	if err != nil {
		return nil, err
	}
	if asset == nil {
		return nil, fmt.Errorf("the asset: %s does not exist", assetId)
	}
	if asset.Protocol != "ssh" {
		return nil, fmt.Errorf("the asset: %s is not an SSH asset, updates are scanned and installed over SSH", assetId)
	}
	if jobType != PatchJobScan && jobType != PatchJobInstall && jobType != PatchJobReboot {
		return nil, fmt.Errorf("unknown job type: %s", jobType)
	}
	if ids == nil {
		ids = []string{}
	}
	for _, id := range ids {
		if !patchIdRegex.MatchString(id) {
			return nil, fmt.Errorf("invalid update: %s", id)
		}
	}

	job := &PatchJob{
		Owner:       asset.Owner,
		Name:        util.GenerateId(),
		CreatedTime: util.GetCurrentTime(),
		Asset:       asset.GetId(),
		Type:        jobType,
		Updates:     ids,
		User:        user,
		Status:      PatchJobRunning,
	}
	run := &patchRun{asset: asset, job: job}

	patchRunsLock.Lock()
	if existing := patchRuns[asset.GetId()]; existing != nil {
		patchRunsLock.Unlock()
		return nil, fmt.Errorf("the asset: %s is already running a %s job", asset.GetId(), strings.ToLower(existing.job.Type))
	}
	patchRuns[asset.GetId()] = run
	patchRunsLock.Unlock()

	_, err = adapter.engine.Insert(job)
	if err == nil {
		err = savePatchStatus(&PatchStatus{Owner: asset.Owner, Name: asset.Name, Job: job.Name, JobType: jobType}, "job", "job_type")
	}
	if err != nil {
		patchRunsLock.Lock()
		delete(patchRuns, asset.GetId())
		patchRunsLock.Unlock()
		return nil, err
	}

	go run.run()
	return job, nil
}

func (r *patchRun) Write(p []byte) (int, error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.out.Write(p)
	r.dirty = true
	return len(p), nil
}

func (r *patchRun) logf(format string, args ...interface{}) {
	line := fmt.Sprintf("[%s] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
	_, _ = r.Write([]byte(line))
}

func (r *patchRun) output() string {
	r.lock.Lock()
	defer r.lock.Unlock()
	output := r.out.String()
	if len(output) > maxPatchJobOutput {
		output = "...\n" + output[len(output)-maxPatchJobOutput:]
	}
	return output
}

func (r *patchRun) flush() {
	r.lock.Lock()
	dirty := r.dirty
	r.dirty = false
	r.lock.Unlock()
	if !dirty {
		return
	}

	r.job.Output = r.output()
	_, err := adapter.engine.ID(core.PK{r.job.Owner, r.job.Name}).Cols("output").Update(r.job)
	if err != nil {
		logs.Error("failed to save the output of the patch job: %s: %s", r.job.Name, err.Error())
	}
}

func (r *patchRun) run() {
	patchSlots <- struct{}{}
	defer func() { <-patchSlots }()

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				r.flush()
			case <-done:
				return
			}
		}
	}()

	err := r.execute()
	close(done)

	r.job.Status = PatchJobSucceeded
	if err != nil {
		r.job.Status = PatchJobFailed
		r.job.Message = truncate(err.Error(), 1000)
		r.logf("Failed: %s", err.Error())
	} else {
		r.logf("Done")
	}
	r.job.EndTime = util.GetCurrentTime()
	r.job.Output = r.output()
	_, dbErr := adapter.engine.ID(core.PK{r.job.Owner, r.job.Name}).Cols("status", "message", "end_time", "output").Update(r.job)
	if dbErr != nil {
		logs.Error("failed to save the patch job: %s: %s", r.job.Name, dbErr.Error())
	}

	status := &PatchStatus{Owner: r.asset.Owner, Name: r.asset.Name}
	cols := []string{"job", "job_type"}
	if err != nil && r.job.Type == PatchJobScan {
		status.Message = truncate(err.Error(), 1000)
		cols = append(cols, "message")
	}
	dbErr = savePatchStatus(status, cols...)
	if dbErr != nil {
		logs.Error("failed to save the patch status of the asset: %s: %s", r.asset.GetId(), dbErr.Error())
	}

	patchRunsLock.Lock()
	delete(patchRuns, r.asset.GetId())
	patchRunsLock.Unlock()
}

func (r *patchRun) execute() error {
	r.logf("Connecting to %s (%s:%d)", r.asset.GetId(), r.asset.Host, r.asset.Port)
	host, err := connectPatchHost(r.asset)
	if err != nil {
		return err
	}
	defer func() { host.close() }()

	switch r.job.Type {
	case PatchJobInstall:
		if len(r.job.Updates) == 0 {
			r.logf("Installing all available updates")
		} else {
			r.logf("Installing %d selected updates", len(r.job.Updates))
		}
		err = host.install(r, r.job.Updates)
		if err != nil {
			return err
		}
	case PatchJobReboot:
		host, err = r.reboot(host)
		if err != nil {
			return err
		}
	}

	r.logf("Scanning for updates")
	status, err := host.scan(r)
	if err != nil {
		return err
	}
	return r.saveScan(status)
}

func (r *patchRun) saveScan(status *PatchStatus) error {
	status.Owner = r.asset.Owner
	status.Name = r.asset.Name
	status.ScannedTime = util.GetCurrentTime()
	status.Message = ""
	status.UpdateCount = len(status.Updates)
	status.SecurityCount = 0
	for _, update := range status.Updates {
		if update.IsSecurity {
			status.SecurityCount++
		}
	}

	r.logf("%s, %d updates (%d security), restart required: %t", status.Os, status.UpdateCount, status.SecurityCount, status.RebootRequired)
	return savePatchStatus(status, "platform", "os", "manager", "updates", "update_count", "security_count", "reboot_required", "scanned_time", "message")
}

// reboot restarts the asset and waits until it is back with a new boot, then returns a new connection.
func (r *patchRun) reboot(host patchHost) (patchHost, error) {
	before, err := host.bootId()
	if err != nil {
		return host, err
	}

	r.logf("Restarting the asset")
	err = host.reboot(r)
	if err != nil {
		return host, err
	}
	host.close()

	r.logf("Waiting for the asset to come back")
	time.Sleep(20 * time.Second)
	deadline := time.Now().Add(30 * time.Minute)
	var lastErr error
	for time.Now().Before(deadline) {
		newHost, err := connectPatchHost(r.asset)
		if err == nil {
			var after string
			after, err = newHost.bootId()
			if err == nil && after != before {
				r.logf("The asset is back")
				return newHost, nil
			}
			newHost.close()
		}
		lastErr = err
		time.Sleep(10 * time.Second)
	}

	if lastErr != nil {
		return nil, fmt.Errorf("the asset did not come back in 30 minutes: %s", lastErr.Error())
	}
	return nil, fmt.Errorf("the asset did not restart in 30 minutes")
}

func connectPatchHost(asset *Asset) (patchHost, error) {
	client, err := DialSsh(asset)
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	code, err := runSshCommand(client, "uname -s", "", &out, io.Discard)
	if err == nil && code == 0 && strings.TrimSpace(out.String()) == "Linux" {
		host, err := newLinuxHost(client)
		if err != nil {
			client.Close()
			return nil, err
		}
		return host, nil
	}

	out.Reset()
	_, err = runSshCommand(client, "cmd /c ver", "", &out, io.Discard)
	if err == nil && strings.Contains(out.String(), "Windows") {
		return &windowsHost{client: client}, nil
	}

	client.Close()
	return nil, fmt.Errorf("the asset: %s is neither Linux nor Windows", asset.GetId())
}

// runSshCommand runs one command with the given stdin and returns its exit code.
func runSshCommand(client *ssh.Client, command string, stdin string, stdout io.Writer, stderr io.Writer) (int, error) {
	session, err := client.NewSession()
	if err != nil {
		return -1, err
	}
	defer session.Close()

	session.Stdin = strings.NewReader(stdin)
	session.Stdout = stdout
	session.Stderr = stderr
	err = session.Run(command)
	if exitErr, ok := err.(*ssh.ExitError); ok {
		return exitErr.ExitStatus(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

// InitPatchJobs fails the jobs left running by a previous run of Casvisor.
func InitPatchJobs() {
	_, err := adapter.engine.Where("status = ?", PatchJobRunning).Cols("status", "message", "end_time").Update(&PatchJob{
		Status:  PatchJobFailed,
		Message: "interrupted, Casvisor was restarted",
		EndTime: util.GetCurrentTime(),
	})
	if err != nil {
		logs.Error("failed to clean up the patch jobs: %s", err.Error())
	}

	_, err = adapter.engine.Where("job <> ''").Cols("job", "job_type").Update(&PatchStatus{})
	if err != nil {
		logs.Error("failed to clean up the patch statuses: %s", err.Error())
	}
}

// ScanStalePatches rescans the assets scanned before when nothing has run on them for patchScanInterval hours (24 by default, 0 turns it off).
func ScanStalePatches() {
	interval := 24
	if value := conf.GetConfigString("patchScanInterval"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			logs.Error("invalid patchScanInterval: %s", value)
			return
		}
		interval = parsed
	}
	if interval <= 0 || conf.GetConfigBool("isDemoMode") {
		return
	}

	statuses := []*PatchStatus{}
	err := adapter.engine.Where("scanned_time <> '' and job = ''").Find(&statuses)
	if err != nil {
		logs.Error("failed to get the patch statuses: %s", err.Error())
		return
	}

	for _, status := range statuses {
		updatedTime, err := time.Parse(time.RFC3339, status.UpdatedTime)
		if err != nil || time.Since(updatedTime) < time.Duration(interval)*time.Hour {
			continue
		}

		_, err = StartPatchJob(util.GetIdFromOwnerAndName(status.Owner, status.Name), PatchJobScan, nil, "")
		if err != nil {
			logs.Info("skipped the scheduled patch scan of %s/%s: %s", status.Owner, status.Name, err.Error())
		}
	}
}
