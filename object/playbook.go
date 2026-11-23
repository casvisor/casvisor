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
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// A playbook of Ansible, run by Casvisor itself over the SSH connections of jobs: the plays,
// their tasks and handlers, with the modules, keywords and Jinja2 that playbooks use most.

type pbPlay struct {
	Index      int
	Name       string
	Hosts      []string
	Vars       map[string]interface{}
	Become     *bool
	BecomeUser string
	Facts      bool
	CheckMode  *bool
	Ignore     bool
	Env        map[string]interface{}
	Tasks      []*pbTask
	Handlers   []*pbTask
}

type pbTask struct {
	Name         string
	Label        string
	Module       string
	Args         interface{}
	Vars         map[string]interface{}
	When         []string
	Register     string
	Loop         interface{}
	WithItems    bool
	LoopVar      string
	LoopLabel    string
	IndexVar     string
	IgnoreErrors interface{}
	ChangedWhen  []string
	FailedWhen   []string
	Notify       []string
	Listen       []string
	Become       *bool
	BecomeUser   string
	CheckMode    *bool
	NoLog        bool
	Env          map[string]interface{}
}

type PlaybookSummary struct {
	Plays    int      `json:"plays"`
	Tasks    int      `json:"tasks"`
	Handlers int      `json:"handlers"`
	Modules  []string `json:"modules"`
	Files    []string `json:"files"`
}

var pbModuleAliases = map[string]string{
	"systemd_service": "systemd",
}

var pbModules = map[string]bool{
	"command": true, "shell": true, "script": true, "raw": true, "copy": true, "template": true, "file": true,
	"lineinfile": true, "apt": true, "yum": true, "dnf": true, "package": true, "service": true, "systemd": true,
	"user": true, "group": true, "get_url": true, "debug": true, "set_fact": true, "fail": true, "assert": true,
	"ping": true,
}

var pbFreeFormModules = map[string]bool{"command": true, "shell": true, "script": true, "raw": true}

var pbTaskKeys = map[string]bool{
	"name": true, "when": true, "register": true, "loop": true, "with_items": true, "with_list": true,
	"loop_control": true, "ignore_errors": true, "changed_when": true, "failed_when": true, "notify": true,
	"tags": true, "become": true, "become_user": true, "become_method": true, "vars": true, "args": true,
	"check_mode": true, "no_log": true, "environment": true, "listen": true,
}

var pbPlayKeys = map[string]bool{
	"name": true, "hosts": true, "vars": true, "become": true, "become_user": true, "become_method": true,
	"gather_facts": true, "tasks": true, "pre_tasks": true, "post_tasks": true, "handlers": true, "tags": true,
	"check_mode": true, "ignore_errors": true, "environment": true, "strategy": true,
}

var pbModuleArgs = map[string][]string{
	"command":    {"cmd", "argv", "chdir", "creates", "removes", "stdin", "free_form", "_raw_params"},
	"shell":      {"cmd", "chdir", "creates", "removes", "executable", "stdin", "free_form", "_raw_params"},
	"script":     {"cmd", "chdir", "creates", "removes", "executable", "free_form", "_raw_params"},
	"raw":        {"free_form", "_raw_params", "executable"},
	"copy":       {"src", "content", "dest", "mode", "owner", "group", "force", "backup"},
	"template":   {"src", "dest", "mode", "owner", "group", "force", "backup"},
	"file":       {"path", "dest", "name", "state", "src", "mode", "owner", "group", "recurse", "force"},
	"lineinfile": {"path", "dest", "name", "line", "regexp", "regex", "search_string", "state", "insertafter", "insertbefore", "create", "backrefs", "firstmatch", "mode", "owner", "group", "backup"},
	"apt":        {"name", "pkg", "package", "state", "update_cache", "cache_valid_time", "purge", "autoremove", "install_recommends"},
	"yum":        {"name", "pkg", "state", "update_cache"},
	"dnf":        {"name", "pkg", "state", "update_cache"},
	"package":    {"name", "state"},
	"service":    {"name", "state", "enabled"},
	"systemd":    {"name", "state", "enabled", "daemon_reload", "daemon-reload"},
	"user":       {"name", "state", "uid", "group", "groups", "append", "shell", "home", "create_home", "createhome", "system", "comment", "password", "remove"},
	"group":      {"name", "state", "gid", "system"},
	"get_url":    {"url", "dest", "mode", "owner", "group", "force", "checksum", "timeout", "headers"},
	"debug":      {"msg", "var", "verbosity"},
	"set_fact":   nil,
	"fail":       {"msg"},
	"assert":     {"that", "msg", "fail_msg", "success_msg", "quiet"},
	"ping":       {"data"},
}

