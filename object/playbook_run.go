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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/casvisor/casvisor/util"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const (
	PlaybookOk      = "ok"
	PlaybookChanged = "changed"
	PlaybookFailed  = "failed"
	PlaybookSkipped = "skipped"

	maxPlaybookTaskOutput = 64 * 1024
	pbNoLogMessage        = "the output has been hidden due to the fact that 'no_log: true' was specified for this result"
)

// PlaybookTaskResult is how a task of a playbook went on an asset.
type PlaybookTaskResult struct {
	Play      string                `json:"play"`
	Name      string                `json:"name"`
	Module    string                `json:"module"`
	Handler   bool                  `json:"handler,omitempty"`
	Status    string                `json:"status"`
	Ignored   bool                  `json:"ignored,omitempty"`
	Msg       string                `json:"msg,omitempty"`
	Rc        *int                  `json:"rc,omitempty"`
	Output    string                `json:"output,omitempty"`
	Items     []*PlaybookItemResult `json:"items,omitempty"`
	StartTime string                `json:"startTime"`
	EndTime   string                `json:"endTime"`
}

type PlaybookItemResult struct {
	Label  string `json:"label"`
	Status string `json:"status"`
	Msg    string `json:"msg,omitempty"`
}

// pbBlockedError is a command stopped by a command rule, or not approved by a reviewer.
type pbBlockedError struct {
	msg string
}

func (e *pbBlockedError) Error() string {
	return e.msg
}

type pbRunner struct {
	run       *jobRun
	rr        *jobResultRun
	ctx       context.Context
	asset     *Asset
	sessionId string
	client    *ssh.Client
	sftp      *sftp.Client

	extraVars map[string]interface{}
	hostVars  map[string]interface{}
	magic     map[string]interface{}
	facts     map[string]interface{}
	groups    []string
	blocked   error

	stats map[string]int
}

// pbTaskCtx is how the commands of a task run: as whom, where and with which environment.
type pbTaskCtx struct {
	scope      *pbScope
	become     bool
	becomeUser string
	env        map[string]string
	check      bool
	noLog      bool
}

func (t *pbTaskCtx) withoutBecome() *pbTaskCtx {
	c := *t
	c.become = false
	return &c
}

func (r *pbRunner) printf(format string, args ...interface{}) {
	_, _ = r.rr.Write([]byte(fmt.Sprintf(format, args...)))
}

func (r *pbRunner) printHeader(title string) {
	line := title + " "
	if n := 79 - len([]rune(line)); n > 0 {
		line += strings.Repeat("*", n)
	}
	r.printf("\n%s\n", line)
}

func (r *pbRunner) addTask(task *PlaybookTaskResult) {
	r.rr.lock.Lock()
	defer r.rr.lock.Unlock()
	r.rr.tasks = append(r.rr.tasks, task)
	r.rr.dirty = true
}

func (r *pbRunner) touchTasks() {
	r.rr.lock.Lock()
	r.rr.dirty = true
	r.rr.lock.Unlock()
}

// checkCommand checks a command against the command rules: a Block rule stops it, a Review rule
// waits for another administrator. A task is always recorded, a command it runs only when a rule matches it.
func (r *pbRunner) checkCommand(text string, always bool) error {
	rule, err := matchJobRule(r.run.user, r.asset, text)
	if err != nil {
		return err
	}
	if rule == nil && !always {
		return nil
	}

	command := &Command{
		Owner:    r.asset.Owner,
		Session:  r.sessionId,
		Asset:    r.asset.GetId(),
		User:     r.run.job.User,
		ClientIp: r.run.clientIp,
		Text:     text,
	}
	if rule != nil {
		command.Action = rule.Action
		command.Rule = rule.GetId()
		r.rr.result.Rule = rule.GetId()
	}

	if rule != nil && rule.Action == CommandActionReview {
		approved, err := HoldCommand(command, rule, r.ctx.Done(), func(review *CommandReview) {
			message := GetHeldMessage(rule, review)
			r.rr.logf("%s", strings.ToUpper(message[:1])+message[1:])
		})
		addErr := AddCommand(command)
		if err != nil {
			return err
		}
		if addErr != nil {
			return addErr
		}
		if r.ctx.Err() != nil {
			return r.ctx.Err()
		}
		if !approved {
			r.blocked = &pbBlockedError{msg: command.Error}
			return r.blocked
		}
		r.rr.logf("The command is approved")
		return nil
	}

	err = AddCommand(command)
	if err != nil {
		return err
	}
	if rule != nil && rule.Action == CommandActionBlock {
		r.blocked = &pbBlockedError{msg: fmt.Sprintf("the command is blocked by the rule: %s", rule.GetDisplayName())}
		return r.blocked
	}
	return nil
}

