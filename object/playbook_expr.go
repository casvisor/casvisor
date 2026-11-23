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
	"math"
	"sort"
	"strconv"
	"strings"
)

// The expressions of playbooks: the variables, literals, operators, tests and filters of Jinja2
// that conditions and {{ }} use most, evaluated in Go.

type undefinedValue struct {
	name string
}

type pbEnv interface {
	lookup(name string) (interface{}, error)
}

type pbToken struct {
	kind  byte // n: number, s: string, i: identifier, o: operator
	text  string
	value interface{}
}

type pbNode interface {
	eval(env pbEnv) (interface{}, error)
}

var pbFilters = map[string]bool{
	"default": true, "d": true, "lower": true, "upper": true, "trim": true, "int": true, "float": true,
	"string": true, "bool": true, "length": true, "count": true, "join": true, "first": true, "last": true,
	"quote": true, "replace": true, "list": true, "unique": true, "sort": true, "capitalize": true,
}

var pbTests = map[string]bool{
	"defined": true, "undefined": true, "none": true, "string": true, "number": true, "sequence": true,
	"iterable": true, "mapping": true, "succeeded": true, "success": true, "failed": true, "failure": true,
	"changed": true, "change": true, "skipped": true, "skip": true,
}

func tokenizePbExpr(s string) ([]pbToken, error) {
	tokens := []pbToken{}
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c >= '0' && c <= '9':
			j := i
			isFloat := false
			for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == '.' && j+1 < len(s) && s[j+1] >= '0' && s[j+1] <= '9') {
				if s[j] == '.' {
					isFloat = true
				}
				j++
			}
			text := s[i:j]
			if isFloat {
				f, err := strconv.ParseFloat(text, 64)
				if err != nil {
					return nil, err
				}
				tokens = append(tokens, pbToken{kind: 'n', text: text, value: f})
			} else {
				n, err := strconv.ParseInt(text, 10, 64)
				if err != nil {
					return nil, err
				}
				tokens = append(tokens, pbToken{kind: 'n', text: text, value: int(n)})
			}
			i = j
		case c == '\'' || c == '"':
			var b strings.Builder
			j := i + 1
			for ; j < len(s) && s[j] != c; j++ {
				if s[j] == '\\' && j+1 < len(s) {
					j++
					switch s[j] {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					default:
						b.WriteByte(s[j])
					}
					continue
				}
				b.WriteByte(s[j])
			}
			if j >= len(s) {
				return nil, fmt.Errorf("unterminated string in: %s", s)
			}
			tokens = append(tokens, pbToken{kind: 's', text: s[i : j+1], value: b.String()})
			i = j + 1
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			j := i
			for j < len(s) && (s[j] == '_' || s[j] >= 'a' && s[j] <= 'z' || s[j] >= 'A' && s[j] <= 'Z' || s[j] >= '0' && s[j] <= '9') {
				j++
			}
			tokens = append(tokens, pbToken{kind: 'i', text: s[i:j]})
			i = j
		default:
			if i+1 < len(s) {
				two := s[i : i+2]
				if two == "==" || two == "!=" || two == "<=" || two == ">=" || two == "//" || two == "**" {
					tokens = append(tokens, pbToken{kind: 'o', text: two})
					i += 2
					continue
				}
			}
			if !strings.ContainsRune("<>()[].,|+-*/%~:{}", rune(c)) {
				return nil, fmt.Errorf("unexpected character %q in: %s", c, s)
			}
			tokens = append(tokens, pbToken{kind: 'o', text: string(c)})
			i++
		}
	}
	return tokens, nil
}

type pbParser struct {
	tokens []pbToken
	pos    int
	src    string
}

func parsePbExpr(s string) (pbNode, error) {
	tokens, err := tokenizePbExpr(s)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty expression")
	}
	p := &pbParser{tokens: tokens, src: s}
	node, err := p.parseCondExpr()
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.tokens) {
		return nil, fmt.Errorf("unexpected %q in: %s", p.tokens[p.pos].text, s)
	}
	return node, nil
}

