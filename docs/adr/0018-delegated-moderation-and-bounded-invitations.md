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

## A timeout restores only the powers the account otherwise has

A timeout suspends an account until a time. When it expires, the account gets back
**precisely what it had before** — its roles, its channel access, its positions — and
nothing else. A timeout is not a role change that happens to be undone, and it must
not be implemented as one: an account that was banned before a timeout, or had a role
removed during it, returns to the state that was recorded, not to a default.

Because that is easy to get subtly wrong, expiry restores the recorded snapshot and
**clears the timeout row in the same transaction**, so there is no window in which an
account is both suspended and unsuspended, and a re-login mid-expiry cannot observe one
without the other.

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