type limitedBuffer struct {
	bytes.Buffer
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxJobOutput {
		b.truncated = true
		if room := maxJobOutput - b.Len(); room > 0 {
			b.Buffer.Write(p[:room])
		}
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

func (r *pbRunner) wrap(t *pbTaskCtx, cmd string) (string, string) {
	inner := cmd
	if len(t.env) > 0 {
		keys := make([]string, 0, len(t.env))
		for key := range t.env {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		exports := ""
		for _, key := range keys {
			exports += fmt.Sprintf("export %s=%s; ", key, shellQuote(t.env[key]))
		}
		inner = exports + inner
	}

	user := t.becomeUser
	if user == "" {
		user = "root"
	}
	if !t.become || user == r.asset.Username {
		return "sh -c " + shellQuote(inner), ""
	}
	if r.asset.Password != "" {
		return fmt.Sprintf("sudo -H -S -p '' -u %s -- sh -c %s", shellQuote(user), shellQuote("exec </dev/null; "+inner)), r.asset.Password + "\n"
	}
	return fmt.Sprintf("sudo -H -n -u %s -- sh -c %s", shellQuote(user), shellQuote(inner)), ""
}

// sh runs a command on the asset as the task says, after the command rules allow it.
func (r *pbRunner) sh(t *pbTaskCtx, cmd string) (int, string, string, error) {
	return r.exec(t, cmd, false)
}

func (r *pbRunner) exec(t *pbTaskCtx, cmd string, checked bool) (int, string, string, error) {
	if r.ctx.Err() != nil {
		return -1, "", "", r.ctx.Err()
	}
	if !checked {
		err := r.checkCommand(cmd, false)
		if err != nil {
			return -1, "", "", err
		}
	}

	command, stdin := r.wrap(t, cmd)
	session, err := r.client.NewSession()
	if err != nil {
		return -1, "", "", err
	}
	defer session.Close()

	stop := context.AfterFunc(r.ctx, func() {
		_ = session.Signal(ssh.SIGTERM)
		time.Sleep(2 * time.Second)
		_ = session.Signal(ssh.SIGKILL)
	})
	defer stop()

	var stdout, stderr limitedBuffer
	session.Stdin = strings.NewReader(stdin)
	session.Stdout = &stdout
	session.Stderr = &stderr
	err = session.Run(command)
	out, errOut := strings.ToValidUTF8(stdout.String(), "�"), strings.ToValidUTF8(stderr.String(), "�")
	if exitErr, ok := err.(*ssh.ExitError); ok {
		return exitErr.ExitStatus(), out, errOut, nil
	}
	if err != nil {
		if r.ctx.Err() != nil {
			return -1, out, errOut, r.ctx.Err()
		}
		return -1, out, errOut, err
	}
	return 0, out, errOut, nil
}

// shOk runs a command that should succeed and returns its output.
func (r *pbRunner) shOk(t *pbTaskCtx, cmd string) (string, error) {
	rc, out, errOut, err := r.sh(t, cmd)
	if err != nil {
		return "", err
	}
	if rc != 0 {
		msg := strings.TrimSpace(errOut)
		if msg == "" {
			msg = strings.TrimSpace(out)
		}
		return "", fmt.Errorf("%s exited with code %d: %s", strings.SplitN(cmd, " ", 2)[0], rc, truncate(msg, 2000))
	}
	return out, nil
}

func (r *pbRunner) getSftp() (*sftp.Client, error) {
	if r.sftp != nil {
		return r.sftp, nil
	}
	client, err := sftp.NewClient(r.client)
	if err != nil {
		return nil, fmt.Errorf("failed to open SFTP: %s", err.Error())
	}
	r.sftp = client
	return client, nil
}

// uploadTemp puts data in a temporary file of the account the asset is logged in as.
func (r *pbRunner) uploadTemp(t *pbTaskCtx, data io.Reader, executable bool) (string, error) {
	client, err := r.getSftp()
	if err != nil {
		return "", err
	}
	name := "/tmp/.casvisor-pb-" + strings.ReplaceAll(util.GenerateId(), "-", "")
	file, err := client.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		return "", fmt.Errorf("failed to upload to %s: %s", name, err.Error())
	}
	_, err = io.Copy(file, data)
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	mode := 0o600
	if t.become && t.becomeUser != "" && t.becomeUser != "root" {
		mode = 0o644
	}
	if executable {
		mode |= 0o100
		if mode&0o004 != 0 {
			mode |= 0o011
		}
	}
	if err == nil {
		err = client.Chmod(name, os.FileMode(mode))
	}
	if err != nil {
		_ = client.Remove(name)
		return "", fmt.Errorf("failed to upload to %s: %s", name, err.Error())
	}
	return name, nil
}

func (r *pbRunner) removeTemp(name string) {
	if client, err := r.getSftp(); err == nil {
		_ = client.Remove(name)
	}
}

func getPbNodeAncestors(nodeMap map[string]*AssetNode, name string) []string {
	res := []string{}
	for i := 0; name != "" && i < 100; i++ {
		res = append(res, name)
		node := nodeMap[name]
		if node == nil {
			break
		}
		name = node.Parent
	}
	return res
}

// getPbGroups is the groups an asset is in, for the hosts of plays: its tag and its nodes with their parents.
func getPbGroups(asset *Asset) []string {
	groups := []string{}
	if asset.Tag != "" {
		groups = append(groups, asset.Tag)
	}
	nodes, err := GetAssetNodes(asset.Owner)
	if err != nil {
		return groups
	}
	nodeMap := getAssetNodeMap(nodes)
	for _, name := range asset.Nodes {
		for _, group := range getPbNodeAncestors(nodeMap, name) {
			if !slices.Contains(groups, group) {
				groups = append(groups, group)
			}
		}
	}
	return groups
}

// matchPbHosts tells whether a play's hosts pick the asset: all, its name, its groups and
// wildcards, with ! to leave out and & to require.
func matchPbHosts(patterns []string, name string, groups []string) bool {
	match := func(pattern string) bool {
		if pattern == "all" || pattern == "*" {
			return true
		}
		candidates := append([]string{name}, groups...)
		for _, candidate := range candidates {
			if ok, _ := path.Match(pattern, candidate); ok {
				return true
			}
		}
		return false
	}

	res := false
	for _, pattern := range patterns {
		if !strings.HasPrefix(pattern, "!") && !strings.HasPrefix(pattern, "&") && match(pattern) {
			res = true
		}
	}
	for _, pattern := range patterns {
		if strings.HasPrefix(pattern, "!") && match(pattern[1:]) {
			return false
		}
		if strings.HasPrefix(pattern, "&") && !match(pattern[1:]) {
			return false
		}
	}
	return res
}

func (r *pbRunner) baseScope(play *pbPlay, taskVars map[string]interface{}) *pbScope {
	return &pbScope{layers: []map[string]interface{}{r.extraVars, r.hostVars, taskVars, play.Vars, r.magic, r.facts}}
}

func (r *pbRunner) gatherFacts() error {
	script := "uname -s; uname -r; uname -m; hostname; echo ===; cat /etc/os-release 2>/dev/null; echo ===; " +
		"for c in apt-get dnf yum; do command -v $c >/dev/null 2>&1 && echo $c; done; echo ===; " +
		"[ -d /run/systemd/system ] && echo systemd; echo ===; id -un; id -u"
	out, err := r.shOk(&pbTaskCtx{}, script)
	if err != nil {
		return err
	}
	sections := strings.Split(strings.ReplaceAll(out, "\r", ""), "===\n")
	for len(sections) < 5 {
		sections = append(sections, "")
	}
	lines := strings.Split(strings.TrimSpace(sections[0]), "\n")
	for len(lines) < 4 {
		lines = append(lines, "")
	}

	osRelease := map[string]string{}
	for _, line := range strings.Split(sections[1], "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			osRelease[key] = strings.Trim(value, `"'`)
		}
	}
	family := ""
	ids := strings.Fields(osRelease["ID"] + " " + osRelease["ID_LIKE"])
	for _, id := range ids {
		switch id {
		case "debian", "ubuntu":
			family = "Debian"
		case "rhel", "centos", "fedora", "rocky", "almalinux", "ol", "amzn", "anolis", "openEuler", "opencloudos", "kylin":
			family = "RedHat"
		case "suse", "opensuse", "sles":
			family = "Suse"
		case "alpine":
			family = "Alpine"
		case "arch":
			family = "Archlinux"
		}
		if family != "" {
			break
		}
	}
	pkgMgr := ""
	for _, line := range strings.Split(strings.TrimSpace(sections[2]), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			pkgMgr = strings.TrimSuffix(line, "-get")
			break
		}
	}
	serviceMgr := "service"
	if strings.TrimSpace(sections[3]) == "systemd" {
		serviceMgr = "systemd"
	}
	hostname := strings.TrimSpace(lines[3])
	user := strings.Fields(sections[4])

	facts := map[string]interface{}{
		"system":               strings.TrimSpace(lines[0]),
		"kernel":               strings.TrimSpace(lines[1]),
		"architecture":         strings.TrimSpace(lines[2]),
		"hostname":             strings.SplitN(hostname, ".", 2)[0],
		"fqdn":                 hostname,
		"nodename":             hostname,
		"distribution":         osRelease["NAME"],
		"distribution_version": osRelease["VERSION_ID"],
		"distribution_release": osRelease["VERSION_CODENAME"],
		"os_family":            family,
		"pkg_mgr":              pkgMgr,
		"service_mgr":          serviceMgr,
	}
	if id := osRelease["ID"]; id != "" {
		names := map[string]string{"ubuntu": "Ubuntu", "debian": "Debian", "centos": "CentOS", "rhel": "RedHat", "fedora": "Fedora", "rocky": "Rocky", "almalinux": "AlmaLinux", "alpine": "Alpine", "amzn": "Amazon", "opensuse-leap": "openSUSE Leap", "sles": "SLES", "arch": "Archlinux"}
		if name, ok := names[id]; ok {
			facts["distribution"] = name
		}
	}
	if v := osRelease["VERSION_ID"]; v != "" {
		facts["distribution_major_version"] = strings.SplitN(v, ".", 2)[0]
	}
	if len(user) >= 2 {
		facts["user_id"] = user[0]
		facts["user_uid"] = user[1]
	}

	r.facts = map[string]interface{}{"ansible_facts": facts}
	for key, value := range facts {
		r.facts["ansible_"+key] = value
	}
	return nil
}

