# Changelog

Dated changes to BrrpBrrp, newest first. Each entry is a summary of a record under
[docs/releases](docs/releases/0.4.0-alpha.9.md): read that record for what was measured,
what was not, and what the checkpoint does not prove. Dates are the ones the records state;
where a record states none, none is given here either.

Every version below is a **local development checkpoint**, not a published release, and
none of them marks a G0–G8 release gate passed. Read the record's "Known issues" section
before deciding anything on the strength of one.

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

## Not yet released

1.0.0 has no entry, because it has not been built, packaged or qualified. The draft notes
are in [docs/releases/1.0.0-notes-draft.md](docs/releases/1.0.0-notes-draft.md) and nothing
in them is a claim about a released artifact.
