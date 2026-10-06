# Code layout and operating model

BrrpBrrp is one Go process. `cmd/brrpbrrp` handles commands and platform service control. `internal/app` owns host configuration, filesystem containment, logging, TLS and graceful shutdown. The frontend is a Svelte app in `web/src`; `web/assets.go` embeds its compiled files. There is no application server in Node.js.

| Package | Responsibility |
| --- | --- |
| `internal/api` | Authentication, HTTP routes, WebSocket commands, moderation, settings and background jobs |
| `internal/db` | SQLite schema, migrations, message hydration and offline account creation |
| `internal/ws` | Bounded socket fan-out and presence |
| `internal/rtc` | Opus SFU rooms and optional TURN relay |
| `internal/scraper` | Constrained HTTPS fetching and metadata extraction |
| `internal/backup` | Consistent database snapshots, ZIP streaming and offline restore |
| `internal/installer` | Local browser setup, configuration preflight, native service registration and administrator creation |
| `web/src/lib/components` | Chat screens, forms and administration views |
| `web/src/lib/workspace.svelte.ts` | Session/workspace state, messages and reconnects |
| `web/src/lib/replies.svelte.ts` | Bounded reply pages, realtime invalidation and stale-request protection |
| `web/src/lib/voice.svelte.ts` | Microphone lifecycle, peer connection, playback and speaking state |

## Data and messages

SQLite runs in WAL mode with foreign keys, a busy timeout and one connection. This serializes writes at the intended small-group scale. IDs are UUIDv7; server timestamps use fixed-width UTC strings. Messages, attachments, reactions and mentions are related by foreign keys. FTS5 triggers keep search synchronized with message edits and deletion.

A message's idempotency key is unique per author. Retrying the same send returns the existing message. Uploads are staged as unclaimed attachments and claimed transactionally during message creation. An uploaded file is written to a temporary file in its destination directory, flushed, renamed onto its final name and followed by a directory synchronization before any row refers to it, so an interrupted upload leaves nothing and a committed reference always has its bytes; the Windows and Linux semantics and the boundaries this does not cover are in [ADR 0007](adr/0007-attachment-durability.md). History is fetched in pages of 50, with a sliding 250-message in-memory display window. Up to five inactive channel windows are kept alongside the active channel, each bounded to 250 messages; all are cleared when the session ends. Reading positions use a message anchor and pixel offset. Revisiting a cached history window revalidates its bounded ID range; a separate first-unread lookup can find messages outside the latest page. Private read markers only drive that user's unread counts; other people cannot request public read receipts.

WebSocket messages use `{t,d}` envelopes. Connections are authenticated by the same cookie as HTTP. Each connection has a bounded outgoing queue; a slow consumer is disconnected and recovers through state/history reload. Presence reflects all of a user's sockets. Mutations recheck the session and permissions. Read-state updates are sent only to the owning user.

Message and reaction events are coalesced on a 60 ms throttle. The browser fetches affected messages with at most four concurrent lookups, updates visited channels in memory, and fetches unread counts separately. Events in an unvisited channel need only unread counts. Workspace metadata changes and reconnects still fetch the complete state. Request revisions and a short-lived event journal prevent delayed snapshots from restoring deleted or outdated messages.

## Voice

The SFU handles audio only. Each client has one microphone sender and nine receiving slots, with fixed transceivers to avoid renegotiation churn as people join. Rooms are isolated. The server forwards Opus RTP and rewrites sequence/timestamp continuity when a slot is reused. It drops media for mute/deafen restrictions before sending it to peers. RTCP is drained and peer resources close on leave, socket loss, moderation or shutdown. Unnegotiated joins expire after 30 seconds.

A successful media connection emits `voice.user_joined` once to the connected participants in that room. Browsers synthesize a quiet, original two-tone chime locally: the joining person hears it on their first connection, and existing participants hear the room event. Deafen silences it, simultaneous arrivals share one chime, and suspended audio never queues stale sounds. Member-list refreshes and recovery of an existing peer connection do not replay it. Run `node scripts/preview-chime.mjs` with Node 24 to export the same sound as `dist/voice-join.wav`.

The server carries 20 voice participants across all of its rooms and 10 in one room, with one screen publisher per room and two serverwide. Those are implementation boundaries rather than measured capacities. No camera video, recording or voice message history is provided; screen sharing is, with the share's own audio. Browser echo cancellation/noise suppression are requested when available. Speaking indicators use a local analyser and bounded signaling. Device permissions and audio playback policy remain browser/OS responsibilities.

## Resource bounds and jobs