// runPlaybook runs the plays whose hosts pick the asset, task by task as Ansible's free strategy does.
func (r *pbRunner) runPlaybook() (string, error) {
	job := r.run.job
	plays, _, err := parsePlaybook(job.Content, job.Files)
	if err != nil {
		return "", err
	}
	r.extraVars, err = parsePbExtraVars(job.Variables)
	if err != nil {
		return "", err
	}
	r.hostVars = map[string]interface{}{}
	r.facts = map[string]interface{}{}
	r.stats = map[string]int{}
	r.groups = getPbGroups(r.asset)
	groups := []interface{}{}
	for _, group := range r.groups {
		groups = append(groups, group)
	}
	r.magic = map[string]interface{}{
		"inventory_hostname":       r.asset.Name,
		"inventory_hostname_short": strings.SplitN(r.asset.Name, ".", 2)[0],
		"ansible_host":             r.asset.Host,
		"ansible_port":             r.asset.Port,
		"ansible_user":             r.asset.Username,
		"group_names":              groups,
		"ansible_check_mode":       job.CheckMode,
		"omit":                     "__omit_place_holder__",
	}

	host, err := detectJobHost(r.client)
	if err != nil {
		return "", err
	}
	if !host.unix {
		return "", fmt.Errorf("playbooks run on Linux and Unix assets")
	}

	failed := false
	gathered := false
	matched := 0
	for _, play := range plays {
		if !matchPbHosts(play.Hosts, r.asset.Name, r.groups) {
			continue
		}
		matched++
		name := play.Name
		if name == "" {
			name = strings.Join(play.Hosts, ",")
		}
		r.printHeader(fmt.Sprintf("PLAY [%s]", name))

		if play.Facts && !gathered {
			r.printHeader("TASK [Gathering Facts]")
			result := &PlaybookTaskResult{Play: name, Name: "Gathering Facts", Module: "setup", StartTime: nowTime()}
			r.addTask(result)
			err = r.gatherFacts()
			result.EndTime = nowTime()
			if err != nil {
				if errors.Is(err, r.ctx.Err()) || r.blocked != nil {
					return r.endStatus(err)
				}
				result.Status, result.Msg = PlaybookFailed, err.Error()
				r.stats[PlaybookFailed]++
				r.printf("fatal: [%s]: FAILED! => %s\n", r.asset.Name, err.Error())
				r.touchTasks()
				failed = true
				break
			}
			result.Status = PlaybookOk
			r.stats[PlaybookOk]++
			r.printf("ok: [%s]\n", r.asset.Name)
			r.touchTasks()
			gathered = true
		}

		notified := map[string]bool{}
		for _, task := range play.Tasks {
			ok := r.runTask(play, name, task, false, notified)
			if r.ctx.Err() != nil || r.blocked != nil {
				return r.endStatus(r.ctx.Err())
			}
			if !ok {
				failed = true
				break
			}
		}
		if failed {
			break
		}

		for _, handler := range play.Handlers {
			run := notified[handler.Name]
			for _, topic := range handler.Listen {
				run = run || notified[topic]
			}
			if !run {
				continue
			}
			ok := r.runTask(play, name, handler, true, notified)
			if r.ctx.Err() != nil || r.blocked != nil {
				return r.endStatus(r.ctx.Err())
			}
			if !ok {
				failed = true
				break
			}
		}
		if failed {
			break
		}
	}

	if matched == 0 {
		r.printf("\nskipping: no play of the playbook picks the host %s\n", r.asset.Name)
	}
	r.printRecap()
	if failed {
		return JobFailed, fmt.Errorf("%d task(s) failed", max(r.stats[PlaybookFailed], 1))
	}
	return JobSucceeded, nil
}

