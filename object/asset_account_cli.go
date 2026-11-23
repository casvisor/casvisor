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
	"io"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	cliStepTimeout = 30 * time.Second
	cliMaxAnswers  = 20
)

var (
	cliEscapePattern  = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\x1b[()][A-Z0-9]|\x1b[=>]`)
	cliPagerPattern   = regexp.MustCompile(`(?i)(-+ *more *-+|\(more\)|--more--|press any key)[^\n]*$`)
	cliYesNoPattern   = regexp.MustCompile(`(?i)(\[y/n\]|\(y/n\)|\[yes/no\]|\(yes/no\))[^\n]*[:?]?\s*$`)
	cliConfirmPattern = regexp.MustCompile(`(?i)(\[confirm\]|\[[^\]\n]*\]\s*[?:])\s*$`)
)

// cliSession is a shell on a network device, driven a line at a time: each line is sent and its
// output read up to the next prompt, answering pagers, password prompts and confirmations on the way.
type cliSession struct {
	client  *ssh.Client
	session *ssh.Session
	stdin   io.Writer
	output  *lockedBuffer
	read    int

	prompt  *regexp.Regexp
	errors  *regexp.Regexp
	secrets []string
}

func cleanCliText(text string) string {
	text = cliEscapePattern.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "\r", "")
	for strings.Contains(text, "\b") {
		i := strings.Index(text, "\b")
		if i > 0 {
			text = text[:i-1] + text[i+1:]
		} else {
			text = text[1:]
		}
	}
	return text
}

func lastCliLine(text string) string {
	return text[strings.LastIndex(text, "\n")+1:]
}

func openCliSession(asset *Asset, platform *Platform) (*cliSession, error) {
	if !strings.EqualFold(asset.Protocol, "SSH") {
		return nil, fmt.Errorf("the passwords of %s are changed over SSH, the asset: %s uses %s", platform.getDisplayName(), asset.Name, asset.Protocol)
	}
	prompt, err := regexp.Compile(platform.PromptPattern)
	if err != nil {
		return nil, err
	}
	var errorPattern *regexp.Regexp
	if strings.TrimSpace(platform.ErrorPattern) != "" {
		errorPattern, err = regexp.Compile(platform.ErrorPattern)
		if err != nil {
			return nil, err
		}
	}

	client, err := DialSsh(asset)
	if err != nil {
		return nil, err
	}
	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, err
	}
	c := &cliSession{client: client, session: session, output: newLockedBuffer(), prompt: prompt, errors: errorPattern, secrets: []string{asset.Password, asset.ElevationPassword}}
	err = session.RequestPty("vt100", 200, 500, ssh.TerminalModes{ssh.ECHO: 1})
	if err == nil {
		c.stdin, err = session.StdinPipe()
	}
	if err == nil {
		session.Stdout = c.output
		session.Stderr = c.output
		err = session.Shell()
	}
	if err != nil {
		c.close()
		return nil, err
	}

	_, err = c.waitForPrompt("", false, "")
	if err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

func (c *cliSession) close() {
	_ = c.session.Close()
	_ = c.client.Close()
}

func (c *cliSession) send(text string) error {
	_, err := io.WriteString(c.stdin, text)
	return err
}

func (c *cliSession) masked(text string) string {
	return truncate(strings.TrimSpace(maskSecret(text, c.secrets...)), 500)
}

// waitForPrompt reads the output of the command up to the prompt that follows it, answering a
// password prompt with password, and returns the output. The prompt is only taken after a line
// break, so that the echo of the command can't pass for it.
func (c *cliSession) waitForPrompt(command string, needLineBreak bool, password string) (string, error) {
	answers := 0
	for {
		var text, last string
		found := c.output.waitFor(cliStepTimeout, nil, func(all string) bool {
			text = cleanCliText(all[c.read:])
			if needLineBreak && !strings.Contains(text, "\n") {
				return false
			}
			last = lastCliLine(text)
			if strings.TrimSpace(last) == "" {
				return false
			}
			return c.prompt.MatchString(last) || cliPagerPattern.MatchString(last) || passwordPromptPattern.MatchString(last) || cliYesNoPattern.MatchString(last) || cliConfirmPattern.MatchString(last)
		})
		if !found {
			return "", fmt.Errorf("no prompt after %q in %s: %s", maskSecret(command, c.secrets...), cliStepTimeout, c.masked(text))
		}

		var answer string
		switch {
		case cliPagerPattern.MatchString(last):
			answer = " "
		case passwordPromptPattern.MatchString(last):
			if password == "" {
				return "", fmt.Errorf("the device asked for a password after %q: %s", maskSecret(command, c.secrets...), c.masked(text))
			}
			answer = password + "\r"
		case cliYesNoPattern.MatchString(last):
			answer = "y\r"
		case cliConfirmPattern.MatchString(last) && !c.prompt.MatchString(last):
			answer = "\r"
		default:
			c.read = len(c.output.String())
			return text, nil
		}

		answers++
		if answers > cliMaxAnswers {
			return "", fmt.Errorf("the device keeps asking after %q: %s", maskSecret(command, c.secrets...), c.masked(text))
		}
		c.read = len(c.output.String())
		needLineBreak = answer != " "
		err := c.send(answer)
		if err != nil {
			return "", err
		}
	}
}

// run sends a line and waits for the prompt, failing when the output looks like an error.
func (c *cliSession) run(line string, password string) error {
	c.read = len(c.output.String())
	err := c.send(line + "\r")
	if err != nil {
		return err
	}
	text, err := c.waitForPrompt(line, true, password)
	if err != nil {
		return err
	}
	if c.errors != nil && c.errors.MatchString(text) {
		return fmt.Errorf("%q failed: %s", maskSecret(line, c.secrets...), c.masked(text))
	}
	return nil
}

func getCliLines(commands string, vars map[string]string) []string {
	res := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(commands, "\r", ""), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for key, value := range vars {
			line = strings.ReplaceAll(line, "{"+key+"}", value)
		}
		res = append(res, line)
	}
	return res
}

// setCliAccountPassword signs in to the network device with the asset's own account, elevates
// by the platform's command when it has one, and runs the platform's commands for the password.
func setCliAccountPassword(asset *Asset, platform *Platform, username string, password string, create bool) error {
	commands := platform.ChangeCommands
	if create && strings.TrimSpace(platform.PushCommands) != "" {
		commands = platform.PushCommands
	}
	lines := getCliLines(commands, map[string]string{"username": username, "password": password})
	if len(lines) == 0 {
		return fmt.Errorf("the platform: %s has no commands to change passwords", platform.getDisplayName())
	}

	c, err := openCliSession(asset, platform)
	if err != nil {
		return err
	}
	defer c.close()
	c.secrets = append(c.secrets, password)

	if platform.Elevation == ElevationEnable && strings.TrimSpace(platform.ElevationCommand) != "" {
		err = c.run(strings.TrimSpace(platform.ElevationCommand), asset.ElevationPassword)
		if err != nil {
			if asset.ElevationPassword == "" {
				return fmt.Errorf("%s, fill in the elevation password of the asset", err.Error())
			}
			return err
		}
	}

	for _, line := range lines {
		err = c.run(line, password)
		if err != nil {
			return err
		}
	}
	return nil
}
