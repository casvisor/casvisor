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

package demo

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/beego/beego/logs"
	"golang.org/x/crypto/ssh"
)

const (
	maxFileSize  = 64 * 1024
	maxFileCount = 500
	maxLineSize  = 4096

	Username = "demo"
	Password = "demo"
	hostname = "casvisor-demo"
)

// The demo SSH server never runs anything on the host: every session gets its own fake shell
// over an in-memory file system, so it is safe to expose to anonymous visitors.
func startSshServer(address string) error {
	seed := sha256.Sum256([]byte("casvisor demo ssh host key"))
	signer, err := ssh.NewSignerFromKey(ed25519.NewKeyFromSeed(seed[:]))
	if err != nil {
		return err
	}

	config := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if conn.User() == Username && subtle.ConstantTimeCompare(password, []byte(Password)) == 1 {
				return nil, nil
			}
			return nil, fmt.Errorf("wrong password")
		},
	}
	config.AddHostKey(signer)

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				logs.Error("demo ssh server: %s", err.Error())
				return
			}
			go handleSshConn(conn, config)
		}
	}()
	return nil
}

func handleSshConn(conn net.Conn, config *ssh.ServerConfig) {
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(30 * time.Minute))
	sshConn, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(requests)

	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "only session channels are supported")
			continue
		}

		channel, channelRequests, err := newChannel.Accept()
		if err != nil {
			return
		}
		go handleSshSession(channel, channelRequests)
	}
}

func handleSshSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()

	shell := newFakeShell(channel)
	for request := range requests {
		switch request.Type {
		case "pty-req", "env", "window-change":
			_ = request.Reply(true, nil)
		case "shell":
			_ = request.Reply(true, nil)
			go func() {
				shell.run()
				_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
				_ = channel.Close()
			}()
		case "exec":
			_ = request.Reply(true, nil)
			var payload struct{ Command string }
			_ = ssh.Unmarshal(request.Payload, &payload)
			shell.execute(payload.Command)
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
			return
		default:
			_ = request.Reply(false, nil)
		}
	}
}

type fakeShell struct {
	rw      io.ReadWriter
	files   map[string]string
	dirs    map[string]bool
	cwd     string
	history []string
	started time.Time
	exited  bool
}

func newFakeShell(rw io.ReadWriter) *fakeShell {
	shell := &fakeShell{
		rw:      rw,
		files:   map[string]string{},
		dirs:    map[string]bool{"/": true},
		cwd:     "/home/demo",
		started: time.Now(),
	}
	for path, content := range demoFiles {
		shell.writeFile(path, content)
	}
	shell.mkdirAll("/tmp")
	return shell
}

var demoFiles = map[string]string{
	"/home/demo/README.md": `# Casvisor demo

You are in a sandboxed demo shell. Nothing here touches a real server:
the file system lives in memory and is thrown away when you disconnect.

Things to try:
  ls -l, cat notes.txt, echo hello > hello.txt, history, top, sudo -i

Casvisor records this session: administrators see it in the session list
and can end it at any time.
`,
	"/home/demo/notes.txt": "Casvisor is an open-source bastion host for SSH, RDP and VNC.\nhttps://github.com/casvisor/casvisor\n",
	"/home/demo/.bashrc":   "export PS1='\\u@\\h:\\w\\$ '\n",
	"/etc/hostname":        hostname + "\n",
	"/etc/os-release":      "PRETTY_NAME=\"Ubuntu 24.04 LTS\"\nNAME=\"Ubuntu\"\nVERSION_ID=\"24.04\"\nID=ubuntu\n",
	"/etc/passwd":          "root:x:0:0:root:/root:/bin/bash\ndemo:x:1000:1000:Casvisor Demo:/home/demo:/bin/bash\n",
	"/var/log/auth.log":    "Accepted password for demo from 127.0.0.1 port 51234 ssh2\n",
}

func (s *fakeShell) print(format string, args ...interface{}) {
	text := fmt.Sprintf(format, args...)
	_, _ = io.WriteString(s.rw, strings.ReplaceAll(text, "\n", "\r\n"))
}

func (s *fakeShell) prompt() {
	dir := s.cwd
	if dir == "/home/demo" || strings.HasPrefix(dir, "/home/demo/") {
		dir = "~" + strings.TrimPrefix(dir, "/home/demo")
	}
	s.print("\x1b[1;32m%s@%s\x1b[0m:\x1b[1;34m%s\x1b[0m$ ", Username, hostname, dir)
}