func (r *pbRunner) endStatus(err error) (string, error) {
	r.printRecap()
	if r.blocked != nil {
		return JobBlocked, r.blocked
	}
	return "", err
}

func (r *pbRunner) printRecap() {
	r.printHeader("PLAY RECAP")
	r.printf("%-26s : ok=%-4d changed=%-4d unreachable=0    failed=%-4d skipped=%-4d ignored=%d\n", r.asset.Name,
		r.stats[PlaybookOk]+r.stats[PlaybookChanged], r.stats[PlaybookChanged], r.stats[PlaybookFailed], r.stats[PlaybookSkipped], r.stats["ignored"])
}

func nowTime() string {
	return time.Now().Format(time.RFC3339)
}

// runTask runs a task, for each item of its loop, and returns false when it failed and
// the errors are not ignored.
func (r *pbRunner) runTask(play *pbPlay, playName string, task *pbTask, isHandler bool, notified map[string]bool) bool {
	title := "TASK"
	if isHandler {
		title = "RUNNING HANDLER"
	}
	r.printHeader(fmt.Sprintf("%s [%s]", title, task.Label))
	result := &PlaybookTaskResult{Play: playName, Name: task.Label, Module: task.Module, Handler: isHandler, StartTime: nowTime()}
	r.addTask(result)

	scope := r.baseScope(play, task.Vars)
	ok := r.runTaskItems(play, task, scope, result)
	result.EndTime = nowTime()

	if result.Status == PlaybookChanged {
		for _, name := range task.Notify {
			notified[name] = true
		}
	}
	if result.Status == PlaybookFailed && result.Ignored {
		r.stats["ignored"]++
		r.printf("...ignoring\n")
	} else {
		r.stats[result.Status]++
	}
	r.touchTasks()
	return ok
}

