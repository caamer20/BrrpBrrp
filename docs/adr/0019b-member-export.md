# ADR 0019b — Per-member export

**Status:** decided; implemented by migration 21
**Extends** ADR 0007 (account lifecycle) and 0009 (authority ordering).

## What an export is

A member's own data, in a form they can keep. It is not a backup, and the
distinction is the whole design: a backup is the operator's, covers every account, and
may be read while the server runs. An export is **one account's**, is produced while
that account's authority is current, and contains only what that account is already
permitted to read.

**A member export contains only permitted personal data.** Concretely: the messages
they sent or received in conversations they can currently read, the reactions and
attachments on those messages, their own account record, and their own settings. It
does **not** contain another conversation they are not a member of, anybody else's
credentials or sessions, reviewer-only evidence, operator secrets, or the server's
configuration. The review requirement is in WP-12 and this ADR restates it as a
contract: *a member export never includes another private conversation or
credentials.*

## Why it is a snapshot and not a query

An export that queries live data while producing it is not reproducible and not
safe: a conversation can be removed, a block can be applied, and a membership can be
withdrawn between the first row and the last, producing a file whose contents depend
on timing. So the export is **assembled in one transaction** — the same transaction
that decides the account may export at all — and every row is read from one
consistent state. What the account could read at the moment it asked is what the file
contains, and it is that or nothing.

This has a consequence worth stating: an export does **not** grow. If a message was
deleted before the export started, it is not in it, even if it is still recoverable
from a backup. That is the correct behaviour, and it is the reason the export is not
a substitute for a backup.

## Bounded, and bounded on the write

An export has a hard ceiling on rows, bytes and attachments, applied **while it is
built** rather than checked afterwards. An unbounded export is a way for one account
to fill the server's disk, and a member-triggered job that nobody bounded is exactly
that. When the ceiling is reached the export **stops and says so**: it is not
truncated silently, because a truncated file that looks complete is worse than a
refusal, and a member cannot tell the difference from the outside.

Every export records what it could not include, and that record travels inside the
file. "This export stopped at 50,000 messages" is a fact the member is entitled to;
silently missing the rest is not.

## Fresh authentication, because a file leaves the building

An export leaves the server. It is therefore gated on **recent strong
authentication** — the same gate password changes and second-factor removal use — and
the decision is made in the transaction that reads the data, not by the route alone.
An export initiated while somebody else is at an unattended machine is not the
account holder's export.

## Expiry, and what a lapsed export leaves behind

A produced export has a **lifetime** and is removed on its own. A member's copy of
their own history is on their machine; the server's copy of that file is a liability
that grows without bound otherwise. The lifetime is stated in the file itself, so
somebody who finds the export months later knows what they are looking at and when it
was made.

## Erasure wins

If the account is erased while an export is queued, the queued export **does not
run**. It cannot, because the transaction that would read the data no longer has an
account to read it for. This is stated because the alternative — an export finishing
after the erasure and re-materialising the data — is precisely the failure the
erasure path exists to prevent.

## What an export is not

- **Not a backup.** A backup is the operator's and is a separate, privileged path.
- **Not a legal-compliance artefact.** It is a product contract about what a member
  can retrieve, and nothing here is a claim about any jurisdiction's requirements.
- **Not a way around erasure or retention.** Retention runs first; an export reflects
  the state retention has already decided.

## The server holds a copy, so the server is responsible for it

An export leaves the building as a file. The server keeps one for seven days, and
during that week it is the one delivering a copy of somebody's history — which makes
the delivery path a privacy surface, not a convenience.

It was answering the wrong question. The download checked that the account owned the
file, was in a good state, and that the copy had not expired, and then served the file.
It never asked whether the account could still read the conversations the file was
assembled from. Ownership of a *file* is not ownership of the *conversations inside
it*, and the two came apart the moment a membership ended.

Reproduced: a member's group export, built while access existed, then the membership
removed through the real endpoint. Group history returned **404**. The export download
returned **200 with the removed group's text in the body**.

**The file carries its own scope.** `data_exports.scope` records the conversation ids
the build actually wrote, from the document it produced rather than from the set it
selected — a conversation that failed to read and was omitted is not in the file and
must not be in the scope, or a later download would refuse the whole export for
content it never had. Delivery re-resolves the account's authority in a transaction and
checks each conversation with the *current* access rules: a member who lost a role, a
membership, or a restricted channel's access since the build does not receive the file
that predates it.

