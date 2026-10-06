# BrrpBrrp

A small, self-hosted place for up to 100 people to talk. One native Go binary serves the entire app, with SQLite storage and no separate database service or hosted account system. Run it directly on Linux, Windows or macOS, or use the included Docker setup.

Text channels, persistent messages, replies, pins, file sharing, reactions, mentions, full-text search and Opus voice are included. Administrators invite members, protect accounts with an authenticator, moderate conversations and schedule backups. Roles carry delegation: create a role below your own, grant only capabilities you already hold, and assign it per account. Channels live in ordered categories and can be restricted to named roles or accounts, with an inspector that answers "who can access this?" using the server's own rule; restricted channels and announcement posting stay switched off at the API until the privacy gate passes. The interface has light, dark and system themes, a phone layout, private media previews and an installable PWA shell.

This is an alpha candidate. Automated integration and race checks and local browser checks are recorded in [acceptance.md](docs/acceptance.md). Production sign-off also requires voice, push and service tests on the actual deployment machine and network.

For an ordered rollout and sign-off record covering both Windows and Docker, use the
[production release runbook](docs/production-readiness.md). The
[host qualification procedures](docs/host-qualification.md) are the per-host checklists;
they are written and have not been run, because this development environment has no
Windows host, no Docker, no PowerShell and no Tailscale peer.

The packaged **0.4.0-alpha.5** checkpoint adds message reporting, channel slow mode, per-channel notification muting, explicit recovery for failed sends, and account removal that keeps conversation history under “Deleted member.” It includes focused reply discussions, a keyboard channel picker, roles and channel access controls. See the [community guide](docs/community-guide.md), [alpha.5 release record](docs/releases/0.4.0-alpha.5.md) and [verification](docs/acceptance.md).

The current source candidate **0.4.0-alpha.8** is at **schema 25**. It adds, on top of
opt-in consented direct messaging, groups and group chat, an activity inbox with
notification preferences, named community threads, private calls and screen sharing,
member moderation (report assignment and expiring timeouts), a member's own data
export, and operator alerts that report whether they were *delivered* rather than
merely configured. On top of alpha.7 it carries per-operation retry identity for
uncertain replies, export delivery that is re-authorized per request, a thread's own
reading position and bounded window, and a screen path whose audio is a publication of
its own.

**Read [the alpha.8 record](docs/releases/0.4.0-alpha.8.md) first, and its "Known
issues" section above all.** It is the shortest honest account of what this candidate
is: what has been verified, and what has not. In summary — the private features remain
**default-off and have never been qualified on either required deployment**, no
capacity or soak measurement exists, no physical microphone or blocked-UDP call has
been exercised, no browser viewer decodes a shared screen yet, and no independent
review or beta has happened. Nothing here has been published.

Policies worth reading before changing any of it: the [private conversation
policy](docs/adr/0012-private-conversations-consent-and-shared-storage.md), the
[groups policy](docs/adr/0016-consenting-groups.md), the [media architecture
decision](docs/adr/0019-media-architecture.md) and the [member export
contract](docs/adr/0019b-member-export.md).

## Start a server

Extract the ZIP for your operating system and processor, then open the built-in setup wizard:

- **Windows:** double-click **Setup.cmd** or **brrpbrrp.exe** and accept the administrator prompt.
- **Linux:** run `chmod +x brrpbrrp`, then `sudo ./brrpbrrp install` on a systemd host.
- **macOS:** open **Setup.command**, or run `./brrpbrrp install` as your normal user.

The browser walks you through connection settings, storage, installation and creating your administrator. It checks the selected port, saves the settings, installs the background service and verifies startup. Port **8082** is the default. Your data stays on your server, and no third-party hosting or VPN account is required.

Choose access from this computer, your local network, or an HTTPS proxy you manage. LAN setup includes a server certificate and guidance for trusting it so voice works. You can expose the app online later using your preferred infrastructure. See the [installation walkthrough](docs/installation.md) and [hosting guide](docs/hosting.md).