var (
	pbInlineArgRegex = regexp.MustCompile(`(?:^|\s)(chdir|creates|removes|executable|stdin)=("[^"]*"|'[^']*'|\S+)`)
	pbFqcnRegex      = regexp.MustCompile(`^ansible\.(builtin|legacy|posix)\.`)
)

func pbList(v interface{}) []interface{} {
	switch x := v.(type) {
	case nil:
		return nil
	case []interface{}:
		return x
	}
	return []interface{}{v}
}

func pbStrings(v interface{}) []string {
	res := []string{}
	for _, item := range pbList(v) {
		if s, ok := item.(string); ok {
			res = append(res, s)
		} else {
			res = append(res, pbToString(item))
		}
	}
	return res
}

func pbBoolPtr(v interface{}) *bool {
	if v == nil {
		return nil
	}
	b := pbToBool(v)
	return &b
}

// splitPbArgs splits key=value arguments, keeping quoted values together.
func splitPbArgs(s string) ([]string, error) {
	res := []string{}
	var b strings.Builder
	quote := byte(0)
	inToken := false
	depth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote == 0 && i+1 < len(s) && (s[i:i+2] == "{{" || s[i:i+2] == "}}" && depth > 0) {
			if c == '{' {
				depth++
			} else {
				depth--
			}
			b.WriteString(s[i : i+2])
			inToken = true
			i++
			continue
		}
		switch {
		case depth > 0:
			b.WriteByte(c)
		case quote != 0:
			if c == quote {
				quote = 0
			} else if c == '\\' && i+1 < len(s) && s[i+1] == quote {
				i++
				b.WriteByte(s[i])
			} else {
				b.WriteByte(c)
			}
		case c == '"' || c == '\'':
			quote = c
			inToken = true
		case c == ' ' || c == '\t' || c == '\n':
			if inToken {
				res = append(res, b.String())
				b.Reset()
				inToken = false
			}
		default:
			b.WriteByte(c)
			inToken = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unbalanced quotes in: %s", s)
	}
	if inToken {
		res = append(res, b.String())
	}
	return res, nil
}

func parsePbKeyValues(s string) (map[string]interface{}, error) {
	parts, err := splitPbArgs(s)
	if err != nil {
		return nil, err
	}
	res := map[string]interface{}{}
	for _, part := range parts {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("expected key=value, got: %s", part)
		}
		res[key] = value
	}
	return res, nil
}

// getPbModuleArgs is the arguments of a task's module: a mapping, or a free-form string with key=value
// options, merged with the args keyword.
func getPbModuleArgs(module string, raw interface{}, extra interface{}) (map[string]interface{}, error) {
	args := map[string]interface{}{}
	switch v := raw.(type) {
	case nil:
	case map[string]interface{}:
		for key, value := range v {
			args[key] = value
		}
	case string:
		if pbFreeFormModules[module] {
			text := v
			for _, m := range pbInlineArgRegex.FindAllStringSubmatch(text, -1) {
				args[m[1]] = strings.Trim(m[2], `"'`)
			}
			text = strings.TrimSpace(pbInlineArgRegex.ReplaceAllString(text, " "))
			if text != "" {
				args["_raw_params"] = text
			}
		} else if module == "set_fact" || module == "debug" || module == "fail" || module == "ping" || strings.Contains(v, "=") {
			kv, err := parsePbKeyValues(v)
			if err != nil {
				return nil, err
			}
			args = kv
		} else {
			return nil, fmt.Errorf("the arguments of the module: %s should be key=value pairs or a mapping", module)
		}
	default:
		return nil, fmt.Errorf("the arguments of the module: %s should be a string or a mapping", module)
	}

	if extra != nil {
		m, ok := extra.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("args should be a mapping")
		}
		for key, value := range m {
			args[key] = value
		}
	}
	if v, ok := args["cmd"]; ok && pbFreeFormModules[module] {
		args["_raw_params"] = v
		delete(args, "cmd")
	}
	if v, ok := args["free_form"]; ok {
		args["_raw_params"] = v
		delete(args, "free_form")
	}
	return args, nil
}

