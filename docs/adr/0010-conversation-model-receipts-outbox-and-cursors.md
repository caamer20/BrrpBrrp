# ADR 0010 — Shared conversation model, send receipts, outbox and read cursors (D-11)

- **Status:** accepted (design); first bounded slice implemented on the channel path
- **Date:** 25 September 2026
- **Plan reference:** D-11, D-08, §5.5, §5.6, §5.8, §6.1 (M-C), WP-09–WP-12.
- **Deciders needed:** TL (contract), OWNER (D-08 deletion/tombstone policy before private
  conversations are enabled), SEC (review before any direct/group surface is exposed).

This ADR resolves the contradictions review 6 recorded against the WP-09 prerequisite note. It
states the exact constraints, API outcomes, expiry/admission rules and migration mechanics.
It adds no private feature: DMs and groups remain gated behind D-08, WP-11 and WP-08 as before.

## Decision 1 — One conversation descriptor, channel ids preserved

A `conversations` row has `kind ∈ {channel, direct, group}`. **For a channel conversation the
conversation id is the channel id.** That is what preserves existing message ids, deep links,
search hits, unread state and the current channel authorization path; the mapping is
one-to-one and is created by the migration, not by application code.

`messages.conversation_id` is the scope every new reader and writer uses. `messages.channel_id`
stays `NOT NULL` and is the *projection* for channel conversations; it becomes nullable only in
the later stage that ships direct/group conversations (Decision 4). Because the two are equal
for channel conversations, the compatibility reader is `COALESCE(messages.conversation_id,
messages.channel_id)`, which is correct for rows written before the migration and after it.

Delivery is unified on `conversationAudience` + `publishToConversation`, which carry the
resolved authorization revision exactly as the channel paths do (ADR 0009, §5.4), so the
commit/admission contract covers future direct/group conversations without a second mechanism.
Channel conversations keep `MayReadChannel`; a direct/group conversation is readable only by an
accepted member (`conversation_members.state='accepted'`), and owner/administrator status never
bypasses that check (§5.2 rule 4).

## Decision 2 — Operation identity is per account and key; destination is intent

**Uniqueness is `(account_id, idempotency_key)`** — the same scope the message table already
uses. The destination is *not* part of the key; it is part of the **intent**, because §5.5
requires that reusing a key for different work conflicts instead of silently returning another
message:

- `message_operations(account_id, idempotency_key)` is the primary key. A receipt records
  `conversation_id`, `intent_digest`, `state`, `result_message_id`, `created_at`, `expires_at`.
- `intent_digest` is a SHA-256 over a canonical encoding of the conversation id, the trimmed
  content, the sorted attachment ids and the reply target.
- Exact outcomes for a send whose key already has a receipt:

  | Receipt state | Intent | Message still present | Response |
  | --- | --- | --- | --- |
  | `committed` | digest equal | yes | `200` with the committed message (idempotent replay) |
  | `committed` | digest equal | no (deleted or purged) | `409` "this send was already committed and its content is gone"; never recreates |
  | `committed`/`deleted` | digest different (including a different conversation) | either | `409` "that message key was used for a different message" |
  | `committed` | empty (legacy receipt from the migration), same destination | yes | `200` with that message |
  | `committed` | empty (legacy receipt) | no | `409`, never recreates |
  | compacted spent key | any | no | `409` expired; the original key is never new work |

  The case "different conversation under the same key" is therefore a conflict by intent
  comparison, not by a silently scoped composite key, which is exactly what review 6 asked for.
- A receipt is written **in the same transaction** as the message it acknowledges, so a
  committed send always has its receipt and a rolled-back send has neither. The message table's
  existing `UNIQUE(user_id, idempotency_key)` stays as a second guard.
- Legacy messages get a receipt with an empty digest at migration time, so a retry of an old key
  can never recreate deleted content either. The empty digest means "intent not recorded": a
  replay first checks that the recorded conversation equals the requested destination, then is answered from the message itself; a different destination or missing message is a non-recreating conflict. Nothing is inferred from content that no longer exists.

## Decision 3 — Expiry and admission

