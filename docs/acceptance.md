# Release acceptance

This file separates verified behavior from tests that still need a deployment machine or physical devices. Do not describe the release as production-validated until the manual gates below pass.

## 0.4.0-alpha.3 conversations — 2026-09-23

- Focused reply threads use existing schema 2 data. The API checks authentication, direct-reply isolation, nested discussions, chronological cursor pagination, a 50-item page ceiling, invalid cursors, cross-channel rejection, live counts and preservation of replies when the parent is deleted.
- The API and database suites pass with the race detector. Targeted chat/reply regressions pass after the final query simplification. `go vet ./...` passes.
- All 55 frontend tests pass. New cases cover stale reply responses, deletion during reload, disposal, reconnect invalidation, retry idempotency, preserving channel history, confirming delivery after a lost POST connection, discarding failed sends, and separate thread drafts during update handoff. Svelte checks report zero errors/warnings; formatting and production builds pass.
- The expanded Chromium journey passes first setup, persistent chat, thread drafts, attachment upload, failed-send retry, two-tab live replies/edits/deletions, keeping an unsent draft visible after parent deletion, channel picker keyboard/focus behavior, search, sign-out/sign-in and offline PWA startup. Axe WCAG 2 A/AA checks pass on the new thread view in light/dark themes, the 390px thread layout, and the channel picker. Desktop and phone screenshots were inspected. Browser resizing does not establish physical-device behavior.
- The new discussion view retains up to five pages (250 replies), and all replies remain in channel search/history. Named threads, subscriptions, archive controls and private conversations are future work. See the [roadmap](roadmap.md).
- Version identifiers advance to alpha.3 without a schema migration or new runtime dependency. The project now carries an owner-approved license: MIT, with the copyright holder named in [LICENSE](../LICENSE), generated dependency notices in [THIRD-PARTY-NOTICES.md](../THIRD-PARTY-NOTICES.md), contribution terms in [CLA.md](../CLA.md) and a security policy in [SECURITY.md](../SECURITY.md) ([ADR 0006](adr/0006-licensing-and-contribution-terms.md)). Repository publication, the private security intake and release signing still need an owner-controlled destination and credentials, and Windows/Docker runtime, physical-device, deployment, signing and operational gates from the [production runbook](production-readiness.md) remain open.

## Verified locally

- Configuration validation, path containment, setup token closure, session hashing, origin checks and password/session invalidation.
- Message permissions, idempotency, FTS search after edits, reactions, attachment ownership and upload limits.
- Real WebSocket delivery between clients and immediate logout disconnection.
- SSRF address/redirect/body limits and preview parsing.
- Mention detection and notification routing logic.
- Real local Pion peers: audio packet forwarding, room isolation, capacity, session ownership, server-enforced mute/deafen and cleanup.
- Voice join events reach the joining peer and existing peers in the same room, with no announcement for unconnected reservations or duplicate connection callbacks. Chime tests cover output levels, source cleanup, simultaneous arrivals and suspended playback.
- Admin permission checks, self-lockout protection, ban invalidation and retention confirmation.
- Backup/restore of users, searchable messages and attachments; session removal; malformed/path-traversal ZIP rejection; refusal to overwrite a database.
- Exclusive data-folder lock and literal configuration parsing.
- Frontend type/accessibility compilation checks and production build.
- Browser login, message send/reload/edit/search, light/dark mode, channel creation, health, backup creation and phone-size chat/member views; no observed browser console errors.
- Go race detector passes for the integration suite on the development Mac.
- Installer checks cover loopback restrictions, capability/origin validation, preview mutation denial, all three configuration profiles, occupied ports and existing data, TLS hostname verification, administrator creation through the running server, service startup retry and refusal to resume manually changed settings.
- Installer browser checks cover connection/storage/review navigation, proxy options, inline path validation, light/dark themes and a 390px layout without horizontal overflow. Account creation is integration-tested; native service installation remains a gate below.
- Version 0.3.0 cross-compiles for Linux, Windows and macOS on amd64 and arm64. Release archives include native setup launchers, installation/hosting guides and SHA-256 checksums.
- The 0.3.1 uninstall tests verify legacy detection, unchanged database/configuration/attachment bytes, fresh-folder preflight, lock and symlink rejection, explicit confirmation, elevation checks, stale reviews and exclusive operation state. Browser checks cover the uninstall review in dark/light themes and a 390px layout with no horizontal overflow. Native service removal still needs validation on the target OS.

