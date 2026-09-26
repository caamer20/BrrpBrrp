# Deployment

Use a current, patched Linux, Windows or macOS host with persistent local storage. For a permanent deployment, install the background service first, configure its HTTPS address and data folder, then start it and complete setup. This creates the first administrator in the service's actual database. No Docker, Node.js runtime or separate database is required.

For new installations, use the [built-in installation wizard](installation.md). It configures local hosting on port 8082 by default and starts the service for you. This document covers manual operation, the optional legacy scripts and later updates. The [hosting guide](hosting.md) describes exposing a local installation through your own infrastructure.

Wizard installations store `server.env` in the selected data folder. Default locations are `%ProgramData%\BrrpBrrp` on Windows, `/var/lib/brrpbrrp` on Linux and `~/Library/Application Support/BrrpBrrp/data` on macOS. The Linux wizard places the executable at `/usr/local/lib/brrpbrrp/brrpbrrp`; the optional legacy script below uses `/usr/local/bin/brrpbrrp`.

## Linux systemd

From an extracted release, run `sudo sh scripts/linux/install.sh`. From a source build, pass the binary path as its first argument. The installer copies the executable to `/usr/local/bin/brrpbrrp`, creates the `brrpbrrp` service account, and prepares `/var/lib/brrpbrrp` and `/etc/brrpbrrp/server.env`. It refuses to replace a running service's binary.

Edit `/etc/brrpbrrp/server.env` for the public URL and TLS mode. Then:

```sh
sudo systemctl start brrpbrrp
sudo systemctl status brrpbrrp
sudo journalctl -u brrpbrrp -n 50
sudo /usr/local/bin/brrpbrrp setup-token --config /etc/brrpbrrp/server.env
```

The unit restarts on failure, uses a dedicated account, blocks privilege escalation and restricts writable paths. It runs on an unprivileged app port. Use a proxy or network forwarding for public TCP 443; do not run the entire application as root to bind that port. Backups and restores should run as the service account to preserve file ownership.

`sudo sh scripts/linux/uninstall.sh` removes the service unit and preserves the executable, user account, settings and data.

## Windows service

For the graphical walkthrough, extract the archive and open **Setup.cmd**. The wizard uses the normal administrator prompt and does not need PowerShell scripts. The following steps are the optional manual installation path.

Extract the Windows archive. Open PowerShell **as Administrator**, then run `scripts\windows\install.ps1` (or the BAT wrapper). Follow your organization's PowerShell execution policy; the installer does not weaken it. Pass `-Binary C:\path\brrpbrrp.exe` when installing a separately built executable.

The installer copies the executable to `%ProgramFiles%\BrrpBrrp`, uses `%ProgramData%\BrrpBrrp` for private data/configuration, applies ACLs and registers a delayed automatic Windows service under **LocalService**. It does not start the service or open firewall ports.

For fresh installations, the legacy PowerShell script configures TCP **8082** and `https://localhost:8082`. It preserves any existing configuration. The browser wizard instead uses the connection profile you choose, with port 8082 as its default. The explicit `serve` command without a configuration file retains the older application default, TCP 8080.

Edit `%ProgramData%\BrrpBrrp\server.env` using an administrator editor. Use unquoted Windows paths or single quotes; backslashes inside double-quoted values are escape characters. Then:

```powershell
Start-Service BrrpBrrp
Get-Service BrrpBrrp
& "$env:ProgramFiles\BrrpBrrp\brrpbrrp.exe" setup-token --config "$env:ProgramData\BrrpBrrp\server.env"
Get-Content "$env:ProgramData\BrrpBrrp\logs\server.log" -Tail 30
```

Configure Windows Firewall for the ports listed in [security.md](security.md), scoped to the intended network profile. Do not expose plain HTTP publicly. The binary has native service-control handling and honors stop/shutdown requests.

For an interactive start without installing a service, use `scripts\windows\start.bat` or run `brrpbrrp.exe serve --config C:\path\server.env`. Running the executable without arguments opens the wizard. `scripts\windows\uninstall.ps1` stops and removes the service while retaining files and data.

Release binaries are unsigned; they do not include a purchased Authenticode certificate. Signing and native installer verification are release-operator steps, not claimed by a cross-compile.

## macOS launchd