Two preview workers, one push worker and one hourly maintenance worker share cancellation with server shutdown. Queues are bounded. Previews have a 3-second timeout, a 2 MB body cap, restricted redirects and a bounded cache. Push has a bounded queue and request deadline. Uploads stream to disk with a 12 MB installation maximum. Login hashing concurrency is capped at two. Socket fan-out, account count, channel count and voice seats are limited.

Authenticated attachment previews are separate from external link previews. JPEG, PNG, GIF and WebP inputs become JPEG previews with a maximum 640-pixel edge; animated images use the first frame. Decode input is limited to 16 million pixels and one concurrent decoder, with a two-second queue wait. The disposable `data/cache/previews` directory is capped at 128 files or 64 MiB and is excluded from backups. Originals stay available on request. The browser reserves preview space before loading and defers images when data saver is enabled.

Admin backups use `VACUUM INTO` for a consistent SQLite snapshot, then stream a ZIP whose manifest records the application and schema version and a size and SHA-256 digest for the database and every attachment. The manifest is written last because the inventory is complete only then. Restore validates structure, manifest agreement and size budget before writing anything, hashes each entry as it stages it, and installs by renaming into a data directory the process lock holds; an archive that disagrees with its manifest is refused and an incomplete restore never replaces an existing installation. The same check runs standalone from `verify-backup`, and `production-check` verifies the newest archive and copy so monitoring depends on integrity rather than on a file's timestamp. File cleanup is serialized against backup creation. See [ADR 0008](adr/0008-backup-manifest-and-verification.md) and [ADR 0007](adr/0007-attachment-durability.md).

The build emits gzip and Brotli asset variants, representation-specific ETags, a build ID and a complete startup asset list. Installer, administration and preferences modules load on demand. The native server and installer serve precompressed public assets when the browser supports them.

The service worker installs the complete static startup graph atomically, keeps one previous shell for existing tabs, and bounds deferred-asset caching to 32 entries. Messages, authenticated API responses and uploaded files are not cached for offline access. Offline opening shows a reconnection screen. Updates activate only after the user chooses to refresh, with active voice, pending sends and uploads blocking that action. An explicit update temporarily places text/attachment drafts and a reply reference in tab-scoped session storage for restoration once, for the same account, within 24 hours. Ordinary attachment drafts stay in memory. Text drafts persist between visits only when the user enables that preference; signing out clears them. Theme/resource/privacy preferences are also stored on the device.

## Focused discussions

Focused discussions use `messages.reply_to_id` and its existing index. `GET /api/messages/{id}/replies` requires a session and returns the parent plus a chronological page of up to 50 direct replies, with a `has_older` marker. Replies to replies form separate discussions. The client holds at most five pages, revalidates after relevant realtime events or reconnection, and discards results after deletion or view disposal. Message summaries carry reply counts; create/delete events include the parent ID so visible counts can refresh. Replies remain ordinary channel messages for search, retention, notifications and backups.

Channel and thread composers share the upload and retry implementation. Their in-memory drafts have separate keys; opt-in draft storage and explicit update handoff understand both. Thread sends preserve the channel's history position. The channel picker searches locally within the authenticated workspace state and does not fetch messages or join voice.

## Working on the project

Use short, descriptive names, explicit error handling and small functions. Keep comments for invariants, protocol details and decisions a future maintainer could otherwise miss. Avoid wrappers that only rename a standard library call. Frontend formatting is mechanical. Keep new dependencies justified by a concrete capability.

Run frontend checks/build before Go because the binary embeds the compiled output. Integration tests cover authenticated behavior and actual local Pion connections, not just mocked message shapes. Use the native platform CI matrix before tagging a release. Deployment gates are listed in [acceptance.md](acceptance.md).

The installer uses a separate, temporary loopback listener with a per-session capability and same-origin checks. It embeds the setup interface, writes configuration and a resume record before registering a service, then checks the running server and creates the administrator through its existing setup API. Resuming repeats registration only when the service definition matches this installation. The installer never registers the chat service worker or serves private host files.

## Container packaging

Docker builds the Svelte assets and a static Linux binary in separate build stages, then copies the executable, CA roots and dependency notices into a minimal runtime image. It runs as UID/GID 10001 with `/data` as its persistent volume. Compose makes the root filesystem read-only, provides temporary storage, drops capabilities and rotates standard-output logs. `healthcheck` probes the configured local listener and validates its TLS identity and setup-status response without opening SQLite or acquiring the data lock. Container lifecycle and storage tests are defined in `scripts/check-docker.mjs`; the Docker workflow builds x64 and ARM64 and runs the x64 smoke test on a Docker-enabled runner.
