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

package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

// request is a casvisor:// link. Any web page can open one, so every value is checked against what
// Casvisor puts there before it goes into a command line or a file.
type request struct {
	kind      string
	host      string
	port      string
	user      string
	password  string
	database  string
	dbType    string
	name      string
	username  string
	autoLogon bool
	certHash  string
}

var (
	reHost     = regexp.MustCompile(`^([A-Za-z0-9.\-]{1,253}|\[[0-9A-Fa-f:.]+\])$`)
	reToken    = regexp.MustCompile(`^[A-Za-z0-9._@\-]{1,256}$`)
	reDatabase = regexp.MustCompile(`^[A-Za-z0-9_.$\-]{0,128}$`)
	reUsername = regexp.MustCompile(`^[^\x00-\x1f\x7f"|;]{0,256}$`)
	reUnsafe   = regexp.MustCompile(`[\x00-\x1f\x7f"|;<>:*?/\\]+`)
	reCertHash = regexp.MustCompile(`^([0-9a-f]{64})?$`)
	reNotDns   = regexp.MustCompile(`[^A-Za-z0-9-]+`)
)

func parseRequest(link string) (*request, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(u.Scheme, scheme) || u.Host != "launch" {
		return nil, fmt.Errorf("not a Casvisor link: %s", link)
	}
	q := u.Query()

	r := &request{
		kind:      q.Get("type"),
		host:      q.Get("host"),
		port:      q.Get("port"),
		user:      q.Get("user"),
		password:  q.Get("password"),
		database:  q.Get("database"),
		dbType:    q.Get("dbtype"),
		username:  q.Get("username"),
		autoLogon: q.Get("autologon") == "1",
		certHash:  q.Get("certhash"),
	}
	if r.kind == "rdp" {
		r.user = q.Get("token")
	}
	if r.kind != "ssh" && r.kind != "sftp" && r.kind != "rdp" && r.kind != "db" {
		return nil, fmt.Errorf("unknown type: %s", r.kind)
	}

	if !reHost.MatchString(r.host) {
		return nil, fmt.Errorf("invalid host: %s", r.host)
	}
	if r.port == "" {
		r.port = map[string]string{"ssh": "22", "sftp": "22", "rdp": "443"}[r.kind]
	}
	port, err := strconv.Atoi(r.port)
	if err != nil || port <= 0 || port > 65535 {
		return nil, fmt.Errorf("invalid port: %s", r.port)
	}
	if !reToken.MatchString(r.user) {
		return nil, fmt.Errorf("invalid token or user")
	}
	if r.password != "" && !reToken.MatchString(r.password) {
		return nil, fmt.Errorf("invalid password")
	}
	if !reDatabase.MatchString(r.database) {
		return nil, fmt.Errorf("invalid database: %s", r.database)
	}
	if !reUsername.MatchString(r.username) {
		return nil, fmt.Errorf("invalid username: %s", r.username)
	}
	if !reCertHash.MatchString(r.certHash) {
		return nil, fmt.Errorf("invalid certificate hash: %s", r.certHash)
	}

	r.name = strings.TrimSpace(reUnsafe.ReplaceAllString(q.Get("name"), "_"))
	if len(r.name) > 100 {
		r.name = r.name[:100]
	}
	if r.name == "" {
		r.name = r.host
	}
	return r, nil
}

func launch(link string) error {
	r, err := parseRequest(link)
	if err != nil {
		return err
	}
	config, path, err := loadConfig()
	if err != nil {
		return err
	}

	switch r.kind {
	case "ssh":
		return launchSsh(config, path, r)
	case "sftp":
		return launchSftp(config, path, r)
	case "rdp":
		return launchRdp(config, path, r)
	default:
		return launchDb(config, path, r)
	}
}

func missingClient(kind string, clients string, configPath string) error {
	return fmt.Errorf("no %s client is found. Install %s, or set the path of one in %s", kind, clients, configPath)
}

func launchSsh(config *Config, configPath string, r *request) error {
	name, path := pickClient(config, config.Ssh, sshPreferences)
	if path == "" {
		return missingClient("SSH", sshClients, configPath)
	}
	target := r.user + "@" + r.host
	switch name {
	case "xshell":
		return start(path, "-url", fmt.Sprintf("ssh://%s:%s", target, r.port))
	case "securecrt":
		return start(path, "/SSH2", "/L", r.user, "/P", r.port, r.host)
	case "putty":
		return start(path, "-ssh", "-P", r.port, target)
	case "openssh":
		return startInTerminal(config, configPath, r.name, path, "-p", r.port, target)
	default:
		return fmt.Errorf("unknown SSH client: %s, use one of %s", name, strings.Join(sshPreferences, ", "))
	}
}

