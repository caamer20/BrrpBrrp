# ADR 0016 — Consenting groups on the shared conversation model

**Status:** decided; implemented by migration 16
**Extends** ADR 0012 (direct conversations and consent) and ADR 0015 (activity).

## The model, not a second one

A group is a `conversations` row with `kind='group'` and members in
`conversation_members`. It is not a second message store, a second authorization
path, or a parallel set of unread counters. The same rows, the same read cursor, the
same receipts, the same search, the same attachments, the same activity feed and the
same evidence rules all apply, because they already apply to `kind='direct'`.

The schema has said `group` since migration 6 and every authorization check has been
written against conversation membership rather than against "is this a direct pair".
That is what makes this additive: a group is a new *kind of membership*, not a new
kind of conversation service.

The alternative — group tables beside `direct_pairs` — was rejected because it would
have required a second copy of every access check, and the failure mode of a second
copy is that it eventually disagrees with the first.

## Consent is per group and per act

There is no implicit admission. A person is in a group only because they accepted an
invitation naming it, and acceptance is the recipient's act alone:

- The inviter may not accept on the inviter's own behalf. A request the sender could
  accept is not a request.
- A cross-accepted request never auto-accepts the other. Two people naming each other
  have still each to accept.
- Acceptance is idempotent per (group, account): a replayed acceptance is the same
  acceptance, not a second membership.
- The last accepted member leaving leaves an empty group, which is removed rather
  than left as a conversation nobody can read.

## History: the D-07 rule, and where it is actually stated

**An accepted member of a group can read that group's earlier history.** This is not
a side effect; it is stated before the invitation is accepted, not after, and it is
the same rule that makes a direct conversation's history readable to its second
participant.

Two things follow, and both are consequences rather than separate policies:

- **Withdrawing an invitation does not erase what the invitee already saw.** There is
  no state in which a person read something and then un-read it.
- **Removing a member revokes everything from the moment of removal forward**:
  messages sent after, files, search results, read cursors, mentions, typing
  indicators, unread counts, activity rows and media. What they read before is
  theirs, as it is for a removed direct peer under ADR 0011.

The master plan's conditional phrasing is left as the plan states it, and this ADR is
where the product decision it defers to is made. An owner who wants the opposite rule
for their installation is making a different product, and that is their call to make
explicitly rather than something an implementation should decide silently.

## What blocking means in a group, stated accurately

**A block does not hide a group conversation.** A group conversation is shared
among its members; there is no per-pair view of it to hide. A block between two
members of the same group suppresses *their* direct contact — the blocker's
conversations, the blocked account's messages to them privately, and any future
request between them — and nothing else.

Saying otherwise would be a false promise in the one place it matters most, so the
interface does not offer a "hide their messages in this group" affordance it cannot
honour. What it does offer, and what a member can rely on, is:

- they receive no activity rows for the other account's messages in that group, and
  the payload carries no content at all;
- they can leave the group, which is the only way to stop seeing it;
- they can read it for as long as they are a member.

This is the D-07 rule again, from the blocked member's side: the protection is
membership, and the block is about contact, not about a shared room.

## Group ownership is deterministic

A group has one owner, stored as a conversation-level owner row rather than inferred
from the inviter. Ownership matters for three things and only those three: who may
invite, who may remove, and who may transfer.

- **Transfer** moves ownership atomically. The transfer is a single transaction: the
  new owner must be an accepted member, and the old owner stops being owner in the
  same commit. A transfer to a non-member, to a removed account, or to yourself is
  refused with a stated reason.
- **The owner leaving a group that still has members** does not silently promote
  anyone. The owner must either transfer first or name a successor in the same act;
  otherwise the leave is refused. A group cannot reach a state where nobody can
  administer it, and it cannot reach a state where two owners disagree about who may
  invite.
- **The last member leaving** removes the group, together with its messages, files
  and activity. There is no orphan for a later joiner to inherit.

  The last-member case is deliberately *not* the successor case. Demanding a
  successor from the only remaining member would make the only way out of a group one
  has created alone be to invite somebody first, which is a worse product than the
  problem it solves. So the owner is required to hand over only while there is
  somebody to hand it to, and the group is removed when the last person walks out.

## Bounds

- **Ten accepted members initially.** Named, enforced in the same transaction as
  acceptance, and stated in the invitation so nobody joins expecting a different
  number. Ten pending invitations per group, invitations expiring after seven days,
  and a per-account bound on invitations sent in a day.
- A group carries the same message, upload and retention limits as any other
  conversation. There is no larger allowance for a group.
- Invitation creation, expiry, rejection and revocation are all durable state rather
  than memory, so a restart cannot resurrect a revoked invitation.

## Interaction with what already exists

- **Activity.** A group message is activity, keyed by its message id like any other,
  so a member who was offline catches up. A group is not a separate activity
  surface: it is a `source_kind` value.
- **Evidence.** Private evidence review is about the evidence a person submitted.
  Group messages are private in the sense that they are inside a consented
  conversation, so a member may report a group message exactly as they may report a
  direct one, and a reviewer is given the submitted snapshot and nothing else.
- **Search.** Scoped search covers a group the caller is an accepted member of, and
  global search never returns group content.
- **Restore.** A group is ordinary rows, so a backup carries it. A role or membership
  revoked after the snapshot reads as granted again on the restored copy, and is
  reconciled before cutover like any other post-snapshot authority decision.

## What this does not establish

Local evidence only. Group behaviour is not exercised on a physical client, under a
real network, or at capacity, and none of the qualification targets have been run.
The accessibility and human-review obligations in the master plan are unchanged by
this ADR and are not met by it.
