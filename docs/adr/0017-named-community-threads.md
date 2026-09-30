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
