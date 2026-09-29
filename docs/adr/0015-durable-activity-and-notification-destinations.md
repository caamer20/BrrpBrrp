# ADR 0015 — Durable activity, notification preference precedence and typed push destinations

**Status:** decided; implemented by migration 15
**Supersedes nothing.** Extends ADR 0012 (private conversations) and ADR 0014 (request
evidence) with the activity and notification contract those deferred.

## Why this is a new contract and not a feature

Everything that exists today is either derived or scoped to a channel. Unread counts are
derived from the read cursor. The outstanding incoming-request count is derived from
current requests. The only stored notification state is
`channel_notification_preferences`, which is channel-scoped and affects interruption
rather than history. Push dispatch carries a single `channelID`, and a direct
conversation has none, so a private notice would have to be dispatched with an empty
channel — which the current guard reads as "no check needed". That is the exact bypass
the private-consent work was built to prevent.

So the missing piece is not a UI. It is a durable record of something that happened, a
typed destination that dispatch can re-check, and a stated precedence when several
preferences apply at once. Each of those has more than one defensible answer, so they
are decided here rather than in whichever handler is written first.

## Activity is durable; unread is derived

Two ideas get confused constantly and are kept apart:

- **Unread** is derived. It is a function of messages the account can currently read and
  a read cursor it owns. Nothing is stored, so it cannot disagree with history.
- **Activity** is durable. It is a record that something happened, addressed to an
  account, that survives the recipient being offline and lets them catch up.

`account_activity` rows are created inside the transaction that creates the thing they
describe. A message that is not delivered has no activity row, so there is no way to be
told about a message that does not exist, and no second copy of private message text is
ever stored: the row carries identifiers, a kind and a timestamp, not content.

A conversation deletion cascades its activity away. An account erasure cascades its own
activity away and leaves other accounts' rows intact, because their history of being
notified about a conversation is not the erased account's data. This is the ADR 0011
distinction again, applied to a third kind of record.

## Stable event identity

Every activity row carries a `source_kind` and the identity of the thing it describes:
a message id, or a request `attempt_id` — never a conversation id alone. A pair that
renews its request produces a new attempt, and the two attempts are different events
even though the conversation is the same. This is the same reasoning ADR 0014 used to
give attempts an identity, applied to the notification rather than the evidence.

`UNIQUE(user_id, source_kind, source_id)` is what makes a redelivery idempotent: a
retried send, a replayed request, or a maintenance pass that recomputes a row cannot
produce a second activity entry for the same event.

## Four states that are not one state

They are separate columns or separate fields because they answer different questions
and are reset by different things:

- **Outstanding request** — derived from current requests. Rejected, withdrawn, blocked
  and no-longer-eligible identities are excluded. Cleared by accepting, rejecting,
  withdrawing, blocking or account removal.
- **Unread messages** — derived from the read cursor.
- **Acknowledged** — durable and per account. The recipient has seen the activity list.
  Monotonic: it never moves backwards, so a slow client cannot un-read something by
  re-reading an older page.
- **Delivered interruption** — a device-level fact, not a row. Whether a push actually
  reached a browser is the service worker's business and is never recorded as if it were
  a property of the event.

`acknowledged_at` is compared by the account, so two devices on the same account share
one acknowledgement. That is deliberate: acknowledging on a phone should not make the
laptop look stale.

## Preference precedence

Several preferences can apply to one activity row. The order is stated once, here, and
implemented once:

1. **Feature off** — with `BRRP_ENABLE_PRIVATE_CONVERSATIONS` off there is no private
   activity to deliver, and the interface is not offered. This is not erasure.
2. **Account unavailable** — banned, deactivated or erased accounts are not notified
   about anything, and existing rows are not delivered.
3. **Current access** — at dispatch, not at enqueue. A queued notice cannot disclose
   activity the recipient lost access to while it waited, so eligibility is recomputed
   per device dispatch. This is the property that makes a typed destination load-bearing:
   an empty destination is not "no check", it is "not a channel and not a direct
   conversation", and it is refused.
4. **Channel mute** — for a community channel, a mute suppresses interruption. It never
   suppresses the activity row, the unread count or the mention record. Muting is about
   being interrupted, not about what is readable.
5. **Private preference** — an account-level, per-kind preference for direct messages and
   for incoming requests, separate from channel mute because there is no channel to
   attach it to.
6. **Active view suppression** — the client tells the server which conversation it is
   looking at. Suppression is a client concern and is never a server decision, because a
   client that lies about it loses an interruption, not data.

