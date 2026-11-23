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
	"regexp"
	"strings"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
)

// sqlScanner walks SQL text the way the database reads it, telling the
// strings, quoted identifiers and comments apart from the code.
type sqlScanner struct {
	text     string
	protocol string
	pos      int
}

const (
	sqlTokenCode = iota
	sqlTokenQuoted
	sqlTokenComment
	// a MySQL comment of the form /*! ... */ or /*M! ... */, whose content the server runs
	sqlTokenExecutableComment
)

var (
	// the line of a SQL Server script that ends a batch
	sqlServerBatchSeparator = regexp.MustCompile(`(?i)^[ \t]*GO(?:[ \t]+\d+)?[ \t]*\r?$`)
	// the line of an Oracle script that ends a statement or a PL/SQL block
	oracleBlockSeparator = regexp.MustCompile(`^[ \t]*/[ \t]*\r?$`)
	oraclePlsqlBlock     = regexp.MustCompile(`^(?:BEGIN|DECLARE)\b|^CREATE\s+(?:OR\s+REPLACE\s+)?(?:(?:NON)?EDITIONABLE\s+)?(?:PROCEDURE|FUNCTION|PACKAGE|TRIGGER|TYPE|LIBRARY)\b`)
)

func isIdentChar(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func (s *sqlScanner) isMysql() bool {
	return s.protocol == ProtocolMysql
}

// isOracleLike is Oracle and Dameng, whose scripts end a PL/SQL block with a slash line.
func isOracleLike(protocol string) bool {
	return protocol == ProtocolOracle || protocol == ProtocolDameng
}

// next returns the kind and the end of the token at the position.
func (s *sqlScanner) next() (int, int) {
	text := s.text
	i := s.pos
	c := text[i]
	rest := text[i:]
	isMysql := s.isMysql()
	isPostgresql := s.protocol == ProtocolPostgresql
	// ClickHouse quotes like MySQL, with backslashes in identifiers too
	isClickhouse := s.protocol == ProtocolClickhouse

	switch {
	case s.protocol == ProtocolOracle && (c == 'q' || c == 'Q') && len(rest) > 2 && rest[1] == '\'' && (i == 0 || !isIdentChar(text[i-1])):
		// q'[...]', q'{...}' and the like, with any other character as its own closing one
		closing := rest[2]
		switch closing {
		case '[':
			closing = ']'
		case '{':
			closing = '}'
		case '(':
			closing = ')'
		case '<':
			closing = '>'
		}
		end := strings.Index(rest[3:], string(closing)+"'")
		if end < 0 {
			return sqlTokenQuoted, len(text)
		}
		return sqlTokenQuoted, i + 3 + end + 2
	case c == '[' && s.protocol == ProtocolSqlServer:
		j := i + 1
		for j < len(text) {
			if text[j] == ']' {
				if j+1 < len(text) && text[j+1] == ']' {
					j += 2
					continue
				}
				return sqlTokenQuoted, j + 1
			}
			j++
		}
		return sqlTokenQuoted, len(text)
	case c == '\'' || c == '"' || c == '`' && (isMysql || isClickhouse):
		backslash := isMysql && c != '`' || isClickhouse
		if isPostgresql && c == '\'' && i > 0 && (text[i-1] == 'E' || text[i-1] == 'e') && (i == 1 || !isIdentChar(text[i-2])) {
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
	case strings.HasPrefix(rest, "--") && (!isMysql || len(rest) == 2 || rest[2] <= ' '), c == '#' && (isMysql || isClickhouse):
		j := strings.IndexByte(rest, '\n')
		if j < 0 {
			return sqlTokenComment, len(text)
		}
		return sqlTokenComment, i + j
	case strings.HasPrefix(rest, "/*"):
		kind := sqlTokenComment
		if isMysql && (strings.HasPrefix(rest, "/*!") || strings.HasPrefix(rest, "/*M!")) {
			kind = sqlTokenExecutableComment
		}
		// PostgreSQL and SQL Server nest block comments
		nested := isPostgresql || s.protocol == ProtocolSqlServer || isClickhouse
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
			if nested && strings.HasPrefix(text[j:], "/*") {
				depth++
				j += 2
				continue
			}
			j++
		}
		return kind, len(text)
	case c == '$' && isPostgresql && (i == 0 || !isIdentChar(text[i-1])):
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

// getSeparatorLine returns the end of the line starting at the position when
// the line is a GO of SQL Server or a slash of Oracle, or -1.
func (s *sqlScanner) getSeparatorLine(start int) int {
	var separator *regexp.Regexp
	switch s.protocol {
	case ProtocolSqlServer:
		separator = sqlServerBatchSeparator
	case ProtocolOracle, ProtocolDameng:
		separator = oracleBlockSeparator
	default:
		return -1
	}

	end := strings.IndexByte(s.text[start:], '\n')
	if end < 0 {
		end = len(s.text)
	} else {
		end += start
	}
	if separator.MatchString(s.text[start:end]) {
		return end
	}
	return -1
}

func isOraclePlsqlBlock(statement string) bool {
	text := strings.ToUpper(strings.TrimLeft(NormalizeSqlStatement(statement, ProtocolOracle), "( \t\r\n"))
	return oraclePlsqlBlock.MatchString(text)
}

// SplitSqlStatements splits SQL text at the semicolons outside strings,
// quoted identifiers and comments, leaving out the statements with no code. A
// SQL Server script is split into the batches between its GO lines instead, and
// an Oracle PL/SQL block runs up to a line with a slash.
func SplitSqlStatements(text string, protocol string) []string {
	s := &sqlScanner{text: text, protocol: protocol}
	res := []string{}
	start := 0
	add := func(end int) {
		statement := strings.TrimSpace(text[start:end])
		if NormalizeSqlStatement(statement, protocol) != "" {
			res = append(res, statement)
		}
	}

	if end := s.getSeparatorLine(0); end >= 0 {
		start = end
		s.pos = end
	}
	for s.pos < len(text) {
		kind, end := s.next()
		if kind == sqlTokenCode {
			switch text[s.pos] {
			case ';':
				if protocol != ProtocolSqlServer && !(isOracleLike(protocol) && isOraclePlsqlBlock(text[start:s.pos])) {
					// Oracle wants the semicolon of a statement left out, and takes it only at the end of a block
					add(s.pos)
					start = end
				}
			case '\n':
				if lineEnd := s.getSeparatorLine(end); lineEnd >= 0 {
					add(s.pos)
					start = lineEnd
					end = lineEnd
				}
			}
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
	s := &sqlScanner{text: statement, protocol: protocol}
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

// SplitDatabaseStatements splits what the user ran into the statements that are
// checked, recorded and run one by one: SQL statements, the batches of a SQL
// Server script, the lines of Redis commands or the calls of the MongoDB shell.
func SplitDatabaseStatements(text string, protocol string) []string {
	switch protocol {
	case ProtocolRedis:
		res := []string{}
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				res = append(res, line)
			}
		}
		return res
	case ProtocolMongodb:
		return SplitMongoStatements(text)
	default:
		return SplitSqlStatements(text, protocol)
	}
}

// getStatementForms returns the ways a statement is matched against the command
// rules: as it was typed, without its comments, and for a SQL Server batch, each of
// its lines and statements, as a batch may hold many statements.
func getStatementForms(statement string, protocol string) []string {
	forms := []string{statement}
	add := func(form string) {
		form = strings.TrimSpace(form)
		if form == "" {
			return
		}
		for _, f := range forms {
			if f == form {
				return
			}
		}
		forms = append(forms, form)
	}

	switch protocol {
	case ProtocolRedis:
		args, err := ParseRedisCommand(statement)
		if err == nil {
			add(strings.Join(args, " "))
		}
	case ProtocolMongodb:
		add(NormalizeMongoStatement(statement))
	default:
		normalized := NormalizeSqlStatement(statement, protocol)
		add(normalized)
		if protocol == ProtocolSqlServer {
			s := &sqlScanner{text: normalized, protocol: protocol}
			start := 0
			for s.pos < len(normalized) {
				kind, end := s.next()
				if kind == sqlTokenCode && (normalized[s.pos] == ';' || normalized[s.pos] == '\n') {
					add(normalized[start:s.pos])
					start = end
				}
				s.pos = end
			}
			add(normalized[start:])
		}
	}
	return forms
}

// NormalizeMongoStatement writes a shell statement without its comments and spaces.
func NormalizeMongoStatement(statement string) string {
	tokens, err := lexMongo(statement)
	if err != nil {
		return statement
	}
	var sb strings.Builder
	for i, token := range tokens {
		if i > 0 && tokens[i-1].kind == mongoTokenIdent && token.kind == mongoTokenIdent {
			sb.WriteByte(' ')
		}
		sb.WriteString(statement[token.start:token.end])
	}
	return sb.String()
}

// MatchStatementRule matches the command rules against each form of a
// statement, a Block rule before a Review rule before an Alert rule.
func MatchStatementRule(user *casdoorsdk.User, asset *Asset, statement string) (*CommandRule, error) {
	var res *CommandRule
	for _, form := range getStatementForms(statement, asset.Protocol) {
		rule, err := MatchCommandRule(user, asset, form)
		if err != nil {
			return nil, err
		}
		if rule != nil && rule.Action == CommandActionBlock {
			return rule, nil
		}
		res = MoreSevereRule(res, rule)
	}
	return res, nil
}
