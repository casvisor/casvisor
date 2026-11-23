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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/casvisor/casvisor/util"
)

func pbArg(args map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if v, ok := args[key]; ok && v != nil {
			return pbToString(v)
		}
	}
	return ""
}

func pbHasArg(args map[string]interface{}, key string) bool {
	_, ok := args[key]
	return ok
}

func pbLines(s string) []interface{} {
	res := []interface{}{}
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return res
	}
	for _, line := range strings.Split(s, "\n") {
		res = append(res, strings.TrimSuffix(line, "\r"))
	}
	return res
}

func pbCommandResult(cmd string, rc int, stdout string, stderr string) map[string]interface{} {
	res := map[string]interface{}{
		"cmd":          cmd,
		"rc":           rc,
		"stdout":       strings.TrimRight(stdout, "\n"),
		"stderr":       strings.TrimRight(stderr, "\n"),
		"stdout_lines": pbLines(stdout),
		"stderr_lines": pbLines(stderr),
		"changed":      true,
		"failed":       rc != 0,
	}
	if rc != 0 {
		res["msg"] = fmt.Sprintf("non-zero return code %d", rc)
	}
	return res
}

// getPbTaskText is what a task is recorded as and checked against the command rules: the command
// it runs, or the module and its options.
func (r *pbRunner) getPbTaskText(task *pbTask, args map[string]interface{}) string {
	switch task.Module {
	case "command", "shell", "raw":
		return pbArg(args, "_raw_params")
	case "debug", "set_fact", "fail", "assert", "ping":
		return ""
	}
	keys := pbSortedKeys(args)
	parts := []string{}
	for _, key := range keys {
		value := args[key]
		if key == "_raw_params" {
			parts = append(parts, pbToString(value))
			continue
		}
		text := pbToString(value)
		if key == "content" {
			text = truncate(text, 500)
		}
		if key == "password" {
			text = "***"
		}
		parts = append(parts, key+"="+text)
	}
	return task.Module + ": " + strings.Join(parts, " ")
}

func (r *pbRunner) runModule(t *pbTaskCtx, task *pbTask, args map[string]interface{}) (map[string]interface{}, error) {
	if text := r.getPbTaskText(task, args); text != "" {
		if task.Module == "script" {
			if content, err := r.readPbFile(getPbScriptName(args)); err == nil {
				text += "\n" + string(content)
			}
		}
		err := r.checkCommand(text, true)
		if err != nil {
			return nil, err
		}
	}

	switch task.Module {
	case "command", "shell", "raw", "script":
		return r.runPbCommand(t, task.Module, args)
	case "copy", "template":
		return r.runPbCopy(t, task.Module, args)
	case "file":
		return r.runPbFile(t, args)
	case "lineinfile":
		return r.runPbLineinfile(t, args)
	case "apt":
		return r.runPbApt(t, args)
	case "yum", "dnf", "package":
		return r.runPbPackage(t, task.Module, args)
	case "service", "systemd":
		return r.runPbService(t, task.Module, args)
	case "user":
		return r.runPbUser(t, args)
	case "group":
		return r.runPbGroup(t, args)
	case "get_url":
		return r.runPbGetUrl(t, args)
	case "debug":
		if v, ok := args["var"]; ok {
			name := pbToString(v)
			node, err := parsePbExpr(name)
			if err != nil {
				return nil, err
			}
			value, err := node.eval(t.scope)
			if err != nil {
				return nil, err
			}
			if _, undefined := value.(undefinedValue); undefined {
				value = "VARIABLE IS NOT DEFINED!"
			}
			return map[string]interface{}{name: value}, nil
		}
		msg, ok := args["msg"]
		if !ok {
			msg = "Hello world!"
		}
		return map[string]interface{}{"msg": msg}, nil
	case "set_fact":
		for key, value := range args {
			if key != "cacheable" {
				r.hostVars[key] = value
			}
		}
		return map[string]interface{}{"ansible_facts": args}, nil
	case "fail":
		msg := pbArg(args, "msg")
		if msg == "" {
			msg = "Failed as requested from task"
		}
		return map[string]interface{}{"failed": true, "msg": msg}, nil
	case "assert":
		for _, cond := range pbStrings(args["that"]) {
			ok, err := evalPbConditions([]string{cond}, t.scope)
			if err != nil {
				return nil, err
			}
			if !ok {
				msg := pbArg(args, "fail_msg", "msg")
				if msg == "" {
					msg = "Assertion failed"
				}
				return map[string]interface{}{"failed": true, "assertion": cond, "evaluated_to": false, "msg": msg}, nil
			}
		}
		msg := pbArg(args, "success_msg")
		if msg == "" {
			msg = "All assertions passed"
		}
		return map[string]interface{}{"msg": msg}, nil
	case "ping":
		data := pbArg(args, "data")
		if data == "" {
			data = "pong"
		}
		return map[string]interface{}{"ping": data}, nil
	}
	return nil, fmt.Errorf("the module: %s is not supported", task.Module)
}

func getPbScriptName(args map[string]interface{}) string {
	parts, err := splitPbArgs(pbArg(args, "_raw_params"))
	if err != nil || len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func (r *pbRunner) openPbFile(name string) (*os.File, *JobFile, error) {
	file := findJobFile(r.run.job.Files, name)
	if file == nil {
		return nil, nil, fmt.Errorf("the file: %s is not attached to the job", name)
	}
	localPath, err := GetJobFilePath(r.run.job.Owner, file.Key)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(localPath)
	if err != nil {
		return nil, nil, fmt.Errorf("the attached file: %s is gone, attach it again", file.Name)
	}
	return f, file, nil
}

func (r *pbRunner) readPbFile(name string) ([]byte, error) {
	f, _, err := r.openPbFile(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 16<<20))
}