func (p *pbParser) peek() *pbToken {
	if p.pos < len(p.tokens) {
		return &p.tokens[p.pos]
	}
	return nil
}

func (p *pbParser) isOp(text string) bool {
	t := p.peek()
	return t != nil && t.kind == 'o' && t.text == text
}

func (p *pbParser) isWord(text string) bool {
	t := p.peek()
	return t != nil && t.kind == 'i' && t.text == text
}

func (p *pbParser) expectOp(text string) error {
	if !p.isOp(text) {
		return fmt.Errorf("expected %q in: %s", text, p.src)
	}
	p.pos++
	return nil
}

type pbCondNode struct {
	value, cond, other pbNode
}

func (p *pbParser) parseCondExpr() (pbNode, error) {
	node, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if !p.isWord("if") {
		return node, nil
	}
	p.pos++
	cond, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	var other pbNode = &pbLiteral{value: ""}
	if p.isWord("else") {
		p.pos++
		other, err = p.parseCondExpr()
		if err != nil {
			return nil, err
		}
	}
	return &pbCondNode{value: node, cond: cond, other: other}, nil
}

func (n *pbCondNode) eval(env pbEnv) (interface{}, error) {
	cond, err := n.cond.eval(env)
	if err != nil {
		return nil, err
	}
	ok, err := pbTruthy(cond)
	if err != nil {
		return nil, err
	}
	if ok {
		return n.value.eval(env)
	}
	return n.other.eval(env)
}

type pbLogicNode struct {
	op          string
	left, right pbNode
}

func (p *pbParser) parseOr() (pbNode, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.isWord("or") {
		p.pos++
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &pbLogicNode{op: "or", left: left, right: right}
	}
	return left, nil
}

func (p *pbParser) parseAnd() (pbNode, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.isWord("and") {
		p.pos++
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &pbLogicNode{op: "and", left: left, right: right}
	}
	return left, nil
}

func (n *pbLogicNode) eval(env pbEnv) (interface{}, error) {
	left, err := n.left.eval(env)
	if err != nil {
		return nil, err
	}
	ok, err := pbTruthy(left)
	if err != nil {
		return nil, err
	}
	if n.op == "and" && !ok || n.op == "or" && ok {
		return ok, nil
	}
	right, err := n.right.eval(env)
	if err != nil {
		return nil, err
	}
	return pbTruthy(right)
}

type pbNotNode struct {
	node pbNode
}

func (p *pbParser) parseNot() (pbNode, error) {
	if p.isWord("not") {
		p.pos++
		node, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &pbNotNode{node: node}, nil
	}
	return p.parseCompare()
}

func (n *pbNotNode) eval(env pbEnv) (interface{}, error) {
	value, err := n.node.eval(env)
	if err != nil {
		return nil, err
	}
	ok, err := pbTruthy(value)
	return !ok, err
}

type pbCompareNode struct {
	op          string
	left, right pbNode
}

type pbTestNode struct {
	node   pbNode
	test   string
	negate bool
}

func (p *pbParser) parseCompare() (pbNode, error) {
	left, err := p.parseConcat()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t == nil {
			return left, nil
		}
		switch {
		case t.kind == 'o' && (t.text == "==" || t.text == "!=" || t.text == "<" || t.text == ">" || t.text == "<=" || t.text == ">="):
			p.pos++
			right, err := p.parseConcat()
			if err != nil {
				return nil, err
			}
			left = &pbCompareNode{op: t.text, left: left, right: right}
		case t.kind == 'i' && t.text == "in":
			p.pos++
			right, err := p.parseConcat()
			if err != nil {
				return nil, err
			}
			left = &pbCompareNode{op: "in", left: left, right: right}
		case t.kind == 'i' && t.text == "not" && p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].kind == 'i' && p.tokens[p.pos+1].text == "in":
			p.pos += 2
			right, err := p.parseConcat()
			if err != nil {
				return nil, err
			}
			left = &pbCompareNode{op: "not in", left: left, right: right}
		case t.kind == 'i' && t.text == "is":
			p.pos++
			negate := false
			if p.isWord("not") {
				p.pos++
				negate = true
			}
			name := p.peek()
			if name == nil || name.kind != 'i' {
				return nil, fmt.Errorf("expected a test after \"is\" in: %s", p.src)
			}
			if !pbTests[name.text] {
				return nil, fmt.Errorf("the test: %s is not supported", name.text)
			}
			p.pos++
			left = &pbTestNode{node: left, test: name.text, negate: negate}
		default:
			return left, nil
		}
	}
}