Native installation and physical-device checks remain necessary even when local tests and cross-compilation pass.

## Required before production sign-off

1. Install and start the service on the selected Linux or Windows server. Verify restart after reboot, graceful stop, account permissions and log rotation. Also exercise the other supported service scripts on native machines in CI or a test host.
2. Use the actual public hostname with a trusted certificate. Verify login and WebSocket reconnect from a second device; confirm the backend is not accidentally exposed as plain HTTP.
3. Join voice from two physical browsers on different networks. Check both directions, the join chime on both devices, echo/noise behavior, mute, deafen, moderation, reconnect and a call longer than 30 minutes. Repeat with a restrictive NAT through TURN. This release's TURN is UDP-only.
4. Enable push on supported physical devices, close the app, send a mention from a separate account and verify delivery/click routing. On iOS test an installed Home Screen app. Verify denial/disable paths and a shared-device sign-out.
5. Install the PWA, open it offline, reconnect and verify normal operation after an app update. Test current Chrome/Edge, Firefox and Safari where supported.
6. Run a soak at the 100-account ceiling with up to 20 voice participants serverwide and 10 in one room. Observe resident memory, CPU, UDP bandwidth, database growth and free disk. No resource budget is claimed from the small local smoke tests alone.
7. Create a backup on the target host and rehearse a restore into another folder. Check accounts, search and downloaded attachment bytes. Keep a copy on separate storage.
8. Review the chosen hostname, firewall, certificate renewal, recovery ownership and backup schedule. Sign/notarize release artifacts if required for your distribution channel.

For the installer, also complete each connection profile on a fresh Windows and Linux host. Check cancellation of the administrator prompt, an occupied port, a storage permission failure, service registration/startup failure and retry, and first-account creation after reopening setup. Uninstall both a stopped legacy installation and a running wizard installation; verify the data remains intact, service/firewall cleanup finishes, and a fresh installation succeeds in the suggested folder. Validate certificate trust on a second device for LAN mode and WebSocket forwarding for a user-managed HTTPS proxy. No particular VPN or hosting provider is required.

## QOL update verification — 2026-09-05

- Channel movement is an administrator-only atomic transaction, preserving channel settings and assigning contiguous positions. API regressions cover before/after ordering, category changes, persisted responses, anonymous/member rejection, and missing/self/cross-type targets leaving the order unchanged.
- All 60 server-approved reactions pass add/remove API checks; duplicate palette entries and unsupported reactions are checked.
- Passed: `go test -race ./...`, `go vet ./...`, frontend type/Svelte checks (zero warnings), all 10 frontend tests, formatting checks, and the production frontend build.
- Isolated local browser smoke checks confirmed pointer dragging to the top of the sidebar, click-based channel movement, persisted ordering after reload, reaction search and selection, and Escape dismissal with focus restored to the reaction button. Native HTML dragging was replaced with pointer handling after browser verification exposed unreliable behavior.
- Touch-device behavior and deployment-machine acceptance remain to be verified on real devices; these local checks do not replace production sign-off.

## 0.4.0-alpha.1 verification — 2026-09-07