To replace an older executable and keep using the same server, follow [updates and rollback](docs/deployment.md#updates-and-rollback). For a fresh start, choose **Uninstall an existing version** in the wizard. It removes the old application and service, keeps the old data and settings in place, and selects an unused folder for new setup. Version 0.4.0-alpha.5 upgrades older databases through schema 10 (backup format 2), so take an exported backup (and keep the executable you are replacing) before updating. Start with the [alpha testing guide](docs/alpha-testing.md).

## Run in the background

The wizard installs and starts a systemd service with a dedicated account on Linux, a native LocalService service on Windows, or a per-user launch agent on macOS. The macOS agent becomes available after that user signs in. Finishing setup closes only the wizard; the server continues running.

For a manually configured server, run `brrpbrrp serve --config /absolute/path/server.env`. The optional platform scripts remain available for operators who manage service configuration themselves. See [deployment.md](docs/deployment.md) for those scripts, updates and firewall settings. A reverse proxy carries chat and voice signaling; WebRTC audio still needs its own UDP route.

## Run with Docker

The source includes a multi-stage Dockerfile and Compose configuration for Linux x64 and ARM64. From this source folder:

```sh
cp .env.docker.example .env.docker
# Edit .env.docker for your server's address and voice IP.
docker compose --env-file .env.docker up -d --build
docker compose --env-file .env.docker exec brrpbrrp /usr/local/bin/brrpbrrp setup-token
```

The defaults serve `https://localhost:8082` with a generated local certificate authority. Trust its public root before using voice. The Docker data volume keeps accounts, messages, uploads and certificates across container replacement. Read the [Docker hosting guide](docs/docker.md) for first setup, LAN/public HTTPS, voice ports, backups and updates. Container builds and runtime validation still need a Docker-enabled host; see [acceptance.md](docs/acceptance.md#docker-packaging--2026-09-14).

## Back up and restore

While running, use **Manage server → Maintenance → Create backup**. Copy the downloaded ZIP to separate, private storage. A backup contains account password hashes and push keys; it is not encrypted. TLS certificate files and host configuration are not in the ZIP—back these up separately and privately.

For an offline backup, stop the server and run:

```sh
./brrpbrrp backup --data-dir ./data --out backups/manual.zip --include-uploads=true
```

Restore into a **new** data folder. Place the ZIP inside that folder first:

```sh
mkdir restored-data
cp manual.zip restored-data/restore.zip
./brrpbrrp restore --data-dir ./restored-data --file restore.zip
./brrpbrrp serve --data-dir ./restored-data
```

Every archive carries a manifest with a size and SHA-256 digest for the database and each attachment, so an altered or truncated archive is refused rather than installed. Check a copy where it lives, with no data folder or lock needed:

```sh
./brrpbrrp verify-backup --file /backups/manual.zip
```

Restore verifies the archive and database, preserves messages/search/settings and signs everyone out and clears device push subscriptions. A backup made without uploads restores without attachment records. No existing database is overwritten. Keep the original installation until the restored one has been checked. More in [the admin guide](docs/admin.md).

## Build from source

Use Go 1.27.1 or newer and Node.js 22.12+ (Node 24 is used for checks). Install dependencies and build the frontend before Go:

```sh
npm --prefix web ci
npm --prefix web run check
npm --prefix web run build
go test ./...
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o dist/brrpbrrp ./cmd/brrpbrrp
```

On PowerShell, set `$env:CGO_ENABLED='0'` and use `dist/brrpbrrp.exe`. The compiled frontend in `web/dist` is embedded directly. Node.js, npm and Go are build tools; users of packaged releases do not need them.

`make build`, `make test` and `make check` provide the same workflow; `make browser-check` runs the new browser journey after Playwright Chromium is installed, and `make rehearsal` migrates, backs up, restores into an empty destination and compares the data that survived. `go run scripts/release.go` builds ZIPs for Linux, Windows and macOS on x64 and ARM64, plus a versioned Docker source ZIP, with SHA-256 checksums and dependency notices. CI defines native, browser and Docker checks; manual dispatch produces packages. It does not publish a public release.

The source uses ordinary Go packages and small Svelte components. See [architecture.md](docs/architecture.md) for responsibilities and constraints, [CONTRIBUTING.md](CONTRIBUTING.md) for development and review, and the [community platform roadmap](docs/roadmap.md). `make notices` regenerates the dependency notices from the modules the compiler links and the packages the web bundle contains.

## Focused conversations

Open **Reply thread** from a message's actions, or click its reply count, to follow a discussion with live replies, reactions and attachments. Thread and channel drafts are kept separately. Closing a discussion preserves your place in the channel. Replies stay visible in channel history and search; deleting the original keeps its replies in the channel. The view loads 50 replies at a time, up to 250; search can find older replies.

Press **Ctrl/Cmd + K** to jump to a text channel by name, topic or category. Unread channels and mentions appear first. Use arrow keys and Enter to choose a result. **Ctrl/Cmd + Shift + K** opens message search.

## Scope and limits

One workspace, text and voice channels, capability-based roles and delegation, reply
threads with their own reading positions and archive state, consented direct
conversations and groups with blocking and private evidence reporting, named threads,
member data export, community voice with private calls, and screen sharing with its own
screen audio. Direct conversations and groups are behind the default-off
`BRRP_ENABLE_PRIVATE_CONVERSATIONS` gate, and restricted channels behind
`BRRP_ENABLE_RESTRICTED_CHANNELS`, until their independent reviews are done; every
feature they gate is implemented and locally tested. Friends, multi-server guilds,
public read receipts, camera video and federation are not available. Media is carried
over IPv4 and uses the embedded TURN relay only — no external relay is supported — so a
network that blocks UDP and cannot reach that relay over TCP/TLS is a documented,
unsupported case. Live and reversibly
deactivated accounts are limited to 100 (permanently removed history identities do not
consume a slot), channels to 100, pins to 100 per channel, and voice to 20 simultaneous
participants across the server and 10 in one room. Those are implementation boundaries
rather than measured capacities. Uploads have a hard 12 MB cap. Messages are kept
indefinitely unless an administrator enables retention or deletes them. The app does not
provide end-to-end encryption; the server operator controls…

## Troubleshooting

- **Microphone or install option unavailable:** use trusted HTTPS (localhost HTTP is for development only). Check the browser's site permissions. On iOS, install to the Home Screen before enabling push.
- **Voice connects but nobody can hear:** check mute/deafen, browser audio playback, UDP firewall/NAT forwarding and `BRRP_VOICE_EXTERNAL_IP`. Enable TURN when clients need a relay. A TCP-only reverse proxy cannot carry WebRTC audio.
- **Sign-in works at one address but fails at another:** `BRRP_BASE_URL` must exactly match the browser's origin, including port. Cookies and WebSockets use the same origin.
- **Behind a proxy:** set its exact trusted CIDRs, preserve WebSocket upgrades and forward the original host. Bind the backend to loopback or a private interface.
- **Data folder in use:** stop the existing process before backup/restore/recovery. The lock file can remain after shutdown; its presence alone is normal. Do not delete it to bypass an active lock.
- **Forgotten admin password:** stop the server and use `create-admin --username recovery_admin --password-stdin` with a passphrase supplied on standard input. This creates a new account and never overwrites an existing one. Prefer first-time web setup for a normal installation.
- **Lost authenticator and recovery codes:** an operator removes a lost second factor from **Manage server → People → Manage**. If the *owner* is the locked-out account, stop the server and run `recover-owner --username OWNER --password-stdin`; it is the only path, because only the owner holds those powers. Both are recorded in the audit log. See the [administrator guide](docs/admin.md#recovering-access-when-someone-is-locked-out).
- **Server fails at startup:** inspect terminal output, systemd journal or `data/logs/server.log`. Check port conflicts, folder permissions and certificate/key paths.

## License

BrrpBrrp is released under the [MIT License](LICENSE): you may use, modify and redistribute it, including commercially, as long as the copyright notice and permission notice stay with it. Every release also contains other people's software — the Go packages linked into the binary, the frontend packages bundled into the web assets, and the Go standard library — listed with its own license in [THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md) and included in the packaged downloads.

Contributions are accepted under the project's [Contributor License Agreement](CLA.md), which preserves the ability to relicense the project later; [CONTRIBUTING.md](CONTRIBUTING.md) explains how to sign it. The MIT license grants no trademark rights: forks are welcome, but a fork is not the official release and should not be presented as one.
