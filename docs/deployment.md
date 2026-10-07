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


## Operator alerts

Alerts are delivered to an HTTP endpoint the operator controls. Configure two
environment variables and restart:

```sh
BRRP_ALERT_WEBHOOK=https://alerts.example.net/hooks/your-token
BRRP_ALERT_SECRET=a-shared-secret-the-receiver-also-knows
```

The secret is optional and is only used to sign the body, so a receiver can tell a real
alert from a forged one posted to a URL that leaked. It is never sent in a header or
the body, so a receiver that logs requests does not log the key that verifies them.

**Check that alerting works, not that it is configured.** `GET /api/admin/alerts`
answers that: it reports when an alert last *reached* the destination, alongside the
current conditions and the delivery history. A configured destination that has never
delivered is worse than no destination, because it looks like coverage — and the
operator finds that out during a restore.

The conditions are: a recoverable snapshot older than one and a half times the configured
interval (warning) or twice it (critical), consecutive failed backup attempts, an
independent copy that is behind or failing, and low free space on the data volume. With
the 12-hour default interval that is a warning at 18 hours and critical at 24, the cadence
the recovery objective of at most 24 hours of loss assumes. A failed job or copy is
critical at once, because a server that is running fine and cannot be recovered is the
worst state to be in.

An unchanged condition raises one alert rather than repeating every pass, and the
history is bounded: repeated notices for a problem nobody has fixed are how operators
learn to ignore them.


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
| `BRRP_SQLITE_SYNCHRONOUS` | `FULL`; durability of committed transactions. `NORMAL` may lose recent commits after an operating-system crash or power loss, and is only appropriate for a disposable installation |
| `BRRP_STORAGE_QUOTA_BYTES` | `0` (no quota); total attachment storage the server will hold. An upload that would exceed it is refused with the bytes used and the limit, and the file is not kept |
| `BRRP_STORAGE_RESERVE_BYTES` | `536870912` (512 MiB); free space the server keeps in hand. An upload that would leave less is refused, because an installation that fills its volume cannot write the backup that would recover it. Counted against the largest upload this installation accepts, so the floor holds whatever arrives |
| `BRRP_LOGIN_ATTEMPTS` | `20`; sign-in attempts per address per five minutes. Raise it when everyone signs in from one address — an office NAT, a VPN or a reverse proxy that does not forward the client address — because the budget is shared by every account behind that address |
| `BRRP_ALERT_WEBHOOK` | Empty means no destination, and conditions are only recorded and shown in the interface. An `http(s)` URL receives a `POST` with the alert; delivery is host configuration, so the browser cannot redirect an installation's alerts |
| `BRRP_ALERT_SECRET` | Optional. Signs the payload (`X-Brrpbrrp-Signature: sha256=…` over the timestamp and body), so a receiver can tell a real alert from one forged against a URL that leaked |
| `BRRP_VOICE_EXTERNAL_IP` | Optional reachable media IP for NAT mapping |
| `BRRP_VOICE_PORT_MIN` / `BRRP_VOICE_PORT_MAX` | UDP 50000–50100 |
| `BRRP_BACKUP_COPY_DIR` | Optional absolute destination for extra copies; grant the service account write access |
| `BRRP_ENABLE_RESTRICTED_CHANNELS` | `false`; the restricted-channel feature gate (ADR 0003). Shipped off, and the owner flips it after the independent reviews |
| `BRRP_ENABLE_PRIVATE_CONVERSATIONS` | `false`; the consented-private-conversation feature gate (ADR 0012). Consent is required by the product rather than by this switch. Shipped off, and the owner flips it after the independent reviews |

### What the disk reserve guarantees

`BRRP_STORAGE_RESERVE_BYTES` is the room the server keeps for its own recovery work — a
database vacuum, the next backup archive, the writes of the next minute. It is a floor, not
a budget: a write that would take the volume below it is refused with 507.

The guard reads the free space at the start of each upload and plans for the largest upload
this installation accepts, because a file's real size is not known until its bytes have
arrived. That check is a moment old by the time the write reaches the filesystem, and
several uploads can be deciding at once. What that means in practice is:

- **The reserve is measured per write, not reserved per writer.** Two uploads that both
  pass the guard can still, together, take the volume closer to the floor than either would
  alone. The guard is a conservative estimate of one write's cost, not a lock on the
  remaining space.
- **The floor is what the write path enforces, not the check.** A write that reaches the
  filesystem after the volume has filled underneath the check fails with the operating
  system's disk-full error, and the upload path answers it with the same 507 it answers the
  guard, on every platform. The refusal is a property of the volume, so it holds however
  many uploads were in flight.

So the reserve holds against a single upload, and the second line of defence — mapping the
filesystem's own disk-full error to a storage refusal — holds when the estimate is wrong or
overtaken. Neither can promise an exact number of free bytes after a burst of concurrent
uploads; both together promise that a full volume is refused honestly rather than turned
into a server fault.

