<div align="center">
  <a href="https://casvisor.org">
    <img src="https://cdn.casbin.org/img/casvisor-logo_1200x256.png" alt="Casvisor" width="500">
  </a>

  <h3>An open-source bastion host (jump server) for SSH, RDP, VNC, Telnet and databases</h3>

  <p>
    Casvisor gives your team one place to reach servers, desktops and databases from the browser.<br>
    It connects over <strong>SSH</strong>, <strong>RDP</strong>, <strong>VNC</strong>, <strong>Telnet</strong>, <strong>MySQL</strong>, <strong>PostgreSQL</strong>, <strong>SQL Server</strong>, <strong>Oracle</strong>, <strong>Redis</strong>, <strong>MongoDB</strong> and <strong>HTTP</strong> for internal websites, with an <strong>SSH gateway</strong> for <code>ssh</code>, <code>scp</code> and VS Code,<br>
    and records every session, checks every command and keeps the passwords to itself.
  </p>

  <p>
    <a href="https://casvisor.org"><strong>Website</strong></a> &middot;
    <a href="https://casvisor.org/docs"><strong>Documentation</strong></a> &middot;
    <a href="https://demo.casvisor.com"><strong>Live demo</strong></a> &middot;
    <a href="https://discord.gg/yfKcuHVAHB"><strong>Discord</strong></a>
  </p>

  <p>
    <a href="https://github.com/casvisor/casvisor/releases/latest">
      <img src="https://img.shields.io/github/v/release/casvisor/casvisor?style=flat-square&color=blue" alt="Release">
    </a>
    <a href="https://hub.docker.com/r/casbin/casvisor">
      <img src="https://img.shields.io/docker/pulls/casbin/casvisor?style=flat-square&color=brightgreen" alt="Docker Pulls">
    </a>
    <a href="https://github.com/casvisor/casvisor/actions/workflows/build.yml">
      <img src="https://img.shields.io/github/actions/workflow/status/casvisor/casvisor/build.yml?branch=master&style=flat-square&label=build" alt="Build Status">
    </a>
    <a href="https://github.com/casvisor/casvisor/blob/master/go.mod">
      <img src="https://img.shields.io/github/go-mod/go-version/casvisor/casvisor?style=flat-square&logo=go&logoColor=white" alt="Go version">
    </a>
    <a href="https://discord.gg/yfKcuHVAHB">
      <img src="https://img.shields.io/discord/1022748306096537660?style=flat-square&logo=discord&label=Discord&color=5865F2" alt="Discord">
    </a>
    <a href="https://github.com/casvisor/casvisor/blob/master/LICENSE">
      <img src="https://img.shields.io/github/license/casvisor/casvisor?style=flat-square&color=orange" alt="License">
    </a>
  </p>
</div>

<div align="center">
  <a href="https://demo.casvisor.com">
    <img src="https://cdn.casbin.org/img/casvisor-demo.gif" alt="Casvisor workbench: opening an SSH session in the browser, a dangerous command blocked by a command rule, a VNC desktop, and the command records" width="900">
  </a>
  <p><sub>SSH and VNC in the browser, with <code>rm -rf /</code> stopped by a command rule and every command on record.</sub></p>
</div>

<table>
  <tr>
    <td width="33%" valign="top" align="center">
      <a href="https://casvisor.org/docs/audit/recordings"><img src="https://cdn.casbin.org/img/casvisor-session-replay.png" alt="Casvisor session replay of an SSH session, with the list of commands and their timestamps beside the terminal"></a>
      <sub><b>Session replay.</b> Play back SSH, Telnet, VNC and RDP sessions in the browser, with the commands beside them.</sub>
    </td>
    <td width="33%" valign="top" align="center">
      <a href="https://casvisor.org/docs/audit/commands"><img src="https://cdn.casbin.org/img/casvisor-commands.png" alt="Casvisor command records with a blocked rm -rf command"></a>
      <sub><b>Command records.</b> Every command and SQL statement, and which rule blocked or flagged it.</sub>
    </td>
    <td width="33%" valign="top" align="center">
      <a href="https://casvisor.org/docs/access-control/permissions"><img src="https://cdn.casbin.org/img/casvisor-permissions.png" alt="Casvisor permissions for users, groups, assets and asset tags"></a>
      <sub><b>Permissions.</b> Who can reach which assets, from where, until when and with which actions.</sub>
    </td>
  </tr>
