<h1 align="center" style="border-bottom: none;">📦⚡️ Casvisor</h1>
<h3 align="center">An open-source bastion host (jump server) for SSH, RDP, VNC and Telnet, written in Go and React.</h3>
<p align="center">
  <a href="https://github.com/casvisor/casvisor/releases/latest">
    <img alt="GitHub Release" src="https://img.shields.io/github/v/release/casvisor/casvisor.svg">
  </a>
  <a href="https://hub.docker.com/r/casbin/casvisor">
    <img alt="docker pull casbin/casvisor" src="https://img.shields.io/docker/pulls/casbin/casvisor.svg">
  </a>
  <a href="https://goreportcard.com/report/github.com/casvisor/casvisor">
    <img alt="Go Report Card" src="https://goreportcard.com/badge/github.com/casvisor/casvisor">
  </a>
  <a href="https://github.com/casvisor/casvisor/blob/master/LICENSE">
    <img alt="license" src="https://img.shields.io/github/license/casvisor/casvisor">
  </a>
  <a href="https://github.com/casvisor/casvisor/stargazers">
    <img alt="GitHub stars" src="https://img.shields.io/github/stars/casvisor/casvisor">
  </a>
</p>

<p align="center">
  <a href="https://demo.casvisor.com">Online demo</a> |
  <a href="https://casvisor.org">Documentation</a> |
  <a href="https://github.com/casvisor/casvisor/issues">Issues</a>
</p>

Casvisor sits between the people who run your servers and the servers themselves. People open SSH, RDP, VNC and Telnet sessions from a browser tab, or use their own `ssh`, `scp` and VS Code through Casvisor's SSH gateway. They only reach the servers a permission gives them, never see the servers' passwords, and every session is recorded.