## Keeping a copy that is not on the server

`BRRP_BACKUP_COPY_DIR` copies each verified backup to a second destination, and a second
folder on the same disk is not disaster recovery: the disaster that takes the server takes
that folder with it. The copy that matters is encrypted and off the host.

The server cannot do this part itself, because it must not hold the key that opens it. So
the procedure is three steps, and the key custody rules are the ones that matter:

1. **The server writes a verified archive.** Scheduled backups (or
   `brrpbrrp backup --data-dir … --out backups/name.zip`) and every archive is verified
   entry-by-entry against its manifest. `BRRP_BACKUP_COPY_DIR` can carry it to a staging
   folder on a separate disk.
2. **Something that is not the server encrypts it before it leaves.** The recommended tool
   is [age](https://age-encryption.org/), because the server only ever needs a *public*
   recipient key:

   ```console
   $ age-keygen -o ~/brrp-backup-identity.txt          # on the operator's own machine, once
   # the public key is printed; put only that in /etc/brrpbrrp/backup-recipient.txt on the server
   $ age --encrypt --recipients-file /etc/brrpbrrp/backup-recipient.txt \
         --output "$STAGING/brrp-$(date +%F).zip.age" "$STAGING/brrp-$(date +%F).zip"
   $ rm "$STAGING/brrp-$(date +%F).zip"
   ```

   `gpg --encrypt --recipient <keyid>` with a keyring of its own works the same way. If
   neither is installed, `openssl enc -aes-256-cbc -pbkdf2 -iter 600000 -salt` with the
   passphrase read from a file (`-pass file:…`) is the fallback that is on every host —
   with one trade-off to know: `openssl enc` has no authenticated mode, so a corrupted or
   wrong-key ciphertext is not detected by the tool itself. The archive's own manifest is
   what catches it, when `brrpbrrp verify-backup` runs after decryption.
3. **The encrypted copy is the copy.** Delete the plaintext staging file in the same step
   that encrypts it, or the procedure is theatre.

**Key custody.** The private key — the age identity, the gpg keyring, the openssl
passphrase — never lives on the server: the server holds only the public recipient, which
can encrypt and cannot decrypt. Keep at least two copies of the private key, in two places
that do not fail together (a password manager and an offline copy held by a second person),
and record who holds them in the recovery plan. The backups are only as recoverable as that
key is findable by somebody other than the one person who set it up.

**If the key is lost**, the encrypted copies are unrecoverable — there is no recovery path
and no backdoor, by design. What remains is the most recent archive that is still on the
host and any plaintext copy still inside the retention window; the encrypted copies stay
useless forever. Treat a lost key as an incident: rotate to a new key pair, keep the old
ciphertext in case the key resurfaces, and take a fresh backup immediately.

**To restore**, on the new host:

```console
$ age --decrypt --identity ~/brrp-backup-identity.txt --output restore.zip brrp-2026-10-03.zip.age
$ brrpbrrp restore --data-dir /var/lib/brrpbrrp --file restore.zip
$ brrpbrrp verify --data-dir /var/lib/brrpbrrp
```

Then start the server and prove it: sign in, search for a known message, download an
attachment and compare its bytes.

`node scripts/backup-encrypted-rehearsal.mjs` runs exactly those steps against a real
installation — including deleting the plaintext and restoring only from the encrypted copy —
and reports which tool it exercised. It is part of the release evidence, not a suggestion:
a procedure nobody has run is a procedure that does not work.

## Watching a running server

Two things answer "is this installation healthy", and they answer different questions.

`GET /api/admin/metrics` (operator authority) is the machine-readable view: per-route
request counts, status classes and latency buckets — labelled by route *pattern*, never by
path — the process's resident memory, CPU time and goroutine count beside Go's own heap
figures, what the media path has forwarded and dropped, the connection count, storage
sizes, the backup job's status, and an `about` block with the build's schema number,
backup format, websocket protocol range, platform and TLS mode. It deliberately does not count messages: a metrics
endpoint that runs a table count every fifteen seconds costs more than it reports. Nothing
in it is message content, a member name or a conversation identifier.

Every figure names what it measures, because a metric whose name and value disagree is
worse than no metric:

- **GC pauses** are reported as `go_gc_pause_ns_latest` (the most recent pause) and
  `go_gc_pause_ns_max_last_256` (the largest pause inside the runtime's 256-pause ring,
  over the pauses that have actually happened). There is no `go_gc_pause_ns_max`; that
  field used to hold the latest pause under a maximum's name.
- **Storage sizes** (`uploads_bytes`, `backups_bytes`) come from a measurement taken by a
  background worker every five minutes, not from a walk on the request. The value carries
  `measured_at` and `age_seconds`, so a stale figure is visibly stale, and
  `measurement_error` when the walk could not read a directory. A recursive walk is not
  free at scale — 20,000 small files measured 124 ms on the development host — and a
  scrape every fifteen seconds must not pay it.
- **Process counters** (`resident_bytes`, `cpu_seconds`) are the operating system's own
  figures on Linux, macOS and Windows. `reported: false` means the platform could not
  supply them; the Go heap figures beside them are named `go_heap_bytes` and `go_sys_bytes`
  and are not the process's resident set.

`make probe BASE=https://chat.example.com` (add `COOKIE='brrp_session=…'` for the
authenticated checks) asks the deployment from outside, over the network, the way a
member's browser does: setup status, the shell, one of the assets the shell names, the
security headers, the session and the metrics endpoint, and the websocket handshake. It
distinguishes the two ways a websocket fails — refused, which is a proxy doing its job on
an unauthenticated request; upgraded but silent, which is the application not speaking; and
never answered, which is a proxy eating the upgrade — because they are fixed in different
places. Run it from outside the network on a schedule and alert on a nonzero exit.

Resource/UI/retention/TURN settings are stored in SQLite and managed through the admin UI. Do not edit that table while the app is running. Keep the TURN relay range 49152–49999 separate from a customized WebRTC range.

**What this release's media does and does not do (owner's decision, 2 October 2026).** The
relay is the embedded TURN server this binary runs — it serves UDP, TCP and TLS — and it is
the only relay 1.0.0 supports: there is no external TURN setting, and pointing clients at
somebody else's relay is not a supported configuration. Media is carried over IPv4; IPv6
is not offered as a media transport. A network that blocks UDP *and* cannot reach the
embedded relay over TCP/TLS is therefore unsupported, and host qualification records which
restrictive networks the embedded relay does traverse. The TURN setting and the forced-relay
setting in the admin UI are how an operator makes such a network work as far as this release
can — forcing the relay only asks clients to prefer it, and the embedded relay still has to
be reachable — and the per-call log line `voice path established … relayed: true` is the
evidence that it did.

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

