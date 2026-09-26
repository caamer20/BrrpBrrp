# Implementation and release record

Source: the full BrrpBrrp implementation plan supplied by the owner. Phases are implemented and verified in order. A compile pass is not evidence that browser, voice or operating-system acceptance tests passed.

## Decisions

- Keep Go, Svelte 5, SQLite, one embedded frontend and native deployment.
- Use a currently supported Go release rather than the plan's old minimum.
- Use fixed receive audio slots for up to ten participants to avoid renegotiation races in the SFU.
- Authenticate every API mutation except setup and login, which have bootstrap/credential checks, rate limits and same-origin protection.
- Validate proxy source networks, not only a user-controlled forwarding hop count.
- Implement Windows service lifecycle integration before offering sc.exe installation.
- Confine data files to the data directory. Treat original upload names as display-only.
- Hold the final production designation until browser, network and service acceptance gates are verified.

## Phase 0 — Scaffolding

Files: Go module and entry point; embedded web assets; Svelte/Vite/TypeScript scaffold; Makefile; environment example; README and this record.

Commands: `npm install`, `npm run check`, `npm run build`, `go test ./...`, `go build -o dist/brrpbrrp ./cmd/brrpbrrp`, run binary and HTTP GET `/`.

Passed: Svelte checks (zero errors/warnings), production frontend build, embedded compiled-asset test, Go build, actual page served from binary. Official Go 1.27.1 archive checksum verified before installation to `.tools/go`.

Limitations: scaffold only; application features are not yet implemented.

## Phase 1 — Configuration and HTTPS

Files: `internal/app/config.go`, `server.go`, `config_test.go`, CLI entry point.

Commands: `go test ./...`, `go build`, HTTPS startup with a temporary data directory.

Passed: invalid configuration rejection, traversal/symlink containment, new data directory through macOS temporary-directory aliases, TLS identities for localhost/IPv4/IPv6, Go build and HTTPS listener startup.

Limitations: ACME issuance needs a reachable domain and has not been exercised against a certificate authority. Native installation tests remain for packaging.

## Phase 2 — Database

Files: `internal/db/schema.sql`, `db.go`, `models.go`, `db_test.go`.

Commands: `go test ./...`, `go build`. Passed: repeated migration, FTS insert/update/delete, foreign-key cascades and WAL configuration.

Limitations: live voice state will be held in memory rather than persisted across restarts. This prevents stale voice participants after a crash.

## Phase 3 — Authentication and setup

Files: `internal/api/router.go`, `middleware.go`, `auth.go`, `bootstrap.go`, `auth_test.go`; CLI wiring.

Commands: `go test ./...`, `go build`. Passed: bootstrap protection, session lifecycle, case-insensitive login, banned login, origin checks and password-change revocation.

Limitations: socket disconnection hooks will be added with the realtime hub. Passwords use bcrypt cost 12 and are limited to its 72-byte input limit.

## Phase 4 — Core REST API

Files: channel, message, reaction, upload and workspace-state handlers; batched message loading; API behavior tests.

Commands: `go test ./...`, `go build`. Passed: permissions, idempotent sends, edits, deletes, search syntax, reaction toggles, upload size, attachment ownership and content disposition.

Limitations: twelve Unicode reactions are supported initially. Physical orphan-file cleanup is part of the background maintenance phase.

## Phase 5 — Realtime

Files: `internal/ws/hub.go`, `internal/api/websocket.go`, mutation broadcasts, session disconnection and private unread updates.

Commands: `go test ./...` with loopback network access, `go build`. Passed: authenticated two-client delivery, WebSocket send, immediate logout disconnection and cleanup.

Limitations: realtime events carry IDs so clients can fetch their own reaction/unread state. Typing events carry a five-second expiry; clients clear stale indicators without periodic server broadcasts.

## Phase 6 — Link previews

Files: `internal/scraper`, `internal/api/embeds.go`, two bounded background workers and message hooks.

Commands: `go test ./...`, `go build`. Passed: public-address filtering, metadata parsing, direct GIF URL handling and build. Fetches use a three-second deadline, a two-megabyte body limit, validated redirect targets and a dialer pinned to validated DNS addresses.