**There is no partial delivery.** The file is a single document, and a member handed
half of one has been handed content they cannot read by any other route, with no marker
saying which half. The refusal says why and points at a new export, which will be built
from what is readable now.

**A row with no recorded scope is refused.** Exports written before the column existed
cannot be checked, and "we cannot tell what this contains" is not a reason to hand it
over.

**The build is gated too, not only the request.** The route's feature check covers the
request; the build runs after the response and was selecting private conversations by
querying the conversation table directly, so an export on a server with private
conversations disabled contained them anyway. The selection now asks the same question
the interface asks.

What this does not change: a copy a member legitimately downloaded before a
withdrawal is theirs, and it cannot be recalled. This is about the server's own
undelivered copy.

## What the server holds is bounded, reclaimed and swept

Three more obligations follow from the server keeping a copy.

**A lifetime is not a count.** Seven days says how long a copy may exist, not how many
may exist. Finished copies are bounded per account (three) and on the server (fifty),
and the oldest are reclaimed rather than the request refused: a member asking for a new
export and being told to delete something first is a worse answer than losing a copy
they can rebuild at no cost to anybody. The count is taken *before* the new build, so
the bound accounts for the copy being created rather than only for what exists.

The row goes first and the file follows outside the transaction, so nothing can
authorize a download of a file that is about to be unlinked. A rollback therefore
leaves an orphan file rather than a row pointing at nothing.

**Erasing an account removes its files.** The export row cascades with the account, so
the ids are collected *inside* the erasure transaction — afterwards there is nothing
left to name them, and a file with no record is a file no sweep can ever reach. An
export is the account's own content and nobody else's, so removing it erases nothing
anybody else is entitled to.

**A file with no row behind it is swept.** Every other maintenance pass works from the
table, so a crash between reclaiming a row and unlinking its file would otherwise leave
a copy of somebody's history that nothing can ever reach again. Orphans are removed on
the same grace period as an unclaimed upload: a file younger than the grace may belong
to an export whose row committed moments ago, and unlinking it would break a build in
progress.

## Delivery is registered, so a withdrawal can interrupt it

The scope check decides whether a download may *start*. An export can be hundreds of
megabytes, so a membership revoked midway through has to interrupt the transfer —
and a response that is not in the delivery registry cannot be interrupted by
anything. The download is now registered through the same registry every attachment
uses, and served through a revocable reader, so an authority change ends the copy
rather than letting it run to completion. Range requests still resolve, because a
member resuming a large download needs them.

`ServeFile` is gone from this path for the same reason: it hands the response a file
handle the server has no handle on, so the registry cannot see the transfer at all.

**Turning the private feature off ends delivery too.** The build is gated on the same
flag, so a file containing a private conversation can only predate the change — but
that file lives for up to seven days, and the operator's decision was that the private
conversations inside it should stop being reachable. Serving it anyway is the same
leak as serving an export after a removal, with a different cause and the same
seven-day tail.

## A full server refuses; an interrupted job is closed

**The server bound was only ever enforced per account.** The clamp that limits how
much of one account's excess the server's spare capacity can relieve made the reclaim
a no-op when the server was full — and the new job was then admitted anyway, so the
server ended up holding one more copy than its own ceiling states. A bound enforced
only for the account is not a server bound. A full server now refuses with a 409 that
says so and points at the two things that clear it: a copy expiring, or the member
deleting one of theirs.

**An interrupted build blocked the account forever.** A `building` row is the only
thing standing between an account and asking again, so a crash between the insert and
the build finishing left a permanent "an export is already being prepared" — answered
by a job that no longer exists, and indistinguishable from one that is genuinely
running. Rows older than an hour are closed, at startup as well as on the hourly pass,
because a member who pressed the button a moment before the server stopped should not
wait a maintenance interval to press it again. A *recent* row is left alone: it may
really be building, and a second build beside it would be worse.

**The byte ceiling now means something.** The oversized branch recorded the omission
and then wrote the oversized file anyway, so the ceiling protected nothing except the
reader's patience — a member whose export was over the limit received a document the
server had already declared it would not produce. The file is not written; the job is
recorded as failed with the counts it reached, so the member can see how far it got
and why there is nothing to download. Marking it `failed` rather than `ready` is the
point: a "successful" export with no file is worse than a refusal, because the
interface would offer a download that 404s.
