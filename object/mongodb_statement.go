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
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// The MongoDB console reads the commands of the MongoDB shell, such as
// db.users.find({age: {$gt: 18}}).sort({name: 1}).limit(10), without running
// any JavaScript: the calls are parsed and run by the Go driver.

const (
	mongoTokenEOF = iota
	mongoTokenIdent
	mongoTokenString
	mongoTokenNumber
	mongoTokenRegex
	mongoTokenPunct
)

type mongoToken struct {
	kind  int
	text  string
	start int
	end   int
	// the token is the first of its line
	newline bool
}

func isMongoIdentStart(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isMongoIdentChar(c byte) bool {
	return isMongoIdentStart(c) || c >= '0' && c <= '9'
}

// lexMongo splits shell code into tokens, telling a regular expression from a
// division by the token before it, the way JavaScript does.
func lexMongo(text string) ([]mongoToken, error) {
	tokens := []mongoToken{}
	i := 0
	newline := true
	for i < len(text) {
		c := text[i]
		switch {
		case c == '\n':
			newline = true
			i++
			continue
		case c == ' ' || c == '\t' || c == '\r':
			i++
			continue
		case strings.HasPrefix(text[i:], "//"):
			j := strings.IndexByte(text[i:], '\n')
			if j < 0 {
				i = len(text)
			} else {
				i += j
			}
			continue
		case strings.HasPrefix(text[i:], "/*"):
			j := strings.Index(text[i+2:], "*/")
			if j < 0 {
				return nil, fmt.Errorf("unterminated comment")
			}
			if strings.Contains(text[i:i+2+j], "\n") {
				newline = true
			}
			i += j + 4
			continue
		}

		token := mongoToken{start: i, newline: newline}
		newline = false
		switch {
		case c == '\'' || c == '"' || c == '`':
			s, end, err := readMongoString(text, i)
			if err != nil {
				return nil, err
			}
			token.kind, token.text, token.end = mongoTokenString, s, end
		case c >= '0' && c <= '9' || c == '.' && i+1 < len(text) && text[i+1] >= '0' && text[i+1] <= '9':
			j := i
			if strings.HasPrefix(text[i:], "0x") || strings.HasPrefix(text[i:], "0X") {
				j += 2
				for j < len(text) && strings.IndexByte("0123456789abcdefABCDEF", text[j]) >= 0 {
					j++
				}
			} else {
				for j < len(text) && (text[j] >= '0' && text[j] <= '9' || text[j] == '.') {
					j++
				}
				if j < len(text) && (text[j] == 'e' || text[j] == 'E') {
					j++
					if j < len(text) && (text[j] == '+' || text[j] == '-') {
						j++
					}
					for j < len(text) && text[j] >= '0' && text[j] <= '9' {
						j++
					}
				}
			}
			token.kind, token.text, token.end = mongoTokenNumber, text[i:j], j
		case isMongoIdentStart(c):
			j := i
			for j < len(text) && isMongoIdentChar(text[j]) {
				j++
			}
			token.kind, token.text, token.end = mongoTokenIdent, text[i:j], j
		case c == '/' && allowsMongoRegex(tokens):
			j := i + 1
			inClass := false
			for j < len(text) && (text[j] != '/' || inClass) {
				if text[j] == '\\' {
					j++
				} else if text[j] == '[' {
					inClass = true
				} else if text[j] == ']' {
					inClass = false
				} else if text[j] == '\n' {
					return nil, fmt.Errorf("unterminated regular expression")
				}
				j++
			}
			if j >= len(text) {
				return nil, fmt.Errorf("unterminated regular expression")
			}
			j++
			for j < len(text) && text[j] >= 'a' && text[j] <= 'z' {
				j++
			}
			token.kind, token.text, token.end = mongoTokenRegex, text[i:j], j
		default:
			token.kind, token.text, token.end = mongoTokenPunct, text[i:i+1], i+1
		}
		tokens = append(tokens, token)
		i = token.end
	}
	return tokens, nil
}

func allowsMongoRegex(tokens []mongoToken) bool {
	if len(tokens) == 0 {
		return true
	}
	last := tokens[len(tokens)-1]
	return last.kind == mongoTokenPunct && strings.Contains("(,[{:;!=&|?", last.text)
}

func readMongoString(text string, start int) (string, int, error) {
	quote := text[start]
	var sb strings.Builder
	i := start + 1
	for i < len(text) {
		c := text[i]
		if c == quote {
			return sb.String(), i + 1, nil
		}
		if c == '\n' && quote != '`' {
			break
		}
		if c != '\\' || i+1 >= len(text) {
			sb.WriteByte(c)
			i++
			continue
		}

		i++
		switch e := text[i]; e {
		case 'n':
			sb.WriteByte('\n')
		case 'r':
			sb.WriteByte('\r')
		case 't':
			sb.WriteByte('\t')
		case 'b':
			sb.WriteByte('\b')
		case 'f':
			sb.WriteByte('\f')
		case 'v':
			sb.WriteByte('\v')
		case '0':
			sb.WriteByte(0)
		case 'x', 'u':
			n := 2
			if e == 'u' {
				n = 4
			}
			if i+n < len(text) {
				if v, err := strconv.ParseUint(text[i+1:i+1+n], 16, 32); err == nil {
					sb.WriteRune(rune(v))
					i += n
					break
				}
			}
			sb.WriteByte(e)
		case '\n':
		default:
			sb.WriteByte(e)
		}
		i++
	}
	return "", 0, fmt.Errorf("unterminated string")
}

// SplitMongoStatements splits shell code into its statements, at the
// semicolons and at the line ends outside brackets; a line starting with a dot
// continues the statement before it.
func SplitMongoStatements(text string) []string {
	tokens, err := lexMongo(text)
	if err != nil {
		statement := strings.TrimSpace(text)
		if statement == "" {
			return []string{}
		}
		return []string{statement}
	}

	res := []string{}
	depth := 0
	start := -1
	end := 0
	flush := func() {
		if start >= 0 {
			res = append(res, strings.TrimSpace(text[start:end]))
		}
		start = -1
	}
	for i, token := range tokens {
		if depth == 0 && token.newline && start >= 0 && token.text != "." && tokens[i-1].text != "." {
			flush()
		}
		if token.kind == mongoTokenPunct {
			switch token.text {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				if depth > 0 {
					depth--
				}
			case ";":
				if depth == 0 {
					if start >= 0 {
						end = token.end
					}
					flush()
					continue
				}
			}
		}
		if start < 0 {
			start = token.start
		}
		end = token.end
	}
	flush()
	return res
}

type mongoChainCall struct {
	name string
	args []interface{}
}

// mongoCall is a parsed statement of the MongoDB shell.
type mongoCall struct {
	show string
	use  string
	// just "db", which shows the current database
	db         bool
	database   string
	collection string
	method     string
	args       []interface{}
	chain      []mongoChainCall
}

var (
	mongoShowRegex = regexp.MustCompile(`^\s*show\s+(\w+)\s*;?\s*$`)
	mongoUseRegex  = regexp.MustCompile(`^\s*use\s+([^\s;]+)\s*;?\s*$`)
)

type mongoParser struct {
	tokens []mongoToken
	pos    int
}

func (p *mongoParser) peek() mongoToken {
	if p.pos >= len(p.tokens) {
		return mongoToken{kind: mongoTokenEOF}
	}
	return p.tokens[p.pos]
}

func (p *mongoParser) next() mongoToken {
	token := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return token
}

func (p *mongoParser) isPunct(text string) bool {
	token := p.peek()
	return token.kind == mongoTokenPunct && token.text == text
}

func (p *mongoParser) expectPunct(text string) error {
	token := p.next()
	if token.kind != mongoTokenPunct || token.text != text {
		return fmt.Errorf("expected %q but got %s", text, describeMongoToken(token))
	}
	return nil
}

func describeMongoToken(token mongoToken) string {
	if token.kind == mongoTokenEOF {
		return "end of statement"
	}
	return strconv.Quote(token.text)
}

// ParseMongoStatement parses a statement such as show dbs, use app,
// db.runCommand({ping: 1}) or db.users.find({}).limit(10).
func ParseMongoStatement(statement string) (*mongoCall, error) {
	if m := mongoShowRegex.FindStringSubmatch(statement); m != nil {
		return &mongoCall{show: strings.ToLower(m[1])}, nil
	}
	if m := mongoUseRegex.FindStringSubmatch(statement); m != nil {
		return &mongoCall{use: strings.Trim(m[1], `"'`)}, nil
	}

	tokens, err := lexMongo(statement)
	if err != nil {
		return nil, err
	}
	p := &mongoParser{tokens: tokens}
	call := &mongoCall{}

	first := p.next()
	if first.kind != mongoTokenIdent || first.text != "db" {
		return nil, fmt.Errorf("a statement should start with db, show or use, such as db.users.find({})")
	}

	for {
		if p.isPunct(";") {
			p.next()
		}
		if p.peek().kind == mongoTokenEOF {
			break
		}
		if call.method != "" {
			if !p.isPunct(".") {
				return nil, fmt.Errorf("unexpected %s", describeMongoToken(p.peek()))
			}
			p.next()
			name := p.next()
			if name.kind != mongoTokenIdent {
				return nil, fmt.Errorf("expected a method after the dot but got %s", describeMongoToken(name))
			}
			args, err := p.parseArgs()
			if err != nil {
				return nil, err
			}
			call.chain = append(call.chain, mongoChainCall{name: name.text, args: args})
			continue
		}

		var name string
		if p.isPunct("[") {
			p.next()
			token := p.next()
			if token.kind != mongoTokenString {
				return nil, fmt.Errorf("expected a collection name in the brackets but got %s", describeMongoToken(token))
			}
			if err = p.expectPunct("]"); err != nil {
				return nil, err
			}
			call.collection = token.text
			continue
		}
		if err = p.expectPunct("."); err != nil {
			return nil, err
		}
		token := p.next()
		if token.kind != mongoTokenIdent {
			return nil, fmt.Errorf("expected a name after the dot but got %s", describeMongoToken(token))
		}
		name = token.text

		if !p.isPunct("(") {
			if call.collection == "" {
				call.collection = name
			} else {
				call.collection += "." + name
			}
			continue
		}

		args, err := p.parseArgs()
		if err != nil {
			return nil, err
		}
		if call.collection == "" && (name == "getSiblingDB" || name == "getCollection") {
			if len(args) != 1 {
				return nil, fmt.Errorf("%s() takes a name", name)
			}
			value, ok := args[0].(string)
			if !ok || value == "" {
				return nil, fmt.Errorf("%s() takes a name", name)
			}
			if name == "getSiblingDB" {
				call.database = value
			} else {
				call.collection = value
			}
			continue
		}
		call.method = name
		call.args = args
	}

	if call.method == "" {
		if call.collection != "" {
			return nil, fmt.Errorf("call a method of the collection: %s, such as db.%s.find({})", call.collection, call.collection)
		}
		call.db = true
	}
	return call, nil
}

func (p *mongoParser) parseArgs() ([]interface{}, error) {
	if err := p.expectPunct("("); err != nil {
		return nil, err
	}
	args := []interface{}{}
	for !p.isPunct(")") {
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		args = append(args, value)
		if p.isPunct(",") {
			p.next()
			continue
		}
		if !p.isPunct(")") {
			return nil, fmt.Errorf("expected \",\" or \")\" but got %s", describeMongoToken(p.peek()))
		}
	}
	p.next()
	return args, nil
}

func (p *mongoParser) parseValue() (interface{}, error) {
	token := p.next()
	switch token.kind {
	case mongoTokenString:
		return token.text, nil
	case mongoTokenNumber:
		return parseMongoNumber(token.text, false)
	case mongoTokenRegex:
		end := strings.LastIndexByte(token.text, '/')
		return bson.Regex{Pattern: token.text[1:end], Options: token.text[end+1:]}, nil
	case mongoTokenPunct:
		switch token.text {
		case "{":
			return p.parseObject()
		case "[":
			return p.parseArray()
		case "-", "+":
			number := p.next()
			if number.kind == mongoTokenIdent && number.text == "Infinity" {
				if token.text == "-" {
					return math.Inf(-1), nil
				}
				return math.Inf(1), nil
			}
			if number.kind != mongoTokenNumber {
				return nil, fmt.Errorf("expected a number after %q", token.text)
			}
			return parseMongoNumber(number.text, token.text == "-")
		}
	case mongoTokenIdent:
		switch token.text {
		case "true":
			return true, nil
		case "false":
			return false, nil
		case "null":
			return nil, nil
		case "undefined":
			return bson.Undefined{}, nil
		case "Infinity":
			return math.Inf(1), nil
		case "NaN":
			return math.NaN(), nil
		case "new":
			name := p.next()
			if name.kind != mongoTokenIdent {
				return nil, fmt.Errorf("expected a type after new but got %s", describeMongoToken(name))
			}
			return p.parseConstructor(name.text)
		default:
			if p.isPunct("(") {
				return p.parseConstructor(token.text)
			}
			if token.text == "MinKey" {
				return bson.MinKey{}, nil
			}
			if token.text == "MaxKey" {
				return bson.MaxKey{}, nil
			}
		}
	}
	return nil, fmt.Errorf("unexpected %s", describeMongoToken(token))
}

func (p *mongoParser) parseObject() (interface{}, error) {
	doc := bson.D{}
	for !p.isPunct("}") {
		key := p.next()
		if key.kind != mongoTokenIdent && key.kind != mongoTokenString && key.kind != mongoTokenNumber {
			return nil, fmt.Errorf("expected a key but got %s", describeMongoToken(key))
		}
		if err := p.expectPunct(":"); err != nil {
			return nil, err
		}
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		doc = append(doc, bson.E{Key: key.text, Value: value})
		if p.isPunct(",") {
			p.next()
			continue
		}
		if !p.isPunct("}") {
			return nil, fmt.Errorf("expected \",\" or \"}\" but got %s", describeMongoToken(p.peek()))
		}
	}
	p.next()
	return doc, nil
}

func (p *mongoParser) parseArray() (interface{}, error) {
	array := bson.A{}
	for !p.isPunct("]") {
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		array = append(array, value)
		if p.isPunct(",") {
			p.next()
			continue
		}
		if !p.isPunct("]") {
			return nil, fmt.Errorf("expected \",\" or \"]\" but got %s", describeMongoToken(p.peek()))
		}
	}
	p.next()
	return array, nil
}

func parseMongoNumber(text string, negative bool) (interface{}, error) {
	sign := ""
	if negative {
		sign = "-"
	}
	if strings.HasPrefix(text, "0x") || strings.HasPrefix(text, "0X") {
		v, err := strconv.ParseInt(sign+text[2:], 16, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number: %s", text)
		}
		return narrowMongoInt(v), nil
	}
	if !strings.ContainsAny(text, ".eE") {
		if v, err := strconv.ParseInt(sign+text, 10, 64); err == nil {
			return narrowMongoInt(v), nil
		}
	}
	v, err := strconv.ParseFloat(sign+text, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid number: %s", text)
	}
	return v, nil
}

// narrowMongoInt stores a whole number the way the shell does: as a 32-bit integer when it fits.
func narrowMongoInt(v int64) interface{} {
	if v >= math.MinInt32 && v <= math.MaxInt32 {
		return int32(v)
	}
	return v
}

func (p *mongoParser) parseConstructor(name string) (interface{}, error) {
	if !p.isPunct("(") {
		return nil, fmt.Errorf("expected \"(\" after %s", name)
	}
	args, err := p.parseArgs()
	if err != nil {
		return nil, err
	}
	str := func(i int) (string, bool) {
		if i >= len(args) {
			return "", false
		}
		s, ok := args[i].(string)
		return s, ok
	}

	switch name {
	case "ObjectId", "ObjectID":
		if len(args) == 0 {
			return bson.NewObjectID(), nil
		}
		s, _ := str(0)
		id, err := bson.ObjectIDFromHex(s)
		if err != nil {
			return nil, fmt.Errorf("invalid ObjectId: %v", args[0])
		}
		return id, nil
	case "ISODate", "Date":
		if len(args) == 0 {
			return bson.NewDateTimeFromTime(time.Now()), nil
		}
		if s, ok := str(0); ok {
			t, err := parseMongoDate(s)
			if err != nil {
				return nil, err
			}
			return bson.NewDateTimeFromTime(t), nil
		}
		switch v := args[0].(type) {
		case int32:
			return bson.DateTime(v), nil
		case int64:
			return bson.DateTime(v), nil
		case float64:
			return bson.DateTime(int64(v)), nil
		}
		return nil, fmt.Errorf("invalid date: %v", args[0])
	case "NumberLong", "Long", "NumberInt", "Int32", "Double":
		var f float64
		switch v := firstArg(args).(type) {
		case string:
			parsed, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid number: %s", v)
			}
			f = parsed
			if name == "NumberLong" || name == "Long" {
				if n, err := strconv.ParseInt(v, 10, 64); err == nil {
					return n, nil
				}
			}
		case int32:
			f = float64(v)
		case int64:
			f = float64(v)
		case float64:
			f = v
		case nil:
		default:
			return nil, fmt.Errorf("invalid number: %v", v)
		}
		switch name {
		case "NumberInt", "Int32":
			return int32(f), nil
		case "Double":
			return f, nil
		default:
			if n, ok := firstArg(args).(int64); ok {
				return n, nil
			}
			return int64(f), nil
		}
	case "NumberDecimal", "Decimal128":
		s := fmt.Sprint(firstArg(args))
		if v, ok := str(0); ok {
			s = v
		}
		d, err := bson.ParseDecimal128(s)
		if err != nil {
			return nil, fmt.Errorf("invalid decimal: %s", s)
		}
		return d, nil
	case "UUID":
		s, _ := str(0)
		b, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
		if err != nil || len(b) != 16 {
			return nil, fmt.Errorf("invalid UUID: %s", s)
		}
		return bson.Binary{Subtype: 4, Data: b}, nil
	case "BinData":
		if len(args) != 2 {
			return nil, fmt.Errorf("BinData() takes a subtype and a base64 string")
		}
		subtype, ok := args[0].(int32)
		s, ok2 := str(1)
		if !ok || !ok2 {
			return nil, fmt.Errorf("BinData() takes a subtype and a base64 string")
		}
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("invalid base64 in BinData(): %s", err.Error())
		}
		return bson.Binary{Subtype: byte(subtype), Data: b}, nil
	case "Timestamp":
		if len(args) == 1 {
			if doc, ok := args[0].(bson.D); ok {
				var t, i uint32
				for _, e := range doc {
					n, _ := toMongoInt(e.Value)
					if e.Key == "t" {
						t = uint32(n)
					} else if e.Key == "i" {
						i = uint32(n)
					}
				}
				return bson.Timestamp{T: t, I: i}, nil
			}
		}
		if len(args) == 2 {
			t, ok := toMongoInt(args[0])
			i, ok2 := toMongoInt(args[1])
			if ok && ok2 {
				return bson.Timestamp{T: uint32(t), I: uint32(i)}, nil
			}
		}
		return nil, fmt.Errorf("Timestamp() takes the seconds and an increment")
	case "RegExp":
		pattern, ok := str(0)
		if !ok {
			if regex, ok := firstArg(args).(bson.Regex); ok {
				return regex, nil
			}
			return nil, fmt.Errorf("RegExp() takes a pattern")
		}
		flags, _ := str(1)
		return bson.Regex{Pattern: pattern, Options: flags}, nil
	case "MinKey":
		return bson.MinKey{}, nil
	case "MaxKey":
		return bson.MaxKey{}, nil
	}
	return nil, fmt.Errorf("unknown function: %s()", name)
}

