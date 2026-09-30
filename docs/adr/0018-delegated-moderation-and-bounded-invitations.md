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