</table>

## 🚀 Try it in 30 seconds

No database and no config file needed. This runs Casvisor on SQLite with its own accounts:

```bash
docker run -d --name casvisor -p 19000:19000 -p 2222:2222 \
  -v casvisor-db:/db -v casvisor-recordings:/recordings casbin/casvisor
docker logs casvisor
```

Open <http://localhost:19000> and sign in:

| Field | Value |
|-------|-------|
| Username | `admin` |
| Password | the one printed in `docker logs casvisor` |

> Pass `-e adminPassword=<password>` to choose the first password instead. Either way, change it under **My Account** after you sign in.

Prefer not to install anything? The hosted demo at **[demo.casvisor.com](https://demo.casvisor.com)** comes with a sandboxed Linux shell (SSH) and a private desktop (VNC) to connect to. Nothing there touches a real server, sessions end after 15 minutes, and each visitor can keep two sessions open at a time.

## 🤔 Why Casvisor

Casvisor sits between the people who run your servers and the servers themselves. People open sessions from a browser tab, or with their own `ssh`, `scp` and VS Code through the SSH gateway. They only reach the servers and databases a permission gives them, and never see the stored passwords.

- **One Go binary.** Casvisor speaks SSH, RDP, VNC, Telnet, MySQL, PostgreSQL, SQL Server, Oracle, Redis, MongoDB and HTTP itself, with nothing to install on the servers. It runs on SQLite with its own accounts out of the box.
- **Apache-2.0, no enterprise edition.** Recording, the SSH gateway, network gateways, system accounts, jobs and everything else in this README are in the open-source project.
- **Policy you can read.** Permissions are enforced with [Casbin](https://casbin.apache.org), and sign-in can be handed to [Casdoor](https://casdoor.ai) for SSO, MFA, LDAP and social logins.
- **Audit by default.** Sessions are recorded, commands and SQL statements are checked against your rules and logged, and the logs export to CSV or a syslog server.

Coming from another bastion host? See how Casvisor [compares with JumpServer, Teleport and others](https://casvisor.org/docs/more/comparison).

## 📦 Installation

Three supported paths, fastest first. All of them end up at <http://localhost:19000>.

### Docker

```bash
docker run -d --name casvisor -p 19000:19000 -p 2222:2222 \
  -v casvisor-db:/db -v casvisor-recordings:/recordings casbin/casvisor
```

Port 19000 is the web console and port 2222 is the SSH gateway. The two volumes keep the database and the session recordings when the container is replaced.

Guide: [Quick start](https://casvisor.org/docs/getting-started/quickstart)

### Docker Compose

[`docker-compose.yml`](docker-compose.yml) builds the image from this repository and runs it with the same ports and volumes:

```bash
docker compose up -d
```

Set `adminPassword` in the compose file to choose the first password of `admin`; when it is empty, a random one is printed in the log.

### From source — for development

Use this if you intend to modify Casvisor. Prerequisites: **Go 1.23+** (see [`go.mod`](go.mod)), **Node.js 20 LTS** and **Yarn 1.x**.

```bash
git clone https://github.com/casvisor/casvisor
cd casvisor/web && yarn install && yarn build && cd ..
go run main.go
```

Guide: [Build from source](https://casvisor.org/docs/getting-started/source)

### Configuration

Settings are read from [`conf/app.conf`](conf/app.conf). Put the settings of your own server in `conf/app.local.conf` (same format, not tracked by git), or pass any of them as environment variables.

**Database.** The default is SQLite. To use MySQL instead (the database is created if it does not exist):

```ini
driverName = mysql
dataSourceName = root:123456@tcp(localhost:3306)/
dbName = casvisor
```

**Sign in with Casdoor.** Instead of its own accounts, Casvisor can sign users in with [Casdoor](https://casdoor.ai), for SSO, MFA, LDAP and third-party logins, and use Casdoor's groups in permissions. Create an organization and an application for Casvisor in Casdoor, add `http://<your Casvisor host>/callback` to the redirect URLs of the application, and set:

```ini
casdoorEndpoint = https://<your Casdoor host>
clientId = <client ID of the application>
clientSecret = <client secret of the application>
casdoorOrganization = <organization>
casdoorApplication = <application>
```

When `casdoorEndpoint` is empty, Casvisor uses its own accounts.

**Recordings.** Sessions are recorded to `recordings/` under the working directory (`/recordings` in Docker). `recordingPath` moves them, `recordingRetentionDays` deletes the ones older than that many days, and `disableRecording = true` turns recording off. To keep them in an S3-compatible bucket instead, set:

```ini
recordingStorage = s3
recordingS3Endpoint = s3.us-east-1.amazonaws.com
recordingS3Region = us-east-1
recordingS3Bucket = <bucket>
recordingS3Prefix = casvisor
recordingS3AccessKey = <access key>
recordingS3SecretKey = <secret key>
```

Recordings are still written to the disk first, so a session never waits for the bucket; when the session ends the recording is uploaded and the local copy deleted (`recordingKeepLocal = true` keeps it). If the bucket can't be reached, the upload is retried every 5 minutes. `recordingS3PathStyle = true` is for MinIO and others that need path-style URLs.

**SSH certificates.** To let users sign in to the SSH gateway with OpenSSH certificates instead of registering public keys, set `sshGatewayTrustedUserCaKeys` to the CA public keys (one per line or separated by `;`) or to a file of them, like sshd's `TrustedUserCAKeys`. The certificate's principal is the Casvisor user, `<user>` or `<organization>/<user>`; validity, `source-address` and the login rules are checked.

**Metrics.** Set `metricsToken` and point Prometheus at `/metrics` with `authorization: {credentials: <token>}`. It is off while `metricsToken` is empty.

Guide: [Configuration](https://casvisor.org/docs/operations/configuration) &middot; [Sign in with Casdoor](https://casvisor.org/docs/getting-started/casdoor) &middot; [Behind a reverse proxy](https://casvisor.org/docs/operations/reverse-proxy)

## 👉 After you sign in

At this point you have a running bastion host with nothing behind it yet. Next:

1. **Change the `admin` password** under **My Account**, and turn on two-step verification there.
2. **[Add an asset](https://casvisor.org/docs/assets/assets)** under **Assets** (a server, a desktop or a database) and click **Connect**. Administrators reach every asset.
3. **[Let other people in](https://casvisor.org/docs/access-control/permissions)**: add them under **Users** and give them a **Permission** on assets or asset tags.
4. **[Use the SSH gateway](https://casvisor.org/docs/assets/ssh-gateway)**: users add their public key under **SSH keys** and connect with `ssh -p 2222 <asset>@<your Casvisor host>`.
5. **[Add command rules](https://casvisor.org/docs/audit/commands)** to block, hold for review or flag commands such as `rm -rf /` or `DROP DATABASE`.

## ✨ Features

**🔌 Connect**

- **SSH** — an xterm.js terminal in the browser, with a file panel beside it for upload and download over SFTP
- **RDP** — [gordp](https://github.com/casvisor/gordp), a pure-Go RDP client: NLA, the GFX graphics pipeline (ClearCodec, RemoteFX Progressive) like the Windows client, sharp text on high-DPI screens, the remote cursor, clipboard with text, images and files, resizing with the window, and file transfer through a redirected drive. Works with Windows Server, Windows 10/11 and FreeRDP-based hosts such as Ubuntu
- **VNC** — noVNC in the browser; Casvisor does the VNC authentication itself, so the password stays on the server
- **Telnet** — for network gear and old systems, logging in with the asset's account
- **MySQL, PostgreSQL, SQL Server and Oracle** — a browser SQL console with the databases, tables and columns on the left, one tab per result, and CSV export; SQL Server scripts run batch by batch between `GO` lines, and Oracle PL/SQL blocks end with a `/` line
- **Redis and MongoDB** — the same console for Redis commands, with the keys and their types on the left, and for MongoDB shell calls such as `db.users.find({age: {$gt: 18}}).sort({name: 1})`, with the collections and their fields on the left
- **Websites** — internal web apps such as Grafana, Jenkins or a router's admin page, each opened on a subdomain of its own through Casvisor's reverse proxy, WebSockets included, with optional HTTP basic authentication filled in by Casvisor; uploads and downloads follow the `Upload` and `Download` actions, and pages opened and changing requests are recorded
- **SSH gateway** — `ssh -p 2222 web-01@casvisor.example.com`, `scp`, `sftp`, `rsync`, Xshell and VS Code Remote, signed in with the user's public key, or an OpenSSH certificate from a CA you trust (Vault, step-ca, `ssh-keygen -s`), and under the same permissions as the browser; `-L` also forwards Unix sockets such as `/var/run/docker.sock`
- **Port forwarding to any asset** — `ssh -p 2222 -N -L 13389:win-01:3389 alice@casvisor.example.com`, then mstsc, a VNC viewer or a database client connects to `localhost:13389`
- **Network gateways** — reach assets in another data center or behind NAT, through an SSH jump host or an agent that connects out over HTTPS
- **Stored credentials** — passwords and private keys are encrypted in the database and filled in by Casvisor; SSH host keys and RDP certificates are pinned on first use

**🛡️ Control**

- **Permissions** — on users, groups, assets and asset tags, with the actions `Connect`, `Copy`, `Paste`, `Upload`, `Download` and `Forward`, client IP ranges, a start and end date, and weekly hours such as Monday to Friday 09:00–18:00; open sessions are disconnected when the hours end
- **System accounts** — several accounts per asset, such as `root`, `deploy` and `readonly`; permissions decide which one a user may connect with, and passwords can be changed by hand or on a schedule
- **Access requests** — users ask for an asset with a reason and an end time, an administrator approves, and withdrawing it disconnects open sessions
- **Two-step verification** — for local accounts, with any TOTP authenticator app
- **Command rules** — block or flag commands matching a regular expression, in the web terminal, the SQL console, website requests and through the SSH gateway alike
- **Command review** — a command a Review rule matches waits until another administrator approves it, then runs in the same session; the user can withdraw it with Ctrl+C
- **Revocation takes effect at once** — open sessions are disconnected as soon as their permission or account is edited, deleted or expires

**🔍 Audit**

- **Session recording and playback** — SSH, Telnet, VNC and RDP, replayed in the browser with the same code that drew them live; SSH and Telnet recordings download as asciicast. Recordings are kept on disk or moved to an S3-compatible bucket (AWS S3, Alibaba Cloud OSS, Tencent COS, Cloudflare R2, MinIO) when the session ends
- **Live monitoring** — watch online sessions and stop any of them; an administrator can take part in a terminal session and type in it, the user is told who joined, and the commands are logged with the administrator as the operator
- **Command records** — SSH and Telnet commands are read off a virtual screen, so tab completion, history and line edits are captured as they ran; SQL statements are logged too
- **Operation log** — file transfers and every change made through the API
- **Export and syslog** — session, command and operation logs export to CSV and can be forwarded to a syslog server as RFC 5424 JSON or CEF

**⚙️ Operate**

- **Jobs** — run a command, a Bash or PowerShell script, or a file copy on many SSH assets at once, with concurrency, timeouts and per-asset output and exit codes
- **Patch management** — scan SSH assets for pending system updates, install them and restart, on Windows (Windows Update) and Linux (apt, dnf, yum)
- **REST API** — every console action is also an API call, with [Swagger docs](swagger)
- **Prometheus metrics** — `/metrics` reports connected sessions, sessions, commands, assets, access requests and network agents, behind the bearer token in `metricsToken`
- **Web console** — in 9 languages, built with React, Vite and shadcn/ui

## 🌐 Network gateways

Assets in another data center, or behind NAT in an office, are reached through a network gateway. Add one under **Network gateways** and pick it in the asset's **Network gateway** field. Every connection to the asset (web SSH, Telnet, VNC, RDP, databases, the SSH gateway and patch management) then goes through it.

- **SSH jump host**: a server in that network that Casvisor can log in to over SSH. Nothing is installed on it; its sshd needs `AllowTcpForwarding yes`.
- **Agent**: for networks Casvisor cannot connect into at all. Generate a token on the gateway's page and run the agent on any machine in that network:

  ```bash
  casvisor agent --server https://casvisor.example.com --token cva_... --fingerprint SHA256:...
  ```

  The agent is the same `casvisor` binary (with Docker: `docker run -d --restart unless-stopped casbin/casvisor agent --server ...`). It connects out to Casvisor's web address over WebSocket, so it works wherever HTTPS gets out, listens on no port, and only connects to the **Allowed networks** set on the gateway.

Guide: [Network gateways](https://casvisor.org/docs/assets/network-gateways)

## 🧱 Technology stack

Casvisor is a frontend–backend separated application:

- **Backend** — Go with the [Beego](https://github.com/beego/beego) framework: the REST API, the protocol tunnels and the SSH gateway ([repository root](https://github.com/casvisor/casvisor))
- **Frontend** — React with [shadcn/ui](https://ui.shadcn.com/) on Tailwind CSS, built with Vite: the console, the workbench and the session player ([`web/`](web))
- **RDP** — [casvisor/gordp](https://github.com/casvisor/gordp), a pure-Go RDP client
- **Authorization** — [Casbin](https://casbin.apache.org)
- **Database** — SQLite or MySQL through [XORM](https://xorm.io/)

## 📖 Documentation

The full documentation lives at **[casvisor.org](https://casvisor.org/docs)**. Common starting points:

| I want to… | Go to |
|------------|-------|
| Install Casvisor | [Quick start](https://casvisor.org/docs/getting-started/quickstart) &middot; [From source](https://casvisor.org/docs/getting-started/source) |
| Connect to servers and databases | [Protocols](https://casvisor.org/docs/assets/protocols) &middot; [Databases](https://casvisor.org/docs/assets/databases) &middot; [SSH gateway](https://casvisor.org/docs/assets/ssh-gateway) |
| Decide who can reach what | [Permissions](https://casvisor.org/docs/access-control/permissions) &middot; [Access requests](https://casvisor.org/docs/access-control/access-requests) |
| Review what people did | [Recordings](https://casvisor.org/docs/audit/recordings) &middot; [Commands](https://casvisor.org/docs/audit/commands) &middot; [Export](https://casvisor.org/docs/audit/export) |
| Run it in production | [Configuration](https://casvisor.org/docs/operations/configuration) &middot; [Reverse proxy](https://casvisor.org/docs/operations/reverse-proxy) &middot; [Upgrading](https://casvisor.org/docs/operations/upgrading) |

## 🔒 Security

**Please do not report security vulnerabilities in public GitHub issues.** Email <admin@casdoor.org> instead.

Before exposing a Casvisor instance to the internet:

- Change the `admin` password and turn on two-step verification, or sign in through Casdoor.
- Serve Casvisor over HTTPS only, behind a reverse proxy, and set `trustedProxies` and `allowedOrigins` in [`conf/app.conf`](conf/app.conf).
- Keep port 2222 (the SSH gateway) closed to networks that do not need it.
- Back up the credential key with the database: stored passwords and private keys cannot be decrypted without it. It is `credentialKey` in the config, or `credential.key` next to the SQLite database when that is not set.

## 🤝 Community and support

- **Discord** — [join the community](https://discord.gg/yfKcuHVAHB) for questions and help
- **GitHub Issues** — [bug reports and feature requests](https://github.com/casvisor/casvisor/issues)

## 🌍 Contributing

Contributions are welcome. For anything larger than a small fix, **please open an issue first** so you can agree on the approach with the maintainers before writing code.

**Translations.** User-facing strings in the web console go through [i18next](https://www.i18next.com/). After adding or changing one under [`web/`](web), run `go test -tags manual ./i18n -run TestGenerateI18nStrings` to add the new keys to every catalog in [`web/src/locales/`](web/src/locales), then translate them.

## 📄 License

Casvisor is licensed under the [Apache License 2.0](LICENSE).

---

<div align="center">

If Casvisor is useful to you, a star helps other people find it.

<a href="https://github.com/casvisor/casvisor/stargazers"><img src="https://img.shields.io/github/stars/casvisor/casvisor?style=social&logo=github&label=Star" alt="GitHub Stars"></a>

<sub>© 2026 <a href="https://casvisor.org">Casvisor</a> &middot; <a href="LICENSE">Apache License 2.0</a></sub>

</div>
