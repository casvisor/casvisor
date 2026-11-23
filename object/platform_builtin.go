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

const (
	PlatformCategoryHost     = "Host"
	PlatformCategoryNetwork  = "Network"
	PlatformCategoryDatabase = "Database"
	PlatformCategoryCloud    = "Cloud"
	PlatformCategoryWeb      = "Web"
)

func protocols(pairs ...interface{}) []*PlatformProtocol {
	res := []*PlatformProtocol{}
	for i := 0; i+1 < len(pairs); i += 2 {
		res = append(res, &PlatformProtocol{Name: pairs[i].(string), Port: pairs[i+1].(int)})
	}
	return res
}

func newHostPlatform(name string, displayName string, method string, elevation string, p []*PlatformProtocol) *Platform {
	return &Platform{
		Name: name, DisplayName: displayName, Category: PlatformCategoryHost, Protocols: p, Charset: "UTF-8",
		Elevation: elevation, ElevationCommand: map[string]string{ElevationSu: "su - root"}[elevation], Method: method,
		PasswordLength: 24, PasswordUppercase: true, PasswordLowercase: true, PasswordDigits: true, PasswordSymbols: defaultPasswordSymbols,
		CanChangePassword: method != "", CanPush: method != "", CanGather: platformMethods[method].gather,
	}
}

func newNetworkPlatform(name string, displayName string, elevation string, elevationCommand string, prompt string, errorPattern string, change string, push string) *Platform {
	return &Platform{
		Name: name, DisplayName: displayName, Category: PlatformCategoryNetwork, Protocols: protocols("SSH", 22, "Telnet", 23), Charset: "UTF-8",
		Elevation: elevation, ElevationCommand: elevationCommand, Method: PlatformMethodCli, ChangeCommands: change, PushCommands: push, PromptPattern: prompt, ErrorPattern: errorPattern,
		PasswordLength: 16, PasswordUppercase: true, PasswordLowercase: true, PasswordDigits: true, PasswordSymbols: "#%+-=_.",
		CanChangePassword: true, CanPush: true,
	}
}

func newDatabasePlatform(name string, displayName string, protocol string, port int, passwordLength int) *Platform {
	method := platformMethods[protocol]
	return &Platform{
		Name: name, DisplayName: displayName, Category: PlatformCategoryDatabase, Protocols: protocols(protocol, port), Charset: "UTF-8",
		Method: protocol, PasswordLength: passwordLength, PasswordUppercase: true, PasswordLowercase: true, PasswordDigits: true, PasswordSymbols: "#%+-=_.",
		CanChangePassword: method.change, CanPush: method.push, CanGather: method.gather,
	}
}

var builtInPlatforms = []*Platform{
	newHostPlatform("linux", "Linux", PlatformMethodShell, ElevationSudo, protocols("SSH", 22, "Telnet", 23, "VNC", 5900, "RDP", 3389)),
	newHostPlatform("unix", "Unix", PlatformMethodShell, ElevationSudo, protocols("SSH", 22, "Telnet", 23)),
	func() *Platform {
		p := newHostPlatform("macos", "macOS", PlatformMethodShell, ElevationSudo, protocols("SSH", 22, "VNC", 5900))
		p.CanGather = false
		return p
	}(),
	newHostPlatform("windows", "Windows", PlatformMethodPowerShell, "", protocols("RDP", 3389, "SSH", 22, "VNC", 5900, ProtocolDesktop, 0)),
	newNetworkPlatform("cisco-ios", "Cisco IOS", ElevationEnable, "enable",
		`[\w.\-]+(\([\w.\-]+\))?[>#]\s*$`,
		`(?m)^\s*% `,
		"terminal length 0\nconfigure terminal\nusername {username} secret {password}\nend\nwrite memory",
		""),
	newNetworkPlatform("huawei-vrp", "Huawei VRP", "", "super",
		`[<\[][^<>\[\]\r\n]+[>\]]\s*$`,
		`(?m)^\s*(Error:|\^)`,
		"screen-length 0 temporary\nsystem-view\naaa\nlocal-user {username} password irreversible-cipher {password}\nreturn\nsave",
		"screen-length 0 temporary\nsystem-view\naaa\nlocal-user {username} password irreversible-cipher {password}\nlocal-user {username} service-type ssh\nlocal-user {username} privilege level 1\nquit\nssh user {username} authentication-type password\nssh user {username} service-type stelnet\nreturn\nsave"),
	newNetworkPlatform("h3c-comware", "H3C Comware", "", "super",
		`[<\[][^<>\[\]\r\n]+[>\]]\s*$`,
		`(?m)^\s*(% |\^)`,
		"screen-length disable\nsystem-view\nlocal-user {username} class manage\npassword simple {password}\nreturn\nsave force",
		"screen-length disable\nsystem-view\nlocal-user {username} class manage\npassword simple {password}\nservice-type ssh\nauthorization-attribute user-role network-operator\nreturn\nsave force"),
	newNetworkPlatform("juniper-junos", "Juniper JunOS", "", "",
		`[\w.\-]+@[\w.\-]+[>#%]\s*$`,
		`(?mi)^\s*(error:|syntax error|unknown command)`,
		"set cli screen-length 0\nconfigure\nset system login user {username} authentication plain-text-password\ncommit and-quit",
		"set cli screen-length 0\nconfigure\nset system login user {username} class operator\nset system login user {username} authentication plain-text-password\ncommit and-quit"),
	newDatabasePlatform("mysql", "MySQL", ProtocolMysql, 3306, 24),
	newDatabasePlatform("mariadb", "MariaDB", ProtocolMysql, 3306, 24),
	newDatabasePlatform("postgresql", "PostgreSQL", ProtocolPostgresql, 5432, 24),
	newDatabasePlatform("sqlserver", "SQL Server", ProtocolSqlServer, 1433, 24),
	newDatabasePlatform("oracle", "Oracle", ProtocolOracle, 1521, 24),
	newDatabasePlatform("redis", "Redis", ProtocolRedis, 6379, 32),
	newDatabasePlatform("mongodb", "MongoDB", ProtocolMongodb, 27017, 24),
	{
		Name: "kubernetes", DisplayName: "Kubernetes", Category: PlatformCategoryCloud, Protocols: protocols(ProtocolKubernetes, KubernetesDefaultPort), Charset: "UTF-8",
		PasswordLength: 24, PasswordUppercase: true, PasswordLowercase: true, PasswordDigits: true, PasswordSymbols: defaultPasswordSymbols,
	},
	{
		Name: "website", DisplayName: "Website", Category: PlatformCategoryWeb, Protocols: protocols(ProtocolWeb, 80), Charset: "UTF-8",
		PasswordLength: 24, PasswordUppercase: true, PasswordLowercase: true, PasswordDigits: true, PasswordSymbols: defaultPasswordSymbols,
	},
}