func (s *fakeShell) run() {
	s.print("Welcome to Ubuntu 24.04 LTS (Casvisor demo sandbox)\n\nType 'help' to see the available commands, 'cat README.md' to get started.\n\n")
	s.prompt()

	line := []byte{}
	historyIndex := 0
	buf := make([]byte, 256)
	for !s.exited {
		n, err := s.rw.Read(buf)
		if err != nil {
			return
		}

		for i := 0; i < n; i++ {
			b := buf[i]
			switch {
			case b == '\r' || b == '\n':
				if b == '\n' && i > 0 && buf[i-1] == '\r' {
					continue
				}
				s.print("\n")
				command := strings.TrimSpace(string(line))
				line = line[:0]
				if command != "" {
					s.history = append(s.history, command)
				}
				historyIndex = len(s.history)
				s.execute(command)
				if s.exited {
					return
				}
				s.prompt()
			case b == 0x7f || b == 0x08:
				if len(line) > 0 {
					_, size := utf8.DecodeLastRune(line)
					line = line[:len(line)-size]
					s.print("\b \b")
				}
			case b == 0x03:
				line = line[:0]
				s.print("^C\n")
				s.prompt()
			case b == 0x04:
				if len(line) == 0 {
					s.print("logout\n")
					return
				}
			case b == 0x0c:
				s.print("\x1b[H\x1b[2J")
				s.prompt()
				_, _ = s.rw.Write(line)
			case b == 0x1b && i+2 < n && buf[i+1] == '[':
				key := buf[i+2]
				i += 2
				if key != 'A' && key != 'B' {
					continue
				}
				if key == 'A' && historyIndex > 0 {
					historyIndex--
				} else if key == 'B' && historyIndex < len(s.history) {
					historyIndex++
				} else {
					continue
				}
				s.print("\r\x1b[K")
				s.prompt()
				line = line[:0]
				if historyIndex < len(s.history) {
					line = append(line, s.history[historyIndex]...)
				}
				_, _ = s.rw.Write(line)
			case b >= 0x20 && len(line) < maxLineSize:
				line = append(line, b)
				_, _ = s.rw.Write([]byte{b})
			}
		}
	}
}

func (s *fakeShell) resolve(path string) string {
	if path == "" || path == "~" {
		return "/home/demo"
	}
	if strings.HasPrefix(path, "~/") {
		path = "/home/demo/" + path[2:]
	}
	if !strings.HasPrefix(path, "/") {
		path = s.cwd + "/" + path
	}

	parts := []string{}
	for _, part := range strings.Split(path, "/") {
		switch part {
		case "", ".":
		case "..":
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
		default:
			parts = append(parts, part)
		}
	}
	return "/" + strings.Join(parts, "/")
}

func parentDir(path string) string {
	index := strings.LastIndex(path, "/")
	if index <= 0 {
		return "/"
	}
	return path[:index]
}

func (s *fakeShell) mkdirAll(path string) {
	for path != "/" {
		if len(s.dirs)+len(s.files) >= maxFileCount {
			return
		}
		s.dirs[path] = true
		path = parentDir(path)
	}
}

func (s *fakeShell) writeFile(path string, content string) {
	if _, ok := s.files[path]; !ok && len(s.dirs)+len(s.files) >= maxFileCount {
		return
	}
	if len(content) > maxFileSize {
		content = content[:maxFileSize]
	}
	s.mkdirAll(parentDir(path))
	s.files[path] = content
}

func (s *fakeShell) list(dir string) []string {
	names := []string{}
	prefix := strings.TrimSuffix(dir, "/") + "/"
	for path := range s.dirs {
		if path != "/" && parentDir(path) == dir {
			names = append(names, strings.TrimPrefix(path, prefix)+"/")
		}
	}
	for path := range s.files {
		if parentDir(path) == dir {
			names = append(names, strings.TrimPrefix(path, prefix))
		}
	}
	sort.Strings(names)
	return names
}

func (s *fakeShell) execute(command string) {
	if command == "" {
		return
	}

	redirect := ""
	appendMode := false
	if index := strings.Index(command, ">"); index >= 0 {
		target := command[index+1:]
		if strings.HasPrefix(target, ">") {
			appendMode = true
			target = target[1:]
		}
		redirect = strings.TrimSpace(target)
		command = strings.TrimSpace(command[:index])
	}

	args := strings.Fields(command)
	if len(args) == 0 {
		return
	}

	output := s.run1(args)
	if redirect == "" {
		s.print("%s", output)
		return
	}

	path := s.resolve(redirect)
	if s.dirs[path] {
		s.print("bash: %s: Is a directory\n", redirect)
		return
	}
	if !s.dirs[parentDir(path)] {
		s.print("bash: %s: No such file or directory\n", redirect)
		return
	}
	if appendMode {
		output = s.files[path] + output
	}
	s.writeFile(path, output)
}