// testPbPath runs the creates and removes options: whether the command is skipped.
func (r *pbRunner) testPbPath(t *pbTaskCtx, args map[string]interface{}) (map[string]interface{}, error) {
	if creates := pbArg(args, "creates"); creates != "" {
		rc, _, _, err := r.sh(t, "test -e "+shellQuote(creates))
		if err != nil {
			return nil, err
		}
		if rc == 0 {
			return map[string]interface{}{"changed": false, "rc": 0, "stdout": "", "msg": fmt.Sprintf("Did not run command since '%s' exists", creates)}, nil
		}
	}
	if removes := pbArg(args, "removes"); removes != "" {
		rc, _, _, err := r.sh(t, "test -e "+shellQuote(removes))
		if err != nil {
			return nil, err
		}
		if rc != 0 {
			return map[string]interface{}{"changed": false, "rc": 0, "stdout": "", "msg": fmt.Sprintf("Did not run command since '%s' does not exist", removes)}, nil
		}
	}
	return nil, nil
}

func (r *pbRunner) runPbCommand(t *pbTaskCtx, module string, args map[string]interface{}) (map[string]interface{}, error) {
	res, err := r.testPbPath(t, args)
	if err != nil || res != nil {
		return res, err
	}
	if t.check {
		return map[string]interface{}{"changed": false, "skipped": true, "msg": "Command would have run if not in check mode"}, nil
	}

	raw := pbArg(args, "_raw_params")
	var cmd string
	switch module {
	case "command":
		parts := pbStrings(args["argv"])
		if len(parts) == 0 {
			parts, err = splitPbArgs(raw)
			if err != nil {
				return nil, err
			}
		}
		quoted := []string{}
		for _, part := range parts {
			quoted = append(quoted, shellQuote(part))
		}
		cmd = strings.Join(quoted, " ")
		raw = strings.Join(parts, " ")
	case "shell":
		cmd = raw
		if executable := pbArg(args, "executable"); executable != "" {
			cmd = shellQuote(executable) + " -c " + shellQuote(raw)
		}
	case "raw":
		cmd = raw
	case "script":
		parts, err := splitPbArgs(raw)
		if err != nil || len(parts) == 0 {
			return nil, fmt.Errorf("the module: script needs a file")
		}
		f, _, err := r.openPbFile(parts[0])
		if err != nil {
			return nil, err
		}
		tmp, err := r.uploadTemp(t, f, true)
		_ = f.Close()
		if err != nil {
			return nil, err
		}
		defer r.removeTemp(tmp)
		cmd = shellQuote(tmp)
		if executable := pbArg(args, "executable"); executable != "" {
			cmd = shellQuote(executable) + " " + cmd
		}
		if rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), parts[0])); rest != "" {
			cmd += " " + rest
		}
	}
	if chdir := pbArg(args, "chdir"); chdir != "" && module != "raw" {
		cmd = "cd " + shellQuote(chdir) + " && " + cmd
	}

	start := time.Now()
	rc, stdout, stderr, err := r.exec(t, cmd, true)
	if err != nil {
		return nil, err
	}
	res = pbCommandResult(raw, rc, stdout, stderr)
	res["start"] = start.Format("2006-01-02 15:04:05.000000")
	res["delta"] = time.Since(start).String()
	return res, nil
}

type pbStat struct {
	exists bool
	kind   string
	mode   string
	owner  string
	group  string
}

func (r *pbRunner) stat(t *pbTaskCtx, p string) (*pbStat, error) {
	rc, out, _, err := r.sh(t, "stat -c '%F|%a|%U|%G' -- "+shellQuote(p))
	if err != nil {
		return nil, err
	}
	if rc != 0 {
		return &pbStat{}, nil
	}
	parts := strings.Split(strings.TrimSpace(out), "|")
	if len(parts) < 4 {
		return nil, fmt.Errorf("unexpected output of stat: %s", out)
	}
	return &pbStat{exists: true, kind: parts[0], mode: parts[1], owner: parts[2], group: parts[3]}, nil
}

func (s *pbStat) isDir() bool {
	return s.kind == "directory"
}

func (s *pbStat) isLink() bool {
	return s.kind == "symbolic link"
}

// getPbMode is the mode option as chmod takes it: an integer from YAML is octal, as in Ansible.
func getPbMode(args map[string]interface{}) (string, error) {
	v, ok := args["mode"]
	if !ok || v == nil {
		return "", nil
	}
	if n, ok := v.(int); ok {
		return fmt.Sprintf("%04o", n), nil
	}
	mode := strings.TrimSpace(pbToString(v))
	if mode == "" {
		return "", nil
	}
	if n, err := strconv.ParseUint(mode, 8, 32); err == nil {
		return fmt.Sprintf("%04o", n), nil
	}
	if !regexp.MustCompile(`^[ugoa]*[-+=][rwxXst]*(,[ugoa]*[-+=][rwxXst]*)*$`).MatchString(mode) {
		return "", fmt.Errorf("invalid mode: %s", mode)
	}
	return mode, nil
}

func sameMode(a string, b string) bool {
	x, err1 := strconv.ParseUint(a, 8, 32)
	y, err2 := strconv.ParseUint(b, 8, 32)
	return err1 == nil && err2 == nil && x == y
}

