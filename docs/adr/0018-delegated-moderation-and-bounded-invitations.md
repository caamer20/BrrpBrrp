# ADR 0018 — Delegated moderation, expiring timeouts and bounded invitations

**Status:** decided; implemented by migration 20
**Extends** ADR 0003 (roles), ADR 0009 (authority ordering), ADR 0010 (receipts).

## Assignment is a claim, not a transfer of power

A report can be **assigned** to a moderator. Assignment says who is expected to look
at it; it does not change what that person may do, and it does not stop anyone else
who already holds the capability. This is stated first because "assigned to" is the
kind of phrase that quietly becomes "only they can touch it" — and then a report sits
unreviewed forever because the assignee left.

The consequence is deliberate: **every moderation action checks the actor's own
current capability**, never the assignment. An assignment to an account that has since
lost the capability is a stale claim, not a grant, and the report it names stays
reviewable by whoever still holds the capability.

## A timeout changes nothing, so expiry restores nothing

A timeout suspends an account until a time. It is **not** a role change that happens
to be undone, and it is not implemented as one: it does not touch role assignments at
all. When it expires the account has **precisely what it has now** — its roles, its
channel access, its positions — because nothing was taken away.

Expiry clears the timeout row and nothing else, in the same transaction as the read, so
there is no window in which an account is both suspended and unsuspended and a re-login
mid-expiry cannot observe one without the other.

This section previously specified that expiry **replays a recorded snapshot** of the
account's role assignments, on the reasoning that "a role removed during the timeout must
stay removed". It did the opposite. The snapshot predated the removal, so replaying it
put the role back: reproduced with a role revoked through
`/api/admin/users/{id}/roles` while a timeout ran, which came back when the timeout
expired. A moderator's revocation was undone by a maintenance sweep nobody was
watching, and the test asserted the outcome and called the cost "real".

A snapshot is a statement about the past. Authority is a statement about the present.
The recorded value is still written — for an operator reading a row to see what was
true when the timeout was applied, and for installations upgrading from a build whose
expiry acted on it — and nothing replays it.

**The revision has to advance.** The authority commit protocol *reads* the revision a
change produces; it does not create one. A timeout that reached the protocol without
bumping it first reported success, committed the row, audited it, and left the
revision at 1 → 1 — so work already queued for the account was still admissible, and a
writer that decided afterwards could not tell the difference. Applying and lifting
both advance it, because lifting restores the account's access.

**One target, one commit.** `commitAuthorityChange` commits. The handlers committed
again, so a moderator saw 500 for a timeout that was in force and audited, and a retry
applied a second authority change on top of the first. The route was registered under
`/api/admin/accounts/{id}/timeout` while the interface calls `/api/admin/users/{id}/timeout`,
and `applyTimeout` read its target from the request body while `liftTimeout` read it
from the path — the two halves of the feature disagreed about which account they were
acting on. The route is now the one the interface calls, the route is authoritative,
and a body naming a different account is refused rather than silently resolved.

## A community timeout is not an installation ban

A timeout is a **community** moderation decision. It suspends an account from the
community and nothing else: not its direct conversations, not its private activity, not
a private call, and not any role it holds. An installation-wide ban is a separate
authority with its own route and its own rules.

Routes say which kind they are. `BeyondCommunity` on a route policy marks a surface a
timeout must not close, and the default is closed — a new route is suspended until
somebody has said which kind it is. That asymmetry is deliberate: adding a private route
that forgets the mark leaves it suspended, which is recoverable. Forgetting it on a
route that has it leaves a community moderator holding somebody's private life
hostage, which is not.

A timeout does not mark the account banned or deactivated, and does not end its
sessions. A ban does; one that arrives as a side effect of a timeout is a ban nobody
decided to apply.

**The two are separate facts, not one flag with two names.** `Principal.Inactive`
means banned or deactivated; `Principal.Suspended` means a timeout. They were one
field, and folding them together is what made a community timeout behave as an
installation ban: no sign-in, no session, no private history, no direct messages, no
calls. One flag cannot express two answers, and every consumer that cared had to ask
the timeout question again for itself. The split is asserted in both directions — a
timeout leaves the private surfaces open, and a ban closes them.

**The sign-in gate was the visible symptom and the flag was the cause.** The comment
above it said a timeout is "a moderation decision, not a failed sign-in, and the person
is owed the truth about it: the password was right" — and then refused the sign-in
anyway. The comment described the intended behaviour and the code did the opposite for
as long as the two states shared a value. A timed-out member now signs in, holds a real
session, reads and sends in their private conversations, and finds the community
closed.

**A timed-out member can still act on their own account.** Reading `/api/me`, changing
their password, and managing their own sessions and second factor are
`BeyondCommunity`. Without that the client cannot show them *why* the community is
closed, or let them secure the account they are still signed in with — a timeout that
leaves somebody unable to find out what is happening to them is a different failure,
not a smaller one.

**Some paths cannot be decided in the middleware at all.**
`/api/conversations/{id}/messages` and `/api/channels/{id}/messages` are the *same
handler*, and the same URL is a community channel for one id and a private
conversation for another. A timeout's scope is per-destination, not per-path, so those
routes carry `MixedConversation` and the handler decides: it already resolves the
conversation in order to answer at all, and it refuses a suspended caller when the
conversation is a channel. Marking them `BeyondCommunity` instead would have let a
timed-out member keep reading community channels, which is the failure the scope
exists to prevent.