Limitations: remote sites can block scrapers or omit metadata; those links remain usable without an image. External browser media follows the user's preview privacy preference.

## Phase 7 — Mentions and push

Files: mention parsing and storage, VAPID persistence, push subscription endpoints, bounded push worker, service worker.

Commands: `go test ./...`, `go build`. Passed: mention parsing, admin-only everyone mentions, private unread counts and persistent VAPID keys.

Limitations: end-to-end browser push requires a real notification permission and subscription. Edited messages update mention records without sending a second notification.

## Phase 8 — Voice

Files: Pion SFU with nine stable receive slots per client, per-room and global capacity, server-enforced mute/deafen, authenticated signaling, embedded TURN with expiring credentials.

Commands: `go test ./...`, `go build`. Passed: three real WebRTC peers, RTP forwarding, separate-room isolation, channel capacity, signaling ownership, forced mute that a client cannot override, deafen and channel cleanup.

Limitations: NAT/TURN routing and physical microphone playback require real deployment devices. TURN allocations can reach only the server's own interface/public addresses.

## Phase 9 — Chat interface

Files: Svelte workspace state, authentication, message list/composer, attachments/previews, reactions, search, preference dialogs, theme tokens and responsive styling.

Commands: `npm run check`, `npm run build`, `go build`; local browser checks. Passed: zero Svelte errors/warnings; login, send, persistence after reload, edit, search and both themes. JavaScript: approximately 31 KB gzip before voice/admin additions.

Found and fixed during browser QA: app-shell redirects caused a loop. Added a regression test for direct app navigation.

Limitations: voice/admin controls are completed in the following phases. Mobile and notification acceptance are tracked under final polish.

## Phase 10 — Voice interface

Files: browser WebRTC lifecycle, voice participant panel and controls, speaking activity, autoplay recovery and one automatic reconnect attempt.

Commands: `npm run check`, `npm run build`, `go build`. Passed: frontend checks with zero errors/warnings and integrated binary build. Microphone access begins only after an explicit Join voice click.

Limitations: physical microphone/audio and target-browser testing remain on the release checklist. Local Pion peers already exercise the same negotiated track layout.

## Phase 11 — Administration

Files: account management, moderation, settings, audit log, maintenance, backup/restore implementation and admin interface.

Validation pending: last-admin protection, bans/session revocation, settings constraints, retention and backup round trip. Backups use SQLite VACUUM INTO; restore requires an empty destination and revokes restored sessions.

### Phase 11 — administration and backups
- Added admin API handlers and People, Channels, Settings and Maintenance screens.
- Added hourly retention, expired session/orphan file cleanup, optional audit logs, health statistics and consistent SQLite ZIP snapshots.
- Restore validates paths, entry limits, schema and references, requires an empty destination, checks attachments and revokes restored sessions.
- Ran `npm --prefix web run check`, `npm --prefix web run build`, `go test ./...`: passed, zero frontend warnings. Admin permissions, self-lockout, ban invalidation, retention confirmation, backup round-trip/search/attachments and malformed ZIP checks pass.
- Limit: restoration is deliberately an offline command; external server installation remains to be verified in phase 13.

### Phase 12 — installable app and responsive polish
- Added a manifest, generated app icons and bounded static-shell caching. Conversations, API responses and uploads are never cached by the service worker.
- Added mobile member drawer and larger touch controls; fixed coalesced unread events suppressing message refreshes.
- Frontend checks/build and embedded frontend tests pass. Browser verified channel creation, backup download link, health, 390×844 chat/member views; no browser errors.
- Limits: actual OS installation and background push delivery require a trusted HTTPS deployment and permission on a physical device.