- Passed the full Go suite with the race detector, `go vet ./...`, and Windows-targeted vet for the desktop companion, CLI and installer. Native Windows execution is still a manual gate.
- Certificate regressions verify a real CA-to-leaf trust chain, hostname rejection, preservation of the trusted root across repair, renewal of expiring leaves and changed hostnames, explicit legacy migration, refusal to replace unrecognized certificates, and restoration of the previous pair when repair fails. Installer regressions check DER public-root downloads, confirmation/fingerprint/preview/busy guards, remote/origin/token rejection, and preservation of the prior executable during update.
- Frontend type/Svelte checks passed with zero warnings; all ten existing frontend tests, formatting checks, and the production build passed. `npm audit` found zero vulnerabilities. Go's vulnerability scanner found zero affected application symbols or imported packages, with one advisory confined to an unused package in a required module.
- Browser checks on an isolated local fixture covered 320px and 390px phone layouts, a 320×400 short viewport, and a 1440×900 desktop. Chat, long unbroken messages, expanding multiline drafts, channel/member drawers, Escape/focus restoration, and the server-settings deep link behaved as expected. The composer ended at the viewport bottom; the narrow dialog stayed within its height. No horizontal page overflow was observed in those layouts and no browser warnings/errors were observed after the final reload.
- The installer connection/storage flow was checked at 320px. A read-only Windows-ready fixture verified the update/certificate instructions at 390px, including the fingerprint, device instructions and disabled mutation controls in preview. These fixtures do not exercise the actual Windows certificate store, UAC or service manager.
- All Windows PowerShell files passed syntax parsing. The diagnostic now reports public root fingerprint/trust and tray shortcuts, using normal TLS validation.
- The native macOS binary passed the package smoke check: CLI administrator creation, offline backup/restore, existing-data refusal, exclusive locking, embedded frontend/manifest and graceful shutdown. Packages contain no preview accounts, data or private certificates.