Mute and unblock are deliberately different things. Muting is reversible and about
interruption. Unblocking restores contact and does **not** replay anything that arrived
while blocked, does not resurrect a withdrawn request, and does not re-deliver old
activity: the rows are already acknowledged or expired, and re-delivering them would be
the "block must not be a mute with extra steps" failure.

## What a private notification may say

A default private notification says that something happened, not what. It names the
sender and nothing else: no message text, no introduction, no reason. The recipient opens
the conversation to read it, under the same authorization as reading the conversation
itself. This is the same disclosure boundary the evidence reporter is given, and it is
recorded here because a push payload is the one place where text most easily leaks —
through a notification preview on a lock screen.

An account that wants the sender's name in the notification has it; an account that
wants nothing but "you have a new message" has that too. Neither choice affects what a
third party can observe.

## Retention and bounds

Activity rows are retained **30 days** and reclaimed in bounded batches by the existing
maintenance pass. Thirty days is deliberately shorter than the 90-day evidence window:
activity is a catch-up affordance, and evidence is a submitted record someone may appeal.
Conflating them would either over-retain notifications or under-retain evidence.

A per-account bound of **2,000 unacknowledged rows** is applied **on the write**, and
an unacknowledged row is reclaimed after **30 days on its own age**, independent of the
cap. Both halves matter, and both were missed once:

- The cap that is not applied on insertion is not a cap. Seeding exactly 2,000 rows
  and then posting a real message produced 2,001, because the bound was only ever
  consulted by a later sweep.
- The cap that is only consulted above its own limit is not a retention rule either. A
  member who was away a month came back to a backlog nothing had ever touched, because
  they were under the limit the whole time.

**What the bound reclaims, and what it never does.** The message is durable whatever
the backlog is doing: the bound governs the *notice*, and the conversation remains the
record. Where a member is over the bound, the oldest rows are reclaimed to make room —
acknowledged ones first, because those are history the conversation still carries and
the row is only a pointer to it. A member who is over the bound with nothing
acknowledged has the oldest unacknowledged rows reclaimed rather than the new message
being lost, so the backlog is bounded and the conversation is intact.

The honest cost is that a member who is away long enough does not come back to a
complete list of what happened. That is a real loss and is stated rather than hidden:
the conversation is authoritative, the activity list is a bounded catch-up affordance,
and a gap in the list is not a gap in the messages.

## Pagination is account-owned and monotonic

Cursors are opaque, account-scoped and strictly decreasing by id. A cursor is a
position in *this account's* activity, so it cannot be used against another's. Pages are
bounded at 50. Filters (`kind`, `unacknowledged`) are applied before the limit, so a
filtered-out row cannot occupy a page slot or move a cursor.

Acknowledgment is an explicit, separate call rather than a side effect of reading. A read
that fails halfway must not have consumed the acknowledgment, and an acknowledgment must
survive the page it was made from.

## Service worker and cross-tab

The 24-hour deduplication contract is server-side: the same event identity presented
twice within 24 hours does not produce two interruptions on the same installation,
because the activity row already exists and the client keys its own display on
`(source_kind, source_id)`. Two tabs on one account therefore show one interruption, and
so do a tab and the service worker. The 24 hours bound how long the client keeps that key;
it is not a server retention rule and does not affect what is stored.

## Push destinations are typed

`pushJob` gains a destination type rather than a bare channel id:

- `channel` — checked with the existing channel audience, which already re-reads
  current authority per device.
- `direct` — checked with the current pair: both members, not blocked, not hidden in a
  way the account asked for, and both still usable.
- `request` — checked with the current request: still pending, and the recipient is still
  the account being notified.
- `none` — refused. There is no "no destination" case, because that is how the current
  `channelID == ""` path would admit a private notice without any authorization check.

Every check happens at dispatch, per device, inside the same advisory-lock discipline
the channel path already uses, so a slow device cannot let a later device reuse an access
or mute decision made before it.

## Interaction with what already exists

- Nothing here changes unread derivation, read cursors, or the outstanding request count.
  Those stay derived, so they cannot drift from history.
- Nothing here changes `channel_notification_preferences`. It remains channel-scoped and
  remains about interruption.
- Unblocking does not replay. Blocked-pair activity written while blocked exists but is
  never delivered, and is reclaimed on the activity clock.
- Group and thread sources are added later by writing more `source_kind` values, not by
  changing this table. The identity and acknowledgment rules already accommodate them.

## What this does not establish

This is a local contract and local evidence. It does not establish that a push reaches
any particular browser: browser automation is not physical notification qualification,
and a recorded delivery is not a delivered one. The deep link it opens is resolved
against current authority at the moment it is followed, so a revoked grant produces a
refusal rather than a conversation.