func getPbVars(v interface{}, where string) (map[string]interface{}, error) {
	if v == nil {
		return map[string]interface{}{}, nil
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%s: vars should be a mapping", where)
	}
	return m, nil
}

func parsePbTask(raw interface{}, where string, isHandler bool) (*pbTask, error) {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%s: a task should be a mapping", where)
	}
	task := &pbTask{}
	if name, ok := m["name"]; ok {
		task.Name = pbToString(name)
		where = fmt.Sprintf("%s (%s)", where, task.Name)
	}

	for _, key := range []string{"block", "rescue", "always", "include_tasks", "import_tasks", "include_role", "import_role", "include_vars", "import_playbook", "include"} {
		if _, ok := m[key]; ok {
			return nil, fmt.Errorf("%s: %s is not supported, put the tasks in the playbook itself", where, key)
		}
	}
	for _, key := range []string{"delegate_to", "local_action", "run_once", "until", "retries", "delay", "async", "poll", "with_dict", "with_fileglob", "with_together", "with_nested", "with_sequence", "with_subelements", "remote_user", "connection"} {
		if _, ok := m[key]; ok {
			return nil, fmt.Errorf("%s: the keyword %s is not supported", where, key)
		}
	}

	for key, value := range m {
		if pbTaskKeys[key] {
			continue
		}
		module := pbFqcnRegex.ReplaceAllString(key, "")
		if alias, ok := pbModuleAliases[module]; ok {
			module = alias
		}
		if !pbModules[module] {
			if strings.Contains(key, ".") || strings.HasPrefix(key, "win_") {
				return nil, fmt.Errorf("%s: the module: %s is not supported", where, key)
			}
			return nil, fmt.Errorf("%s: unknown keyword or unsupported module: %s", where, key)
		}
		if task.Module != "" {
			return nil, fmt.Errorf("%s: a task can only have one module, it has %s and %s", where, task.Module, module)
		}
		task.Module = module
		task.Args = value
	}
	if task.Module == "" {
		return nil, fmt.Errorf("%s: the task has no module", where)
	}
	task.Label = task.Name
	if task.Label == "" {
		task.Label = task.Module
	}

	args, err := getPbModuleArgs(task.Module, task.Args, m["args"])
	if err != nil {
		return nil, fmt.Errorf("%s: %s", where, err.Error())
	}
	task.Args = args
	allowed := pbModuleArgs[task.Module]
	if allowed != nil {
		for key := range args {
			if !slices.Contains(allowed, key) {
				return nil, fmt.Errorf("%s: the option: %s of the module: %s is not supported", where, key, task.Module)
			}
		}
	}
	err = checkPbModuleArgs(task.Module, args)
	if err != nil {
		return nil, fmt.Errorf("%s: %s", where, err.Error())
	}

	task.Vars, err = getPbVars(m["vars"], where)
	if err != nil {
		return nil, err
	}
	task.When = pbStrings(m["when"])
	task.ChangedWhen = pbStrings(m["changed_when"])
	task.FailedWhen = pbStrings(m["failed_when"])
	task.Notify = pbStrings(m["notify"])
	if isHandler {
		task.Listen = pbStrings(m["listen"])
	} else if m["listen"] != nil {
		return nil, fmt.Errorf("%s: listen is only for handlers", where)
	}
	if v, ok := m["register"]; ok {
		task.Register = pbToString(v)
		if !pbVarNameRegex.MatchString(task.Register) {
			return nil, fmt.Errorf("%s: invalid variable name to register: %s", where, task.Register)
		}
	}
	task.IgnoreErrors = m["ignore_errors"]
	task.Become = pbBoolPtr(m["become"])
	if v, ok := m["become_user"]; ok {
		task.BecomeUser = pbToString(v)
	}
	if v, ok := m["become_method"]; ok && pbToString(v) != "sudo" {
		return nil, fmt.Errorf("%s: only the sudo become_method is supported", where)
	}
	task.CheckMode = pbBoolPtr(m["check_mode"])
	task.NoLog = pbToBool(m["no_log"])
	if v, ok := m["environment"]; ok {
		env, ok := v.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("%s: environment should be a mapping", where)
		}
		task.Env = env
	}

	loops := 0
	for _, key := range []string{"loop", "with_items", "with_list"} {
		if v, ok := m[key]; ok {
			loops++
			task.Loop = v
			task.WithItems = key == "with_items"
		}
	}
	if loops > 1 {
		return nil, fmt.Errorf("%s: use only one of loop, with_items and with_list", where)
	}
	task.LoopVar = "item"
	if v, ok := m["loop_control"]; ok {
		control, ok := v.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("%s: loop_control should be a mapping", where)
		}
		for key, value := range control {
			switch key {
			case "loop_var":
				task.LoopVar = pbToString(value)
			case "label":
				task.LoopLabel = pbToString(value)
			case "index_var":
				task.IndexVar = pbToString(value)
			case "pause", "extended":
			default:
				return nil, fmt.Errorf("%s: loop_control: %s is not supported", where, key)
			}
		}
	}

	for _, value := range []interface{}{task.Args, task.Vars, task.Loop, task.Env, task.Name} {
		err = checkPbTemplates(value)
		if err != nil {
			return nil, fmt.Errorf("%s: %s", where, err.Error())
		}
	}
	for keyword, conds := range map[string][]string{"when": task.When, "changed_when": task.ChangedWhen, "failed_when": task.FailedWhen} {
		for _, cond := range conds {
			_, err = parsePbCondition(cond)
			if err != nil {
				return nil, fmt.Errorf("%s: %s: %s", where, keyword, err.Error())
			}
		}
	}
	return task, nil
}

var pbVarNameRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func checkPbModuleArgs(module string, args map[string]interface{}) error {
	has := func(key string) bool {
		_, ok := args[key]
		return ok
	}
	need := func(keys ...string) error {
		for _, key := range keys {
			if has(key) {
				return nil
			}
		}
		return fmt.Errorf("the module: %s needs %s", module, strings.Join(keys, " or "))
	}

	switch module {
	case "command":
		if !has("_raw_params") && !has("argv") {
			return fmt.Errorf("the module: command needs a command or argv")
		}
	case "shell", "script", "raw":
		if !has("_raw_params") {
			return fmt.Errorf("the module: %s needs a command", module)
		}
	case "copy":
		if err := need("dest"); err != nil {
			return err
		}
		if has("src") == has("content") {
			return fmt.Errorf("the module: copy needs either src or content")
		}
	case "template":
		return need("src")
	case "file":
		return need("path", "dest", "name")
	case "lineinfile":
		if err := need("path", "dest", "name"); err != nil {
			return err
		}
		if has("insertafter") && has("insertbefore") {
			return fmt.Errorf("insertafter and insertbefore can't be used together")
		}
	case "apt", "yum", "dnf", "package":
		if !has("name") && !has("pkg") && !has("package") && !pbToBool(args["update_cache"]) {
			return fmt.Errorf("the module: %s needs name", module)
		}
	case "service", "systemd":
		if !has("name") && !(module == "systemd" && (pbToBool(args["daemon_reload"]) || pbToBool(args["daemon-reload"]))) {
			return fmt.Errorf("the module: %s needs name", module)
		}
	case "user", "group":
		return need("name")
	case "get_url":
		if err := need("url"); err != nil {
			return err
		}
		if err := need("dest"); err != nil {
			return err
		}
		if checksum, ok := args["checksum"].(string); ok && checksum != "" && !strings.HasPrefix(checksum, "sha256:") && !strings.Contains(checksum, "{{") {
			return fmt.Errorf("only sha256:<hex> checksums are supported")
		}
	case "debug":
		if has("msg") && has("var") {
			return fmt.Errorf("msg and var can't be used together")
		}
	case "assert":
		return need("that")
	case "set_fact":
		for key := range args {
			if key == "cacheable" {
				continue
			}
			if !pbVarNameRegex.MatchString(key) {
				return fmt.Errorf("invalid variable name: %s", key)
			}
		}
	}

	if state, ok := args["state"].(string); ok && !strings.Contains(state, "{{") {
		states := map[string][]string{
			"file":       {"file", "directory", "absent", "touch", "link"},
			"lineinfile": {"present", "absent"},
			"apt":        {"present", "installed", "latest", "absent", "removed"},
			"yum":        {"present", "installed", "latest", "absent", "removed"},
			"dnf":        {"present", "installed", "latest", "absent", "removed"},
			"package":    {"present", "installed", "latest", "absent", "removed"},
			"service":    {"started", "stopped", "restarted", "reloaded"},
			"systemd":    {"started", "stopped", "restarted", "reloaded"},
			"user":       {"present", "absent"},
			"group":      {"present", "absent"},
		}[module]
		if states != nil && !slices.Contains(states, state) {
			return fmt.Errorf("the state: %s of the module: %s is not supported, it can be %s", state, module, strings.Join(states, ", "))
		}
	}
	return nil
}