func (n *pbCompareNode) eval(env pbEnv) (interface{}, error) {
	left, err := n.left.eval(env)
	if err != nil {
		return nil, err
	}
	right, err := n.right.eval(env)
	if err != nil {
		return nil, err
	}
	for _, v := range []interface{}{left, right} {
		if u, ok := v.(undefinedValue); ok {
			return nil, fmt.Errorf("'%s' is undefined", u.name)
		}
	}

	switch n.op {
	case "==":
		return pbEqual(left, right), nil
	case "!=":
		return !pbEqual(left, right), nil
	case "in", "not in":
		var found bool
		switch r := right.(type) {
		case string:
			found = strings.Contains(r, pbToString(left))
		case []interface{}:
			for _, item := range r {
				if pbEqual(item, left) {
					found = true
					break
				}
			}
		case map[string]interface{}:
			_, found = r[pbToString(left)]
		case nil:
			return nil, fmt.Errorf("the right side of \"in\" is none")
		default:
			return nil, fmt.Errorf("the right side of \"in\" is not a string, list or mapping")
		}
		return found == (n.op == "in"), nil
	}

	cmp, err := pbCompare(left, right)
	if err != nil {
		return nil, err
	}
	switch n.op {
	case "<":
		return cmp < 0, nil
	case ">":
		return cmp > 0, nil
	case "<=":
		return cmp <= 0, nil
	}
	return cmp >= 0, nil
}

func (n *pbTestNode) eval(env pbEnv) (interface{}, error) {
	value, err := n.node.eval(env)
	if err != nil {
		return nil, err
	}
	_, undefined := value.(undefinedValue)
	var res bool
	switch n.test {
	case "defined":
		res = !undefined
	case "undefined":
		res = undefined
	default:
		if undefined {
			return nil, fmt.Errorf("'%s' is undefined", value.(undefinedValue).name)
		}
		switch n.test {
		case "none":
			res = value == nil
		case "string":
			_, res = value.(string)
		case "number":
			_, isInt := value.(int)
			_, isFloat := value.(float64)
			res = isInt || isFloat
		case "sequence", "iterable":
			switch value.(type) {
			case []interface{}, string, map[string]interface{}:
				res = true
			}
		case "mapping":
			_, res = value.(map[string]interface{})
		default:
			result, ok := value.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("the test: %s needs a task result", n.test)
			}
			failed, _ := pbTruthy(result["failed"])
			changed, _ := pbTruthy(result["changed"])
			skipped, _ := pbTruthy(result["skipped"])
			switch n.test {
			case "succeeded", "success":
				res = !failed
			case "failed", "failure":
				res = failed
			case "changed", "change":
				res = changed
			case "skipped", "skip":
				res = skipped
			}
		}
	}
	return res != n.negate, nil
}

type pbBinaryNode struct {
	op          string
	left, right pbNode
}

func (p *pbParser) parseConcat() (pbNode, error) {
	left, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	for p.isOp("~") {
		p.pos++
		right, err := p.parseAdd()
		if err != nil {
			return nil, err
		}
		left = &pbBinaryNode{op: "~", left: left, right: right}
	}
	return left, nil
}

