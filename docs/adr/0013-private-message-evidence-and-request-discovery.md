# ADR 0013 — Private message evidence, request discovery and the notification boundary

**Date:** 27 September 2026  
**Status:** decided for an opt-in alpha.7 slice; independent privacy acceptance remains open  
**Schema:** 13; backup format 2 (unchanged — a backup of a schema-13 database is still format 2)

This records the contract for a bounded private-reporting and request-discovery slice
before any schema, route or interface is written. The conservative defaults below are
implementation choices made under a continuing development authorization. They do not
open the production privacy gate recorded in ADR 0012, and this slice does not complete
WP-14 or WP-15.

## Evidence scope

A private report carries **exactly one currently readable accepted-direct message's
text**, its author and reporter identities, and the reporter's own explanation. It
carries nothing else. Specifically excluded from the evidence: adjacent messages,
reply content, conversation history, attachment bytes, attachment URLs, thumbnails,
external link previews, and any automatic export. An attachment-only message may be
reported — the disclosure shown to the reporter says so explicitly, because the
evidence is then the absence of text, and pretending otherwise would be a disclosure
the reporter did not consent to.

This is deliberately narrower than the community report queue. It is a single
voluntarily-submitted immutable snapshot, not a second message store and not a route
into the conversation. No reviewer gains membership, history, search, context, replies
or files from holding evidence.

## Storage model and why

Community reports live in `message_reports` (schema 9), which requires `channel_id`
and cascades on channel deletion. A private message has no channel projection, so it
cannot be represented there; relaxing the column and dropping the community-only
`SELECT` filter would make one table carry two different disclosure rules and would
risk community reviewers reading private rows.

Private evidence therefore lives in a **separate `private_message_reports` table**
that shares the well-factored mechanics (expiry, pagination, review notes,
resolve/reopen) without sharing its disclosure boundary. The shared parts are
mechanics, not authority: capability checks, feature-gate checks, admission fences
and the retention sweep are common code paths, while storage, filters and the reviewer
audience are separate. Migration 13 only adds a table; it does not alter
`message_reports`, so existing community report rows, IDs, expiry dates, readable-
channel pagination and channel-deletion semantics are untouched.

## Reviewer authority

A new capability `report.review_private` authorizes reading and reviewing submitted
private evidence. It is deliberately **not** granted to every community moderator and
is not reachable through `is_admin` shortcuts: a reviewer with only
`message.delete_any` cannot see a private queue, its counts, its identities or its
cursor side channels. Owners already hold every valid capability through
`Principal.Allowed`, so the owner needs no new grant; other reviewers require explicit
delegation through an ordinary custom role.

The capability is checked inside the transaction that reads or mutates evidence, so a
reviewer whose grant is revoked cannot read a later page or commit a later review.
Loss of the capability fences the delivery path the same way ADR 0009 fences
authority changes. No recent-authentication requirement is declared for this
capability: evidence is a submitted snapshot the reporter chose to disclose, and a
second factor prompt at review time would not strengthen what the reviewer already
holds. Route and UI behavior stay consistent with that decision — no route declares
`FreshAuth` for it.

A reviewer sees the submitted snapshot and nothing else. In particular, the private
queue is not filtered by the reviewer's community channel list, and no link in it
grants access to the original direct conversation; the queue is not a shortcut around
original-message authorization.

## Preview integrity

The community flow previews cached client text and snapshots current server text on
submit. That is not acceptable for a private disclosure, because the person must
consent to *the exact text that will be saved*.

Submission is therefore a two-step contract. The client fetches an **authorized
server-side preview** of the selected message; the preview carries the current server
text and a version derived from the message's current state. Confirmation submits that
version, and the server re-checks identity, read access and the version inside the
report transaction. If the message changed between preview and confirmation, the server
refuses and returns a fresh preview that must be confirmed again — it never silently
discloses replacement text, and it never accepts client-supplied text as server
evidence.

Fabricated client text is rejected: the saved snapshot is the server's current text,
truncated to the same 4,000-Unicode-character bound the community report uses, and
the client's copy is used only for display.

## Lifecycle

Evidence is available for **90 days from creation**, unchanged by resolution or
reopen. Expiry is a visibility rule, enforced in every read and review query; physical
cleanup is a separate bounded maintenance pass. The two are deliberately distinct:
an expired record is unreadable immediately, and the bytes are reclaimed on the
maintenance cadence rather than at the moment of expiry.

The 90-day clock is the evidence's own, and it is not the audit log's. Each review
also writes an audit entry recording that a review happened, and that entry is
retained for **400 days** — administrative accountability outliving the thing
reviewed. Reclaiming it on the evidence clock would destroy the only record that a
private review ever occurred, which is the opposite of what an audit entry is for.
Ordinary community audit entries keep the shorter 90-day default; the two are
reclaimed by separate statements precisely so neither rule is applied to the
other's rows. ADR 0014 states the full set of durations, including command
receipts, in one place.

- **Source deletion:** deleting the reported message does not erase submitted
  evidence. The evidence was a consented snapshot, and its value is precisely that it
  survives the source going away.
- **Account removal versus erasure:** these are different operations and behave
  differently here, following ADR 0011. *Removal* (the anonymize ceremony) keeps the
  account's stable identity under a tombstoned "Deleted member" presentation, so a
  record that names it keeps naming that identity. *Physical erasure* nulls the
  foreign keys, so the reporter and author columns become empty and the evidence
  text survives without attribution. Neither rewrites the evidence text because of
  an unrelated account's deletion.