- **Server-enforced expiry.** `expires_at` is `created_at + 24h` (the accepted automatic retry
  window). While a receipt exists and its message survives, a replay is answered from the
  message regardless of age. Expiry decides what happens when the message **no longer exists**:
  inside the window the outcome is `409` "committed and since deleted"; outside it the outcome is
  `409` "that send has expired — your text can be recovered and sent again with a new key". The
  two are distinct outcomes so a client can act correctly, and neither recreates content.
- **Receipt compaction, never key reuse.** After a deleted receipt is at least 30 days old,
  hourly maintenance clears its destination, digest and timestamps, retaining only the
  account/key and terminal deleted state. Each pass compacts at most 1,000 records. Arbitrary
  legacy keys cannot prove when the original request was created, so deleting the guard would
  let a delayed request recreate deleted content. The minimal spent-key record therefore lives
  until the account is erased or removed. Live-message receipts remain replayable. Receipts
  contain no bodies, but the count of spent keys can grow with the number of deleted sends;
  the batch limit bounds maintenance work, **not total lifetime database storage**. A future
  versioned, authenticated operation-age protocol would be needed to safely expire these guards.
- **An expired operation is never silently new work.** The client stops automatic retries at
  24 hours and offers the unsent text for an explicit new send with a new key (Decision 5). If a
  late retry arrives anyway, the receipt answers it as expired/different rather than creating a
  message, so a delayed original request cannot be mistaken for new intent.

## Decision 4 — Migration mechanics (M-C), exactly

Schema 6 is one transactional migration:

1. Create `conversations` (id, kind, channel_id unique, state, created_at, updated_at) and
   `conversation_members` (conversation_id, user_id, state ∈ {pending, accepted, left,
   removed}, invited_by, joined_at, updated_at, primary key (conversation_id, user_id)) with the
   membership index. `conversations.channel_id` references `channels(id) ON DELETE CASCADE`, so
   deleting a legacy channel removes its conversation and memberships with it.
2. Insert exactly one `kind='channel'` conversation per channel, `id = channels.id`. This is
   idempotent against a repeated attempt (`INSERT ... WHERE NOT EXISTS`).
3. `ALTER TABLE messages ADD COLUMN conversation_id TEXT` (nullable, no foreign key at this
   stage), then `UPDATE messages SET conversation_id = channel_id WHERE conversation_id IS
   NULL`. **Rows are updated, not recreated**: `id`, `rowid`, `created_at`, reply and attachment
   references are untouched, and because `content` does not change, no FTS trigger fires and the
   external-content index stays valid. The earlier note's claim of "no row rewrite" was wrong;
   this ADR states the real behaviour and bounded cost (one indexed update per message).
4. Add `messages_conversation ON messages(conversation_id, id DESC)` and
   `message_operations` with its primary key, expiry index and the `messages` `AFTER DELETE`
   trigger that marks the matching receipt `state='deleted'`, `result_message_id=NULL` in the
   same transaction as the erasure. Backfill one receipt per existing message with an empty
   intent digest (Decision 2).
5. The transactional migration is followed by the existing postconditions
   (`db.Migrate`): the search index is rebuilt and reference integrity is verified before the
   installation serves. A failed or interrupted attempt rolls back completely.
6. **`channel_id` stays `NOT NULL` and `conversation_id` gains no foreign key in this stage.**
   Tightening either — making `channel_id` nullable for direct/group conversations, adding the
   `conversation_id` foreign key, or moving `NOT NULL` onto it — requires a table rebuild, and a
   rebuild **cannot** run through the current migration transaction: SQLite ignores `PRAGMA
   foreign_keys` inside a transaction, and `DROP TABLE messages` with foreign keys enabled
   performs an implicit delete that cascades into replies, reactions, pins, mentions and
   attachments. The later stage must therefore run with `PRAGMA foreign_keys=OFF` set *outside*
   the transaction, copy rows with an explicit `rowid` (`INSERT INTO messages_new(rowid, id,
   conversation_id, channel_id, …) SELECT rowid, id, … FROM messages`), rename, and rebuild
   `message_fts` (the existing postcondition does this), and it is not part of schema 6. Schema 6
   is additive and reversible by restoring the pre-upgrade archive, as ADR 0002 defines.
7. **Lifecycle prerequisite.** D-08 (account deletion/tombstone policy) and the account-deletion
   schema must be recorded before any direct/group conversation is enabled; WP-12's later tools
   cannot supply a missing privacy prerequisite retroactively. Schema 6 accounts for it by
   referencing `users(id) ON DELETE CASCADE` from `conversation_members` and receipts, so an
   erased account cannot leave a readable membership or an orphaned receipt behind.

