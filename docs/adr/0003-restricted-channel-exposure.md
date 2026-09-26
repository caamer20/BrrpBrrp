# ADR 0003 — Restricted-channel enforcement ships disabled

**Status:** Implemented; the gate stays closed until the listed gaps are closed
**Date:** 24 September 2026
**Plan reference:** WP-07, WP-08, INV-01, INV-04, RK-01, §7.3
**Deciders needed:** TL + SEC (approve opening the gate), OWNER (accept the
declared capability limits)

## Context

The plan is explicit that an unfinished private surface must not be reachable:

> Ship incomplete private-data features disabled at the API boundary as well as
> the UI until the privacy gate passes.

RK-01 is the corresponding risk — private-data enforcement that is complete on
some delivery paths and missing on others. Implemented enforcement therefore has
to arrive behind a switch that an operator opens deliberately, and the switch must
be accompanied by a written list of what is and is not enforced.

`BRRP_ENABLE_RESTRICTED_CHANNELS` controls whether private channels and
announcement posting policy can be created or changed. It defaults to off. The
enforcement code, the schema and the tests are live regardless, so the policy is
exercised on every run.

## What is enforced when the gate is open

Covered by `internal/api/privacy_test.go`, which is the T-AUTH-01 scenario:

- Channel listing, workspace state, unread counts and cursors.
- Message history, single message, context, replies, pins.
- Search across all channels, and search scoped to one channel.
- Original files and thumbnails (access resolved through the file's current
  conversation; an unclaimed upload stays uploader-only).
- Read cursors, reactions, message sends and edits.
- Mentions: a target that cannot read the channel is recorded as no mention, so it
  produces no count, no notice and no push job.
- Realtime delivery: message, reaction, typing and mention events go to a resolved
  audience rather than a global broadcast.
- Push dispatch re-checks access when a queued job is sent.
- Revocation advances the authorization revision inside the same commit, answers
  the next request with a non-disclosing 404, and tells affected clients to
  refetch, which the web client does by discarding its cached conversations
  before it asks what it may still read.
- Voice presence: the `voice_states` snapshot is filtered by the same policy rule
  as channel content, `voice.state_updated` is delivered one channel at a time to
  that channel's audience, and the SFU only ever routes audio, join notices and
  room routing between peers in the same channel. Joining or moving into a
  channel the caller may not read is refused exactly like a missing channel, so a
  guessed identifier confirms nothing.
- Revocation reaches an active voice stream: withdrawing the grant, the role or
  the administrator status that admitted an account ends its media connection and
  revokes its TURN credentials in the same request, tells the account why, and
  records `voice.access_revoked` in the audit log.

Foreign keys, the FTS index and the migration path are covered elsewhere.

## What is **not** enforced yet, and why the gate stays closed

1. **Voice still has no admission contract beyond reading the channel.** A
   private *call* (invite, ringing, per-call participants) and the track/signaling
   redesign are WP-19/WP-20. A restricted voice channel is now as safe as a
   restricted text channel for presence, join and revocation, but it has no ring
   or admission step of its own, and no browser/network qualification evidence.
2. **A metadata response already being written finishes.** Access is admitted when
   the request arrives, so a JSON body that started a few milliseconds earlier is
   not retracted; the plan's guarantee is "no newly admitted server send after the
   revocation boundary". Everything that streams for longer than that is now
   revoked in place: a file or preview download ends mid-copy
   (`TestRevokedAccessEndsAnInFlightDownload`, which was also checked against a
   build with the revocation removed), a queued notification is dropped at dispatch
   (`TestQueuedNotificationCannotOutliveAccess`), and both an active and a detached,
   resumable voice connection are ended (`TestRevocationReachesAnActiveVoiceStream`,
   `TestADetachedVoicePeerCannotResumeAfterRevocation`).
3. **File caching.** A client that already downloaded a file keeps it. That is
   unavoidable and stated by INV-04. The web client now handles the rest:
   `authorization.changed` drops every cached conversation and refetches the
   snapshot, a channel the snapshot no longer returns is swept from the cache even
   when the account was reading it, and the attachment bytes already on that
   device are the part that cannot be recalled.
4. **No independent review.** WP-28 has not run. The plan requires it before
   private features are exposed to a pilot, not merely before release.
5. **Managers can read every community channel, including restricted ones** —
   voice included. This is disclosed in the access inspector and in
   `docs/admin.md` rather than being implied, and it is the owner's decision to
   accept or replace it.

## Consequences

- An operator can create restricted text and voice channels today and get the
  enforcement above. Restricted **voice** channels still lack the private-call
  admission contract, browser/network qualification and review that WP-19, WP-20
  and WP-28 owe, so the documentation has to say so plainly.
- Opening the gate is a documented operator decision, not an implicit consequence
  of upgrading, because an upgrade must not silently create a privacy promise the
  server does not keep.
- The gate is not a substitute for the remaining work; it exists so the delivered
  half cannot be mistaken for the whole.