// applyPbAttrs gives a path the mode, owner and group of the options, and tells whether anything changed.
func (r *pbRunner) applyPbAttrs(t *pbTaskCtx, p string, args map[string]interface{}, recurse bool) (bool, error) {
	mode, err := getPbMode(args)
	if err != nil {
		return false, err
	}
	owner, group := pbArg(args, "owner"), pbArg(args, "group")
	if mode == "" && owner == "" && group == "" {
		return false, nil
	}

	if recurse {
		_, octalErr := strconv.ParseUint(mode, 8, 32)
		symbolic := mode != "" && octalErr != nil
		checks := []string{}
		if owner != "" {
			checks = append(checks, "! -user "+shellQuote(owner))
		}
		if group != "" {
			checks = append(checks, "! -group "+shellQuote(group))
		}
		if mode != "" && !symbolic {
			checks = append(checks, "! -perm "+mode)
		}
		changed := false
		if len(checks) > 0 {
			out, err := r.shOk(t, fmt.Sprintf("find %s \\( %s \\) -print | head -n 1", shellQuote(p), strings.Join(checks, " -o ")))
			if err != nil {
				return false, err
			}
			changed = strings.TrimSpace(out) != ""
		}
		if t.check || !changed && !symbolic {
			return changed, nil
		}
		changed = true
		if owner != "" || group != "" {
			if _, err = r.shOk(t, "chown -R "+shellQuote(owner+pbGroupSuffix(group))+" -- "+shellQuote(p)); err != nil {
				return false, err
			}
		}
		if mode != "" {
			if _, err = r.shOk(t, "chmod -R "+shellQuote(mode)+" -- "+shellQuote(p)); err != nil {
				return false, err
			}
		}
		return changed, nil
	}

	st, err := r.stat(t, p)
	if err != nil {
		return false, err
	}
	if !st.exists {
		if t.check {
			return true, nil
		}
		return false, fmt.Errorf("%s does not exist", p)
	}
	changed := false
	if (owner != "" && owner != st.owner) || (group != "" && group != st.group) {
		changed = true
		if !t.check {
			if _, err = r.shOk(t, "chown "+shellQuote(owner+pbGroupSuffix(group))+" -- "+shellQuote(p)); err != nil {
				return false, err
			}
		}
	}
	if mode != "" && !sameMode(mode, st.mode) {
		if _, err := strconv.ParseUint(mode, 8, 32); err == nil {
			changed = true
			if !t.check {
				if _, err = r.shOk(t, "chmod "+mode+" -- "+shellQuote(p)); err != nil {
					return false, err
				}
			}
		} else if !t.check {
			if _, err = r.shOk(t, "chmod "+shellQuote(mode)+" -- "+shellQuote(p)); err != nil {
				return false, err
			}
			after, err := r.stat(t, p)
			if err != nil {
				return false, err
			}
			changed = changed || after.mode != st.mode
		}
	}
	return changed, nil
}

func pbGroupSuffix(group string) string {
	if group == "" {
		return ""
	}
	return ":" + group
}

func (r *pbRunner) checksum(t *pbTaskCtx, p string) (string, error) {
	rc, out, _, err := r.sh(t, fmt.Sprintf("[ -f %s ] && { sha256sum -- %s 2>/dev/null || shasum -a 256 -- %s; }", shellQuote(p), shellQuote(p), shellQuote(p)))
	if err != nil {
		return "", err
	}
	if rc != 0 {
		return "", nil
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], nil
}

// writePbFile puts data at p through a temporary file, as the task's user, keeping the mode of a file already there.
func (r *pbRunner) writePbFile(t *pbTaskCtx, p string, data io.Reader, backup bool) (string, error) {
	tmp, err := r.uploadTemp(t, data, false)
	if err != nil {
		return "", err
	}
	defer r.removeTemp(tmp)

	backupFile := ""
	cmd := ""
	if backup {
		backupFile = fmt.Sprintf("%s.%s~", p, time.Now().Format("2006-01-02@15:04:05"))
		cmd = fmt.Sprintf("if [ -f %s ]; then cp -p -- %s %s; fi && ", shellQuote(p), shellQuote(p), shellQuote(backupFile))
	}
	cmd += fmt.Sprintf("cat -- %s > %s", shellQuote(tmp), shellQuote(p))
	_, err = r.shOk(t, cmd)
	return backupFile, err
}

func (r *pbRunner) runPbCopy(t *pbTaskCtx, module string, args map[string]interface{}) (map[string]interface{}, error) {
	dest := pbArg(args, "dest")
	if dest == "" {
		return nil, fmt.Errorf("dest is required")
	}

	var data []byte
	var name string
	if module == "template" || pbHasArg(args, "src") {
		src := pbArg(args, "src")
		content, err := r.readPbFile(src)
		if err != nil {
			return nil, err
		}
		name = path.Base(strings.ReplaceAll(src, "\\", "/"))
		data = content
		if module == "template" {
			tmpl, err := parsePbTemplate(string(content))
			if err != nil {
				return nil, fmt.Errorf("the template: %s: %s", name, err.Error())
			}
			value, err := tmpl.render(t.scope)
			if err != nil {
				return nil, fmt.Errorf("the template: %s: %s", name, err.Error())
			}
			data = []byte(pbToString(value))
		}
	} else {
		switch content := args["content"].(type) {
		case string:
			data = []byte(content)
		default:
			encoded, err := json.Marshal(content)
			if err != nil {
				return nil, err
			}
			data = encoded
		}
	}

	st, err := r.stat(t, dest)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(dest, "/") || st.isDir() {
		if name == "" {
			return nil, fmt.Errorf("dest: %s is a directory, content needs a file", dest)
		}
		dest = path.Join(dest, name)
		if st, err = r.stat(t, dest); err != nil {
			return nil, err
		}
	}

	sum := sha256.Sum256(data)
	checksum := hex.EncodeToString(sum[:])
	res := map[string]interface{}{"dest": dest, "checksum": checksum, "size": len(data), "changed": false}

	force := !pbHasArg(args, "force") || pbToBool(args["force"])
	if st.exists && !force {
		res["msg"] = "dest exists and force is no"
		return res, nil
	}
	remote := ""
	if st.exists {
		if remote, err = r.checksum(t, dest); err != nil {
			return nil, err
		}
	}

	if remote != checksum {
		res["changed"] = true
		if !t.check {
			parent := path.Dir(dest)
			if parentStat, err := r.stat(t, parent); err != nil {
				return nil, err
			} else if !parentStat.exists {
				return nil, fmt.Errorf("Destination directory %s does not exist", parent)
			}
			backupFile, err := r.writePbFile(t, dest, bytes.NewReader(data), pbToBool(args["backup"]))
			if err != nil {
				return nil, err
			}
			if backupFile != "" && st.exists {
				res["backup_file"] = backupFile
			}
		}
	}
	if !t.check || st.exists {
		changed, err := r.applyPbAttrs(t, dest, args, false)
		if err != nil {
			return nil, err
		}
		res["changed"] = pbToBool(res["changed"]) || changed
	}
	return res, nil
}

