# ADR 0014 — Pending-request evidence, request-attempt identity and command receipts

**Date:** 28 September 2026  
**Status:** implemented in the alpha.7 slice (migration 14); independent privacy acceptance remains open  
**Schema:** 14; backup format 2 (unchanged — a schema 14 database is still format 2)

This records the contract for recipient-only pending-request reporting, immutable
request-attempt identity, the atomic report-and-block operation and its durable
receipts **before** any schema, route or interface is written. It extends
[ADR 0013](0013-private-message-evidence-and-request-discovery.md), which governs
accepted-message evidence; where the two overlap the stricter rule applies.

## What this adds, and what it deliberately does not

A member who receives an unwanted request gains **Report request and block**: they see
the exact introduction the server holds, are told exactly what will be saved and who
can read it, confirm, and in one operation the evidence is stored, the request is
rejected, its introduction is cleared and the sender is blocked.

It does **not** accept the request, create a message, grant membership, enable files
or history, or open any conversation. A second report-only action for pending
introductions is outside this slice. A report that does not also block stays out of
scope: combining the disclosure with an authority change is the hard case, and doing
that once is worth doing once.

## A canonical pair is not a request attempt

`direct_requests` reuses its row when a pair requests again, so a pair identifier
cannot distinguish "the request alice is looking at now" from "the request that existed
last month, before it was rejected, the pair went quiet, and alice asked again".

A report bound only to the pair would therefore be able to capture the wrong
introduction after a renewal. Schema 14 adds a durable, immutable **attempt identity**:

- Every pending request row carries `attempt_id`, rotated on each new attempt.
- Existing pending rows are backfilled with distinct attempt identifiers, so an
  upgrade never produces two rows that look like the same attempt.
- Preview and the operation are bound to the exact attempt. An old attempt is refused
  rather than resolved against whatever the pair says now.

Renewal after rejection is therefore unambiguous: a new attempt means new evidence,
and a retry of the old operation still returns its own historical receipt.

## Evidence shape and source kinds

Private evidence gains an explicit **source discriminator** and a nullable request
reference beside the message reference. A record is exactly one of:

| Source kind | Holds | Never holds |
| --- | --- | --- |
| `message` | the reported message's bounded text, author and reporter | any request context |
| `request` | the server-held introduction, the sender, the reporter, the attempt, the time it was sent | any message, history, files or membership |

A request record stores the conversation only as internal provenance, exactly as a
message record does, and that identifier is **omitted from the reviewer-facing DTO**:
a reviewer sees that the source was a request and who sent it, never a conversation to
open. No synthetic message is created and no existing message id is fabricated.

**Empty introduction.** A request may legitimately carry an empty introduction; the
field defaults to `''` and the API accepts it. The disclosure says so explicitly
("this request has no introduction") rather than rendering an empty box that could be
mistaken for a failure. Text is never fabricated and never taken from a browser cache
after ordinary reject, block, withdraw or account cleanup has erased the source.

## Recipient-only authorization

Preview and confirmation require the authenticated, active **recipient of that exact
current attempt**. The sender, an unrelated member, a non-participating owner and a
reviewer are all refused with one indistinguishable non-disclosing result. Holding
`report.review_private` is not a substitute for being the recipient: a reviewer can
read submitted evidence and cannot create it.

The action is available to a pending recipient who has **no accepted-history access**,
which is why it does not go through the accepted-conversation read check.

## One transaction, or nothing

The whole operation commits together or not at all:

1. Re-establish authentication, the private feature policy and account ownership.
2. Resolve an already-committed receipt for this operation key, if any — **before**
   requiring a still-pending source.
3. Otherwise resolve the exact attempt and recipient, and compare the confirmed
   preview version with current server state.
4. Apply the shared private-evidence rate, capacity and input bounds.
5. Save immutable introduction evidence and the command receipt.
6. Insert the block, reject the request and clear its introduction.
7. Write the required restricted audit entry.
8. Commit through the existing authority protocol and publish invalidations after.

Any failure in evidence, block, rejection, audit or commit rolls back everything: no
report, no quota consumption, no block, no rejection, no receipt. **Ordinary Block
stays available** when reporting is full, rate-limited or unavailable, and the
interface says the combined action did not complete rather than implying it did.