func (s *fakeShell) run1(args []string) string {
	name := args[0]
	switch name {
	case "help":
		return "Available commands: ls, cd, pwd, cat, echo, touch, mkdir, rm, history, clear,\n" +
			"whoami, id, hostname, uname, date, uptime, df, free, ps, top, sudo, exit\n"
	case "pwd":
		return s.cwd + "\n"
	case "cd":
		path := "/home/demo"
		if len(args) > 1 {
			path = s.resolve(args[1])
		}
		if !s.dirs[path] {
			return fmt.Sprintf("bash: cd: %s: No such file or directory\n", args[len(args)-1])
		}
		s.cwd = path
		return ""
	case "ls", "ll":
		long := name == "ll"
		paths := []string{}
		for _, arg := range args[1:] {
			if strings.HasPrefix(arg, "-") {
				long = long || strings.Contains(arg, "l")
			} else {
				paths = append(paths, arg)
			}
		}
		if len(paths) == 0 {
			paths = []string{"."}
		}

		var sb strings.Builder
		for _, arg := range paths {
			path := s.resolve(arg)
			if _, ok := s.files[path]; ok {
				sb.WriteString(arg + "\n")
				continue
			}
			if !s.dirs[path] {
				sb.WriteString(fmt.Sprintf("ls: cannot access '%s': No such file or directory\n", arg))
				continue
			}
			for _, entry := range s.list(path) {
				if strings.HasPrefix(entry, ".") && !long {
					continue
				}
				if !long {
					sb.WriteString(entry + "  ")
					continue
				}
				if strings.HasSuffix(entry, "/") {
					sb.WriteString(fmt.Sprintf("drwxr-xr-x 2 demo demo  4096 %s \x1b[1;34m%s\x1b[0m\n", s.started.Format("Jan _2 15:04"), strings.TrimSuffix(entry, "/")))
				} else {
					size := len(s.files[strings.TrimSuffix(path, "/")+"/"+entry])
					sb.WriteString(fmt.Sprintf("-rw-r--r-- 1 demo demo %5d %s %s\n", size, s.started.Format("Jan _2 15:04"), entry))
				}
			}
			if !long {
				sb.WriteString("\n")
			}
		}
		return sb.String()
	case "cat":
		var sb strings.Builder
		for _, arg := range args[1:] {
			path := s.resolve(arg)
			if content, ok := s.files[path]; ok {
				sb.WriteString(content)
			} else if s.dirs[path] {
				sb.WriteString(fmt.Sprintf("cat: %s: Is a directory\n", arg))
			} else {
				sb.WriteString(fmt.Sprintf("cat: %s: No such file or directory\n", arg))
			}
		}
		return sb.String()
	case "echo":
		return strings.Trim(strings.Join(args[1:], " "), "\"'") + "\n"
	case "touch":
		for _, arg := range args[1:] {
			path := s.resolve(arg)
			if _, ok := s.files[path]; !ok && !s.dirs[path] && s.dirs[parentDir(path)] {
				s.writeFile(path, "")
			}
		}
		return ""
	case "mkdir":
		for _, arg := range args[1:] {
			if !strings.HasPrefix(arg, "-") {
				s.mkdirAll(s.resolve(arg))
			}
		}
		return ""
	case "rm":
		var sb strings.Builder
		for _, arg := range args[1:] {
			if strings.HasPrefix(arg, "-") {
				continue
			}
			path := s.resolve(arg)
			if _, ok := s.files[path]; ok {
				delete(s.files, path)
			} else {
				sb.WriteString(fmt.Sprintf("rm: cannot remove '%s': No such file or directory\n", arg))
			}
		}
		return sb.String()
	case "history":
		var sb strings.Builder
		for i, command := range s.history {
			sb.WriteString(fmt.Sprintf("%5d  %s\n", i+1, command))
		}
		return sb.String()
	case "clear":
		return "\x1b[H\x1b[2J"
	case "whoami":
		return Username + "\n"
	case "id":
		return "uid=1000(demo) gid=1000(demo) groups=1000(demo)\n"
	case "hostname":
		return hostname + "\n"
	case "uname":
		if len(args) > 1 && args[1] == "-a" {
			return "Linux " + hostname + " 6.8.0-45-generic #45-Ubuntu SMP x86_64 GNU/Linux\n"
		}
		return "Linux\n"
	case "date":
		return time.Now().UTC().Format("Mon Jan _2 15:04:05 UTC 2006") + "\n"
	case "uptime":
		return fmt.Sprintf(" %s up %d min,  1 user,  load average: 0.08, 0.03, 0.01\n", time.Now().UTC().Format("15:04:05"), int(time.Since(s.started).Minutes()))
	case "df":
		return "Filesystem      Size  Used Avail Use% Mounted on\n/dev/vda1        40G  6.1G   32G  17% /\ntmpfs           2.0G     0  2.0G   0% /dev/shm\n"
	case "free":
		return "               total        used        free      shared  buff/cache   available\nMem:           3.8Gi       612Mi       2.6Gi       1.0Mi       650Mi       3.0Gi\nSwap:             0B          0B          0B\n"
	case "ps", "top", "htop":
		return "    PID USER      %CPU %MEM COMMAND\n      1 root       0.0  0.3 systemd\n    612 root       0.0  0.2 sshd\n   1024 demo       0.0  0.1 -bash\n   1088 demo       0.0  0.1 " + name + "\n"
	case "sudo", "su":
		return "demo is not in the sudoers file.  This incident will be reported.\n"
	case "exit", "logout":
		s.exited = true
		return ""
	}
	return fmt.Sprintf("%s: command not found\n", name)
}
