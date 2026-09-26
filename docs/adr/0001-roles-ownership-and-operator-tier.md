# ADR 0001 — Roles, ownership and the non-delegable operator tier

**Status:** Implemented in schema 3, restricted-channel exposure still gated
**Date:** 24 September 2026
**Plan reference:** D-06, D-07, WP-06, WP-07, WP-08 (partly), §5.2
**Deciders needed:** OWNER (ratify the operator-tier boundary and the
manager-visibility disclosure), TL, SEC

## Context

Before this change, authority was one boolean per account (`users.is_admin`) and
every channel was shared with every active member. That model cannot express
"this channel is for these people", and it cannot separate community moderation
from powers that defeat a privacy boundary: an administrator could download a
whole-server backup or set another account's password, so any claim that an
administrator cannot read a private conversation would have been false.

## Decision

1. **Additive roles plus explicit channel grants.** `roles` holds capability
   sets with a `position` used for delegation ordering. An account's authority is
   the union of the roles assigned to it and the default role. There is no
   inherited allow/deny override algebra in v1.
2. **The default role applies to everyone and is not deletable.** An account with
   no assignment still resolves with ordinary member authority, so no code path
   can leave an account with undefined authority by forgetting an assignment.
3. **One transferable owner.** `users.is_owner` is unique by partial index.
   Ownership is never inferred: migration 3 designates nobody, and the first
   owner is chosen by an explicit ceremony (`POST /api/owner`, an administrator
   before any owner exists, only the owner afterwards).
4. **The operator tier is not delegable.** `operator.backup`,
   `operator.credential_recovery` and `operator.ownership_transfer` are refused by
   `CanGrantCapabilities` unless the actor is the owner, so a custom role cannot
   be used to hand them out. Whole-server export and chosen-password takeover now
   require them.
5. **Compatibility bridge while no owner exists.** An upgraded installation has
   administrators and no owner, so administrators keep exactly the operator powers
   they had before this change. Designating an owner closes that bridge for
   delegated administrators. This is what makes stage M-A's promise — "all
   existing admins retain intended authority" — true without silently creating an
   owner.
6. **Delegation only flows downward.** A role manager may act on roles strictly
   below its own position, may grant only capabilities it holds, and cannot remove
   the last active administrator or the last owner.
7. **Manager visibility is disclosed, not hidden.** Per §5.2, an account with
   `channel.manage` may read every community channel, including restricted ones.
   The access inspector reports `managers_can_read: true` and lists the effective
   members, so the product never implies a confidentiality it does not provide.
8. **Legacy `users.is_admin` and the administrator role move together**, in both
   directions, inside the transaction that changes them, with the authorization
   revision bumped in the same commit.

## Consequences

- Every route now declares a capability or an explanation; the inventory test
  fails if a new endpoint is added without one.
- A delegated administrator no longer inherits export or password-takeover power
  once an owner is designated. Installations that relied on every administrator
  having those powers must designate an owner, or keep the compatibility bridge by
  leaving ownership unset.
- Member MFA enrollment still requires an administrator. That is a deliberate,
  recorded remnant of the pre-role model: replacing it is WP-05, and the policy
  test pins the exact two routes so it cannot spread.
- The owner and the host operator remain trusted with the storage itself. This is
  not E2EE and does not claim to be.

## Outstanding

- Custom-role management UI, invitation role selection, and the "who can access
  this?" presentation in the client are not built yet (WP-07 remainder).
- Moderation capabilities exist in the catalog, but timeouts, reports, slow mode
  and audit-failure policy remain WP-15.
- The plan's full review of DM membership rules (D-07) applies to conversations
  that do not exist yet (WP-09–12).