func (p *pbParser) parseAdd() (pbNode, error) {
	left, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	for p.isOp("+") || p.isOp("-") {
		op := p.peek().text
		p.pos++
		right, err := p.parseMul()
		if err != nil {
			return nil, err
		}
		left = &pbBinaryNode{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *pbParser) parseMul() (pbNode, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.isOp("*") || p.isOp("/") || p.isOp("%") || p.isOp("//") {
		op := p.peek().text
		p.pos++
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &pbBinaryNode{op: op, left: left, right: right}
	}
	return left, nil
}

func (n *pbBinaryNode) eval(env pbEnv) (interface{}, error) {
	left, err := n.left.eval(env)
	if err != nil {
		return nil, err
	}
	right, err := n.right.eval(env)
	if err != nil {
		return nil, err
	}
	for _, v := range []interface{}{left, right} {
		if u, ok := v.(undefinedValue); ok {
			return nil, fmt.Errorf("'%s' is undefined", u.name)
		}
	}

	if n.op == "~" {
		return pbToString(left) + pbToString(right), nil
	}
	if n.op == "+" {
		if l, ok := left.(string); ok {
			if r, ok := right.(string); ok {
				return l + r, nil
			}
		}
		if l, ok := left.([]interface{}); ok {
			if r, ok := right.([]interface{}); ok {
				return append(append([]interface{}{}, l...), r...), nil
			}
		}
	}

	l, lok := pbNumber(left)
	r, rok := pbNumber(right)
	if !lok || !rok {
		return nil, fmt.Errorf("unsupported operand types for %s", n.op)
	}
	_, lInt := left.(int)
	_, rInt := right.(int)
	var res float64
	switch n.op {
	case "+":
		res = l + r
	case "-":
		res = l - r
	case "*":
		res = l * r
	case "/":
		if r == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		return l / r, nil
	case "//":
		if r == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		res = math.Floor(l / r)
	case "%":
		if r == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		res = math.Mod(l, r)
	}
	if lInt && rInt {
		return int(res), nil
	}
	return res, nil
}

type pbNegNode struct {
	node pbNode
}

func (p *pbParser) parseUnary() (pbNode, error) {
	if p.isOp("-") {
		p.pos++
		node, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &pbNegNode{node: node}, nil
	}
	return p.parseFiltered()
}

func (n *pbNegNode) eval(env pbEnv) (interface{}, error) {
	value, err := n.node.eval(env)
	if err != nil {
		return nil, err
	}
	switch v := value.(type) {
	case int:
		return -v, nil
	case float64:
		return -v, nil
	}
	return nil, fmt.Errorf("bad operand type for unary -")
}

type pbFilterNode struct {
	node pbNode
	name string
	args []pbNode
}

func (p *pbParser) parseFiltered() (pbNode, error) {
	node, err := p.parsePostfix()
	if err != nil {
		return nil, err
	}
	for p.isOp("|") {
		p.pos++
		name := p.peek()
		if name == nil || name.kind != 'i' {
			return nil, fmt.Errorf("expected a filter after \"|\" in: %s", p.src)
		}
		if !pbFilters[name.text] {
			return nil, fmt.Errorf("the filter: %s is not supported", name.text)
		}
		p.pos++
		args := []pbNode{}
		if p.isOp("(") {
			args, err = p.parseArgs(")")
			if err != nil {
				return nil, err
			}
		}
		node = &pbFilterNode{node: node, name: name.text, args: args}
	}
	return node, nil
}

func (p *pbParser) parseArgs(end string) ([]pbNode, error) {
	p.pos++
	args := []pbNode{}
	for !p.isOp(end) {
		// keyword arguments of filters, such as default(true, boolean=true), are taken by position
		if t := p.peek(); t != nil && t.kind == 'i' && p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].text == "=" {
			p.pos += 2
		}
		arg, err := p.parseCondExpr()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
		if p.isOp(",") {
			p.pos++
			continue
		}
		if !p.isOp(end) {
			return nil, fmt.Errorf("expected %q in: %s", end, p.src)
		}
	}
	p.pos++
	return args, nil
}

func (n *pbFilterNode) eval(env pbEnv) (interface{}, error) {
	value, err := n.node.eval(env)
	if err != nil {
		return nil, err
	}
	args := []interface{}{}
	for _, arg := range n.args {
		v, err := arg.eval(env)
		if err != nil {
			return nil, err
		}
		args = append(args, v)
	}

	if n.name == "default" || n.name == "d" {
		var def interface{} = ""
		if len(args) > 0 {
			def = args[0]
		}
		if _, ok := value.(undefinedValue); ok {
			return def, nil
		}
		if len(args) > 1 {
			if boolean, _ := pbTruthy(args[1]); boolean {
				if ok, _ := pbTruthy(value); !ok {
					return def, nil
				}
			}
		}
		return value, nil
	}
	if u, ok := value.(undefinedValue); ok {
		return nil, fmt.Errorf("'%s' is undefined", u.name)
	}

	switch n.name {
	case "lower":
		return strings.ToLower(pbToString(value)), nil
	case "upper":
		return strings.ToUpper(pbToString(value)), nil
	case "capitalize":
		s := strings.ToLower(pbToString(value))
		if s == "" {
			return s, nil
		}
		return strings.ToUpper(s[:1]) + s[1:], nil
	case "trim":
		return strings.TrimSpace(pbToString(value)), nil
	case "string":
		return pbToString(value), nil
	case "int":
		if f, ok := pbNumber(value); ok {
			return int(f), nil
		}
		s := strings.TrimSpace(pbToString(value))
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return int(f), nil
		}
		if len(args) > 0 {
			return args[0], nil
		}
		return 0, nil
	case "float":
		if f, ok := pbNumber(value); ok {
			return f, nil
		}
		if f, err := strconv.ParseFloat(strings.TrimSpace(pbToString(value)), 64); err == nil {
			return f, nil
		}
		return 0.0, nil
	case "bool":
		return pbToBool(value), nil
	case "length", "count":
		switch v := value.(type) {
		case string:
			return len([]rune(v)), nil
		case []interface{}:
			return len(v), nil
		case map[string]interface{}:
			return len(v), nil
		}
		return nil, fmt.Errorf("the value has no length")
	case "join":
		sep := ""
		if len(args) > 0 {
			sep = pbToString(args[0])
		}
		list, ok := value.([]interface{})
		if !ok {
			return pbToString(value), nil
		}
		parts := []string{}
		for _, item := range list {
			parts = append(parts, pbToString(item))
		}
		return strings.Join(parts, sep), nil
	case "first", "last":
		switch v := value.(type) {
		case []interface{}:
			if len(v) == 0 {
				return undefinedValue{name: "first"}, nil
			}
			if n.name == "first" {
				return v[0], nil
			}
			return v[len(v)-1], nil
		case string:
			r := []rune(v)
			if len(r) == 0 {
				return "", nil
			}
			if n.name == "first" {
				return string(r[0]), nil
			}
			return string(r[len(r)-1]), nil
		}
		return nil, fmt.Errorf("the value has no %s item", n.name)
	case "quote":
		return shellQuote(pbToString(value)), nil
	case "replace":
		if len(args) < 2 {
			return nil, fmt.Errorf("replace needs two arguments")
		}
		return strings.ReplaceAll(pbToString(value), pbToString(args[0]), pbToString(args[1])), nil
	case "list":
		switch v := value.(type) {
		case []interface{}:
			return v, nil
		case string:
			res := []interface{}{}
			for _, r := range v {
				res = append(res, string(r))
			}
			return res, nil
		case map[string]interface{}:
			keys := pbSortedKeys(v)
			res := []interface{}{}
			for _, key := range keys {
				res = append(res, key)
			}
			return res, nil
		}
		return []interface{}{value}, nil
	case "unique", "sort":
		list, ok := value.([]interface{})
		if !ok {
			return nil, fmt.Errorf("%s needs a list", n.name)
		}
		res := []interface{}{}
		for _, item := range list {
			seen := false
			if n.name == "unique" {
				for _, r := range res {
					if pbEqual(r, item) {
						seen = true
						break
					}
				}
			}
			if !seen {
				res = append(res, item)
			}
		}
		if n.name == "sort" {
			var sortErr error
			sort.SliceStable(res, func(i, j int) bool {
				c, err := pbCompare(res[i], res[j])
				if err != nil {
					sortErr = err
				}
				return c < 0
			})
			if sortErr != nil {
				return nil, sortErr
			}
		}
		return res, nil
	}
	return nil, fmt.Errorf("the filter: %s is not supported", n.name)
}

