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

- **Source deletion:** deleting the reported message does not erase submitted
  evidence. The evidence was a consented snapshot, and its value is precisely that it
  survives the source going away.
- **Account removal/erasure:** reporter and author identities are cleared the way
  community reports clear them, so a removed account leaves no identity attached. The
  evidence text itself is not rewritten by an unrelated account's deletion.
- **Reviewer revocation:** as above, checked at transaction time.
- **Backup/restore:** a restored copy carries its snapshot's evidence and expiry. A
  restore does not resurrect the snapshot's reviewer grants or role changes; those need
  the same explicit reconciliation as any other post-snapshot authority decision.
- **Private feature off:** when `BRRP_ENABLE_PRIVATE_CONVERSATIONS` is off, private
  report submission and review are refused and the private UI is not shown. Stored
  evidence is retained until its normal expiry. Disabling the feature is not erasure,
  and this ADR does not pretend otherwise: the operator still controls the database
  and the backups.

## Limits and concurrency

Private submissions reuse the community size limits (4,000 characters of text, 2,000
for the explanation). The **durable per-account rate is ten new private reports per
hour**, counted in the database rather than in memory, so a restart cannot reset it;
the community limiter remains in-memory and is not described as durable. Exceeding it
returns `429` with `Retry-After`. A server storage bound is enforced by refusing new
evidence when unexpired evidence exceeds the bound, with a clear error, rather than
silently evicting unexpired records.

Concurrent retried submissions are deduplicated without replacing the original
evidence or explanation: the first submission wins and a retry returns that same
receipt, mirroring the community report's receipt-only response. The submit response
carries only the reporter's own receipt — never a review note, another reporter's
identity, or their reason.

## Audit and nonpunitive review

Private review mutations write restricted audit metadata atomically with the review
change, with an explicit failure policy: an audit write that fails aborts the
transaction, exactly as the community review path does. No private text, reason, or
membership ever enters the general community audit feed; the audit entry names the
reviewer, the action and the evidence id only, and is readable only through the
appropriate evidence policy.

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