- **One Go binary.** All four protocols are handled inside Casvisor, with nothing to install on the servers. It runs on SQLite with its own accounts out of the box.
- **Apache-2.0, no enterprise edition.** Recording, the SSH gateway, network gateways and everything else in this README are in the open-source project.
- **Policy you can read.** Permissions are enforced with [Casbin](https://casbin.apache.org), and sign-in can be handed to [Casdoor](https://casdoor.ai) for SSO, MFA, LDAP and social logins.

## Features

**Connect**

- **SSH** in an xterm.js terminal, with a file panel beside it for upload and download over SFTP.
- **RDP** with [gordp](https://github.com/casvisor/gordp), a pure-Go RDP client: NLA, the GFX graphics pipeline (ClearCodec, RemoteFX Progressive) like the Windows client, sharp text on high-DPI screens, the remote cursor, clipboard, resizing with the window, and file transfer through a redirected drive. Works with Windows Server, Windows 10/11 and FreeRDP-based hosts such as Ubuntu.
- **VNC** with noVNC. Casvisor does the VNC authentication itself, so the password stays on the server.
- **Telnet** for network gear and old systems, logging in with the asset's account.
- **SSH gateway** on port 2222: `ssh -p 2222 web-01@casvisor.example.com`, `scp`, `sftp`, `rsync`, Xshell and VS Code Remote, signed in with the user's public key and under the same permissions as the browser.
- **Network gateways** for assets Casvisor cannot reach directly: through an SSH jump host, or through an agent that connects out from a private network over HTTPS.
- **Stored credentials.** Passwords and private keys are encrypted in the database and filled in by Casvisor. SSH host keys and RDP certificates are pinned on first use.

**Control**

- **Permissions** on users, groups, assets and asset tags, with the actions `Connect`, `Copy`, `Paste`, `Upload`, `Download` and `Forward`, client IP ranges and a time window.
- **Command rules** that block or flag commands matching a regular expression, in the web terminal and through the SSH gateway alike.
- **Revocation takes effect at once.** Open sessions are disconnected as soon as their permission is edited, deleted or expires.

**Audit**

- **Session recording and playback** for SSH, Telnet, VNC and RDP, replayed in the browser with the same code that drew them live. SSH and Telnet recordings can be downloaded as asciicast.
- **Live monitoring** of online sessions, and stopping any of them.
- **Command records** of SSH and Telnet sessions, read off a virtual screen, so tab completion, history and line edits are captured as they ran.
- **Operation log** of file transfers and every change made through the API.

**Operate**

- **Patch management** for SSH assets: scan for pending system updates, install them and restart, on Windows (Windows Update) and on Linux (apt, dnf, yum).
- **Web console** in 9 languages, built with React, Vite and shadcn/ui, with a RESTful API and [Swagger docs](swagger).

## Quick start

```shell
docker run -d --name casvisor -p 19000:19000 -p 2222:2222 \n  -v casvisor-db:/db -v casvisor-recordings:/recordings casbin/casvisor
docker logs casvisor
```

Open http://localhost:19000 and sign in as `admin` with the password printed in the log, then change it in **My Account**. Pass `-e adminPassword=<password>` to choose the first password instead.

Then add a server under **Assets** and click **Connect**. Administrators reach every asset; to let other people in, add them under **Users** and give them a **Permission**. The repository's [docker-compose.yml](docker-compose.yml) does the same with Docker Compose.

Port 2222 is the SSH gateway. Users add their public key under **SSH keys** and connect with `ssh -p 2222 <asset>@<your Casvisor host>`.

## Installation from source

Requires Go 1.23 and Node.js 20 with Yarn.

```shell
git clone https://github.com/casvisor/casvisor
cd casvisor/web && yarn install && yarn build && cd ..
go run main.go
```

Settings are read from [conf/app.conf](conf/app.conf). Put the settings of your own server in `conf/app.local.conf` (same format, not tracked by git), or pass any of them as environment variables.

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

Instead of its own accounts, Casvisor can sign users in with [Casdoor](https://casdoor.ai), for SSO, MFA, LDAP and third-party logins, and use Casdoor's groups in permissions. Create an organization and an application for Casvisor in Casdoor, add `http://<your Casvisor host>/callback` to the redirect URLs of the application, and set:

```ini
casdoorEndpoint = https://<your Casdoor host>
clientId = <client ID of the application>
clientSecret = <client secret of the application>
casdoorOrganization = <organization>
casdoorApplication = <application>
```

When `casdoorEndpoint` is empty, Casvisor uses its own accounts.

### Recordings

Sessions are recorded to `recordings/` under the working directory (`/recordings` in Docker). `recordingPath` moves them, `recordingRetentionDays` deletes the ones older than that many days, and `disableRecording = true` turns recording off.

## Network gateways

Assets in another data center, or behind NAT in an office, are reached through a network gateway. Add one under **Network gateways** and pick it in the asset's **Network gateway** field. Every connection to the asset (web SSH, Telnet, VNC, RDP, the SSH gateway and patch management) then goes through it.

- **SSH jump host**: a server in that network that Casvisor can log in to over SSH. Nothing is installed on it; its sshd needs `AllowTcpForwarding yes`.
- **Agent**: for networks Casvisor cannot connect into at all. Generate a token on the gateway's page and run the agent on any machine in that network:

  ```shell
  casvisor agent --server https://casvisor.example.com --token cva_... --fingerprint SHA256:...
  ```

  The agent is the same `casvisor` binary (with Docker: `docker run -d --restart unless-stopped casbin/casvisor agent --server ...`). It connects out to Casvisor's web address over WebSocket, so it works wherever HTTPS gets out, listens on no port, and only connects to the **Allowed networks** set on the gateway. The command shown when generating the token has every flag filled in.

See the [documentation](https://casvisor.org) for the token options, host key pinning and how the agent reports its status.

## Architecture

| Part     | Description                               | Stack                                 | Source                 |
|----------|-------------------------------------------|---------------------------------------|------------------------|
| Frontend | Web console, workbench and session player | TypeScript, React, Vite, shadcn/ui    | [web](web)             |
| Backend  | RESTful API, protocol tunnels, SSH gateway | Go, Beego, xorm, SQLite / MySQL      | [casvisor](.)          |
| RDP      | Pure-Go RDP client                        | Go                                    | [casvisor/gordp](https://github.com/casvisor/gordp) |

## Contribute

Questions and ideas are welcome in [issues](https://github.com/casvisor/casvisor/issues). Pull requests are welcome too, though we recommend opening an issue first to discuss larger changes.

## License

[Apache-2.0](LICENSE)