If accept, withdraw, plain block or account removal wins the race first, a later
confirmation is refused without capturing client text. If report-and-block wins
first, a later acceptance must fail. Unrelated conversations and users are untouched.

## Receipts are operations, not open reports

Report-and-block deliberately destroys its own source, so deduplicating by "does an
open report exist" cannot express retry. A stable **account-owned operation key**
carries the attempt and the full intent — the action and the explanation — bound
together:

- A lost response or a concurrent retry returns the original minimal receipt with no
  second evidence record, no second quota charge and no repeated block.
- The same key with a **different intent is a conflict**, not a silent overwrite.
- Another account cannot read or replay the receipt.
- Resolving or reopening the evidence does not release the operation identity.
- A later deliberate unblock stays effective: **a replay must not block again.**
  The receipt reports the historical outcome and never asserts that the person is
  currently blocked — current state comes from a fresh read.
- An old preview or operation never affects a later request in the same pair.
- The first success keeps the original evidence and explanation; retries cannot
  overwrite them.
- Expiry and cleanup never turn an old action into new work, and a receipt never
  exposes expired text. Receipts outlive the evidence they describe by a bounded
  window, defined below.

## Own-outcome reconciliation

An ordinary reporter has no access to the reviewer queue, so uncertainty must not be
answered by telling them to open one. A narrow **own-receipt check** returns only the
calling account's minimal outcome for its own operation key: whether it committed,
when, and what it did. It never returns reviewer notes, other reporters, evidence
bodies or any review data, and it never grants the reviewer capability.

The reporter's dialog offers "check what happened" for the operation it started. A
replay returns that historical outcome; it does not re-block, and it says so plainly
when the block was since lifted deliberately.

## Retention, stated separately for each thing

Retention is specified per record rather than implied, because the three durations
are genuinely different and conflating them would over-promise:

- **Evidence**: available 90 days from creation; resolve and reopen cannot extend it;
  reclaimed by bounded maintenance.
- **Restricted review audit metadata**: retained for **400 days**, independent of the
  evidence it describes. An audit record is administrative accountability, so it
  outlives the evidence; deleting it with the evidence would erase the only record
  that a review happened. It is readable only under the evidence policy and is cleared
  when the acting account is physically erased, never on tombstoned removal.
- **Command receipts**: retained in full for **30 days** from the operation, then
  **compacted to a spent guard**. The key stays reserved for the account's lifetime —
  deleting the row would let a late retry become new work — but the payload is not kept
  for that lifetime: the outcome, the evidence identity, the source, the attempt, the
  reporter and the subject are cleared, and `outcome` becomes `spent`, which the replay
  path reads as the defined expired result. A guard row is therefore a statement about
  the *key* ("this was used"), never about the operation it performed.
- **Request evidence** is reclaimed on the same 90-day window as message evidence. A
  reader loses access to it long before the bytes go, and a submitted introduction
  that outlives its stated window is a retention promise quietly not being kept.
- **Backups**: outside all three. A backup retains whatever it captured; the operator's
  copy is not rewritten when a record expires, and the interface never implies
  otherwise.

## Interface state lifetimes

Four different things must not be conflated, because conflating them is how a person
ends up re-consenting to something or losing their own words:

- **Restricted preview** — fenced by current authority and the exact attempt; discarded
  when either moves.
- **Explanation and operation identity** — owned by the account and session; survives
  unrelated refreshes and the report's own authority invalidation.
- **Receipt and outcome** — the original account's reconciliation of that operation.
- **Current block and request state** — always freshly fetched, never inferred from a
  historical receipt.

An operation's own authority invalidation can arrive before its HTTP response. That is
expected, not failure: the dialog keeps the explanation, reconciles **the same
operation**, and never silently substitutes a different report or repeats a block.

The reviewer queue gains clear **Message** and **Request introduction** source labels
and no navigation to any original conversation.

## Consequences

This makes request-attempt identity a first-class durable fact rather than an
inference from a pair, and it makes the combined action's retry semantics independent
of the evidence it creates. In exchange, a request report cannot be separated from its
block, and an audit record outlives the evidence — both deliberate, and both stated
here so the trade is visible rather than discovered.

Not claimed: full activity history, groups, private calls, screen sharing, named
threads, or independent security and privacy review. No G0–G8 gate is passed.