func parsePbCondition(cond string) (pbNode, error) {
	cond = strings.TrimSpace(cond)
	if strings.HasPrefix(cond, "{{") && strings.HasSuffix(cond, "}}") {
		cond = strings.TrimSpace(cond[2 : len(cond)-2])
	}
	return parsePbExpr(cond)
}

// checkPbTemplates parses every {{ }} in a value, so that a playbook with an unsupported
// expression is refused before it runs.
func checkPbTemplates(v interface{}) error {
	switch x := v.(type) {
	case string:
		_, err := parsePbTemplate(x)
		return err
	case []interface{}:
		for _, item := range x {
			if err := checkPbTemplates(item); err != nil {
				return err
			}
		}
	case map[string]interface{}:
		for key, item := range x {
			if err := checkPbTemplates(key); err != nil {
				return err
			}
			if err := checkPbTemplates(item); err != nil {
				return err
			}
		}
	}
	return nil
}

func parsePbTasks(raw interface{}, where string, isHandler bool) ([]*pbTask, error) {
	if raw == nil {
		return nil, nil
	}
	list, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("%s should be a list of tasks", where)
	}
	res := []*pbTask{}
	for i, item := range list {
		task, err := parsePbTask(item, fmt.Sprintf("%s %d", where, i+1), isHandler)
		if err != nil {
			return nil, err
		}
		res = append(res, task)
	}
	return res, nil
}