func (r *pbRunner) runPbFile(t *pbTaskCtx, args map[string]interface{}) (map[string]interface{}, error) {
	p := pbArg(args, "path", "dest", "name")
	st, err := r.stat(t, p)
	if err != nil {
		return nil, err
	}
	state := pbArg(args, "state")
	if state == "" {
		state = "file"
		if st.isDir() {
			state = "directory"
		}
	}
	res := map[string]interface{}{"path": p, "state": state, "changed": false}
	run := func(cmd string) error {
		res["changed"] = true
		if t.check {
			return nil
		}
		_, err := r.shOk(t, cmd)
		return err
	}

	switch state {
	case "absent":
		if st.exists {
			if err = run("rm -rf -- " + shellQuote(p)); err != nil {
				return nil, err
			}
		}
		return res, nil
	case "directory":
		if st.exists && !st.isDir() {
			return nil, fmt.Errorf("%s already exists as a %s", p, st.kind)
		}
		if !st.exists {
			if err = run("mkdir -p -- " + shellQuote(p)); err != nil {
				return nil, err
			}
		}
	case "file":
		if !st.exists {
			return nil, fmt.Errorf("file (%s) is absent, cannot continue", p)
		}
	case "touch":
		if err = run("touch -- " + shellQuote(p)); err != nil {
			return nil, err
		}
	case "link":
		src := pbArg(args, "src")
		if src == "" {
			return nil, fmt.Errorf("src is required for a link")
		}
		current := ""
		if st.isLink() {
			out, err := r.shOk(t, "readlink -- "+shellQuote(p))
			if err != nil {
				return nil, err
			}
			current = strings.TrimSpace(out)
		} else if st.exists && !pbToBool(args["force"]) {
			return nil, fmt.Errorf("%s already exists as a %s, set force to replace it", p, st.kind)
		}
		if current != src {
			if err = run(fmt.Sprintf("ln -sfn -- %s %s", shellQuote(src), shellQuote(p))); err != nil {
				return nil, err
			}
		}
		return res, nil
	}

	if t.check && !st.exists {
		return res, nil
	}
	changed, err := r.applyPbAttrs(t, p, args, state == "directory" && pbToBool(args["recurse"]))
	if err != nil {
		return nil, err
	}
	res["changed"] = pbToBool(res["changed"]) || changed
	return res, nil
}

func (r *pbRunner) runPbLineinfile(t *pbTaskCtx, args map[string]interface{}) (map[string]interface{}, error) {
	p := pbArg(args, "path", "dest", "name")
	state := pbArg(args, "state")
	if state == "" {
		state = "present"
	}
	line := pbArg(args, "line")
	if state == "present" && !pbHasArg(args, "line") {
		return nil, fmt.Errorf("line is required with state=present")
	}
	var re *regexp.Regexp
	if expr := pbArg(args, "regexp", "regex"); expr != "" {
		var err error
		re, err = regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("invalid regexp: %s", err.Error())
		}
	}
	search := pbArg(args, "search_string")
	backrefs := pbToBool(args["backrefs"])
	firstmatch := pbToBool(args["firstmatch"])
	if backrefs && re == nil {
		return nil, fmt.Errorf("regexp is required with backrefs")
	}

	st, err := r.stat(t, p)
	if err != nil {
		return nil, err
	}
	content := ""
	if st.exists {
		content, err = r.shOk(t, "cat -- "+shellQuote(p))
		if err != nil {
			return nil, err
		}
	} else if state == "absent" {
		return map[string]interface{}{"changed": false, "found": 0, "msg": "file not present"}, nil
	} else if !pbToBool(args["create"]) {
		return nil, fmt.Errorf("Destination %s does not exist !", p)
	}

	lines := []string{}
	if content != "" {
		lines = strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	}
	matches := func(l string) bool {
		if re != nil {
			return re.MatchString(l)
		}
		if search != "" {
			return strings.Contains(l, search)
		}
		return l == line
	}

	res := map[string]interface{}{"changed": false, "msg": ""}
	changed := false
	if state == "absent" {
		kept := []string{}
		found := 0
		for _, l := range lines {
			if matches(l) {
				found++
				continue
			}
			kept = append(kept, l)
		}
		res["found"] = found
		if found > 0 {
			lines, changed = kept, true
			res["msg"] = fmt.Sprintf("%d line(s) removed", found)
		}
	} else {
		index := -1
		if re != nil || search != "" {
			for i, l := range lines {
				if matches(l) {
					index = i
					if firstmatch {
						break
					}
				}
			}
		}
		if index >= 0 {
			newLine := line
			if backrefs {
				m := re.FindStringSubmatchIndex(lines[index])
				newLine = string(re.ExpandString(nil, regexp.MustCompile(`\\(\d+)`).ReplaceAllString(line, "$${$1}"), lines[index], m))
			}
			if lines[index] != newLine {
				lines[index] = newLine
				changed = true
				res["msg"] = "line replaced"
			}
		} else if !backrefs && !slices.Contains(lines, line) {
			pos := len(lines)
			after, before := pbArg(args, "insertafter"), pbArg(args, "insertbefore")
			if before == "BOF" {
				pos = 0
			} else if before != "" || after != "" && after != "EOF" {
				expr := before
				if expr == "" {
					expr = after
				}
				anchor, err := regexp.Compile(expr)
				if err != nil {
					return nil, fmt.Errorf("invalid regular expression: %s", err.Error())
				}
				for i, l := range lines {
					if anchor.MatchString(l) {
						pos = i
						if before == "" {
							pos = i + 1
						}
						if firstmatch {
							break
						}
					}
				}
			}
			lines = slices.Insert(lines, pos, line)
			changed = true
			res["msg"] = "line added"
		}
	}

	if changed && !t.check {
		data := strings.Join(lines, "\n")
		if len(lines) > 0 {
			data += "\n"
		}
		backupFile, err := r.writePbFile(t, p, strings.NewReader(data), pbToBool(args["backup"]))
		if err != nil {
			return nil, err
		}
		if backupFile != "" && st.exists {
			res["backup"] = backupFile
		}
	}
	res["changed"] = changed
	if !t.check || st.exists {
		attrsChanged, err := r.applyPbAttrs(t, p, args, false)
		if err != nil {
			return nil, err
		}
		res["changed"] = changed || attrsChanged
	}
	return res, nil
}