type pbVarNode struct {
	name string
}

type pbAttrNode struct {
	node  pbNode
	attr  string
	index pbNode
	label string
}

type pbLiteral struct {
	value interface{}
}

type pbListNode struct {
	items []pbNode
}

type pbDictNode struct {
	keys, values []pbNode
}

func (p *pbParser) parsePostfix() (pbNode, error) {
	node, label, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		if p.isOp(".") {
			p.pos++
			name := p.peek()
			if name == nil || name.kind != 'i' && name.kind != 'n' {
				return nil, fmt.Errorf("expected a name after \".\" in: %s", p.src)
			}
			p.pos++
			if p.isOp("(") {
				return nil, fmt.Errorf("method calls such as .%s() are not supported, use a filter instead", name.text)
			}
			label += "." + name.text
			node = &pbAttrNode{node: node, attr: name.text, label: label}
			continue
		}
		if p.isOp("[") {
			p.pos++
			index, err := p.parseCondExpr()
			if err != nil {
				return nil, err
			}
			if err = p.expectOp("]"); err != nil {
				return nil, err
			}
			label += "[...]"
			node = &pbAttrNode{node: node, index: index, label: label}
			continue
		}
		if p.isOp("(") {
			return nil, fmt.Errorf("function calls such as %s() are not supported", label)
		}
		return node, nil
	}
}

