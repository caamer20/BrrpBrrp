# ADR 0012 — Private conversations, consent and shared storage

**Date:** 26 September 2026  
**Status:** implemented as an opt-in alpha.6 slice; independent privacy acceptance remains open  
**Schema:** 11–12; backup format 2

## Storage and migration

Schema 11 rebuilds `messages` with explicit rowid copying, a required conversation foreign key, and a nullable channel projection. A channel message must project to its matching channel conversation; a direct/group message must have no channel projection. Triggers reject inconsistent inserts, updates and descriptor reclassification. No shadow channels or separate private-message table exist.

The migration pins one connection and changes foreign-key mode outside its transaction. It validates references before commit and restores enforcement before releasing the connection, including cancellation and SQL failure. The existing durable upgrade marker covers the remaining FTS rebuild and verification; opening an interrupted committed upgrade completes its postconditions. Rollback uses the previous executable and its pre-upgrade archive.

Schema 12 adds canonical unordered direct pairs, request state, blocks, contact preferences, per-account hidden/read state and destination-bound staged uploads. Pair identifiers deliberately survive physical account deletion; they identify history and never grant authority. Accepted membership and a currently active identity authorize reads. No administrator/owner role bypasses private membership.

## Consent and contact

A direct conversation begins with a request containing at most 500 Unicode characters. Only its recipient may accept. The recipient can reject or the requester can withdraw a pending request. Simultaneous opposite requests share one canonical pair without implicitly consenting. Before acceptance there is no message history, file claim or call.

Defaults are conservative implementation choices under the continuing development authorization: requests from community members are permitted unless the recipient opts out; five pending outgoing requests, twenty pending incoming requests, ten newly requested pairs per rolling day, and a 24-hour rejection cooldown. Limits and request state survive restart. Existing accepted history can be reopened without granting a new contact request.

Blocking in either direction makes an accepted conversation read-only. It prevents sends, edits, reactions, scoped uploads/claims, new requests and unsolicited contact events. Own-content deletion remains available, and necessary deletion updates still reach eligible history readers. Unblocking never automatically resends old operations. A block does not remove either person's access to shared community channels. Hide preferences are personal and are preserved until explicitly changed.

## Shared message behavior

DM history, replies, context, reactions, pins, editing, deletion, read cursors, idempotency receipts and outbox recovery use the community message pipeline with conversation authorization. Search inside a DM is explicitly scoped; global search remains community-only. Files are staged for a destination, claimed in the send transaction, and read through current conversation membership. Unscoped/community uploads cannot be silently claimed into a DM. Read cursors remain monotonic and private.

Private pin/deletion activity is excluded from the community audit feed. DMs do not enter the community report queue, and private URL previews are not fetched externally. This slice provides live message events and unread counts; dedicated DM push/mention policy, selected-evidence private reports, private calls and groups remain required future work.

Account removal cancels pending requests and clears introductions authored by the removed identity. Accepted history remains accessible to the surviving accepted member; deleted identities cannot write or regain membership. Physical erasure removes that person's messages/files under the existing lifecycle policy. Backups retain their own snapshots. The server operator controls the database and media; this is not end-to-end encryption.

## Exposure and evidence

`BRRP_ENABLE_PRIVATE_CONVERSATIONS` defaults to false. The API and workspace capability flag enforce it, including on retained message/file URLs. Opt in on a test installation to exercise the alpha interface; local regression success does not grant production or independent review acceptance.

Migration fixtures cover exact rowids, relationship/FTS/receipt preservation, rollback, cancellation and reopen recovery. API tests cover consent, durable limits, participant-only reads/files/search/events, operator denial, cross-destination claims, blocked in-flight writes, retained history and peer erasure. Browser tests use two real signed-in members plus outsider/owner controls, scoped uploads, held sends and explicit recovery. Consult the release record for final executed results.

## Note of 6 October 2026 — the switch now defaults to on

This record is left as it was written. What it says about the default is superseded: from
1.0.0-rc.1, private conversations are on unless an operator sets the variable to `false`, and an
installation that had set it keeps what it set. The owner made that decision on 6 October
2026. It was **not** made on the evidence this record asked for — no independent review has
run — and the documents say so wherever they describe the feature
(`docs/known-limitations.md`, `docs/admin.md`, `docs/upgrading.md`). The enforcement this
record describes is unchanged.