func (r *pbRunner) getTaskCtx(play *pbPlay, task *pbTask, scope *pbScope) (*pbTaskCtx, error) {
	t := &pbTaskCtx{scope: scope, check: r.run.job.CheckMode, noLog: task.NoLog}
	if play.CheckMode != nil {
		t.check = *play.CheckMode
	}
	if task.CheckMode != nil {
		t.check = *task.CheckMode
	}
	if play.Become != nil {
		t.become = *play.Become
	}
	if task.Become != nil {
		t.become = *task.Become
	}
	becomeUser := play.BecomeUser
	if task.BecomeUser != "" {
		becomeUser = task.BecomeUser
	}
	var err error
	t.becomeUser, err = renderPbString(becomeUser, scope)
	if err != nil {
		return nil, err
	}

	t.env = map[string]string{}
	for _, env := range []map[string]interface{}{play.Env, task.Env} {
		for key, value := range env {
			rendered, err := renderPbString(pbToString(value), scope)
			if err != nil {
				return nil, err
			}
			if !pbVarNameRegex.MatchString(key) {
				return nil, fmt.Errorf("invalid environment variable: %s", key)
			}
			t.env[key] = rendered
		}
	}
	return t, nil
}

func (r *pbRunner) runTaskItems(play *pbPlay, task *pbTask, scope *pbScope, result *PlaybookTaskResult) bool {
	fail := func(err error) bool {
		result.Status, result.Msg = PlaybookFailed, err.Error()
		r.printf("fatal: [%s]: FAILED! => %s\n", r.asset.Name, err.Error())
		ignored, ignoreErr := r.isIgnored(play, task, scope)
		result.Ignored = ignored && ignoreErr == nil && r.blocked == nil && r.ctx.Err() == nil
		return result.Ignored
	}

	ignored, err := r.isIgnored(play, task, scope)
	if err != nil {
		return fail(err)
	}

	if task.Loop == nil {
		res, status, err := r.runTaskItem(play, task, scope)
		if err != nil {
			return fail(err)
		}
		r.applyResult(task, res, status, result, "")
		if task.Register != "" {
			r.hostVars[task.Register] = res
		}
		if status == PlaybookFailed {
			result.Ignored = ignored
			return ignored
		}
		return true
	}

	value, err := renderPbValue(task.Loop, scope)
	if err != nil {
		return fail(err)
	}
	items, ok := value.([]interface{})
	if !ok {
		if s, isString := value.(string); isString && task.WithItems {
			items = []interface{}{s}
		} else {
			return fail(fmt.Errorf("the loop should be a list, got: %s", pbToString(value)))
		}
	}
	if task.WithItems {
		flat := []interface{}{}
		for _, item := range items {
			if list, ok := item.([]interface{}); ok {
				flat = append(flat, list...)
			} else {
				flat = append(flat, item)
			}
		}
		items = flat
	}

	results := []interface{}{}
	statuses := []string{}
	for i, item := range items {
		layer := map[string]interface{}{task.LoopVar: item, "ansible_loop_var": task.LoopVar}
		if task.IndexVar != "" {
			layer[task.IndexVar] = i
		}
		itemScope := scope.with(layer)
		label := pbToString(item)
		if task.LoopLabel != "" {
			label, err = renderPbString(task.LoopLabel, itemScope)
			if err != nil {
				return fail(err)
			}
		}
		res, status, err := r.runTaskItem(play, task, itemScope)
		if err != nil {
			return fail(err)
		}
		res[task.LoopVar] = item
		res["ansible_loop_var"] = task.LoopVar
		results = append(results, res)
		statuses = append(statuses, status)
		r.printItem(task, res, status, label)
		result.Items = append(result.Items, &PlaybookItemResult{Label: truncate(label, 200), Status: status, Msg: truncate(r.getMsg(task, res), 1000)})
		if output := r.getOutput(task, res); output != "" {
			result.Output = truncate(result.Output+output, maxPlaybookTaskOutput)
		}
		r.touchTasks()
	}

	aggregate := map[string]interface{}{"results": results, "changed": slices.Contains(statuses, PlaybookChanged), "failed": slices.Contains(statuses, PlaybookFailed), "msg": "All items completed"}
	switch {
	case slices.Contains(statuses, PlaybookFailed):
		result.Status = PlaybookFailed
		aggregate["msg"] = "One or more items failed"
	case slices.Contains(statuses, PlaybookChanged):
		result.Status = PlaybookChanged
	case len(statuses) > 0 && !slices.ContainsFunc(statuses, func(s string) bool { return s != PlaybookSkipped }):
		result.Status = PlaybookSkipped
		aggregate["skipped"] = true
	default:
		result.Status = PlaybookOk
	}
	if len(items) == 0 {
		result.Status = PlaybookSkipped
		aggregate["skipped"] = true
		r.printf("skipping: [%s] => no items\n", r.asset.Name)
	}
	if task.Register != "" {
		r.hostVars[task.Register] = aggregate
	}
	result.Msg = truncate(pbToString(aggregate["msg"]), 1000)
	if result.Status == PlaybookFailed {
		r.printf("fatal: [%s]: FAILED! => One or more items failed\n", r.asset.Name)
		result.Ignored = ignored
		return ignored
	}
	return true
}