Database migrations are transactional and idempotent. The current source candidate writes schema 25 and upgrades supported earlier schemas in the order the registry lists them; the packaged alpha.5 checkpoint writes schema 10. Each migration stage has its own transaction and the framework verifies postconditions before admitting normal service. Use the actual candidate's `brrpbrrp preflight` to see the source and target schema, integrity, journal mode and pending migrations without touching anything. The binary refuses a database from a future unsupported schema instead of downgrading it. Test updates and restores on a copy before applying them to the live group. After a schema upgrade, rollback requires a verified pre-upgrade backup and the matching older executable.

`make rehearsal` (or `node scripts/upgrade-recovery.mjs`) rehearses fresh installation and same-build recovery on disposable data directories: preflight, install, a message and an attachment, backup, restore into an empty destination, verify, and a comparison of the message and attachment bytes after the restore. It does not cross a release boundary. Run it before an upgrade, and then repeat the restore on the real destination with the archives you actually intend to keep, because a temporary directory on the server cannot prove an off-host copy is readable.

`make rollback-rehearsal` (or `node scripts/upgrade-rollback.mjs`) crosses a version boundary using a real archived prior release. It installs with that executable, writes community messages/reply/pin, an invitation, a file and MFA, and saves the old executable's snapshot. It records the actual baseline schema and backup format, checks whether the prior executable can read a candidate-created snapshot of the still-unmigrated installation, then upgrades and compares rows, attachment bytes and MFA. It records post-snapshot writes, restores the old snapshot with the old executable into a clean folder, and checks both retained data and expected later-write loss. An injected incomplete-upgrade marker also exercises restart/postcondition/search-index recovery. Use `--old-release ZIP` or `--old-binary PATH` to choose an exact baseline; otherwise it looks for an archived platform release in `dist/releases` and fails if none exists. Save JSON with `--report FILE`. Current fixtures do not establish private-conversation recovery, native service/container lifecycle, off-host restoration or capacity.

Save the old executable and its verified pre-upgrade snapshot together. Candidate-created pre-upgrade archive compatibility depends on the actual format and database schema: the recorded alpha.5 baseline accepts format 2 with schema 10, while legacy alpha.3 refuses format 2. An archive of the already-upgraded schema is not an old-version rollback source. Verify the intended archive with the retained executable and rehearse restoration before relying on it. Quiesce writes before the snapshot: rollback loses later writes, including later security decisions that must be reconciled before serving users again.

After the server is updated, open clients show **An update is ready**. Users can also check under **Your preferences → Account → Check for updates**. **Update now** preserves unsent text, staged attachments and the current reply for that refresh. It becomes available after voice ends and pending sends/uploads finish. If browser storage cannot preserve a draft, the app asks the user to send or copy it before refreshing. Existing clients choose their refresh time; a server restart can still briefly disconnect chat and voice.

## References

Service behavior follows [Go's Windows service package](https://pkg.go.dev/golang.org/x/sys/windows/svc) and [systemd's execution settings](https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html). Installation scripts are supplied for review and execution on the target host; cross-compilation alone does not prove native installation behavior.