## Decision 5 — Drafts, outbox and recoverable text

- **Opt-in persistence.** The outbox exists only when the account has explicitly enabled
  device persistence for unsent text. Nothing is written to durable device storage otherwise,
  and passwords, session tokens and attachment bytes are never written.
- **Keys are stable and scoped:** `(account_id, conversation_id, operation_key)`. A scope change
  or an account switch on a shared device cannot restore another account's or another
  conversation's text, and a restored draft is verified against the account and destination
  before it is rendered.
- **Bounds:** pending and recovery entries share a 50-record, 64 KiB UTF-8 text budget per
  account. Automatic retry ends at 24 hours; an expired pending entry becomes text-only recovery
  for up to 24 further hours from its first display. Complete intent is validated before a retry:
  malformed text, attachments or reply targets are rejected rather than truncated into a changed
  send. Entries outside the count/byte/age bounds are evicted oldest first; this is recovery help,
  not a durable personal archive.
- **Three outcomes are kept apart.** (a) *Automatic retry cancellation*: a send that fails for a
  reason the server cannot accept (conflict, expired, revoked destination, unavailable
  attachment) stops being retried immediately. (b) *Server-content removal*: content revoked by
  access loss is removed from the retry payload; the client never re-claims an attachment the
  server has released. (c) *Recoverable user-authored text*: the unsent text itself is preserved
  with an actionable explanation ("you no longer have access to this conversation", "this send
  expired") so the user can copy it or send it somewhere they may write. Losing access to a
  destination never silently discards what the user typed, and never keeps retrying it.
- Recovery is shown in a separate **Recovered messages** panel with **Copy** and **Dismiss**.
  It never replaces the active draft, chooses a new destination or sends by itself. Refused
  operations lose their attachment/reply claims and stop retrying even when persistence is off;
  their text remains in memory for the current account. With opt-in, it can survive a reload.
- **Acknowledgement is what clears a pending entry**; sign-out clears the whole account scope
  on that device. Turning persistence off removes saved records. Preferences are account-scoped;
  a second account on the same browser must make its own choice.

## Decision 6 — Read cursors

- A cursor is per `(account, conversation)` and **advances monotonically**: a stale or
  out-of-order response can never move one backward. The server applies `max(existing, requested)` atomically, so an older request succeeds as a no-op without moving the cursor backward. Current session and channel-read authority are checked in that same transaction.
- A cursor advances only when the relevant messages were actually visible under the agreed
  focus rules; opening an inbox, a background tab, a history window or another thread does not
  mark unseen content read.
- On reconnect the client reconciles from its last cursor and the server's snapshot: events
  older than the snapshot are ignored, and the unread count is recomputed only from a confirmed
  snapshot, never from an unconfirmed one (generalizing the existing `unreadFor` withholding
  contract). Resource versions travel with events so a stale event cannot resurrect deleted
  content.

## Compatibility and rollback

- Channel behaviour is unchanged: same ids, links, history, drafts keyed by channel id, search
  and unread semantics. `conversation_id` is written for every new channel message and read
  through `COALESCE`, so a row written by an older binary (before the migration) is still
  readable and a row written by schema 6 is readable by the same readers.
- No backward schema migration is promised (ADR 0002): rolling back means restoring the
  pre-upgrade archive and the previous binary. A receipt table and an extra nullable column are
  additive, so a restored pre-upgrade archive simply has neither.
- Direct/group surfaces stay disabled until D-08 (ADR 0011) is ratified for private history, WP-11's consent/blocking contract
  exists and WP-08's privacy gate passes.

## Scenario-to-test map

| Scenario | Where it is proved |
| --- | --- |
| Legacy schema-5 fixture upgrades additively; ids, rowids, replies, attachments and FTS intact | `internal/db/migration_test.go` (schema 6 fixture), `internal/api/conversations_test.go` |
| Channel send writes `conversation_id = channel_id`; readers resolve `COALESCE` | `internal/api/conversations_test.go`, `internal/db/messages.go` |
| Lost acknowledgement → message deleted → retry: no replacement message, attachment claim or notification | `internal/api/message_receipts_test.go` |
| Same key, different conversation or content: `409`, no second message | `internal/api/message_receipts_test.go` |
| Receipt older than the retry window with no surviving message: distinct expired outcome, no recreation | `internal/api/message_receipts_test.go` |
| More than 1,000 old receipts compact in bounded passes, keep spent keys, strip metadata, and refuse replay forever | `internal/db/receipts_test.go`, `internal/api/message_receipts_test.go` |
| Account removal and erasure remove memberships and receipts; preserved posts keep a removed identity | `internal/api/account_lifecycle_test.go`, ADR 0011 |
| Reconnect/stale-cursor reconciliation | `web/tests/workspace-state.test.mjs` (a cursor advances only where the server confirmed it, an older window sends nothing, a refused mark is offered again); read-cursor monotonicity in `internal/api`; `web/e2e/privacy.spec.mjs` (an unconfirmed send survives a reload) |
| Direct/group creation, consent and blocking | WP-10/WP-11 (not implemented here) |

## Consequences and what this does not claim

- The first slice is deliberately channel-based. It proves the shared model while retaining the
  current chat behaviour; it is not a DM feature and carries no private-call promise.
- `conversation_members` exists in schema 6 as a prerequisite for later direct/group work.
  Account lifecycle cleanup can remove its rows; no creation, invitation or private-history
  feature is exposed by this table alone.
- The client outbox and read-cursor reconciliation are implemented against the server contract
  above: `web/src/lib/outbox.ts` holds the opt-in, account-scoped, bounded device scope and the
  outcome classification, and `web/src/lib/workspace.svelte.ts` restores each entry, verifies its
  destination against the snapshot, reconciles it through the same operation key, recovers the
  author's text when an outcome ends the operation, and advances a read cursor only where the
  server confirmed it. Bounds, scoping, reconciliation and the cursor rule each have regressions,
  and the reconciled retry is a rendered journey. Attachment ids are held to the same shape as
  other identifiers and a recovered text keeps its wording rather than an attachment claim.
- Independent review (WP-28) and both-host lifecycle evidence (WP-24) remain external
  dependencies, as the completion reviews record.


## Re-checked: a second thread is a conflict, and that is the decision

A later handoff listed this as a defect — "`sendIntentDigest` omits thread identity;
identical content/key sent to another thread replays the wrong message" — and proposed
putting the destination into the key. It was implemented as migration 25 (rebuilding
both `message_operations` and `messages` with the thread in their unique keys) and the
proposed test passed against it.

It was then reverted, because the existing test `TestAThreadIsPartOfTheSendIntent` had
asserted the opposite all along, with a comment explaining exactly why, and because
Decision 2 above is the approved answer:

> The destination is *not* part of the key; it is part of the **intent** … The case
> "different conversation under the same key" is therefore a conflict by intent
> comparison, not by a silently scoped composite key, which is exactly what review 6
> asked for.

Three reasons the composite key is wrong, and none of them are visible in a test that
only sends twice:

- **It cannot express the conflict.** The whole point of the receipt is that reusing a
  key for other work is *refused with a reason*. Scoping the key by destination turns a
  refusal into a second delivery, and the client's retry machinery — which keeps the key
  precisely so a retry reconciles rather than duplicating — loses the ability to tell a
  lost acknowledgement from a second send.
- **It weakens the legacy guard the comment describes.** `TestAThreadIsPartOfTheSendIntent`
  notes that `UNIQUE(user_id, idempotency_key)` is the second of two guards and "the
  digest is the durable guard; the index is the accident that has been catching the
  everyday case." Making the index carry the thread keeps the everyday case and leaves
  the durable guard doing the same work twice, in two places, which is the drift this
  file exists to prevent.
- **The evidence for the defect was mine.** The replay I observed — a 200 carrying the
  first thread's message for a send into the second — was produced by removing the
  digest's thread from the *comparison path* in the test harness I had just written,
  not by the shipped code. The shipped code answers that case 409, which is correct.

**What is actually load-bearing here** is the pairing the existing test already
documents: the digest carries the thread, so a receipt survives the deletion of the row
the index was guarding, and a retry after deletion reconciles against the receipt
instead of recreating content. That is a real, tested property. "Two threads, one key"
returning a conflict is the approved behaviour, not a gap.