The [alpha rehearsal](alpha-testing.md#alpha-rehearsal) tracks the outstanding Windows tray/Explorer/UAC/reboot/update/uninstall checks, physical mobile keyboards and certificate trust, and two-device voice tests. The Windows package remains unsigned. Do not treat cross-compilation or desktop viewport resizing as completion of these native gates.

## UX readiness pass — 2026-09-14

This pass prepares a candidate for deployment testing. The physical-device and native service gates above are still required for production sign-off.

### Changes

- Phone and touch layouts use a compact message action button with labeled reply, reaction, pin, edit and delete actions. Desktop hover actions remain available. Reply selection focuses the composer; editing focuses the message text.
- Search opens with its input focused, gives useful initial/no-result guidance, and hides results when the query or channel changes. Requests capture the submitted query so delayed results cannot be highlighted using newer text. Pins show a retry action on failure.
- Chat displays connection loss and persistent message-load errors, including on phones. Earlier-message requests cannot overlap. Changing channels clears the previous channel's pagination state.
- Sending from history returns to the live conversation. Failed sends remain retryable in the current session, including after switching channels. A confirmed server delivery cannot be marked failed by a later POST network error.
- Session changes invalidate in-flight state, history and message requests. Browser storage failures no longer prevent startup or interrupt preference changes and sign-out.
- Uploads finishing after a channel switch stay with the original draft. Upload progress names the file and destination channel; oversized files report the configured limit. In-flight uploads cannot show notifications after the composer unmounts. Attachment drafts remain limited to the current page lifetime.
- Ordinary API requests time out after 30 seconds with actionable feedback. Backup creation explicitly opts out because large archives can take longer. Invalid JSON from a proxy is reported as a failure.
- Sign-in has a password visibility control and focuses a requested second-factor code. The theme toggle reflects the resolved system theme. Browser titles identify the current channel and server, and search shortcuts use the platform's modifier.

### Verified

- Svelte/type/accessibility checks: **0 errors, 0 warnings**. Formatting and the production frontend build pass.
- **24 frontend tests pass**: the original 10 plus 14 request, storage and conversation regressions. The eight new workspace regression cases fail against the pre-change controller and pass against this candidate.
- Full Go integration suite passes with the race detector; `go vet ./...` passes. The embedded-frontend tests also pass after rebuilding the final assets.
- `npm audit` reports **0 vulnerabilities**, including development dependencies. Go vulnerability scanning reports **0 affected application symbols and 0 affected imported packages**. One advisory is confined to an unused package in a required module.
- Browser checks on an isolated fixture covered desktop light/dark themes, 390px and 320px phone layouts, message action menus, reply/send, search input focus, matching/no-result/changed-query states, Escape focus restoration, sign-out and password visibility. At 320px, the search dialog stayed inside the viewport with no horizontal page overflow.
- Stopping and restarting the fixture verified the connection banner, a persistent channel-load error, automatic recovery, and retry of a failed message after navigating away and back. The retried message appeared once.
- The final macOS candidate passes CLI setup, offline backup/restore, refusal to overwrite restored data, exclusive locking, embedded frontend/manifest and graceful shutdown smoke tests. The Windows x64 candidate cross-compiles; it has not been run on Windows.

Local binaries are `dist/brrpbrrp-ux-candidate` (macOS x64) and `dist/brrpbrrp-ux-candidate-windows.exe` (Windows x64), with hashes in `dist/UX-CANDIDATE-SHA256SUMS`. They retain the existing prerelease version. Existing release archives have not been replaced or published.

### Still required

Complete the [production sign-off gates](#required-before-production-sign-off) on the intended host: native installation/reboot/update, trusted HTTPS and a second device, physical voice and TURN, push delivery, PWA offline/update behavior, the multi-user soak, and an on-host restore rehearsal. Browser resizing does not validate physical mobile keyboards, microphone permissions, or native services.

## Performance and navigation pass — 2026-09-14

All six follow-up improvements are included in the current local candidate:

1. Message/reaction events update affected messages, with coalesced lookups and lightweight unread counts. Workspace metadata changes and reconnects still resynchronize state.
2. Up to five inactive channel windows plus the active channel are held in memory, capped at 250 messages each. Returning to a channel restores the reading anchor. A visible first-unread shortcut can reach older unread history.
3. Authenticated 640-pixel image previews reserve their layout space, defer loading in data saver, and link to the unchanged original. Decoding and disk cache use explicit resource limits.
4. Installer, administration and preferences code loads on demand. The embedded server serves prebuilt gzip/Brotli assets with conditional requests.
5. Preferences has Appearance, Audio, Notifications and Account tabs. Notifications show browser/device status; voice has microphone selection and testing before joining.
6. The complete first-install app shell works offline. Updates wait for the user's action, preserve text/attachment/reply drafts for that refresh, and wait for active voice, sends and uploads to finish.

### Measured locally

The saved pre-change binary and final candidate were served directly over loopback HTTP on the development Mac. Numbers are response-body bytes, excluding headers. KB below means 1,000 bytes. These measure transfer size and local server work, not mobile rendering or field performance. Raw results are in [performance-2026-09-14.json](performance-2026-09-14.json).

| Measurement | Before | Candidate | Result |
| --- | ---: | ---: | --- |
| Initial JavaScript + CSS transferred with Brotli requested | 259.4 KB, uncompressed | 58.3 KB | 77.5% fewer bytes |
| Initial JavaScript + CSS transferred with gzip requested | 259.4 KB, uncompressed | 66.7 KB | 74.3% fewer bytes |
| Initial JavaScript before compression | 211.6 KB | 168.0 KB | 20.6% less JavaScript to parse |
| One reaction refresh, fixture with 50 visible messages | 21,747 bytes for state + history | 381 bytes for one message | 98.2% fewer response bytes |
| New-message refresh, same fixture | 21,747 bytes for state + history | 661 bytes for one message + unread counts | 97.0% fewer response bytes |
| Activity in an unvisited channel | State + visible history | 280 bytes for unread counts | No message/history lookup |
| Synthetic 3200 × 2000 JPEG in chat | 799,700 bytes | 36,887 bytes, 640 × 400 | 95.4% fewer image bytes |

Event rows compare the old and new controller request patterns using actual fixture responses; they exclude WebSocket envelopes and read-marker acknowledgements. Burst coalescing can reduce requests further. Image content affects compression; this gradient fixture is not a representative photo dataset. The first preview took 86.4 ms after deleting its generated cache entry. Twenty warm requests had 0.69 ms p50 and 1.03 ms p95.

The existing workloads ran for 120 seconds with `BRRP_LOAD_SECONDS=120 go test ./internal/api ./internal/rtc -run '^TestLocalWorkload$' -count=1 -v`:

- **Chat:** 20 connected WebSocket clients, 479 successful message creations; p50 **2.81 ms**, p95 **4.12 ms**. These timings measure server message creation, not WebSocket arrival or browser paint. Sampled combined server/client Go heap peaked at **5.76 MB**.
- **Voice:** 10 local Pion peers, 60-byte payloads every 20 ms, **59,960** sent packets and **53,955–53,956** packets received per peer. Sampled combined server/client Go heap peaked at **45.44 MB**. This exercises packet forwarding, not physical microphones, audio quality or TURN across networks.

These are synthetic loopback runs, not a production resource budget, a long soak, or an INP/LCP/CLS measurement.

### Verified

- **45 frontend tests pass**, with **0 Svelte/type/accessibility errors or warnings**. Formatting, the production build and `npm audit` pass; the audit reports zero vulnerabilities.
- Go integration packages pass with the race detector. The compressed-asset suite passes after fixing HEAD response lengths, including after the final frontend rebuild. Native and Windows-targeted vet pass.
- Current Go vulnerability scanning reports **0 affected application symbols and 0 affected imported packages**; one advisory concerns an unused package in a required module. The new `golang.org/x/image` dependency provides WebP decoding and image scaling. `go mod tidy` passes.
- Regression checks cover stale/deleted message responses, snapshot/event races, cache eviction and sign-out cleanup, first-unread boundaries, preview authentication/ownership/dimensions/orientation, account-scoped push-device status, compression negotiation/ETags/HEAD, partial offline installs, private-cache exclusion and update guards/retry.
- Browser checks on disposable accounts confirmed exact channel anchor restoration (same message and **−4.83 px** offset), 640 × 400 previews in reserved frames, data saver placeholders and explicit loading, tabbed preferences with arrow-key navigation, notification permission guidance, pre-join microphone controls and deferred administration screens.
- With 59 unread fixture messages, the latest page started at message 010; **Jump to first unread** loaded and centered message 001. The shortcut remained visible above the scrolling conversation.
- A real two-build browser update retained an unsent text draft, uploaded attachment and reply target. A second update also preserved a draft in an inactive channel. The first-load dialog close/focus race was found in browser testing and fixed before packaging.
- A fresh origin was visited once, its server stopped, and the page reloaded: the complete styled reconnect screen opened offline. This confirms first-install shell caching in the available browser.
- The final macOS binary passes CLI setup, backup/restore, existing-data refusal, locking, embedded UI/manifest and graceful-stop smoke tests. The Windows x64 candidate cross-compiles. Both retain version **0.4.0-alpha.1**; checksums are in `dist/UX-CANDIDATE-SHA256SUMS`. Existing release archives are unchanged.

### Remaining release gates

The [production sign-off gates](#required-before-production-sign-off) still apply. Verify on real low-end phones and supported browsers, including physical keyboards, microphone capture, push delivery, PWA installation/update, restrictive NAT/TURN, native Windows service behavior and an extended multi-user soak. The responsive viewport control did not change the browser's measured 1280 × 720 viewport in this follow-up pass, so no new phone-size result is claimed here. The earlier UX pass's narrow-layout results do not replace retesting the new preferences and unread controls on actual devices.

## Docker packaging — 2026-09-14

- Native Linux packages already support **amd64/x64** and **arm64**. Both current source variants cross-compile as stripped, statically linked ELF executables, including the latest UX/performance changes and the new health-check command. Current standalone binaries are `dist/brrpbrrp-linux-amd64` and `dist/brrpbrrp-linux-arm64`.
- Added a multi-stage Dockerfile that builds frontend assets and the static server from source, then assembles a non-root runtime containing the binary, CA roots and dependency notices. Compose supplies persistent `/data`, a read-only root filesystem, temporary storage, rotated logs, restart behavior and TCP/UDP mappings. An optional second Compose file publishes the built-in TURN ports.
- Docker's official standalone Compose **5.5.1** validates the base configuration and the merged TURN configuration. BuildKit **0.33.0** parses all Dockerfile instructions. Its ignore-pattern library verifies required build inputs and exclusion of local data, credentials, compiled artifacts and dependency caches. These parser checks are not an image build.
- A clean install from the extracted source bundle exposed an invalid `@jridgewell/gen-mapping` lockfile URL for the unavailable `0.4.0-alpha.13` tarball. It now resolves to published **0.3.13**, matching the previously installed package and the parent dependency range, using the registry's verified metadata. Fresh `npm ci`, all **45 frontend tests**, Svelte checks (**0 errors/warnings**), audit (**0 vulnerabilities**) and the production build pass. The resulting frontend has the same build ID as the prior candidate. The extracted bundle also cross-compiles to a byte-identical Linux x64 binary.
- The new `healthcheck` command validates the configured local HTTP/TLS listener and its setup-status response without acquiring the data lock. HTTP success/failure, malformed responses, redirects, certificate trust/hostname rejection and cancellation tests pass with the Go race detector. Native app/CLI vet passes. The package smoke test verifies health while the server owns its database lock and failure after shutdown, alongside setup, backup/restore, UI and graceful-stop checks.
- The notice-only release-helper mode generates notices for the Go dependencies, Svelte, the Vite preload helper and the Go standard library. The Docker smoke script passes JavaScript syntax checking. It is designed to exercise non-root startup, validated HTTPS, first-account creation, messages/uploads and certificates surviving container recreation, compression, backups and graceful stop using its own disposable volume.
- Added a Docker CI workflow that validates Compose/Dockerfile inputs, builds and smoke-tests x64, and builds ARM64. No workflow has been dispatched from this workspace and no image has been published.

**Still unverified:** this Mac has no Docker Engine, Docker Desktop or compatible container runtime. The image itself has not been built or run here; volume ownership, Docker port forwarding, runtime health and the container smoke script must be verified by the supplied CI job or a Docker-enabled host. Real-device voice, TURN, push, certificate trust and the other production gates remain required. The [Docker guide](docker.md) includes startup, networking, backup and update instructions. `dist/brrpbrrp-docker-source.zip` contains the buildable source bundle, not a prebuilt container image.

## 0.4.0-alpha.2 release preparation — 2026-09-22

- Administrator **Sign out everywhere** now removes the account's push subscriptions in the same transaction as its sessions. The new API regression checks that an unconfirmed request changes nothing and a confirmed request ends the session and removes the subscription.
- A host-side `production-check` command checks the local listener, the configured HTTPS browser address and certificate validity, free data-disk space, enabled complete backups, the last backup error, and fresh local/extra archives. Read-only SQLite access works while the server holds its data lock. It returns a nonzero status for monitoring. It does not measure external reachability from another network, voice or push.
- A disposable Playwright journey now covers first setup, message send/reload/search, sign-out/sign-in, a 390px layout and offline PWA startup. Axe scans on the setup and workspace screens pass WCAG 2 A/AA rules in light and dark themes after correcting faint text contrast. The journey passed locally with system Chrome and Playwright Chromium. It is wired into CI after the native OS matrix; CI itself has not run from this source snapshot.
- Clean `npm ci`, all 45 frontend tests, Svelte checks (0 errors/warnings), formatting, the production frontend build and `npm audit` (0 vulnerabilities) pass. The full Go suite passes with the race detector; `go vet ./...` passes. Govulncheck v1.8.0 reports 0 affected application symbols and 0 affected imported packages; one advisory remains in an unused package of a required module.
- All six 0.4.0-alpha.2 native targets built. Independent SHA-256 verification and ZIP integrity/content checks passed; packages include the production runbook. The packaged macOS x64 executable passed CLI setup, backup/restore, exclusive locking, health, embedded UI/manifest and graceful-stop smoke checks.
- Official Docker Compose 5.5.1 parses the base, TURN, backup-copy and combined override configurations. The new optional backup override mounts a separate host destination for scheduled copies. No Docker Engine is available here, so image construction, container lifecycle, volume permissions and UDP forwarding remain unverified. Native Windows execution also remains unverified. The previously documented Windows/Tailscale address timed out during a read-only HTTPS probe and needs current access details.
- The release helper creates a versioned Docker source ZIP with its own SHA-256 file, containing only build inputs and public guides. It excludes local data, keys, dependency caches and host-specific operational notes. Its checksum and archive integrity pass; a clean extraction passes Compose parsing, `npm ci`, the frontend build and a static Linux x64 cross-build. A live Docker image build and container run from this archive remain target-host gates.

The [production release runbook](production-readiness.md) lists the still-pending host, network, physical-device, soak, restore and alert-delivery gates. Keep this build an alpha until those gates pass on both intended deployments.
