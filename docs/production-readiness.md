# Production release runbook

Use this runbook for both the Windows service and Linux Docker deployments. Keep the release marked as an alpha until every applicable gate below has recorded evidence on the exact build being shipped. Automated checks prepare a candidate; they do not prove audio, push, native service behavior or restoration on another machine.

## 1. Prepare one candidate

1. With Go 1.27.1 and Node 24, run `npm --prefix web ci`, `npm --prefix web run check`, `npm --prefix web test`, `npm --prefix web run format:check`, `npm --prefix web run build` and `npm --prefix web audit --audit-level=moderate`. Also run `node scripts/check-docker-inputs.mjs --self-test`, `node scripts/check-docker-inputs.mjs` and `BRRP_GO=go node scripts/check-source-package.mjs` (all part of `make check`): they fail when the Docker build context or the source package loses an input, and they are the only guard that runs without a Docker host. Confirm `go test ./internal/app -run TestVersionIdentityAgrees` passes, so the executable, the frontend metadata and the release helper all carry one version.
2. Run `go vet ./...`, `go test -race ./...` and `go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...`. Build `dist/brrpbrrp-e2e` with `go build -o dist/brrpbrrp-e2e ./cmd/brrpbrrp`; from `web`, install Playwright Chromium with `npx playwright install chromium` and run `npm run test:e2e`. The browser suite starts and removes its own temporary server data. On a Mac with system Chrome, set `BRRP_E2E_BROWSER=chrome` when running it.
3. Let one workflow carry every required check. `.github/workflows/check.yml` runs the OS matrix, the browser journey, the static Docker input audit and the source-package contents check, and calls `.github/workflows/docker.yml` for the image build plus its container lifecycle check. Its manual `packages` job requires `test`, `browser` **and** `docker`, so a distribution cannot be built while any required job has not passed; there is no separate promotion path. Record the revision and the generated checksums. Do not substitute an older binary or a locally modified archive.
4. If distributing downloaded Windows or macOS packages, sign/notarize through the chosen distribution identity and verify the resulting files. No signing identity is stored in this repository.

## 2. Install and operate both hosts

### Windows service