Run `sh scripts/macos/install.sh` as the user who will run the server. It copies the app to `~/Library/Application Support/BrrpBrrp`, creates a private data folder and writes a per-user plist in `~/Library/LaunchAgents`. Paths are inserted with `plutil`, so spaces are supported.

Edit the generated `server.env`. Load the agent using the exact command printed by the installer. This is a user agent: it starts after login, not before anyone has logged in. Logs go to the rotating `data/logs/server.log`. Use `launchctl bootout gui/USER_ID/co.brrpbrrp.server` to stop/unload before an update. The uninstall script preserves application files and data.

The macOS artifacts are not notarized. Use the normal trusted software process for your organization; building from the reviewed source is also supported.

## Configuration

All data paths below are resolved within `BRRP_DATA_DIR`. A `server.env` file read with `--config` may be in a protected host configuration location such as `/etc/brrpbrrp`; it contains literal settings, not secrets fetched by the app. Environment values take priority over file values.

The `production-check --config PATH` command is safe while the service is running. It checks the local listener, the configured browser HTTPS address and certificate lifetime, free data-disk space, enabled complete backups, the last backup result, and fresh local/extra backup ZIPs. Its default thresholds are 14 days of certificate life, 2 GiB free and backups no older than 36 hours. It requires `BRRP_BACKUP_COPY_DIR` by default. Schedule it and alert on a nonzero exit; also monitor the browser address from another network. Details and acceptance steps are in the [production runbook](production-readiness.md).

| Setting | Default / purpose |
| --- | --- |
| `BRRP_DATA_DIR` | `./data`; use an absolute path for a service |
| `BRRP_ADDR` | `:8080`; TCP listening address |
| `BRRP_BASE_URL` | `https://localhost:8080`; exact browser origin |
| `BRRP_TLS_MODE` | `auto`, `https`, `http-behind-proxy`, or loopback-only `http-local` |
| `BRRP_DOMAIN` / `BRRP_ACME_EMAIL` | Optional automatic certificate domain/contact |
| `BRRP_CERT_FILE` / `BRRP_KEY_FILE` | `certs/tls.crt`, `certs/tls.key` |
| `BRRP_BEHIND_PROXY` | `false`; enable only with explicit trusted proxies |
| `BRRP_TRUSTED_PROXIES` | Comma-separated CIDRs; required when behind a proxy |
| `BRRP_ALLOW_INSECURE_HTTP` | `false`; local development override only |
| `BRRP_DB_FILE` | `brrp.db` |
| `BRRP_UPLOAD_DIR` | `uploads` |
| `BRRP_SETUP_TOKEN_FILE` | `setup-token` |
| `BRRP_LOG_LEVEL` | `info`; debug/info/warn/error/off |
| `BRRP_LOG_FILE` | Empty means stderr; `logs/server.log` enables bounded file rotation |
| `BRRP_MAX_UPLOAD_BYTES` | `12582912`; installation cap, maximum 12 MiB |
| `BRRP_LOGIN_ATTEMPTS` | `20`; sign-in attempts per address per five minutes. Raise it when everyone signs in from one address — an office NAT, a VPN or a reverse proxy that does not forward the client address — because the budget is shared by every account behind that address |
| `BRRP_VOICE_EXTERNAL_IP` | Optional reachable media IP for NAT mapping |
| `BRRP_VOICE_PORT_MIN` / `BRRP_VOICE_PORT_MAX` | UDP 50000–50100 |
| `BRRP_BACKUP_COPY_DIR` | Optional absolute destination for extra copies; grant the service account write access |

Resource/UI/retention/TURN settings are stored in SQLite and managed through the admin UI. Do not edit that table while the app is running. Keep the TURN relay range 49152–49999 separate from a customized WebRTC range.

## Reverse proxy

Set an HTTPS public base URL, proxy mode and trusted proxy CIDRs. For a same-host proxy, listen on `127.0.0.1:8082` and trust `127.0.0.1/32,::1/128`. Preserve the original host, set `X-Forwarded-For`, forward `/ws` with the WebSocket Upgrade/Connection headers, disable WebSocket buffering and use a sufficiently long proxy read timeout. Set the proxy upload limit above the selected file cap plus multipart overhead.

An example Nginx location block (inside your existing TLS server):