func launchSftp(config *Config, configPath string, r *request) error {
	name, path := pickClient(config, config.Sftp, []string{"winscp", "filezilla"})
	if path == "" {
		return missingClient("SFTP", "WinSCP or FileZilla", configPath)
	}
	link := fmt.Sprintf("sftp://%s@%s:%s/", r.user, r.host, r.port)
	switch name {
	case "winscp", "filezilla":
		return start(path, link)
	default:
		return fmt.Errorf("unknown SFTP client: %s, use winscp or filezilla", name)
	}
}

func launchDb(config *Config, configPath string, r *request) error {
	choice, cli := config.Mysql, "mysql"
	if r.dbType == "postgresql" {
		choice, cli = config.Postgresql, "psql"
	} else if r.dbType != "mysql" {
		return fmt.Errorf("unknown database type: %s", r.dbType)
	}

	name, path := pickClient(config, choice, []string{cli, "dbeaver"})
	if path == "" {
		return missingClient("database", cli+" or DBeaver", configPath)
	}
	switch name {
	case "mysql":
		args := []string{"-h", r.host, "-P", r.port, "-u", r.user, "--password=" + r.password}
		if r.database != "" {
			args = append(args, r.database)
		}
		return startInTerminal(config, configPath, r.name, path, args...)
	case "psql":
		conninfo := fmt.Sprintf("host=%s port=%s user=%s password=%s", strings.Trim(r.host, "[]"), r.port, r.user, r.password)
		if r.database != "" {
			conninfo += " dbname=" + r.database
		}
		return startInTerminal(config, configPath, r.name, path, conninfo)
	case "dbeaver":
		con := fmt.Sprintf("driver=%s|host=%s|port=%s|user=%s|password=%s|name=%s|connect=true|openConsole=true|save=false", r.dbType, strings.Trim(r.host, "[]"), r.port, r.user, r.password, r.name)
		if r.database != "" {
			con += "|database=" + r.database
		}
		return start(path, "-con", con)
	default:
		return fmt.Errorf("unknown %s client: %s, use %s or dbeaver", r.dbType, name, cli)
	}
}

// getRdpAddress is the name of the remote computer in mstsc, which the gateway does not look at: the asset's
// name under .casvisor, so that a certificate trusted for it is never trusted for a real computer.
func getRdpAddress(r *request) string {
	return strings.Trim(reNotDns.ReplaceAllString(r.name, "-"), "-") + ".casvisor"
}

// getRdpFile is the .rdp file that has Remote Desktop Connection reach the asset through Casvisor's
// Remote Desktop Gateway with the token, as the web console downloads it. When Casvisor signs in to the
// asset, mstsc speaks TLS to Casvisor without CredSSP.
func getRdpFile(r *request) string {
	gateway := r.host
	if r.port != "443" {
		gateway += ":" + r.port
	}
	lines := []string{
		"full address:s:" + getRdpAddress(r),
		"username:s:" + r.username,
		"gatewayhostname:s:" + gateway,
		"gatewayusagemethod:i:1",
		"gatewayprofileusagemethod:i:1",
		"gatewaycredentialssource:i:5",
		"gatewayaccesstoken:s:" + r.user,
		"promptcredentialonce:i:0",
		"screen mode id:i:2",
		"authentication level:i:2",
	}
	if r.autoLogon {
		lines = append(lines, "enablecredsspsupport:i:0")
	}
	return string(rune(0xFEFF)) + strings.Join(lines, "\r\n") + "\r\n"
}

// writeRdpFile writes the .rdp file in UTF-16 as mstsc saves them, and removes it after a minute, as the
// token in it is used once.
func writeRdpFile(r *request) (string, func(), error) {
	b := make([]byte, 6)
	_, err := rand.Read(b)
	if err != nil {
		return "", nil, err
	}
	path := filepath.Join(os.TempDir(), fmt.Sprintf("casvisor-%s-%s.rdp", strings.ReplaceAll(r.name, " ", "_"), hex.EncodeToString(b)))

	data := []byte{}
	for _, unit := range utf16.Encode([]rune(getRdpFile(r))) {
		data = append(data, byte(unit), byte(unit>>8))
	}
	err = os.WriteFile(path, data, 0o600)
	if err != nil {
		return "", nil, err
	}
	return path, func() {
		time.Sleep(time.Minute)
		_ = os.Remove(path)
	}, nil
}

// start runs a client and leaves it running.
func start(path string, args ...string) error {
	cmd := exec.Command(path, args...)
	err := cmd.Start()
	if err != nil {
		return err
	}
	return cmd.Process.Release()
}
