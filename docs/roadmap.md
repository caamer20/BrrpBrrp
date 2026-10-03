# Toward a self-hosted community platform

BrrpBrrp aims to become an open-source alternative for communities that want to run their own chat and voice server. The current implementation is an alpha for small groups. This roadmap distinguishes working features from future work; it does not promise delivery dates. The state column below is dated **2 October 2026**, application **0.4.0-alpha.8** at schema **25**; the record for that build is [docs/releases/0.4.0-alpha.8.md](releases/0.4.0-alpha.8.md), and the evidence behind it is the execution ledger under `planning/` in the repository (the packaged source does not carry the development record, so it is named rather than linked).

## Principles

People should own their server and data, back it up and move it, and use the application without a vendor account. A small installation should remain one executable and one data directory, with Docker as an alternative. Accessibility, privacy, moderation and recovery belong in each feature's completion criteria.

The server operator remains trusted: the app does not provide end-to-end encryption. Consented private conversations ship behind a default-off gate until their independent review is done.

## Current foundation

Text and voice channels; persistent chat, replies, pins, reactions, mentions, search and uploads; capability-based roles, delegation and restricted channels; consented direct conversations, groups, blocking and private evidence reporting; named threads with their own reading positions and archive state; activity and notification preferences; moderation timeouts and bounded invitations; member data export; community voice with private calls and screen sharing that carries its own screen audio; embedded TURN over UDP, TCP and TLS; verified backups with manifests; operator alerts and capacity tooling; an installable web app, native services and an install wizard.

Two feature gates ship **off**: `BRRP_ENABLE_PRIVATE_CONVERSATIONS` and `BRRP_ENABLE_RESTRICTED_CHANNELS`. Everything they gate is implemented and locally tested; what is missing is the independent review and the deployment qualification named in the delivery table, not the feature.

Already established on this machine, and what it does not establish: the whole-tree `go test ./...` and a `-race` sweep are green; the frontend unit suite, `svelte-check`, formatting, build and dependency audit are green; browser journeys pass in both feature modes for the community surfaces, and the screen path is exercised end to end in two browsers. None of that is qualification: an outside review, physical devices, two real hosts, a UDP-blocked network and a beta are still ahead. See [production-readiness.md](production-readiness.md).

## Delivery sequence

| Milestone | User outcome | Completion requirements | State (2 October 2026) |
| --- | --- | --- | --- |
| Reliable small communities | Install, chat, join voice and recover the server | Actual Windows and Docker validation, two-network voice, mobile push, restore drill and configured alerts | Install, recovery rehearsals, packaged archives and alert delivery to a real receiver are locally verified; the Windows/Docker hosts and a two-network call are not |
| Community permissions | Delegate moderation and restrict channels | Roles, permission evaluation, channel visibility, audit trail, and REST/WebSocket/search/upload/notification tests | Implemented and tested across delivery paths; ships behind `BRRP_ENABLE_RESTRICTED_CHANNELS` until the independent privacy review runs |
| Private conversations | Message individuals and small groups | Membership, blocking/reporting, unread state, private attachments, scoped realtime delivery and notifications | Implemented and locally tested — consented requests, groups, blocking, private evidence, per-member interruption preferences; ships behind `BRRP_ENABLE_PRIVATE_CONVERSATIONS`, never qualified on either required deployment |
| Organized discussions | Follow busy channels | Named threads, subscriptions, unread replies, archiving, safe rich formatting and notification preferences | Named and archived threads with independent read state, notification preferences and follow state are in; emphasis and block quotes in message formatting are the next slice, lists are deferred |
| Richer live rooms | Share screens and use video | Explicit capture controls, bandwidth budgets, permissions, reconnects, TURN and device/browser testing | Screen publication with separate screen audio, capture fenced to its audience, embedded TURN and per-call path reporting are in; bandwidth presets and caps, device selection and Firefox/WebKit automation are the next slice, camera video is not scheduled |
| Larger servers | Predict resource use and capacity | Repeatable load tests, configurable limits, queue measurements and evidence before raising caps | The fixture and load tooling exist and speak the deployment's own transport; no run has been made against hardware sized for the reference profile |
| Public ecosystem | Contribute, maintain and integrate | Selected license, public repository, private vulnerability reporting, release policy, signing and documented APIs | The MIT licence, CLA, notices, SBOM and release tooling are in place; there is no remote, no live private security intake and no signing identity yet |

Build features in complete slices. Private channels and direct messages require authorization across every delivery path. Media work needs measured network and device behavior. Raising limits requires evidence that the installation supports them.

## Current boundaries

One workspace, delegable roles with a non-delegable operator tier held by the single owner, 100 accounts, 100 channels, ten concurrent voice participants and a 12 MiB upload ceiling. Private conversations and restricted channels are implemented but ship switched off until the gates above open. No federation, camera video, multi-server guilds or public read receipts. Screen sharing carries the display and its system audio; there is no camera publishing. **Media is IPv4 and the embedded TURN relay is the only relay** (decided 2 October 2026): an external TURN server is not configured or supported in v1, IPv6 media is not carried, and a call across a network that blocks UDP remains unmeasured — host qualification records which such networks the embedded relay does traverse. See [README.md](../README.md) and [production-readiness.md](production-readiness.md) for supported behavior and outstanding operational evidence.

BrrpBrrp ships under the MIT License ([LICENSE](../LICENSE)) with generated third-party notices ([THIRD-PARTY-NOTICES.md](../THIRD-PARTY-NOTICES.md)) and contribution terms in the [CLA](../CLA.md); [ADR 0006](adr/0006-licensing-and-contribution-terms.md) records why. Repository publication, the private security intake and release signing still need owner-controlled destinations and credentials, and [SECURITY.md](../SECURITY.md) says its channel is not live yet.
