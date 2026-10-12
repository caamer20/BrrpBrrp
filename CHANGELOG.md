# Changelog

Dated changes to BrrpBrrp, newest first. Each entry is a summary of a record under
[docs/releases](docs/releases/1.0.0.md): read that record for what was measured,
what was not, and what the checkpoint does not prove. Dates are the ones the records state;
where a record states none, none is given here either.

The dated entries preserve development checkpoints. Publication status and exact-build
qualification belong in [GitHub Releases](https://github.com/caamer20/BrrpBrrp/releases).
No checkpoint alone marks a production gate passed; read its known issues and verification scope.

## 1.0.0 — 12 October 2026

- Align the executable, frontend/lockfile and archive builder on version 1.0.0.
- Prepare stable draft metadata for versions without a prerelease suffix; publication remains
  a deliberate step after exact-source qualification, checksums and provenance verification.
- Publish the stable channel at the owner's request, with the remaining signing, host/device,
  independent-review, capacity and beta work disclosed. No application behavior or schema changes
  are introduced relative to the qualified RC.
- Keep `main` and the badge-only `badges` snapshot branch; the live download count needs no job.
- Exact published source, hosted results and asset identities are in [GitHub Releases](https://github.com/caamer20/BrrpBrrp/releases/tag/v1.0.0).

## Release toolchain hardening — 12 October 2026

- Go 1.27.2 is the minimum build compiler and the pinned CI/Docker toolchain, fixing the
  standard-library vulnerabilities that blocked the first complete hosted qualification.
- Update `golang.org/x/net` and regenerate dependency notices and the SBOM.
- Run strict npm/Go vulnerability audits in the shared audit gate before native tests;
  release Docker qualification also waits for that gate through the shared test job.
- Isolate Chromium launch flags and optional channels from WebKit/Firefox qualification,
  and pass Firefox's audio preference through its supported launch options. A regression
  checks the projects after Playwright resolves inherited configuration.
- Use the standard release-asset download badge and remove its scheduled workflow,
  allowing `main` to remain the only permanent branch without daily source changes.
- Give hosted Linux Firefox a silent virtual audio backend so real audio contexts
  advance and the synthetic voice/screen-share transport journeys can execute.
- Fresh complete-source review and full qualification remain required before publication.

## Release preparation — 11 October 2026

- Public repository, private security intake and owner-supplied conduct/privacy contact.
- Efficient contribution CI, CodeQL, historical secret scanning and fixed standard runner labels.
- Shipped-default restricted-channel browser coverage and WebKit core qualification.
- Fixed the access inspector clearing its own successful-save confirmation.
- Protected-main-compatible download badge branch, current SHA-pinned Actions, artifact retention
  and signed build-provenance verification before draft release creation.
- Stable production qualification remains open; see deployment readiness.
- Activity filters use SQL parameters; explicit port/ZIP path guards and test/audit cleanup
  address full-main CodeQL findings. Any individually reviewed false positives are bound to
  exact result/flow/query-pack identities and the analyzed source tree.

## 1.0.0-rc.1 — 6 October 2026

The first release candidate for 1.0.0: the intended feature set, not yet a release.

- Restricted channels and private conversations are **on by default**. An installation that
  set either switch explicitly keeps its value.
- A new installation starts with scheduled backups on, every 12 hours.
- A member can change their own display name and colour.
- The sign-in throttle counts failures only, so people arriving together from one address
  are not refused; a flood of failed sign-ins can no longer close the server to its members.
- A first-run checklist for the owner and a "Server details" section in Manage server →
  Maintenance.
- `Strict-Transport-Security` on real HTTPS deployments.
- Docker: eight settings that never reached the container now do, and the relay's TCP and
  TLS fallback ports are published.
- Fixed: a mention inside a right-to-left message was drawn `name@`; offline members were
  drawn below the contrast requirement; a link inside a message was told apart by colour
  alone.
- The documents a public release needs: code of conduct, support policy, compatibility,
  known limitations and an upgrade guide.

## 0.4.0-alpha.9 — 4 October 2026

Viewer controls, operator metrics, encrypted backup custody and the matrix gates.

- `GET /api/admin/metrics` is a dependency-free operational surface behind operator
  authority: per-route counts, parameterised latency histograms, status classes, SFU
  forwarding and drop counters, scheduler and queue depths, and process RSS/CPU from
  `getrusage` rather than Go's heap alone.
- The encrypted off-host backup procedure is written and rehearsed, naming its tools in
  order (age, then gpg, then openssl) and never putting the key on the server.
- The negative matrix requires every route to state how an authenticated outsider is
  denied, and the acceptance matrix maps every scenario to named, existence-checked tests.
  Both run in `make check`.
- The shared-screen path is complete end to end, including viewer controls, output-device
  selection, a measured reconnect budget and the VP8 fallback.
- A websocket protocol handshake: the `hello` frame refuses an incompatibly old client
  without losing its drafts.
- Reading and writing at phone width; an Arabic journey that found and fixed a direction
  rule written as an invalid CSS value.

## 0.4.0-alpha.8

Durable retries, revocable exports and the screen path.

- A reply whose outcome is uncertain keeps the operation identity it was submitted under,
  including across close, reopen and opted-in reload recovery, and never crosses accounts,
  threads or channels.
- Export delivery re-answers against current authority on every request, including each
  range request, so a withdrawal cannot fit between a stale check and admission.
- A thread has its own reading position, unread count and bounded window, so reading a
  thread stops marking the channel and the reverse.

## 0.4.0-alpha.7

Threads, private calls, screen sharing, member export and operator alerts.

- Named threads, which inherit the channel's authorization wholesale; a thread cannot
  become a way around a channel restriction.
- Private calls in a direct conversation or a group, using the same membership rule a
  private message uses.
- Screen sharing, where a publication is an object with an owner, a kind and a lifecycle
  rather than a position in a fixed list of tracks.
- A member's own data export, built in one transaction and bounded while it is built.
- Moderation: reports, assignment as a claim rather than a grant, timeouts that restore
  exactly what an account held, and bounded invitations.
- Operator alerts delivered to an HTTP endpoint the operator controls.

## 0.4.0-alpha.6 — 26 September 2026

Consented direct messaging.

- An opt-in Messages inbox with message requests, acceptance and rejection, withdrawal,
  contact preferences and blocking, on the shared conversation, attachment, receipt, search
  and notification pipeline.
- Private-edit existence disclosure, upload capability changes during transfer, private
  pin/deletion metadata in the community audit feed, and private link-preview fetches were
  fixed in the same tranche.
- Direct calls, groups, private evidence reporting and dedicated DM push controls were not
  yet included at this checkpoint.

## 0.4.0-alpha.5 — 25 September 2026

Community controls and dependable recovery.

- Recovered messages for a send that was permanently refused or expired; message reports
  with selected text; durable channel slow mode; voice acquisition with a timeout and a
  retry; per-channel notification muting; and account removal that keeps conversation
  history under **Deleted member**.
- A group of send-identity, session, push, read-cursor and account-capacity repairs, each
  with the regression that fails without it.

## Version numbers at each checkpoint

| Version | Database | Backup format | Websocket protocol |
| --- | --- | --- | --- |
| 1.0.0 | 25 | 2 | 1 |
| 1.0.0-rc.1 | 25 | 2 | 1 |
| 0.4.0-alpha.9 | 25 | 2 | 1 |
| 0.4.0-alpha.8 | 25 | 2 | — |
| 0.4.0-alpha.7 | 21 | 2 | — |
| 0.4.0-alpha.6 | 12 | 2 | — |
| 0.4.0-alpha.5 | 10 | 2 | — |

The websocket protocol number was introduced with alpha.9's handshake, so the earlier
checkpoints do not carry one. [docs/compatibility.md](docs/compatibility.md) explains what
each number means for an upgrade and a rollback. Read the numbers from the code if they
disagree with this table: `internal/db/migrations.go`, `internal/backup/manifest.go` and
`internal/ws/protocol.go` are the sources of truth, and `make check` compares the ones the
documentation states.

## Publication record



The stable release record is [docs/releases/1.0.0.md](docs/releases/1.0.0.md).
Historical planning drafts remain archived and do not supply qualification evidence.
