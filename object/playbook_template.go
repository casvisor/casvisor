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
	"strings"
)

type pbSegment struct {
	text string
	expr pbNode
}

type pbTemplate struct {
	segments []pbSegment
	// whole is a template that is a single {{ }}, whose value keeps its type
	whole bool
}

// parsePbTemplate reads the {{ }} of a string; {% %} statements are not supported, {# #} comments are dropped.
func parsePbTemplate(s string) (*pbTemplate, error) {
	t := &pbTemplate{}
	if !strings.Contains(s, "{{") && !strings.Contains(s, "{%") && !strings.Contains(s, "{#") {
		t.segments = []pbSegment{{text: s}}
		return t, nil
	}

	rest := s
	for rest != "" {
		i := strings.Index(rest, "{")
		for i >= 0 && i+1 < len(rest) && rest[i+1] != '{' && rest[i+1] != '%' && rest[i+1] != '#' {
			next := strings.Index(rest[i+1:], "{")
			if next < 0 {
				i = -1
				break
			}
			i += next + 1
		}
		if i < 0 || i+1 >= len(rest) {
			t.segments = append(t.segments, pbSegment{text: rest})
			break
		}
		if i > 0 {
			t.segments = append(t.segments, pbSegment{text: rest[:i]})
		}
		switch rest[i+1] {
		case '%':
			return nil, fmt.Errorf("Jinja2 statements such as {%% %%} are not supported, only {{ }} expressions")
		case '#':
			end := strings.Index(rest[i:], "#}")
			if end < 0 {
				return nil, fmt.Errorf("unclosed {# in: %s", s)
			}
			rest = rest[i+end+2:]
			continue
		}
		end := strings.Index(rest[i:], "}}")
		if end < 0 {
			return nil, fmt.Errorf("unclosed {{ in: %s", s)
		}
		inner := strings.TrimSpace(strings.Trim(rest[i+2:i+end], "-"))
		expr, err := parsePbExpr(inner)
		if err != nil {
			return nil, fmt.Errorf("%s, in: %s", err.Error(), s)
		}
		t.segments = append(t.segments, pbSegment{expr: expr})
		rest = rest[i+end+2:]
	}

	t.whole = len(t.segments) == 1 && t.segments[0].expr != nil && strings.HasPrefix(strings.TrimSpace(s), "{{") && strings.HasSuffix(strings.TrimSpace(s), "}}")
	return t, nil
}

func (t *pbTemplate) render(env pbEnv) (interface{}, error) {
	if t.whole {
		value, err := t.segments[0].expr.eval(env)
		if err != nil {
			return nil, err
		}
		if u, ok := value.(undefinedValue); ok {
			return nil, fmt.Errorf("'%s' is undefined", u.name)
		}
		return value, nil
	}

	var b strings.Builder
	for _, segment := range t.segments {
		if segment.expr == nil {
			b.WriteString(segment.text)
			continue
		}
		value, err := segment.expr.eval(env)
		if err != nil {
			return nil, err
		}
		if u, ok := value.(undefinedValue); ok {
			return nil, fmt.Errorf("'%s' is undefined", u.name)
		}
		b.WriteString(pbToString(value))
	}
	return b.String(), nil
}

// renderPbValue renders the templates in a value and in the strings, lists and mappings in it.
func renderPbValue(v interface{}, env pbEnv) (interface{}, error) {
	switch x := v.(type) {
	case string:
		t, err := parsePbTemplate(x)
		if err != nil {
			return nil, err
		}
		return t.render(env)
	case []interface{}:
		res := make([]interface{}, 0, len(x))
		for _, item := range x {
			value, err := renderPbValue(item, env)
			if err != nil {
				return nil, err
			}
			res = append(res, value)
		}
		return res, nil
	case map[string]interface{}:
		res := make(map[string]interface{}, len(x))
		for key, item := range x {
			value, err := renderPbValue(item, env)
			if err != nil {
				return nil, err
			}
			res[key] = value
		}
		return res, nil
	}
	return v, nil
}

func renderPbString(s string, env pbEnv) (string, error) {
	value, err := renderPbValue(s, env)
	if err != nil {
		return "", err
	}
	return pbToString(value), nil
}

// evalPbConditions tells whether all the conditions hold, as when, changed_when and failed_when take them.
func evalPbConditions(conds []string, env pbEnv) (bool, error) {
	for _, cond := range conds {
		node, err := parsePbCondition(cond)
		if err != nil {
			return false, err
		}
		value, err := node.eval(env)
		if err != nil {
			return false, fmt.Errorf("the condition: %s failed: %s", cond, err.Error())
		}
		if s, ok := value.(string); ok {
			value = pbToBool(s)
		}
		ok, err := pbTruthy(value)
		if err != nil {
			return false, fmt.Errorf("the condition: %s failed: %s", cond, err.Error())
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// pbScope is the variables of a task: the layers are looked up in order, and the variables
// whose values are templates are rendered when they are used.
type pbScope struct {
	layers []map[string]interface{}
	depth  int
}

func (s *pbScope) lookup(name string) (interface{}, error) {
	for _, layer := range s.layers {
		value, ok := layer[name]
		if !ok {
			continue
		}
		if s.depth > 20 {
			return nil, fmt.Errorf("the variable: %s refers to itself", name)
		}
		s.depth++
		defer func() { s.depth-- }()
		return renderPbValue(value, s)
	}
	return undefinedValue{name: name}, nil
}

func (s *pbScope) with(layer map[string]interface{}) *pbScope {
	return &pbScope{layers: append([]map[string]interface{}{layer}, s.layers...)}
}