### Phase 13 — native operation and packaging
- Added server/backup/restore/setup-token/create-admin/certificate commands, literal configuration files, exclusive data locks and bounded file log rotation.
- Added native Windows service handling with LocalService, Windows BAT/PowerShell helpers, Linux systemd installation and macOS launch-agent installation. Installers preserve data and do not start an unconfigured public server.
- Added cross-platform ZIP/checksum packaging, dependency notices, CI checks and administration/security/deployment/architecture/acceptance documentation.
- All six targets compiled: Linux, Windows and macOS, each amd64 and arm64. Local binary smoke check passed offline account creation, backup, restore, refusal to overwrite/in-use data, embedded UI/manifest and graceful shutdown.
- Go integration and race tests passed, including real TURN packet relay. `go vet ./...` passed. Frontend checks/build pass with zero warnings; the main JS bundle is approximately 42 KB gzip.
- Current security review found and patched reachable STUN crash advisory [GO-2026-6163](https://pkg.go.dev/vuln/GO-2026-6163) with pion/stun/v3 3.1.5. Also updated JWT to 5.2.2 to remove an unused-parser advisory. A final scan is recorded below after rebuilding.
- npm installation's audit reported zero vulnerabilities. Two subsequent standalone production audit requests timed out at npm's advisory endpoint; do not treat those timeouts as successful scans.
- Final native service execution on Linux/Windows, real-device HTTPS/PWA/push, separate-network voice and a full 10–20-user soak remain explicit production acceptance gates. No target host/hostname was supplied. See `docs/acceptance.md`.

### Deliberate refinements to the original plan
- Restore requires an empty destination and clears sessions/device push subscriptions. It never replaces a live database.
- Offline administrator passwords are supplied on stdin, not command arguments.
- Audit, backup and health screens share a Maintenance tab to keep administration compact.
- The log level is a host setting in the configuration file; resource and product toggles are managed in the UI.
- Retention runs hourly; reactions use a small fixed Unicode palette; TURN is IPv4/UDP. These limits are documented rather than hidden behind nonfunctional controls.
- Source formatting, names and comments follow ordinary Go/Svelte conventions, with no generated branding or unnecessary application layers.

### Final verification — 2026-09-04
- Final `go test -race ./...` and `go vet ./...` passed after the security and rate-limit changes.
- Final Go vulnerability scan: zero reachable vulnerabilities and zero advisories in imported packages. One advisory remains in the dependency graph for the unmaintained OpenPGP subpackage of x/crypto; BrrpBrrp does not import or call that subpackage.
- A fixed STUN dependency and patched JWT dependency are pinned in `go.mod`/`go.sum`.
- Six native ZIPs and SHA-256 checksums are generated in `dist/releases`; files include platform installers and third-party notices. Final UI grouping was checked to avoid duplicate default categories and respect voice categories.
- The local preview uses a separate temporary data directory and disposable account. No service was installed on the user's machine and no public server was exposed.

### Voice join chime — 2026-09-04
- Added an original 0.8-second glass chime for the joining participant and people already connected to the same room. Sound is synthesized locally with no download or additional dependency.
- Join announcements follow successful media connections. Deafen silences playback, simultaneous arrivals share one chime, and blocked audio does not queue old notifications.
- Real peer integration tests with the race detector verify delivery, room boundaries and duplicate suppression. Three frontend audio tests, type/accessibility checks, formatting and the production build pass.
- Physical browser speaker and microphone checks remain part of deployment acceptance.

### Voice room navigation — 2026-09-04
- The initial join opens room details. Joining closes the panel, and selecting another room while in voice switches directly. The current room is highlighted; its details remain available from the voice controls.
- Pending joins show progress and a cancel action. Connection errors remain visible in the sidebar, and rooms already known to be full are rejected before leaving the current call.
- Frontend type/accessibility checks, audio tests and build pass. Controller checks with simulated media permissions cover direct switching, duplicate clicks, full rooms, server disconnection, denied microphone access and cancellation.

### Collapsible members panel — 2026-09-04
- Desktop users can collapse the members panel from an arrow in its header and restore it from a small edge control. The device remembers the choice across reloads.
- The existing people button remains on compact layouts, where members appear as an overlay. Both controls expose clear names to assistive technology.
- Frontend type/accessibility checks, formatting and the production build pass in light and dark theme compatible styles.
