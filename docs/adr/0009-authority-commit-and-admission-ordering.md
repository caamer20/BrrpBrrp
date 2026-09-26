# ADR 0009 — Authority commit and admission ordering

- **Status:** accepted
- **Date:** 25 September 2026
- **Plan reference:** WP-06, WP-08, §5.4; closes review 6 findings R6-01 and R6-02.
- **Deciders needed:** TL (protocol), SEC (independent acceptance of the evidence).

## Context

The authorization revision is the linearization point for admitting new reads, mutations,
streams and events (§5.4). Review 5 introduced a delivery fence (`Server.authority`) under
which a revision-changing commit and the hub invalidation it produces happen as one step,
while a socket writer decides admission under the read lock. Two defects remained:

1. **R6-01 — a lock-order cycle.** `commitAuthorityChange` ran with a live transaction (which
   holds the store's only connection) and then took the fence. The standalone invalidations
   (`invalidateWorkspace`, `publishAuthorizationChange`) took the fence and then read the
   committed revision through the store. Two operations therefore waited on each other in
   opposite orders, and only request cancellation broke the cycle.
2. **R6-02 — credential and session commits bypassed the protocol.** A password reset (and
   every other credential/session transition) committed its session deletions with a plain
   `tx.Commit()` and only afterwards disconnected sockets and published an invalidation. A
   queued private event could be admitted in that gap, and a socket that registered in that
   gap was never invalidated at all.

## Decision

**One acquisition order: the store's connection first, the authority fence second. Nothing
waits for the store's connection while holding the fence.**

The protocol has three shapes, all of them following that order:

1. **Committing change.**
   1. begin the write transaction (takes the connection),
   2. prepare the change, re-establish authority inside the transaction, read the revision
      through the transaction,
   3. take the fence, commit the transaction, record the committed revision on the change's
      audience in the hub (`commitAuthorityChange` / `commitAuthorityChangeFor`), release the
      fence,
   4. run the slow follow-ups **after** the fence is released: ending file/preview streams,
      enforcing voice access, disconnecting sockets, clearing cookies.
2. **Standalone invalidation** (`invalidateWorkspace`, `publishAuthorizationChange`), which
   has no transaction: read the committed revision first
   (`authorityRevisionBeforeFence`), then take the fence and invalidate. The read is a floor,
   never a ceiling: `ObserveRevision` only moves forward, and a change that commits between
   the read and the fence records its own newer revision under the fence itself. The refetch
   signal is queued at the revision the connection ends up on, so it is never refused by the
   admission rule it exists to satisfy.
3. **Connection registration** (`registerConnection`): re-establish the account and its
   session from the store inside a read transaction, take the fence, commit that transaction,
   insert the connection into the hub with the revision it was admitted under, release the
   fence. A change that commits before the check is observed by it (the caller is refused); a
   change that commits after insertion finds the connection and invalidates it. There is no
   window in which a connection carries authority that has been withdrawn and has not been
   told.

**Invariant:** nothing in the critical section performs a network write, waits for another
connection, or reaches the store through anything but the transaction the caller already
holds. The store holds a single connection, so a committing transaction and a
connection-holding registration cannot be in flight at the same time; a fence holder either
already holds the connection (a commit) or needs nothing from the store (an invalidation).

**Post-commit refusal and pre-commit admission.** A writer either takes the fence's read lock
before the critical section — deciding under the authority in force before the commit, a
legitimate linearization before it — or after it, in which case its event's generation is
stale and it is refused. A change never retracts bytes already written; that boundary is
unchanged.

## Transition inventory

Every transition that moves authorization, credentials or session validity commits through
one of the two helpers. The audience is the set of connections whose delivery eligibility the
change ends; the slow work listed in the last column runs after the fence.

| Transition | Route / handler | Commit helper | Invalidation audience | After the fence |
| --- | --- | --- | --- | --- |
| Channel access / visibility | `PUT /api/channels/{id}/access`, `updateChannel` | `commitAuthorityChange(before ∪ after)` | affected users | `deliveries.revoke`, `enforceVoiceAccess` |
| Channel removal | `DELETE /api/channels/{id}` | `commitAuthorityChange` (workspace) | every connection | `deliveries.revoke`, `voice.CloseChannel` |
| Role create / update / delete / assign | `/api/roles/*`, `PUT /api/admin/users/{id}/roles` | `commitAuthorityChange(holders)` | effective holders | `deliveries.revokeAccount`, `enforceVoiceAccess` |
| Administrator gain/loss | `PATCH /api/admin/users/{id}` | `commitAuthorityChange(target)` | the target account | `deliveries.revokeAccount`, `enforceVoiceAccess` |
| Ban / deactivate | `PATCH /api/admin/users/{id}` | `commitAuthorityChange(target)` | the target account | `revokeAccount`, `leaveVoice`, `Disconnect` |
| Account deletion | `DELETE /api/admin/users/{id}` | `commitAuthorityChange(target)` | the target account | `revokeAccount`, `leaveVoice`, `Disconnect` |
| Operator password reset | `POST /api/admin/users/{id}/reset-password` | `commitAuthorityChange(target)` | the target account | `clearCookie`-equivalent, `leaveVoice`, `Disconnect`, `revokeAccount` |
| Operator factor removal | `DELETE /api/admin/users/{id}/mfa` | `commitAuthorityChange(target)` | the target account | as above |
| Session kick | `POST /api/admin/users/{id}/kick` | `commitAuthorityChange(target)` | the target account | as above |
| Password change | `POST /api/me/password` | `commitAuthorityChangeFor(UserIDs)` | the account | `clearCookie`, `leaveVoice`, `Disconnect`, `revokeAccount` |
| Sign out | `POST /api/logout` | `commitAuthorityChangeFor(SessionHashes)` | that session only | `clearCookie`, `leaveVoice`, `Disconnect(session)`, `revokeSession` |
| Revoke one device | `DELETE /api/me/sessions/{id}` | `commitAuthorityChangeFor(SessionHashes)` | that session only | `Disconnect(session)`, `revokeSession` |
| Revoke other devices | `DELETE /api/me/sessions` | `commitAuthorityChangeFor(UserIDs)` | the account's connections | `Disconnect(user)`, `revokeAccountExceptSession` |
| Factor enrollment / removal | `POST /api/me/mfa/enable`, `/disable` | `commitAuthorityChangeFor(UserIDs)` | the account | `Disconnect(user)`, `revokeAccountExceptSession` |
| Ownership ceremony / transfer | `POST /api/owner` | `commitAuthorityChange` (workspace) | every connection | — |
| Category / channel ordering | `PATCH/DELETE /api/categories/*`, `POST /api/channels/{id}/move` | plain commit, then `invalidateWorkspace` | every connection | — |
| Sign-in (new session) | `POST /api/login`, `POST /api/invitations/accept` | plain commit | none (the new session has no connection; registration re-verifies) | — |
| Socket registration | `GET /ws` | `registerConnection` (read transaction + fence) | — | — |

Deliberately outside the fence: file and preview streaming through `deliveries`, voice
enforcement through `voiceGate`, socket disconnection, cookie clearing and every network
write. A delay in any of them can no longer reopen the window the finding lived in.

## Consequences

- A committing transaction never waits for the fence while another path waits for the
  connection: the cycle is gone, and ordinary completion no longer depends on cancellation.
- Credential and session changes have the same guarantee as role changes: a committed reset,
  kick, ban or sign-out refuses new admission on exactly the connections it ended, while
  unaffected sessions and users continue.
- The revision lookup for a standalone invalidation is outside the fence, so it may name a
  revision one step behind a concurrent change. That is harmless by construction:
  `ObserveRevision` is forward-only and the refetch signal is tagged with the connection's
  own resulting revision.
- Registration costs one read transaction per socket connection. Connections are rare
  compared with requests, and the transaction is a pure read that releases the connection on
  every path.

## Evidence

- `internal/api/commit_ordering_test.go`
  - `TestAStandaloneInvalidationNeverWaitsForTheDatabaseUnderTheFence` (R6-01). Fails before
    the fix with "a standalone invalidation holds the authority fence while waiting for the
    store's connection" and a commit that completes only by cancellation.
  - `TestACredentialCommitRefusesAQueuedWriteOnTheRevokedSession` (R6-02). Fails before the
    fix with "a revoked session was handed a newly admitted private payload"; the same test
    carries the permitted controls (the other member of the restricted channel receives its
    event during the window, the reset answers success, the new credential signs in and
    reads the channel).
  - `TestASocketRegisteredAfterItsSessionEndedIsRefused` (R6-02 registration). Fails before
    the fix with "a connection whose session had ended was admitted and received hello"; the
    control dials with a session that did not move and receives the ordinary hello frame.
- Preserved regressions: `coherent_audience_test.go`, `revocation_boundary_test.go`,
  `voice_privacy_test.go`, `sessions_test.go`, `mfa_test.go`, `privacy_test.go` — channel,
  role, session-stream, voice-presence and restricted-surface behaviour is unchanged.
- Not covered here: independent review (WP-28) and the two-host lifecycle evidence (WP-24).