func (p *pbParser) parsePrimary() (pbNode, string, error) {
	t := p.peek()
	if t == nil {
		return nil, "", fmt.Errorf("unexpected end of: %s", p.src)
	}
	p.pos++
	switch t.kind {
	case 'n', 's':
		return &pbLiteral{value: t.value}, t.text, nil
	case 'i':
		switch t.text {
		case "true", "True":
			return &pbLiteral{value: true}, t.text, nil
		case "false", "False":
			return &pbLiteral{value: false}, t.text, nil
		case "none", "None":
			return &pbLiteral{value: nil}, t.text, nil
		}
		return &pbVarNode{name: t.text}, t.text, nil
	}

	switch t.text {
	case "(":
		node, err := p.parseCondExpr()
		if err != nil {
			return nil, "", err
		}
		return node, "(...)", p.expectOp(")")
	case "[":
		p.pos--
		items, err := p.parseArgs("]")
		if err != nil {
			return nil, "", err
		}
		return &pbListNode{items: items}, "[...]", nil
	case "{":
		node := &pbDictNode{}
		for !p.isOp("}") {
			key, err := p.parseCondExpr()
			if err != nil {
				return nil, "", err
			}
			if err = p.expectOp(":"); err != nil {
				return nil, "", err
			}
			value, err := p.parseCondExpr()
			if err != nil {
				return nil, "", err
			}
			node.keys = append(node.keys, key)
			node.values = append(node.values, value)
			if p.isOp(",") {
				p.pos++
			} else if !p.isOp("}") {
				return nil, "", fmt.Errorf("expected \"}\" in: %s", p.src)
			}
		}
		p.pos++
		return node, "{...}", nil
	}
	return nil, "", fmt.Errorf("unexpected %q in: %s", t.text, p.src)
}

func (n *pbLiteral) eval(env pbEnv) (interface{}, error) {
	return n.value, nil
}

func (n *pbVarNode) eval(env pbEnv) (interface{}, error) {
	return env.lookup(n.name)
}

func (n *pbListNode) eval(env pbEnv) (interface{}, error) {
	res := []interface{}{}
	for _, item := range n.items {
		v, err := item.eval(env)
		if err != nil {
			return nil, err
		}
		res = append(res, v)
	}
	return res, nil
}

func (n *pbDictNode) eval(env pbEnv) (interface{}, error) {
	res := map[string]interface{}{}
	for i := range n.keys {
		key, err := n.keys[i].eval(env)
		if err != nil {
			return nil, err
		}
		value, err := n.values[i].eval(env)
		if err != nil {
			return nil, err
		}
		res[pbToString(key)] = value
	}
	return res, nil
}