func getPbPackages(args map[string]interface{}) []string {
	res := []string{}
	for _, key := range []string{"name", "pkg", "package"} {
		for _, item := range pbStrings(args[key]) {
			for _, name := range strings.Split(item, ",") {
				if name = strings.TrimSpace(name); name != "" {
					res = append(res, name)
				}
			}
		}
	}
	return res
}

var pbPackageNameRegex = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+_:~*-]*([=<>]{1,2}[A-Za-z0-9.+_:~*-]+)?$`)

func checkPbPackages(packages []string) error {
	for _, name := range packages {
		if !pbPackageNameRegex.MatchString(name) {
			return fmt.Errorf("invalid package name: %s", name)
		}
	}
	return nil
}

func quotePbAll(items []string) string {
	quoted := []string{}
	for _, item := range items {
		quoted = append(quoted, shellQuote(item))
	}
	return strings.Join(quoted, " ")
}

func (r *pbRunner) runPbApt(t *pbTaskCtx, args map[string]interface{}) (map[string]interface{}, error) {
	packages := getPbPackages(args)
	if err := checkPbPackages(packages); err != nil {
		return nil, err
	}
	state := pbArg(args, "state")
	if state == "" || state == "installed" {
		state = "present"
	}
	if state == "removed" {
		state = "absent"
	}
	res := map[string]interface{}{"changed": false}
	stdout := ""

	if pbToBool(args["update_cache"]) {
		res["cache_updated"] = !t.check
		if !t.check {
			out, err := r.shOk(t, "apt-get update -q")
			if err != nil {
				return nil, err
			}
			stdout += out
		}
		if len(packages) == 0 {
			res["changed"] = true
			res["stdout"] = stdout
			return res, nil
		}
	}

	versions := func() (map[string]string, error) {
		names := []string{}
		for _, name := range packages {
			names = append(names, strings.SplitN(name, "=", 2)[0])
		}
		_, out, _, err := r.sh(t, "dpkg-query -W -f='${Package} ${Status} ${Version}\\n' "+quotePbAll(names)+" 2>/dev/null")
		if err != nil {
			return nil, err
		}
		res := map[string]string{}
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 5 && fields[3] == "installed" {
				res[strings.SplitN(fields[0], ":", 2)[0]] = fields[4]
			}
		}
		return res, nil
	}
	before, err := versions()
	if err != nil {
		return nil, err
	}

	env := "DEBIAN_FRONTEND=noninteractive "
	opts := "-y -q -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold"
	if v, ok := args["install_recommends"]; ok {
		if pbToBool(v) {
			opts += " --install-recommends"
		} else {
			opts += " --no-install-recommends"
		}
	}
	switch state {
	case "present", "latest":
		targets := packages
		if state == "present" {
			targets = []string{}
			for _, name := range packages {
				if _, ok := before[strings.SplitN(name, "=", 2)[0]]; !ok || strings.Contains(name, "=") {
					targets = append(targets, name)
				}
			}
		}
		if len(targets) == 0 {
			break
		}
		if t.check || state == "latest" {
			out, err := r.shOk(t, env+"apt-get install -s "+opts+" "+quotePbAll(targets))
			if err != nil {
				return nil, err
			}
			if !strings.Contains(out, "\nInst ") && !strings.HasPrefix(out, "Inst ") {
				break
			}
			if t.check {
				res["changed"] = true
				break
			}
		}
		out, err := r.shOk(t, env+"apt-get install "+opts+" "+quotePbAll(targets))
		if err != nil {
			return nil, err
		}
		stdout += out
		after, err := versions()
		if err != nil {
			return nil, err
		}
		res["changed"] = !mapsEqual(before, after)
	case "absent":
		targets := []string{}
		for _, name := range packages {
			if _, ok := before[name]; ok {
				targets = append(targets, name)
			}
		}
		if len(targets) == 0 {
			break
		}
		res["changed"] = true
		if t.check {
			break
		}
		command := "remove"
		if pbToBool(args["purge"]) {
			command = "purge"
		}
		if pbToBool(args["autoremove"]) {
			command += " --autoremove"
		}
		out, err := r.shOk(t, env+"apt-get "+command+" "+opts+" "+quotePbAll(targets))
		if err != nil {
			return nil, err
		}
		stdout += out
	}
	res["stdout"] = stdout
	return res, nil
}

func mapsEqual(a map[string]string, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func (r *pbRunner) runPbPackage(t *pbTaskCtx, module string, args map[string]interface{}) (map[string]interface{}, error) {
	if module == "package" && (r.facts["ansible_pkg_mgr"] == "apt" || r.facts["ansible_pkg_mgr"] == nil) {
		rc, _, _, err := r.sh(t, "command -v apt-get")
		if err != nil {
			return nil, err
		}
		if rc == 0 {
			return r.runPbApt(t, args)
		}
	}

	manager := module
	if module != "dnf" {
		rc, _, _, err := r.sh(t, "command -v dnf")
		if err != nil {
			return nil, err
		}
		if rc == 0 && module == "package" {
			manager = "dnf"
		} else if rc2, _, _, err := r.sh(t, "command -v yum"); err == nil && rc2 == 0 {
			manager = "yum"
		} else if rc == 0 {
			manager = "dnf"
		} else {
			return nil, fmt.Errorf("neither yum nor dnf is found on the asset")
		}
	}

	packages := getPbPackages(args)
	if err := checkPbPackages(packages); err != nil {
		return nil, err
	}
	state := pbArg(args, "state")
	if state == "" || state == "installed" {
		state = "present"
	}
	if state == "removed" {
		state = "absent"
	}
	res := map[string]interface{}{"changed": false}
	stdout := ""
	if pbToBool(args["update_cache"]) && !t.check {
		out, err := r.shOk(t, manager+" -q -y makecache")
		if err != nil {
			return nil, err
		}
		stdout += out
	}
	if len(packages) == 0 {
		res["stdout"] = stdout
		return res, nil
	}

	versions := func() (map[string]string, error) {
		_, out, _, err := r.sh(t, "rpm -q --qf '%{NAME} %{VERSION}-%{RELEASE}.%{ARCH}\\n' "+quotePbAll(packages)+" 2>/dev/null")
		if err != nil {
			return nil, err
		}
		res := map[string]string{}
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 {
				res[fields[0]] = res[fields[0]] + fields[1] + " "
			}
		}
		return res, nil
	}
	before, err := versions()
	if err != nil {
		return nil, err
	}

	switch state {
	case "present", "latest":
		missing := []string{}
		for _, name := range packages {
			if _, ok := before[name]; !ok {
				missing = append(missing, name)
			}
		}
		if state == "latest" {
			rc, _, _, err := r.sh(t, manager+" -q check-update "+quotePbAll(packages))
			if err != nil {
				return nil, err
			}
			if rc == 100 || len(missing) > 0 {
				res["changed"] = true
				if !t.check {
					out, err := r.shOk(t, manager+" -y install "+quotePbAll(packages)+" && "+manager+" -y upgrade "+quotePbAll(packages))
					if err != nil {
						return nil, err
					}
					stdout += out
				}
			}
			break
		}
		if len(missing) == 0 {
			break
		}
		res["changed"] = true
		if !t.check {
			out, err := r.shOk(t, manager+" -y install "+quotePbAll(missing))
			if err != nil {
				return nil, err
			}
			stdout += out
		}
	case "absent":
		installed := []string{}
		for _, name := range packages {
			if _, ok := before[name]; ok {
				installed = append(installed, name)
			}
		}
		if len(installed) == 0 {
			break
		}
		res["changed"] = true
		if !t.check {
			out, err := r.shOk(t, manager+" -y remove "+quotePbAll(installed))
			if err != nil {
				return nil, err
			}
			stdout += out
		}
	}
	if !t.check && pbToBool(res["changed"]) {
		after, err := versions()
		if err != nil {
			return nil, err
		}
		res["changed"] = !mapsEqual(before, after)
	}
	res["stdout"] = stdout
	return res, nil
}

var pbServiceNameRegex = regexp.MustCompile(`^[A-Za-z0-9@._:-]+$`)

func (r *pbRunner) runPbService(t *pbTaskCtx, module string, args map[string]interface{}) (map[string]interface{}, error) {
	name := pbArg(args, "name")
	if name != "" && !pbServiceNameRegex.MatchString(name) {
		return nil, fmt.Errorf("invalid service name: %s", name)
	}
	rc, _, _, err := r.sh(t, "[ -d /run/systemd/system ]")
	if err != nil {
		return nil, err
	}
	systemd := rc == 0
	if module == "systemd" && !systemd {
		return nil, fmt.Errorf("the asset doesn't run systemd")
	}

	res := map[string]interface{}{"name": name, "changed": false}
	act := func(cmd string) error {
		res["changed"] = true
		if t.check {
			return nil
		}
		_, err := r.shOk(t, cmd)
		return err
	}

	if pbToBool(args["daemon_reload"]) || pbToBool(args["daemon-reload"]) {
		if !t.check {
			if _, err = r.shOk(t, "systemctl daemon-reload"); err != nil {
				return nil, err
			}
		}
	}
	if name == "" {
		return res, nil
	}

	q := shellQuote(name)
	if v, ok := args["enabled"]; ok {
		enabled := pbToBool(v)
		if !systemd {
			return nil, fmt.Errorf("enabled is only supported with systemd")
		}
		_, out, _, err := r.sh(t, "systemctl is-enabled "+q)
		if err != nil {
			return nil, err
		}
		current := strings.TrimSpace(out) == "enabled"
		if current != enabled {
			cmd := "systemctl disable " + q
			if enabled {
				cmd = "systemctl enable " + q
			}
			if err = act(cmd); err != nil {
				return nil, err
			}
		}
		res["enabled"] = enabled
	}

	state := pbArg(args, "state")
	if state == "" {
		return res, nil
	}
	statusCmd := "service " + q + " status"
	control := func(action string) string { return "service " + q + " " + action }
	if systemd {
		statusCmd = "systemctl is-active --quiet " + q
		control = func(action string) string { return "systemctl " + action + " " + q }
	}
	rc, _, _, err = r.sh(t, statusCmd)
	if err != nil {
		return nil, err
	}
	running := rc == 0
	switch state {
	case "started":
		if !running {
			err = act(control("start"))
		}
	case "stopped":
		if running {
			err = act(control("stop"))
		}
	case "restarted":
		err = act(control("restart"))
	case "reloaded":
		action := "reload"
		if !running {
			action = "start"
		}
		err = act(control(action))
	}
	if err != nil {
		return nil, err
	}
	res["state"] = state
	return res, nil
}

var pbAccountNameRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*\$?$`)