Use the extracted candidate and [alpha rehearsal](alpha-testing.md#alpha-rehearsal) on a disposable or backed-up Windows installation. Confirm UAC cancellation, service start/stop/restart, reboot and sign-in behavior, tray/Explorer behavior, update, rollback and uninstall. Compare an old message, search result and attachment bytes before and after updating. Run the read-only `diagnose-brrpbrrp.ps1` script and inspect the service log. Use the wizard's exact installed binary/configuration paths for checks.

Set `BRRP_BACKUP_COPY_DIR` in the protected `server.env` to a private, separate destination writable by LocalService. A separate disk that is replicated to encrypted off-host storage is suitable; a second folder on the same disk is not disaster recovery. Enable scheduled backups with uploads under **Manage server → Maintenance**. Protect the destination, `server.env`, TLS keys and the local CA private key separately. Restore a backup into a new data folder and compare login, search and downloaded attachment bytes before inviting the group.

Run the installed binary's `production-check --config PATH_TO_SERVER_ENV` from the host. It exits nonzero for an unhealthy local listener, failed HTTPS probe of the configured browser address, certificate with fewer than 14 days left, under 2 GiB free, disabled/incomplete scheduled backups, a reported backup failure, a backup older than 36 hours, or a missing/stale extra copy. Thresholds are configurable with `--cert-min-validity`, `--min-free-gib` and `--backup-max-age`. Schedule it every 15 minutes and route nonzero exit/stderr to the chosen alert service. Test alert delivery by temporarily setting a deliberately impossible free-space threshold. This host-side probe cannot prove that another network can reach the server.

### Linux Docker

Create a private directory on a separate mounted backup destination and grant UID/GID 10001 write access. Add `BRRP_BACKUP_HOST_DIR` to `.env.docker`, then run Compose with both `compose.yaml` and `compose.backup.yaml`. Keep `compose.turn.yaml` in the same command when using built-in TURN. For example:

```sh
docker compose --env-file .env.docker -f compose.yaml -f compose.backup.yaml up -d --build
docker compose --env-file .env.docker -f compose.yaml -f compose.backup.yaml exec -T brrpbrrp /usr/local/bin/brrpbrrp production-check
```

Enable scheduled backups with uploads in the admin UI, protect and replicate the copied archives off host, and back up `.env.docker` and `/data/certs` privately. Run `scripts/check-docker.mjs` against the candidate image on a Docker-enabled machine. Recreate the container, then prove messages, uploads, certificates and backups survive. Rehearse restoration into a fresh volume and keep the old image and volume until validation passes. The current local development machine has no Docker Engine; do not mark this row passed from Compose parsing alone.

## 3. Record physical acceptance

Record date, candidate checksum, host OS, browser/device versions, pass/fail, and a short evidence link for each row. Repeat the network rows for both deployments.

| Gate | Windows | Docker |
| --- | --- | --- |
| Native service or container restarts after reboot; stop is graceful; permissions and log rotation checked | Pending | Pending |
| Exact browser origin has trusted HTTPS, working login/WebSocket reconnect and no accidental plain-HTTP exposure from a second device | Pending | Pending |
| Two physical browsers on different networks have two-way voice, moderation and a call over 30 minutes | Pending | Pending |
| Restrictive NAT routes through TURN; document any UDP-blocked network that remains unsupported | Pending | Pending |
| Push mention arrives and opens the right message after app close; iOS Home Screen, denial, disable and shared-device sign-out checked | Pending | Pending |
| PWA installs, opens a reconnect screen offline, recovers online and handles an app update without losing a draft | Pending | Pending |
| A 10–20-user session with up to 10 voice users stays stable; record server RSS, CPU, UDP bandwidth, database growth, free disk and user-visible latency | Pending | Pending |
| A complete backup and separate host configuration/TLS copy restore on a fresh destination; accounts, search and attachment bytes match | Pending | Pending |
| Fresh-install recovery passes locally: install, back up, verify, restore into an empty destination, then prove the account, channel, message and attachment bytes survived there (`make rehearsal`). This is *not* the upgrade/rollback or off-host row above | Passed locally | Pending |
| Production check and external availability monitor alert when deliberately made unhealthy | Pending | Pending |

The rehearsal row is the part of the restore claim this machine can execute: `node scripts/upgrade-recovery.mjs` (or `make rehearsal`) builds a candidate, creates a fresh installation, writes a message and an attachment through the HTTP API, backs it up, verifies the archive, restores it into a second empty data directory and serves the restored copy long enough to prove the account, channel, message and attachment bytes all survived. Its scope is deliberately narrow: **same build, one machine, second directory**. It does not upgrade an old alpha.3/schema-2 installation to the candidate, does not roll back to the matching prior executable, does not interrupt an upgrade, and does not reach a fresh Windows, Docker or off-host destination; it also covers no host configuration or TLS material. Those are the rows above, and they stay pending until hosts exist.

Set resource budgets from the first real soak on each host and rerun against them before wider rollout. Short loopback tests in [release acceptance](acceptance.md#performance-and-navigation-pass--2026-09-14) are useful regressions, not host sizing evidence. A screen-reader and physical keyboard pass on a low-end phone and desktop should accompany the automated browser accessibility scan.

## 4. Release decision and response

Ship only after all required rows pass on the tested candidate and no serious defects remain. Keep a recent complete backup, the matching prior executable/image, and a tested restore path. If an update fails, stop the service/container, restore the prior build and, after any schema change, restore its matching pre-upgrade backup into a fresh data folder. See [deployment rollback](deployment.md#updates-and-rollback) and [Docker rollback](docker.md#updates-and-backups).

`production-check` is a host-side signal. Run an independent HTTPS/WebSocket monitor from another network and alert on its failures too. Voice and push still require periodic real-device checks. Decide who receives alerts, who owns certificate renewal and backups, and who can perform recovery before production sign-off.

Rehearse the credential lockout paths on a disposable copy of the candidate before sign-off, so nobody improvises them on the live server: remove one member's lost second factor from **People**, run `recover-owner --username OWNER --password-stdin` with the service stopped for the owner, sign in afterwards, enroll a new factor and confirm both events appear in **Recent activity** as `user.mfa_removed` and `owner.recovered_offline`. Keep the verified steps, and the passphrase delivery method, in the operator's own records. The [administrator guide](admin.md#recovering-access-when-someone-is-locked-out) documents the procedure.
