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

## Network gateways

Assets that Casvisor cannot reach directly, such as servers in another data center or behind NAT in an office, are reached through a network gateway. Add one under **Network gateways** and pick it in the asset's **Network gateway** field. Every connection to the asset (web SSH, Telnet, VNC, RDP, the SSH gateway and patch management) then goes through it. There are two types:

- **SSH jump host**: a server in that network that Casvisor can log in to over SSH. Nothing is installed on it; Casvisor opens a `direct-tcpip` channel to each asset, so its sshd needs `AllowTcpForwarding yes`. Its host key is pinned on the first connection.
- **Agent**: for networks Casvisor cannot connect into at all. Generate a token on the gateway's page and run the agent on any machine in that network:

  ```shell
  casvisor agent --server casvisor.example.com:2222 --token cva_... --fingerprint SHA256:...
  ```

  The agent is the same `casvisor` binary, and with Docker it is `docker run -d --restart unless-stopped casbin/casvisor agent --server ...`. It connects out to the SSH gateway port, so `sshGatewayPort` has to be set, keeps the connection open and reconnects by itself. Casvisor opens a channel on that connection for every connection to an asset, and the agent connects to the asset from inside. It listens on no port and only connects where Casvisor asks, within the **Allowed networks** set on the gateway (and `--allow`, if given). The token is shown once and only a hash of it is stored; regenerating or revoking it disconnects the agent at once. `--fingerprint` is the SSH gateway's host key, shown on the **SSH keys** page, and the agent refuses a server with any other key. The token can also be passed in the environment variable `CASVISOR_AGENT_TOKEN`. The gateway's page shows whether the agent is online, when it was last online, its version and the address it connects from.

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