func (r *pbRunner) runPbUser(t *pbTaskCtx, args map[string]interface{}) (map[string]interface{}, error) {
	name := pbArg(args, "name")
	if !pbAccountNameRegex.MatchString(name) {
		return nil, fmt.Errorf("invalid user name: %s", name)
	}
	state := pbArg(args, "state")
	if state == "" {
		state = "present"
	}
	res := map[string]interface{}{"name": name, "state": state, "changed": false}
	run := func(cmd string) error {
		res["changed"] = true
		if t.check {
			return nil
		}
		_, err := r.shOk(t, cmd)
		return err
	}

	rc, out, _, err := r.sh(t, "getent passwd "+shellQuote(name))
	if err != nil {
		return nil, err
	}
	exists := rc == 0
	if state == "absent" {
		if exists {
			cmd := "userdel "
			if pbToBool(args["remove"]) {
				cmd += "-r "
			}
			if err = run(cmd + shellQuote(name)); err != nil {
				return nil, err
			}
		}
		return res, nil
	}

	groups := []string{}
	for _, item := range pbStrings(args["groups"]) {
		for _, group := range strings.Split(item, ",") {
			if group = strings.TrimSpace(group); group != "" {
				groups = append(groups, group)
			}
		}
	}

	opts := []string{}
	add := func(flag string, key string) {
		if v := pbArg(args, key); v != "" {
			opts = append(opts, flag, shellQuote(v))
		}
	}
	if !exists {
		add("-u", "uid")
		add("-g", "group")
		add("-s", "shell")
		add("-d", "home")
		add("-c", "comment")
		add("-p", "password")
		if len(groups) > 0 {
			opts = append(opts, "-G", shellQuote(strings.Join(groups, ",")))
		}
		createHome := true
		if v, ok := args["create_home"]; ok {
			createHome = pbToBool(v)
		} else if v, ok := args["createhome"]; ok {
			createHome = pbToBool(v)
		}
		if createHome {
			opts = append(opts, "-m")
		} else {
			opts = append(opts, "-M")
		}
		if pbToBool(args["system"]) {
			opts = append(opts, "-r")
		}
		if err = run("useradd " + strings.Join(append(opts, shellQuote(name)), " ")); err != nil {
			return nil, err
		}
		return res, nil
	}

	fields := strings.Split(strings.TrimSpace(out), ":")
	for len(fields) < 7 {
		fields = append(fields, "")
	}
	if v := pbArg(args, "uid"); v != "" && v != fields[2] {
		opts = append(opts, "-u", shellQuote(v))
	}
	if v := pbArg(args, "comment"); pbHasArg(args, "comment") && v != fields[4] {
		opts = append(opts, "-c", shellQuote(v))
	}
	if v := pbArg(args, "home"); v != "" && v != fields[5] {
		opts = append(opts, "-d", shellQuote(v))
	}
	if v := pbArg(args, "shell"); v != "" && v != fields[6] {
		opts = append(opts, "-s", shellQuote(v))
	}
	if v := pbArg(args, "group"); v != "" {
		current, err := r.shOk(t, "id -gn "+shellQuote(name))
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(current) != v {
			opts = append(opts, "-g", shellQuote(v))
		}
	}
	if pbHasArg(args, "groups") {
		current, err := r.shOk(t, "id -Gn "+shellQuote(name))
		if err != nil {
			return nil, err
		}
		currentGroups := strings.Fields(current)
		if len(currentGroups) > 0 {
			currentGroups = currentGroups[1:]
		}
		if pbToBool(args["append"]) {
			missing := []string{}
			for _, group := range groups {
				if !slices.Contains(currentGroups, group) {
					missing = append(missing, group)
				}
			}
			if len(missing) > 0 {
				opts = append(opts, "-a", "-G", shellQuote(strings.Join(missing, ",")))
			}
		} else {
			a, b := slices.Clone(groups), slices.Clone(currentGroups)
			slices.Sort(a)
			slices.Sort(b)
			if !slices.Equal(slices.Compact(a), slices.Compact(b)) {
				opts = append(opts, "-G", shellQuote(strings.Join(groups, ",")))
			}
		}
	}
	if v := pbArg(args, "password"); v != "" {
		_, shadow, _, err := r.sh(t, "getent shadow "+shellQuote(name))
		if err != nil {
			return nil, err
		}
		parts := strings.Split(strings.TrimSpace(shadow), ":")
		if len(parts) < 2 || parts[1] != v {
			opts = append(opts, "-p", shellQuote(v))
		}
	}
	if len(opts) > 0 {
		if err = run("usermod " + strings.Join(append(opts, shellQuote(name)), " ")); err != nil {
			return nil, err
		}
	}
	return res, nil
}