func (r *pbRunner) isIgnored(play *pbPlay, task *pbTask, scope *pbScope) (bool, error) {
	if task.IgnoreErrors == nil {
		return play.Ignore, nil
	}
	value, err := renderPbValue(task.IgnoreErrors, scope)
	if err != nil {
		return false, err
	}
	return pbToBool(value), nil
}

// runTaskItem runs the module of a task once, for an item of its loop or for the task, and
// applies when, changed_when and failed_when.
func (r *pbRunner) runTaskItem(play *pbPlay, task *pbTask, scope *pbScope) (map[string]interface{}, string, error) {
	ok, err := evalPbConditions(task.When, scope)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return map[string]interface{}{"changed": false, "skipped": true, "skip_reason": "Conditional result was False", "false_condition": strings.Join(task.When, " and ")}, PlaybookSkipped, nil
	}

	t, err := r.getTaskCtx(play, task, scope)
	if err != nil {
		return nil, "", err
	}
	rendered, err := renderPbValue(task.Args, scope)
	if err != nil {
		return nil, "", err
	}
	args := rendered.(map[string]interface{})
	for key, value := range args {
		if value == "__omit_place_holder__" {
			delete(args, key)
		}
	}

	res, err := r.runModule(t, task, args)
	if err != nil {
		var blocked *pbBlockedError
		if errors.As(err, &blocked) || r.ctx.Err() != nil {
			return nil, "", err
		}
		res = map[string]interface{}{"failed": true, "changed": false, "msg": err.Error()}
	}
	if _, ok := res["changed"]; !ok {
		res["changed"] = false
	}
	if _, ok := res["failed"]; !ok {
		res["failed"] = false
	}
	if skipped, _ := res["skipped"].(bool); skipped {
		return res, PlaybookSkipped, nil
	}

	condScope := scope
	if task.Register != "" {
		condScope = scope.with(map[string]interface{}{task.Register: res})
	}
	if len(task.ChangedWhen) > 0 {
		changed, err := evalPbConditions(task.ChangedWhen, condScope)
		if err != nil {
			return nil, "", err
		}
		res["changed"] = changed
	}
	if len(task.FailedWhen) > 0 {
		failed, err := evalPbConditions(task.FailedWhen, condScope)
		if err != nil {
			return nil, "", err
		}
		res["failed"] = failed
		res["failed_when_result"] = failed
	}

	if pbToBool(res["failed"]) {
		return res, PlaybookFailed, nil
	}
	if pbToBool(res["changed"]) {
		return res, PlaybookChanged, nil
	}
	return res, PlaybookOk, nil
}

