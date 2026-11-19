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
	"strings"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
)

// sqlScanner walks SQL text the way the database reads it, telling the
// strings, quoted identifiers and comments apart from the code.
type sqlScanner struct {
	text    string
	isMysql bool
	pos     int
}

const (
	sqlTokenCode = iota
	sqlTokenQuoted
	sqlTokenComment
	// a MySQL comment of the form /*! ... */ or /*M! ... */, whose content the server runs
	sqlTokenExecutableComment
)

func isIdentChar(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

// next returns the kind and the end of the token at the position.
func (s *sqlScanner) next() (int, int) {
	text := s.text
	i := s.pos
	c := text[i]
	rest := text[i:]

	switch {
	case c == '\'' || c == '"' || c == '`' && s.isMysql:
		backslash := s.isMysql && c != '`'
		if !s.isMysql && c == '\'' && i > 0 && (text[i-1] == 'E' || text[i-1] == 'e') && (i == 1 || !isIdentChar(text[i-2])) {
			backslash = true
		}
		j := i + 1
		for j < len(text) {
			if backslash && text[j] == '\\' {
				j += 2
				continue
			}
			if text[j] == c {
				if j+1 < len(text) && text[j+1] == c {
					j += 2
					continue
				}
				return sqlTokenQuoted, j + 1
			}
			j++
		}
		return sqlTokenQuoted, len(text)
	case strings.HasPrefix(rest, "--") && (!s.isMysql || len(rest) == 2 || rest[2] <= ' '), c == '#' && s.isMysql:
		j := strings.IndexByte(rest, '\n')
		if j < 0 {
			return sqlTokenComment, len(text)
		}
		return sqlTokenComment, i + j + 1
	case strings.HasPrefix(rest, "/*"):
		kind := sqlTokenComment
		if s.isMysql && (strings.HasPrefix(rest, "/*!") || strings.HasPrefix(rest, "/*M!")) {
			kind = sqlTokenExecutableComment
		}
		depth := 1
		j := i + 2
		for j < len(text) {
			if strings.HasPrefix(text[j:], "*/") {
				depth--
				j += 2
				if depth == 0 {
					return kind, j
				}
				continue
			}
			// PostgreSQL nests block comments
			if !s.isMysql && strings.HasPrefix(text[j:], "/*") {
				depth++
				j += 2
				continue
			}
			j++
		}
		return kind, len(text)
	case c == '$' && !s.isMysql && (i == 0 || !isIdentChar(text[i-1])):
		j := i + 1
		for j < len(text) && isIdentChar(text[j]) && text[j] != '$' {
			j++
		}
		if j < len(text) && text[j] == '$' && (j == i+1 || text[i+1] < '0' || text[i+1] > '9') {
			tag := text[i : j+1]
			end := strings.Index(text[j+1:], tag)
			if end < 0 {
				return sqlTokenQuoted, len(text)
			}
			return sqlTokenQuoted, j + 1 + end + len(tag)
		}
	}
	return sqlTokenCode, i + 1
}

// SplitSqlStatements splits SQL text at the semicolons outside strings,
// quoted identifiers and comments, leaving out the statements with no code.
func SplitSqlStatements(text string, protocol string) []string {
	s := &sqlScanner{text: text, isMysql: protocol == ProtocolMysql}
	res := []string{}
	start := 0
	add := func(end int) {
		statement := strings.TrimSpace(text[start:end])
		if NormalizeSqlStatement(statement, protocol) != "" {
			res = append(res, statement)
		}
	}

	for s.pos < len(text) {
		kind, end := s.next()
		if kind == sqlTokenCode && text[s.pos] == ';' {
			add(s.pos)
			start = end
		}
		s.pos = end
	}
	add(len(text))
	return res
}

// NormalizeSqlStatement drops the comments of a statement, keeping the content
// of the MySQL comments the server runs, so that a rule like ^\s*drop sees the
// statement the way the database does.
func NormalizeSqlStatement(statement string, protocol string) string {
	s := &sqlScanner{text: statement, isMysql: protocol == ProtocolMysql}
	var sb strings.Builder
	for s.pos < len(statement) {
		kind, end := s.next()
		switch kind {
		case sqlTokenComment:
			sb.WriteByte(' ')
		case sqlTokenExecutableComment:
			content := strings.TrimSuffix(statement[s.pos+2:end], "*/")
			content = strings.TrimLeft(strings.TrimPrefix(strings.TrimPrefix(content, "M"), "!"), "0123456789")
			sb.WriteString(" " + content + " ")
		default:
			sb.WriteString(statement[s.pos:end])
		}
		s.pos = end
	}
	return strings.TrimSpace(sb.String())
}

// getSqlKeyword returns the first word of a statement, in upper case.
func getSqlKeyword(statement string, protocol string) string {
	text := strings.TrimLeft(NormalizeSqlStatement(statement, protocol), "( \t\r\n")
	end := 0
	for end < len(text) && (text[end] >= 'a' && text[end] <= 'z' || text[end] >= 'A' && text[end] <= 'Z') {
		end++
	}
	return strings.ToUpper(text[:end])
}

// MatchSqlRule matches the command rules against a SQL statement as it was
// typed and without its comments, a Block rule before an Alert rule.
func MatchSqlRule(user *casdoorsdk.User, asset *Asset, statement string) (*CommandRule, error) {
	rule, err := MatchCommandRule(user, asset, statement)
	if err != nil || rule != nil && rule.Action == CommandActionBlock {
		return rule, err
	}

	normalized := NormalizeSqlStatement(statement, asset.Protocol)
	if normalized == statement {
		return rule, nil
	}
	other, err := MatchCommandRule(user, asset, normalized)
	if err != nil {
		return nil, err
	}
	if other != nil && (rule == nil || other.Action == CommandActionBlock) {
		return other, nil
	}
	return rule, nil
}
