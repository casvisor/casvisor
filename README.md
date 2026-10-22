<h1 align="center" style="border-bottom: none;">📦⚡️ Casvisor</h1>
<h3 align="center">An open-source bastion host (jump server) for SSH, RDP, VNC and Telnet, developed by Go and React.</h3>
<p align="center">
  <a href="#badge">
    <img alt="semantic-release" src="https://img.shields.io/badge/%20%20%F0%9F%93%A6%F0%9F%9A%80-semantic--release-e10079.svg">
  </a>
  <a href="https://hub.docker.com/r/casbin/casvisor">
    <img alt="docker pull casbin/casvisor" src="https://img.shields.io/docker/pulls/casbin/casvisor.svg">
  </a>
  <a href="https://github.com/casvisor/casvisor/releases/latest">
    <img alt="GitHub Release" src="https://img.shields.io/github/v/release/casvisor/casvisor.svg">
  </a>
  <a href="https://hub.docker.com/r/casbin/casvisor">
    <img alt="Docker Image Version (latest semver)" src="https://img.shields.io/badge/Docker%20Hub-latest-brightgreen">
  </a>
</p>

<p align="center">
  <a href="https://goreportcard.com/report/github.com/casvisor/casvisor">
    <img alt="Go Report Card" src="https://goreportcard.com/badge/github.com/casvisor/casvisor?style=flat-square">
  </a>
  <a href="https://github.com/casvisor/casvisor/blob/master/LICENSE">
    <img src="https://img.shields.io/github/license/casvisor/casvisor?style=flat-square" alt="license">
  </a>
  <a href="https://github.com/casvisor/casvisor/issues">
    <img alt="GitHub issues" src="https://img.shields.io/github/issues/casvisor/casvisor?style=flat-square">
  </a>
  <a href="https://github.com/casvisor/casvisor/stargazers">
    <img alt="GitHub stars" src="https://img.shields.io/github/stars/casvisor/casvisor?style=flat-square">
  </a>
  <a href="https://github.com/casvisor/casvisor/network">
    <img alt="GitHub forks" src="https://img.shields.io/github/forks/casvisor/casvisor?style=flat-square">
  </a>
</p>

![image](https://github.com/casvisor/casvisor/assets/3787410/c71e9a09-38be-4f76-99a8-595aa859ee58)

## Online demo

https://demo.casvisor.com (the data is restored regularly)

## Documentation

https://casvisor.org

## Architecture

Casvisor contains 2 parts:

| Name     | Description                      | Language                        | Source code                                          |
|----------|----------------------------------|---------------------------------|------------------------------------------------------|
| Frontend | Web frontend UI for Casvisor     | TypeScript + React + shadcn/ui  | https://github.com/casvisor/casvisor/tree/master/web |
| Backend  | RESTful API backend for Casvisor | Golang + Beego + SQLite / MySQL | https://github.com/casvisor/casvisor                 |

## Quick start

```shell
docker run -d --name casvisor -p 19000:19000 -p 2222:2222 -v casvisor-db:/db casbin/casvisor
docker logs casvisor
```

Open http://localhost:19000 and sign in as `admin` with the password printed in the log, then change it in My Account. Pass `-e adminPassword=<password>` to choose the first password instead.

Out of the box, Casvisor keeps its data in SQLite (`/db/casvisor.db`) and its own user accounts: administrators add users, groups and permissions in the web console. Port 2222 is the SSH gateway for `ssh`, `scp`, `sftp`, Xshell and VS Code.

## Installation from source

```shell
git clone https://github.com/casvisor/casvisor
cd casvisor/web && yarn install && yarn build && cd ..
go run main.go
```

Settings are read from `conf/app.conf`. Put the settings of your own server in `conf/app.local.conf` (same format, not tracked by git), or pass any of them as environment variables.

### Database

The default is SQLite:

```ini
driverName = sqlite
dataSourceName = db/casvisor.db
```

To use MySQL instead (the database is created if it does not exist):

```ini
driverName = mysql
dataSourceName = root:123456@tcp(localhost:3306)/
dbName = casvisor
```

### Sign in with Casdoor

Instead of its own accounts, Casvisor can sign users in with [Casdoor](https://casdoor.org), for SSO, MFA, LDAP and third-party logins. Create an organization and an application for Casvisor in Casdoor, add `http://<your Casvisor host>/callback` to the redirect URLs of the application, and set:

```ini
casdoorEndpoint = https://<your Casdoor host>
clientId = <client ID of the application>
clientSecret = <client secret of the application>
casdoorOrganization = <organization>
casdoorApplication = <application>
```

When `casdoorEndpoint` is empty, Casvisor uses its own accounts.

## Contribute

For Casvisor, if you have any questions, you can give Issues, or you can also directly start Pull Requests(but we recommend giving issues first to communicate with the community).

## License

[Apache-2.0](LICENSE)
