# Toward a self-hosted community platform

BrrpBrrp aims to become an open-source alternative for communities that want to run their own chat and voice server. The current implementation is an alpha for small groups. This roadmap distinguishes working features from future work; it does not promise delivery dates.

## Principles

People should own their server and data, back it up and move it, and use the application without a vendor account. A small installation should remain one executable and one data directory, with Docker as an alternative. Accessibility, privacy, moderation and recovery belong in each feature's completion criteria.

The server operator remains trusted: the app does not provide end-to-end encryption. Private conversations will need explicit visibility controls throughout the application before they can ship.

## Current foundation

Text and voice channels; persistent chat, replies, pins, reactions, mentions, search and uploads; invitations and account moderation; admin authenticator protection; browser notifications; an installable web app; native services; and backups and restore.

The next development candidate adds focused reply threads with live counts, pagination, attachments and separate drafts, plus a keyboard channel picker. Threads use the existing reply relationship and work with old messages without migration. Replies stay visible and searchable in the channel. Named threads, subscriptions and archive controls remain future work.

## Delivery sequence

| Milestone | User outcome | Completion requirements |
| --- | --- | --- |
| Reliable small communities | Install, chat, join voice and recover the server | Actual Windows and Docker validation, two-network voice, mobile push, restore drill and configured alerts |
| Community permissions | Delegate moderation and restrict channels | Roles, permission evaluation, channel visibility, audit trail, and REST/WebSocket/search/upload/notification tests |
| Private conversations | Message individuals and small groups | Membership, blocking/reporting, unread state, private attachments, scoped realtime delivery and notifications |
| Organized discussions | Follow busy channels | Named threads, subscriptions, unread replies, archiving, safe rich formatting and notification preferences |
| Richer live rooms | Share screens and use video | Explicit capture controls, bandwidth budgets, permissions, reconnects, TURN and device/browser testing |
| Larger servers | Predict resource use and capacity | Repeatable load tests, configurable limits, queue measurements and evidence before raising caps |
| Public ecosystem | Contribute, maintain and integrate | Selected license, public repository, private vulnerability reporting, release policy, signing and documented APIs |

Build features in complete slices. Private channels and direct messages require authorization across every delivery path. Video and screen sharing need measured network and device behavior. Raising limits requires evidence that the installation supports them.

## Current boundaries

One workspace, delegable roles with a non-delegable operator tier held by the single owner, 100 accounts, 100 channels, ten concurrent voice participants and a 12 MiB upload ceiling. Restricted channels are implemented but ship switched off until the voice and streaming gaps named in [ADR 0003](adr/0003-restricted-channel-exposure.md) close. No direct messages, federation, video or screen sharing yet. See [README.md](../README.md) and [production-readiness.md](production-readiness.md) for supported behavior and outstanding operational evidence.

BrrpBrrp ships under the MIT License ([LICENSE](../LICENSE)) with generated third-party notices ([THIRD-PARTY-NOTICES.md](../THIRD-PARTY-NOTICES.md)) and contribution terms in the [CLA](../CLA.md); [ADR 0006](adr/0006-licensing-and-contribution-terms.md) records why. Repository publication, the private security intake and release signing still need owner-controlled destinations and credentials, and [SECURITY.md](../SECURITY.md) says its channel is not live yet.
