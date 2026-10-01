# ADR 0017 — Named community threads

**Status:** decided; implemented by migration 19
**Extends** ADR 0010 (shared conversations) and ADR 0015 (activity).

## What a thread is, and what it is not

A thread is a **named conversation inside a community channel**. It reuses the
channel's messages, members, read cursors and search — it is a grouping, not a second
message store. That is the whole reason this is a small change and not a large one:
`messages` already carries `conversation_id`, so a thread is a row that narrows which
messages a view shows, not a table that duplicates them.

The alternative — a thread as its own conversation — was rejected. It would have
needed its own authorization path, its own unread state and its own activity
integration, and every one of those is a second copy of something that already exists
and would eventually disagree with it.

## Identity and inheritance

A thread has a stable `id` and a `channel_id`. **Authorization is the channel's**, read
at request time, so a member's access to a thread changes exactly when their access to
its channel changes — and the thread cannot become a way around a channel
restriction. A thread whose channel is restricted is as restricted as the channel; a
thread in a channel the reader cannot see is not found rather than forbidden, so
existence is not disclosed.

## Replies keep their identity

A reply already carries `reply_to_id`. Adding a thread does **not** rewrite existing
rows: a thread is created from an existing message, and the message becomes its root
by being recorded as such. Nothing about the message changes, so old ids and links
keep resolving — the requirement that legacy replies be promoted explicitly rather
than silently re-parented.

A message belongs to a thread through `thread_id`, which is empty for an unthreaded
message. A message whose thread has been archived is still readable; a message whose
*root* has been deleted is a tombstone, shown as "the original was removed" rather than
vanishing and taking its replies' context with it.

## State: active, archived, locked

- **Archived** hides a thread from the channel's list and stops new replies. It is not
  a deletion: the messages, the followers and the history stay, and reopening restores
  the list entry.
- **Locked** stops new replies and leaves everything else alone. A locked thread is
  still followed, still readable and still searchable — the state is about writing,
  not about access, which is a distinction worth making explicit because conflating it
  is how "locked" becomes "hidden" by accident.
- A **deleted** root is a tombstone. The thread itself is removed, because a thread
  without a root has no subject.

## Following is per account and separate from unread

Following is a personal subscription: you get activity for replies in a thread you
follow, and following someone else's interest changes nothing about yours. It is **not**
read state — following a thread you have finished reading does not mark it unread, and
reading a thread does not clear the channel's unread state. Those are two different
questions and keeping them apart is what lets a member read a busy thread without
losing track of the channel.

Following is bounded: a member follows a bounded number of threads, and the bound is
applied on the write rather than by a later sweep, so following cannot be used to
grow a table without limit.

## Reading a thread does not move the channel's unread

Unread is a property of the channel and its read cursor. A thread has its own position
in its own history, and opening one does not acknowledge the channel. This is stated
because it is the failure a member notices: reading a thread, returning to the channel,
and finding the channel still marked unread with no way to tell why.

## Activity

A reply in a thread you follow is activity, with the thread as its source — the same
`source_kind` mechanism ADR 0015 defines, with a new value rather than a new table. The
payload is content-free on the same terms as every other private notification, and a
community thread is not private: its notification names the thread and the channel, and
the text is read by opening the thread.

## Bounds

A thread's history is read with the same bounded cursor pagination as a channel's, and a
thread with a thousand replies is reachable in full: every reply exactly once, in
order, without the client accumulating them unboundedly. The client keeps a window and
requests older pages rather than holding the whole thread, which is the property a
large fixture has to prove rather than assert.


## The thread is part of the send's destination

The operation's identity is the account and the key, and the *destination* travels in
the intent — so reusing a key for other work is a conflict rather than a silent replay.
The thread is part of that destination: a member who asks for the same words in two
threads of one channel has asked for two messages, and a receipt that omitted the
thread would answer the second with the first.

It is worth being precise about how much that was worth here. The message table
carries `UNIQUE(user_id, idempotency_key)`, and *that* index is what produced the
409 in the reproduced case — the intent digest omitting the thread was masked by it.
The index is the older guard, and it disappears with the row it guards, so a retry
after the first message was deleted would have recreated content the moment the index
was no longer there to refuse it. The digest is the durable guard; the index is the
accident that has been catching the everyday case. Both are correct to keep, and only
one of them is load-bearing across deletion.

## The thread's state is re-read in the transaction that inserts

`createThreadMessage` resolved the thread, checked it was not locked, and committed that
decision — then called the shared send, which opens its own transaction and inserts.
A lock landing in between was decided by a check nothing re-asks.

The state is now read again, immediately before the insert and inside the same
transaction. **That re-check cannot be demonstrated by a test, and the reason is
worth recording rather than hiding:** the store holds a single connection, so a
concurrent writer waits for the sending transaction instead of interleaving with it.
The gap was real and unreachable, which is exactly why it survived. The re-check is
defence in depth at the cost of one indexed lookup, on the principle that the route
guard decides whether a handler runs and the committing transaction decides whether a
row may be written — and the second is the only one of the two close enough to the
write to be about it.


## A thread survives its subject

Migration 19 declared `root_message_id ... ON DELETE CASCADE` and wrote, in the
comment above it, that deleting the root "tombstones the thread rather than leaving
replies attached to nothing". The cascade did the opposite.

Deleting a root deleted the **thread row**, and the replies — acknowledged, read
history that other people had linked to — were left carrying a `thread_id` that
resolved to nothing. They were absent from the thread (404), absent from the channel
listing, and still returned by search with a link that 404s. A member who deleted one
message lost the rest of the conversation, and nothing anywhere said so.