- **Reviewer revocation:** as above, checked at transaction time.
- **Backup/restore:** a restored copy carries its snapshot's evidence, expiry *and*
  the roles and grants that existed at snapshot time — those are ordinary rows and
  they do come back. What the backup cannot contain is any revocation made after the
  snapshot, so a role that was withdrawn later reads as granted again on the
  restored copy. That is why the same explicit reconciliation as any other
  post-snapshot authority decision applies to reviewer grants, and why "restores the
  snapshot's authority" is the accurate description rather than "restores the
  current authority".
- **Private feature off:** when `BRRP_ENABLE_PRIVATE_CONVERSATIONS` is off, private
  report submission and review are refused and the private interface is not shown —
  the admin tab is hidden and a queue that was open is cleared, rather than left on
  screen offering actions the server refuses. Stored evidence is retained until its
  normal expiry. Disabling the feature is not erasure, and this ADR does not pretend
  otherwise: the operator still controls the database and the backups.

## Limits and concurrency

Private submissions reuse the community size limits (4,000 characters of text, 2,000
for the explanation). The **durable per-account rate is ten new private reports per
hour**, counted in the database rather than in memory, so a restart cannot reset it;
the community limiter remains in-memory and is not described as durable. Exceeding it
returns `429` with `Retry-After`.

A server storage bound is enforced by refusing new evidence when unexpired evidence
reaches the bound, with a defined `503` and the reporter's text kept, rather than
silently evicting unexpired records. The capacity check runs *before* the hourly
allowance is consumed, so a full store does not also spend the reporter's budget for
a report that was never taken.

The preview version is a fixed-size SHA-256 digest over the length-prefixed message
content and its edit state. It is the same length for every message and carries no
part of the message, which is what keeps a 4,000-character message in a non-Latin
script reportable: a version that grew with the content would have exceeded the
submission bound for exactly the messages most likely to need reporting. The digest
changes if and only if the text changes, so the confirmation check keeps its force.

Concurrent retried submissions are deduplicated without replacing the original
evidence or explanation: the first submission wins and a retry returns that same
receipt, mirroring the community report's receipt-only response. The submit response
carries only the reporter's own receipt — never a review note, another reporter's
identity, or their reason.

## Audit and nonpunitive review

Private review mutations write restricted audit metadata atomically with the review
change, with an explicit failure policy: an audit write that fails aborts the
transaction, exactly as the community review path does. No private text, reason, or
membership ever enters the audit record; it names the reviewer, the action and the
evidence id only.

Those records are also excluded from the *general* community audit feed, not merely
uninteresting there. Naming the reviewer, the report and the time discloses that a
specific person reported a specific private message, to a reader who cannot see
the message — so the community feed filters them out before its limit, where they
could otherwise occupy a page slot or shift a cursor, and a policy-scoped endpoint
serves them to an account holding `report.review_private`. The review remains
accountable; it is simply not disclosed to an audience the evidence policy does not
cover.

Resolving a report records triage. It does not delete a message, ban an account, or
grant any moderation power over a private conversation. Reopen returns the record to
the queue without extending its expiry.

## Request discovery and the notification boundary

An **outstanding incoming request count** is derived from durable current requests
addressed to the signed-in account: pending, eligible, incoming only. Outgoing,
rejected, withdrawn, blocked and unavailable identities are excluded. Opening the
messages dialog does not clear it. It updates on accept, decline, withdraw and block
from any device, and it is reconstructed on initial load and on reconnect.

The count is an **invalidation hint, not authority**: `direct.changed` tells the
client that something moved, and the client then performs an admitted, account-scoped
snapshot fetch. A stale response for a previous account or authority is refused. The
count is surfaced in the account snapshot so a closed workspace does not have to fetch
every request introduction just to draw a badge. P8's independent introduction-draft
ownership is preserved across these refreshes.

**This tranche's notification contract is the request badge plus the existing
unread-message state. There is no new direct-message OS notification, push, sound or
ringing.** Direct conversations are deliberately not routed into the community mention
push path, whose authorization assumptions are channel-specific. Help and settings
copy states this distinction plainly. A dedicated DM activity/notification system with
safe previews, preference precedence, deduplication and active/background/closed-app
behavior is a separate implementation contract.

## Explicit exclusions

This slice does **not** add: file/attachment evidence, report assignment or timeouts,
full activity history, groups, private calls, screen sharing, named threads, or
pending-request introduction reporting.

Pending-introduction reporting is excluded for a specific reason, not for lack of
time. Rejecting or blocking a request erases its introduction, so there is no
server-held evidence of it; a later request-harassment feature needs recipient-only,
server-verified evidence with an explicit snapshot-before-block workflow. This
implementation must never require accepting a harasser in order to report them, never
covertly retain a rejected introduction, and never construct supposed server evidence
from stale client text. That limitation is recorded here and must be resolved before
any broad private-message pilot.

## Consequences and open items

The private evidence queue is a real disclosure surface and is bounded accordingly:
one snapshot, one message, 90 days, an explicit capability, and a visible disclosure
to the reporter of exactly what is saved, who can review it, and what is excluded.
Independent security and privacy review, both-host (Windows/Tailscale and Docker)
qualification, hosted CI, and the broader WP-14/15 scope remain open and are not
claimed by this slice. No G0–G8 release gate is passed.