```nginx
location / {
    proxy_pass http://127.0.0.1:8082;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_buffering off;
    proxy_read_timeout 3600s;
    client_max_body_size 13m;
}
```

You choose and manage the proxy, certificate, DNS and any network forwarding. HTTP tunnels do not automatically carry SFU audio. Configure a reachable UDP route or relay separately; see [hosting.md](hosting.md).

## Updates and rollback

For container deployments, follow the [Docker update and backup instructions](docker.md#updates-and-backups). Docker starts the `serve` command directly and manages restarts.

The alpha wizard supports **Update this installation** for installations whose saved settings still match. Launch setup from the extracted new release, export a backup, confirm the update, and let it restart. The previous executable remains under `previous-build-*` in the application folder. Windows tray processes in other signed-in sessions must be closed. For script-installed or manually reconfigured servers, use the manual procedure below.

1. Create and export a backup, including attachments. Save the existing binary, host settings and TLS files separately.
2. Stop the service and verify that it is stopped. Do not replace a Windows executable while it is running.
3. Replace only the executable with the matching architecture's release. Keep the data/configuration directories.
4. Start the service, inspect logs and verify login, a stored message, search, a download and voice.
5. To roll back after a schema change, restore the prior backup into a fresh folder using the matching older release and point the service there. Do not point an older binary at a database upgraded by a newer schema.

Database migrations are transactional and idempotent. This release writes schema 6 and upgrades schemas 1–5 on first open, each stage in its own transaction, in the order the registry lists them. Use `brrpbrrp preflight` to see the source and target schema, integrity, journal mode and pending migrations without touching anything. The binary refuses a database from a future unsupported schema instead of downgrading it. Test updates and restores on a copy before applying them to the live group. After a schema upgrade, rollback requires the pre-upgrade backup and the matching older executable.

`make rehearsal` (or `node scripts/upgrade-recovery.mjs`) rehearses exactly that path on disposable data directories: preflight, install, a message and an attachment, backup, restore into an empty destination, verify, and a comparison of the message and attachment bytes after the restore. Run it before an upgrade, and then repeat the restore on the real destination with the archives you actually intend to keep, because a temporary directory on the server cannot prove an off-host copy is readable.

`make rollback-rehearsal` (or `node scripts/upgrade-rollback.mjs`) rehearses the path that crosses a version boundary, using a real archived prior release rather than a schema fixture. It installs with the old executable and writes a representative dataset (an owner, a member, three messages with a reply, a pin, an invitation, an uploaded file and an enrolled second factor), snapshots with the old executable, proves the candidate's own snapshot of an un-migrated installation is *not* a rollback archive (format 2, refused by the old executable), upgrades with this tree and checks every row and the exact attachment bytes and factor, records what changed after the snapshot, then restores the old archive with the old executable into a clean folder and checks the snapshot's data and the absence of the later writes. It also injects the state an interruption before the upgrade's postconditions leaves behind, and proves verification refuses it while starting the server completes it, rebuilds the search index and clears the marker. It reads an archived release for this platform from `dist/releases`, or takes `--old-release ZIP`/`--old-binary PATH`; it fails rather than skipping when neither is available. Record its JSON with `--report FILE` in the evidence for a release. Its limits are stated in the report: it does not cover a Windows service or container lifecycle, an off-host copy, or capacity.

Save the old executable and its own snapshot together. The candidate's snapshot of the same installation cannot restore that installation: it is written in the current backup format, and the older executable refuses it. Quiesce writes before the snapshot, because a rollback returns the installation to the moment the snapshot was taken and loses everything after it.

After the server is updated, open clients show **An update is ready**. Users can also check under **Your preferences → Account → Check for updates**. **Update now** preserves unsent text, staged attachments and the current reply for that refresh. It becomes available after voice ends and pending sends/uploads finish. If browser storage cannot preserve a draft, the app asks the user to send or copy it before refreshing. Existing clients choose their refresh time; a server restart can still briefly disconnect chat and voice.

## References

Service behavior follows [Go's Windows service package](https://pkg.go.dev/golang.org/x/sys/windows/svc) and [systemd's execution settings](https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html). Installation scripts are supplied for review and execution on the target host; cross-compilation alone does not prove native installation behavior.