## Hierarchy is decided by authority level, not by who is asking

A moderator may act on an account strictly below their own level. This is checked
inside the same transaction as the change, against **current** authority — a
demotion that lands between the check and the write must not be able to complete
against the older level. The alternative, checking at the route and acting later, is
how a demoted moderator ends up holding a queued action.

## Invitations are bounded, expiring and revocable — and bounded on the write

An invitation is a key that admits people, so it is treated like one: it has an
expiry, a maximum number of uses, an entry role, and a way to be revoked. Three
bounds, all applied **on the write**:

- **uses** — the invitation stops admitting at its maximum;
- **expiry** — it stops admitting at its time;
- **entry role** — the capability it grants, chosen at creation and never more than
  the inviter holds.

That last one is the reason entry roles are bounded: an invitation that could grant
`channel.manage` would be a privilege escalation carried in a link. A person may only
offer what they already have, and never `admin` or `owner`, which no inviter holds
anyway.

A revoked or exhausted invitation is **gone**, not merely inactive: redemption answers
the same refusal whether the invitation was revoked, expired or never existed, so a
link cannot be used to probe which.

## Redemption is idempotent and accounted

Redeeming the same invitation twice is refused the second time rather than silently
succeeding, because a use is a bounded resource and a double redemption would consume
two. The refusal is the same one as a revoked invitation, so the reason is not
disclosed.

## What a member sees is never the moderator's

Assignment, timeouts and invitation management are moderator surfaces. A member's
own view says only what is true about them: whether they are suspended, and until
when. The *reason* and the *who* belong to the audit trail, which is gated on
`audit.read` and classified by ADR 0002's rules.


## A community timeout closes the community, not the account

A moderation timeout is a decision about the *community*. The account stays signed in,
stays a member, keeps every consent it was granted, and keeps the private
conversations it was part of. Two things follow from that, and both were wrong.

**The member has to be able to sign in.** The workspace snapshot is what the client
loads after a successful sign-in, and it carries the server settings, the feature
flags, this account's conversations, groups and request counts — none of which a
community timeout touches. Refusing the snapshot did not make the community smaller; it
made the account unusable, because the client treats a failed snapshot as a failed
sign-in and the member was left on the sign-in form with a correct password. The
snapshot is now served during a timeout with its **community half emptied**: no channels,
no categories, no unread counts, no presence and no voice states. Presence is included
deliberately — a member who cannot enter a channel is not entitled to be told who is in
one. Every other community route still refuses them with a 403 that names the timeout.

**The member has to be told.** `/me`, sign-in and the snapshot's own account each carry
the caller's own `in_timeout` and `timeout_ends_at`. Without it the interface shows an
empty workspace in which every community request is refused and nothing explains why —
the member cannot distinguish "a moderator restricted me" from "the server is broken",
and cannot tell whether the restriction has ended. These fields are applied by an
explicit step rather than in the shared user scanner, because the administrator's
account list, a message author and an audit row must never carry one account's
moderation state; `TestTheSelfReportedTimeoutIsOnlyEverAboutTheCaller` asserts that.

**What the tests had been proving.** Every timeout test asserted a denial. A
restriction that refuses everything passes a denial-only suite perfectly, which is
exactly the wrong thing for a feature whose whole content is *which* things it refuses.
`TestATimedOutMemberStillUsesWhatTheTimeoutDidNotTakeAway` asserts the permitted half
positively — a real sign-in, real private history, a DM that exists in storage
afterwards, the account's own activity — and then the forbidden half exactly, including
that a refused community write leaves no row behind. Reverting the enforcement check
makes it fail, and so does a sign-in that reports the wrong account.


## A block is a two-person fact; an unavailable account is nobody to call

Both gates on a private call came from V14-07, and both over-generalised what that fix
established. The original defect was real — a blocked *direct pair* could open a call the
whole time the block stood — and the repair asked a question that has no single answer.

**A block between two group members is not the group's business.** The check was
`blockedBetween`, which asks whether *any* accepted member blocks the caller. On a group
that is wrong twice over. It excludes the parties from each other's call for as long as
the block stands, and — because the same query runs for every participant — it lets
either of them keep a *third* member out of the room that third member is standing in.
That is not a narrower block; it is the group deciding who may talk to whom. The live
contact gate is now a direct pair's property. A block still governs the message path and
deletion on a group, which is what `conversationAccess` already decided — a group is not
a pair, so there is no two-person relationship to read as blocked — and the call path now
agrees with the message path instead of inventing a second policy.

**An erased, deactivated or banned peer is not somebody to call.** Membership outlives
all three, because they are account lifecycle rather than conversation membership, and
deleting the conversation would destroy the other person's history. So the pair still
reads `accepted` and the call path — which consulted membership — admitted a live room
containing an account that cannot sign in and never will. `conversationAccess` only
requires a *usable other member* on the write path, so reading retained history after a
peer is gone still works, and that is deliberate: the surviving member keeps their side
of the conversation. Joining a **live** call is different in kind, and now asks the
separate question.

The revalidation pass inherits both rules, which produces the two results that look
opposite and are both correct: a direct call empties when a peer becomes unavailable,
because the one member left cannot rejoin into a room the admission rule refuses; a
group call loses only the member who left, because the group is not a pair.

**What is deliberately unchanged.** A blocked direct pair still cannot call, still cannot
message, and still cannot open a fresh request. A blocked member still reads retained
history and can still report it.
