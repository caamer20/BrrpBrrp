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