func firstArg(args []interface{}) interface{} {
	if len(args) == 0 {
		return nil
	}
	return args[0]
}

func toMongoInt(value interface{}) (int64, bool) {
	switch v := value.(type) {
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case float64:
		if v == math.Trunc(v) {
			return int64(v), true
		}
	}
	return 0, false
}

func parseMongoDate(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05Z0700", "2006-01-02 15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid date: %s", s)
}

var mongoTypeNames = map[bson.Type]string{
	bson.TypeDouble:           "double",
	bson.TypeString:           "string",
	bson.TypeEmbeddedDocument: "object",
	bson.TypeArray:            "array",
	bson.TypeBinary:           "binData",
	bson.TypeObjectID:         "objectId",
	bson.TypeBoolean:          "bool",
	bson.TypeDateTime:         "date",
	bson.TypeRegex:            "regex",
	bson.TypeInt32:            "int",
	bson.TypeTimestamp:        "timestamp",
	bson.TypeInt64:            "long",
	bson.TypeDecimal128:       "decimal",
}

// formatMongoValue shows a value the way the shell prints it, with the documents and arrays as JSON.
func formatMongoValue(rv bson.RawValue) *string {
	var s string
	switch rv.Type {
	case bson.TypeNull, bson.TypeUndefined:
		return nil
	case bson.TypeString:
		s = rv.StringValue()
	case bson.TypeObjectID:
		s = fmt.Sprintf("ObjectId(%q)", rv.ObjectID().Hex())
	case bson.TypeDateTime:
		s = rv.Time().UTC().Format("2006-01-02T15:04:05.000Z")
	case bson.TypeInt32:
		s = strconv.FormatInt(int64(rv.Int32()), 10)
	case bson.TypeInt64:
		s = strconv.FormatInt(rv.Int64(), 10)
	case bson.TypeDouble:
		v := rv.Double()
		if math.Abs(v) < 1e15 {
			s = strconv.FormatFloat(v, 'f', -1, 64)
		} else {
			s = strconv.FormatFloat(v, 'g', -1, 64)
		}
	case bson.TypeBoolean:
		s = strconv.FormatBool(rv.Boolean())
	case bson.TypeDecimal128:
		s = rv.Decimal128().String()
	default:
		var value interface{}
		err := rv.Unmarshal(&value)
		if err != nil {
			s = rv.String()
			break
		}
		b, err := bson.MarshalExtJSON(bson.D{{Key: "v", Value: value}}, false, false)
		if err != nil {
			s = rv.String()
			break
		}
		s = strings.TrimSuffix(strings.TrimPrefix(string(b), `{"v":`), "}")
	}

	if len(s) > sqlMaxCellSize {
		b := []byte(s[:sqlMaxCellSize])
		for len(b) > 0 && !utf8.Valid(b) {
			b = b[:len(b)-1]
		}
		s = string(b) + "..."
	}
	return &s
}