// parsePlaybook reads a playbook and checks everything that can be checked before it runs:
// the keywords, the modules and their options, the expressions and the files it uses.
func parsePlaybook(content string, files []*JobFile) ([]*pbPlay, *PlaybookSummary, error) {
	var raw interface{}
	err := yaml.Unmarshal([]byte(content), &raw)
	if err != nil {
		return nil, nil, fmt.Errorf("the playbook is not valid YAML: %s", err.Error())
	}
	list, ok := raw.([]interface{})
	if !ok || len(list) == 0 {
		return nil, nil, fmt.Errorf("a playbook should be a list of plays")
	}

	summary := &PlaybookSummary{Modules: []string{}, Files: []string{}}
	plays := []*pbPlay{}
	for i, item := range list {
		where := fmt.Sprintf("play %d", i+1)
		m, ok := item.(map[string]interface{})
		if !ok {
			return nil, nil, fmt.Errorf("%s: a play should be a mapping", where)
		}
		if _, ok := m["import_playbook"]; ok {
			return nil, nil, fmt.Errorf("%s: import_playbook is not supported, put the plays in the playbook itself", where)
		}
		for key := range m {
			if !pbPlayKeys[key] {
				if key == "roles" || key == "vars_files" || key == "vars_prompt" || key == "remote_user" || key == "connection" || key == "serial" || key == "any_errors_fatal" || key == "max_fail_percentage" {
					return nil, nil, fmt.Errorf("%s: the keyword %s is not supported", where, key)
				}
				return nil, nil, fmt.Errorf("%s: unknown keyword: %s", where, key)
			}
		}

		play := &pbPlay{Index: i + 1, Facts: true}
		if v, ok := m["name"]; ok {
			play.Name = pbToString(v)
		}
		hosts := pbStrings(m["hosts"])
		if len(hosts) == 0 {
			return nil, nil, fmt.Errorf("%s: hosts is missing", where)
		}
		for _, host := range hosts {
			for _, part := range strings.FieldsFunc(host, func(r rune) bool { return r == ',' || r == ':' }) {
				if part = strings.TrimSpace(part); part != "" {
					play.Hosts = append(play.Hosts, part)
				}
			}
		}
		play.Vars, err = getPbVars(m["vars"], where)
		if err != nil {
			return nil, nil, err
		}
		play.Become = pbBoolPtr(m["become"])
		if v, ok := m["become_user"]; ok {
			play.BecomeUser = pbToString(v)
		}
		if v, ok := m["become_method"]; ok && pbToString(v) != "sudo" {
			return nil, nil, fmt.Errorf("%s: only the sudo become_method is supported", where)
		}
		if v, ok := m["gather_facts"]; ok {
			play.Facts = pbToBool(v)
		}
		play.CheckMode = pbBoolPtr(m["check_mode"])
		play.Ignore = pbToBool(m["ignore_errors"])
		if v, ok := m["environment"]; ok {
			env, ok := v.(map[string]interface{})
			if !ok {
				return nil, nil, fmt.Errorf("%s: environment should be a mapping", where)
			}
			play.Env = env
		}
		if v, ok := m["strategy"]; ok && pbToString(v) != "linear" && pbToString(v) != "free" {
			return nil, nil, fmt.Errorf("%s: the strategy: %s is not supported", where, pbToString(v))
		}
		if err = checkPbTemplates(play.Vars); err != nil {
			return nil, nil, fmt.Errorf("%s: %s", where, err.Error())
		}

		for _, section := range []string{"pre_tasks", "tasks", "post_tasks"} {
			tasks, err := parsePbTasks(m[section], fmt.Sprintf("%s, %s", where, strings.ReplaceAll(section, "_", " ")), false)
			if err != nil {
				return nil, nil, err
			}
			play.Tasks = append(play.Tasks, tasks...)
		}
		play.Handlers, err = parsePbTasks(m["handlers"], where+", handler", true)
		if err != nil {
			return nil, nil, err
		}

		handlerNames := []string{}
		for _, handler := range play.Handlers {
			handlerNames = append(handlerNames, handler.Name)
			handlerNames = append(handlerNames, handler.Listen...)
		}
		for _, task := range append(append([]*pbTask{}, play.Tasks...), play.Handlers...) {
			for _, name := range task.Notify {
				if !slices.Contains(handlerNames, name) {
					return nil, nil, fmt.Errorf("%s: the task: %s notifies the handler: %s, which the play doesn't have", where, task.Label, name)
				}
			}
			if !slices.Contains(summary.Modules, task.Module) {
				summary.Modules = append(summary.Modules, task.Module)
			}
			if task.Module == "copy" || task.Module == "template" || task.Module == "script" {
				name := getPbTaskFile(task)
				if name == "" || strings.Contains(name, "{{") {
					continue
				}
				if findJobFile(files, name) == nil {
					return nil, nil, fmt.Errorf("%s: the task: %s uses the file: %s, which is not attached to the job", where, task.Label, name)
				}
				if !slices.Contains(summary.Files, path.Base(name)) {
					summary.Files = append(summary.Files, path.Base(name))
				}
			}
		}

		summary.Tasks += len(play.Tasks)
		summary.Handlers += len(play.Handlers)
		plays = append(plays, play)
	}
	summary.Plays = len(plays)
	return plays, summary, nil
}

// getPbTaskFile is the attached file a copy, template or script task uses.
func getPbTaskFile(task *pbTask) string {
	args := task.Args.(map[string]interface{})
	if task.Module == "script" {
		parts, err := splitPbArgs(pbToString(args["_raw_params"]))
		if err != nil || len(parts) == 0 {
			return ""
		}
		return parts[0]
	}
	if src, ok := args["src"]; ok {
		return pbToString(src)
	}
	return ""
}

// findJobFile finds an attached file by its name, a path like files/nginx.conf matching nginx.conf.
func findJobFile(files []*JobFile, name string) *JobFile {
	base := path.Base(strings.ReplaceAll(name, "\\", "/"))
	for _, file := range files {
		if file.Name == base {
			return file
		}
	}
	return nil
}

func parsePbExtraVars(text string) (map[string]interface{}, error) {
	if strings.TrimSpace(text) == "" {
		return map[string]interface{}{}, nil
	}
	var raw interface{}
	err := yaml.Unmarshal([]byte(text), &raw)
	if err != nil {
		return nil, fmt.Errorf("the variables are not valid YAML or JSON: %s", err.Error())
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("the variables should be a mapping, such as \"port: 8080\"")
	}
	err = checkPbTemplates(m)
	if err != nil {
		return nil, fmt.Errorf("the variables: %s", err.Error())
	}
	return m, nil
}

// CheckPlaybook checks a playbook job before it runs.
func CheckPlaybook(job *Job) (*PlaybookSummary, error) {
	_, err := parsePbExtraVars(job.Variables)
	if err != nil {
		return nil, err
	}
	_, summary, err := parsePlaybook(job.Content, job.Files)
	return summary, err
}