The root is what a thread is *about*, not what it *is*. A subject can be withdrawn and
the discussion can stand: that is what a forum thread is, and it is what somebody who
has read forty replies is owed.

Schema 23 makes `root_message_id` nullable and adds `root_deleted_at`. Deleting a root
clears the reference and records the moment, so a thread can say *why* its first line
is empty rather than presenting an unexplained hole. Two things it deliberately does
**not** do:

- It does not retain the subject's text. The tombstone records that it went, not what
  it said, and nothing in this migration is a content-retention field.
- It does not make the thread special. A tombstoned thread is still listed, still
  readable, still followed, and still accepts replies — a discussion whose subject was
  withdrawn is still a discussion, and a member needs a way back to it.

The table is rebuilt rather than altered, because SQLite cannot drop a foreign key or
widen a `NOT NULL` column in place, and that cascade is the whole problem. The unique
root index becomes partial, since several tombstoned threads may have no root.


## A thread reply is a recoverable send like any other

A thread reply used to have its own HTTP call. That gave it its own outbox (none), its
own draft handling (none) and its own account fence (none), so three ordinary cases
behaved differently inside a thread than in the channel it lives in:

- **A refusal** cleared the composer and rotated the key, so the text was gone and the
  next send was new work.
- **A lost response** did the same, which is the one outcome the whole receipt design
  exists to prevent: the author retried under a new key and sent the message twice.
- **An account switch or authority change** mid-send wrote its result into a view that
  no longer belonged to that account, because nothing fenced the completion.

A member's experience of all three is "threads are the place my messages get lost", and
they are right.

**One send path, and a thread is a destination like any other.** `messagePath` decides
where a message operation goes, and a message carrying a `thread_id` goes to its
thread. That is the whole change: one place builds a message URL, so a rename cannot
make the two drift, and every thread send inherits the receipt, the outbox, the draft
store and the account fence because it is now the *same* send rather than a copy of it.

Two things the caller keeps:

- **Its key.** `sendIntoThread` takes the operation key rather than generating one,
  because a thread composer freezes its key for the life of an unresolved send.
- **Its text.** The composer clears the draft *after* the outcome and restores it on a
  failure, so a refusal and a lost response both leave the author with their words.

**The draft lives in the shared, account-scoped store.** Panel-local state dies with the
panel, so closing a thread to look at a channel and coming back used to lose an unsent
reply. The store is already scoped to the account and already survives a reload when
drafts are saved, so a thread reply is now held exactly the way a channel message is —
  including not carrying into the next account.

**What this does not establish.** These tests assert *which machinery* a thread reply
is handed to, which is only observable in the source; a browser test would confirm a
request was made, not that it was made through the recoverable path. The end-to-end
consequence — a lost response reconciled rather than duplicated — is covered on the
channel path and inherits here by construction, not by observation.


## A thread has its own reading position

Read state was keyed by conversation, so every message in a channel — a thread reply
included — shared one cursor. That is the wrong shape for a thread, and it fails in both
directions at once:

- A member who read forty replies in a thread and then opened the channel had, on the
  only cursor that existed, read the channel as far as the thread's newest reply.
- A member who read the *channel* had marked a thread they never opened as read.

Schema 24 adds `thread_reads`, keyed by thread, with its own position. A thread is a
conversation with a subject and it moves at its own rate: a quiet thread beside a busy
channel, or a busy thread nobody has opened, are the ordinary cases rather than the edge
ones. Reading a thread now moves that thread's position and nothing else, which is the
sentence the interface has to be able to make truthfully.

The position is **scoped to the thread it is set on**. A cursor in one thread pointing
at a message in another would make the next unread count meaningless, so the write
resolves the message *within* that thread and refuses anything else. And the channel's
own access rule still decides, because a thread is only as restricted as the channel it
inherits from: a member who has lost the channel has lost the thread and cannot move a
cursor in it.

The table cascades from `threads`, so a deleted thread takes its readers' cursors with
it and leaves nothing reachable through a thread id that no longer resolves.


## The lock race is produced by stopping the send, not by racing it

`createThreadMessage` resolves the thread and checks its state in one transaction,
commits, and then runs the ordinary send. A lock landing between those two points is
not seen by that check — it has already answered.

Concurrency cannot reach that window. The store is a single connection, so a second
writer waits for the open transaction rather than interleaving with it. No barrier,
no repeated loop and no scheduler manipulation reaches it, which is what the original
comment on the re-read said, and it is why the invariant was asserted by inspection
rather than by a test: **the gap was invisible because it is unreachable by racing.**

It is reachable by stopping. `threadCommitHook` runs at exactly that seam — after the
check has committed, before the send begins — and a test installs it, locks the thread
through the supported route from inside that window, and lets the send continue. The
lock is real state applied through the real route; only the *timing* is staged.

The assertion is the state afterwards rather than the status code, because a 403 alone
does not distinguish "the committing transaction caught the lock" from "the earlier check
happened to run after it". The test checks that the raced reply is absent from the
thread, that the thread is locked, and that a reply sent *before* the race is still
there — a fix that dropped everything would pass the first assertion.

Removing the in-transaction re-read makes it fail with a reply in a locked thread. That
is the whole point of the seam: it turns a comment into a regression.

**Why the hook exists rather than a looser assertion.** The alternative was to assert
the re-read's *presence* — a source string check, or a test that only proves the handler
runs. Both pass against code that re-reads the thread somewhere harmless. This one fails
against code that does not re-read it at the point that decides the write, which is the
only place the distinction lives.