func (r *pbRunner) getMsg(task *pbTask, res map[string]interface{}) string {
	if task.NoLog {
		return pbNoLogMessage
	}
	if msg, ok := res["msg"]; ok && task.Module != "debug" {
		return pbToString(msg)
	}
	return ""
}

func (r *pbRunner) getOutput(task *pbTask, res map[string]interface{}) string {
	if task.NoLog {
		return ""
	}
	if task.Module == "debug" {
		if msg, ok := res["msg"]; ok {
			return pbToString(msg) + "\n"
		}
		for key, value := range res {
			if key != "changed" && key != "failed" {
				return fmt.Sprintf("%s: %s\n", key, pbToString(value))
			}
		}
		return ""
	}
	var b strings.Builder
	for _, key := range []string{"stdout", "stderr"} {
		if s, ok := res[key].(string); ok && s != "" {
			b.WriteString(s)
			if !strings.HasSuffix(s, "\n") {
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

func (r *pbRunner) applyResult(task *pbTask, res map[string]interface{}, status string, result *PlaybookTaskResult, label string) {
	result.Status = status
	result.Msg = truncate(r.getMsg(task, res), 1000)
	if rc, ok := res["rc"].(int); ok {
		result.Rc = &rc
	}
	result.Output = truncate(r.getOutput(task, res), maxPlaybookTaskOutput)
	r.printItem(task, res, status, label)
}

func (r *pbRunner) printItem(task *pbTask, res map[string]interface{}, status string, label string) {
	host := r.asset.Name
	item := ""
	if label != "" {
		item = fmt.Sprintf(" => (item=%s)", truncate(label, 200))
	}
	switch status {
	case PlaybookSkipped:
		r.printf("skipping: [%s]%s\n", host, item)
		return
	case PlaybookFailed:
		if label == "" {
			r.printf("fatal: [%s]: FAILED! => %s\n", host, truncate(r.getMsg(task, res), 2000))
		} else {
			r.printf("failed: [%s]%s => %s\n", host, item, truncate(r.getMsg(task, res), 2000))
		}
	default:
		r.printf("%s: [%s]%s\n", status, host, item)
	}

	if task.NoLog {
		return
	}
	if task.Module == "debug" {
		data, _ := json.MarshalIndent(withoutPbFlags(res), "", "    ")
		r.printf("%s\n", data)
		return
	}
	if output := r.getOutput(task, res); output != "" {
		lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
		if len(lines) > 200 {
			lines = append(lines[:200], fmt.Sprintf("... %d more lines", len(lines)-200))
		}
		for _, line := range lines {
			r.printf("    %s\n", line)
		}
	} else if msg := r.getMsg(task, res); msg != "" && status != PlaybookFailed {
		r.printf("    %s\n", truncate(msg, 2000))
	}
}

func withoutPbFlags(res map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for key, value := range res {
		if key != "changed" && key != "failed" {
			out[key] = value
		}
	}
	return out
}