func (n *pbAttrNode) eval(env pbEnv) (interface{}, error) {
	value, err := n.node.eval(env)
	if err != nil {
		return nil, err
	}
	if _, ok := value.(undefinedValue); ok {
		return undefinedValue{name: n.label}, nil
	}

	var key interface{} = n.attr
	if n.index != nil {
		key, err = n.index.eval(env)
		if err != nil {
			return nil, err
		}
	}

	switch v := value.(type) {
	case map[string]interface{}:
		item, ok := v[pbToString(key)]
		if !ok {
			return undefinedValue{name: n.label}, nil
		}
		return item, nil
	case []interface{}:
		i, ok := key.(int)
		if !ok {
			parsed, err := strconv.Atoi(pbToString(key))
			if err != nil {
				return undefinedValue{name: n.label}, nil
			}
			i = parsed
		}
		if i < 0 {
			i += len(v)
		}
		if i < 0 || i >= len(v) {
			return undefinedValue{name: n.label}, nil
		}
		return v[i], nil
	case string:
		if i, ok := key.(int); ok {
			r := []rune(v)
			if i < 0 {
				i += len(r)
			}
			if i >= 0 && i < len(r) {
				return string(r[i]), nil
			}
		}
	}
	return undefinedValue{name: n.label}, nil
}

func pbNumber(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

func pbEqual(a, b interface{}) bool {
	if x, ok := pbNumber(a); ok {
		if _, isBool := a.(bool); !isBool {
			if y, ok := pbNumber(b); ok {
				if _, isBool := b.(bool); !isBool {
					return x == y
				}
			}
		}
	}
	switch x := a.(type) {
	case []interface{}:
		y, ok := b.([]interface{})
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !pbEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]interface{}:
		y, ok := b.(map[string]interface{})
		if !ok || len(x) != len(y) {
			return false
		}
		for key, value := range x {
			if !pbEqual(value, y[key]) {
				return false
			}
		}
		return true
	}
	return a == b
}

func pbCompare(a, b interface{}) (int, error) {
	if x, ok := pbNumber(a); ok {
		if y, ok := pbNumber(b); ok {
			switch {
			case x < y:
				return -1, nil
			case x > y:
				return 1, nil
			}
			return 0, nil
		}
	}
	x, xok := a.(string)
	y, yok := b.(string)
	if xok && yok {
		return strings.Compare(x, y), nil
	}
	return 0, fmt.Errorf("can't compare %s with %s", pbToString(a), pbToString(b))
}

func pbTruthy(v interface{}) (bool, error) {
	switch x := v.(type) {
	case undefinedValue:
		return false, fmt.Errorf("'%s' is undefined", x.name)
	case nil:
		return false, nil
	case bool:
		return x, nil
	case string:
		return x != "", nil
	case []interface{}:
		return len(x) != 0, nil
	case map[string]interface{}:
		return len(x) != 0, nil
	}
	if n, ok := pbNumber(v); ok {
		return n != 0, nil
	}
	return true, nil
}

// pbToBool reads yes, no, on, off, true, false, 1 and 0 the way Ansible does.
func pbToBool(v interface{}) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "yes", "y", "true", "on", "1", "t":
			return true
		}
		return false
	}
	if n, ok := pbNumber(v); ok {
		return n != 0
	}
	return false
}

func pbSortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// pbToString renders a value as Jinja2 does: lists and mappings as Python writes them.
func pbToString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return "None"
	case bool:
		if x {
			return "True"
		}
		return "False"
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e15 {
			return strconv.FormatFloat(x, 'f', 1, 64)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case undefinedValue:
		return ""
	}
	return pbRepr(v)
}

func pbRepr(v interface{}) string {
	switch x := v.(type) {
	case string:
		return "'" + strings.ReplaceAll(strings.ReplaceAll(x, `\`, `\\`), "'", `\'`) + "'"
	case []interface{}:
		parts := []string{}
		for _, item := range x {
			parts = append(parts, pbRepr(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]interface{}:
		parts := []string{}
		for _, key := range pbSortedKeys(x) {
			parts = append(parts, pbRepr(key)+": "+pbRepr(x[key]))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return pbToString(v)
}