func (r *pbRunner) runPbGroup(t *pbTaskCtx, args map[string]interface{}) (map[string]interface{}, error) {
	name := pbArg(args, "name")
	if !pbAccountNameRegex.MatchString(name) {
		return nil, fmt.Errorf("invalid group name: %s", name)
	}
	state := pbArg(args, "state")
	if state == "" {
		state = "present"
	}
	res := map[string]interface{}{"name": name, "state": state, "changed": false}
	run := func(cmd string) error {
		res["changed"] = true
		if t.check {
			return nil
		}
		_, err := r.shOk(t, cmd)
		return err
	}

	rc, out, _, err := r.sh(t, "getent group "+shellQuote(name))
	if err != nil {
		return nil, err
	}
	exists := rc == 0
	gid := pbArg(args, "gid")
	switch {
	case state == "absent" && exists:
		err = run("groupdel " + shellQuote(name))
	case state == "present" && !exists:
		cmd := "groupadd "
		if gid != "" {
			cmd += "-g " + shellQuote(gid) + " "
		}
		if pbToBool(args["system"]) {
			cmd += "-r "
		}
		err = run(cmd + shellQuote(name))
	case state == "present" && gid != "":
		fields := strings.Split(strings.TrimSpace(out), ":")
		if len(fields) >= 3 && fields[2] != gid {
			err = run("groupmod -g " + shellQuote(gid) + " " + shellQuote(name))
		}
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (r *pbRunner) runPbGetUrl(t *pbTaskCtx, args map[string]interface{}) (map[string]interface{}, error) {
	rawUrl := pbArg(args, "url")
	if !strings.HasPrefix(rawUrl, "http://") && !strings.HasPrefix(rawUrl, "https://") && !strings.HasPrefix(rawUrl, "ftp://") {
		return nil, fmt.Errorf("only http, https and ftp URLs are supported: %s", rawUrl)
	}
	dest := pbArg(args, "dest")
	checksum := strings.ToLower(strings.TrimPrefix(pbArg(args, "checksum"), "sha256:"))
	if pbArg(args, "checksum") != "" && !strings.HasPrefix(pbArg(args, "checksum"), "sha256:") {
		return nil, fmt.Errorf("only sha256:<hex> checksums are supported")
	}
	timeout := 10
	if v := pbArg(args, "timeout"); v != "" {
		timeout, _ = strconv.Atoi(v)
	}

	st, err := r.stat(t, dest)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(dest, "/") || st.isDir() {
		base := path.Base(strings.SplitN(strings.SplitN(rawUrl, "?", 2)[0], "#", 2)[0])
		if base == "" || base == "/" || base == "." {
			return nil, fmt.Errorf("can't name the file of %s in the directory %s", rawUrl, dest)
		}
		dest = path.Join(dest, base)
		if st, err = r.stat(t, dest); err != nil {
			return nil, err
		}
	}
	res := map[string]interface{}{"url": rawUrl, "dest": dest, "changed": false}

	if st.exists {
		current, err := r.checksum(t, dest)
		if err != nil {
			return nil, err
		}
		if checksum != "" && current == checksum || checksum == "" && !pbToBool(args["force"]) {
			res["checksum_dest"] = current
			res["msg"] = "file already exists"
			changed, err := r.applyPbAttrs(t, dest, args, false)
			if err != nil {
				return nil, err
			}
			res["changed"] = changed
			return res, nil
		}
	}

	if t.check {
		res["changed"] = true
		return res, nil
	}

	headers := ""
	if h, ok := args["headers"].(map[string]interface{}); ok {
		for _, key := range pbSortedKeys(h) {
			headers += " -H " + shellQuote(key+": "+pbToString(h[key]))
		}
	}
	tmp := "/tmp/.casvisor-pb-" + strings.ReplaceAll(util.GenerateId(), "-", "")
	download := fmt.Sprintf("{ curl -fsSL --max-time %d%s -o %s %s || wget -q -T %d -O %s %s; }", timeout*6, headers, shellQuote(tmp), shellQuote(rawUrl), timeout, shellQuote(tmp), shellQuote(rawUrl))
	if headers != "" {
		download = fmt.Sprintf("curl -fsSL --max-time %d%s -o %s %s", timeout*6, headers, shellQuote(tmp), shellQuote(rawUrl))
	}
	cmd := fmt.Sprintf("trap 'rm -f %s' EXIT; %s && (sha256sum -- %s 2>/dev/null || shasum -a 256 -- %s)", shellQuote(tmp), download, shellQuote(tmp), shellQuote(tmp))
	cmd += " && " + "{ [ -z " + shellQuote(checksum) + " ] || (sha256sum -- " + shellQuote(tmp) + " 2>/dev/null || shasum -a 256 -- " + shellQuote(tmp) + ") | grep -q " + shellQuote("^"+checksum) + " || { echo 'checksum mismatch' >&2; exit 3; }; }"
	cmd += fmt.Sprintf(" && { cmp -s -- %s %s && echo SAME || { cat -- %s > %s && echo WRITTEN; }; }", shellQuote(tmp), shellQuote(dest), shellQuote(tmp), shellQuote(dest))
	out, err := r.shOk(t, cmd)
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(out)
	if len(fields) > 0 {
		res["checksum_src"] = fields[0]
	}
	res["changed"] = strings.Contains(out, "WRITTEN")
	changed, err := r.applyPbAttrs(t, dest, args, false)
	if err != nil {
		return nil, err
	}
	res["changed"] = pbToBool(res["changed"]) || changed
	return res, nil
}
