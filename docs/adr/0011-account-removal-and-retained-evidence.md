# ADR 0011 — Account removal and retained evidence (D-08)

- **Status:** implemented for community channels; private-history policy requires owner ratification before DMs/groups ship
- **Date:** 25 September 2026
- **Schema:** 7; report retention introduced in schema 9
- **Related:** [conversation contract](0010-conversation-model-receipts-outbox-and-cursors.md), [community guide](../community-guide.md)

## Decision

There are three separate account actions. Their names and consequences must be visible before confirmation.

| Action | Identity and sign-in | Community history | Reversible |
| --- | --- | --- | --- |
| Deactivate | Keeps the profile; ends active access | Keeps posts and files | Yes, by an authorized manager |
| Remove account, keep messages | Replaces the profile with **Deleted member**, removes credentials, factors, sessions, personal preferences and grants | Keeps posts, attached files and other people's replies | No |
| Erase account and messages | Physically deletes the account | Deletes its authored messages and claimed attachments; other authors' replies remain | No in-app recovery |

Removal uses an immutable `users.is_deleted` tombstone. Its generated reserved username cannot be claimed through signup. The database refuses reactivation, restoration of the deleted flag, or promotion to administrator/owner; management routes also refuse such mutations. Retained content may itself identify its author. This action does not promise anonymization of everything the person wrote.

## Authorization and commit

Removal requires `member.manage`, recent strong authentication, current transaction-time authority, and exact typed confirmation of the current username. A manager cannot remove themselves, the installation owner, or an equal/superior account. At least one active administrator must remain. Ownership must be transferred through its separate ceremony first.

The identity update, removal of sessions/factors/recovery codes/grants/push subscriptions/invitations/personal notification and read state/unclaimed upload records/conversation memberships/send receipts, revision bump and audit row commit together. Authority delivery fencing ends revoked access at that boundary; later disconnect cleanup cannot restore removed-profile metadata. File cleanup follows the existing orphan-file maintenance contract.

Removed identities do not consume the 100-account capacity limit. Reversible deactivated/banned accounts still count. Account creation checks capacity in its write transaction, while bootstrap detection counts all identities so retained tombstones never reopen first-time setup.

The stable user ID remains only as the identity of retained history. Send-receipt guards can be removed at this point because the account can no longer authenticate or be reactivated. Creating a later account gives it a different ID.

## Reports and backups

Reporting deliberately preserves the selected message's bounded text and the reporter's explanation for up to 90 days from report creation. That evidence can survive deletion of the source message or author. Physical account erasure sets report account references to null; account removal shows the retained identity as **Deleted member**. Deleting the source channel cascades its reports, since its permissions no longer exist.

Reports are visible only to holders of `message.delete_any` who can currently read the source channel. Expired reports immediately disappear from APIs and are physically purged in hourly batches of at most 500. A backlog can remain on disk beyond the availability deadline until maintenance catches up. Backups retain their own snapshots until the operator removes them; neither account action edits existing backups. This is a product data policy, not a claim of legal erasure compliance.

## Boundary for later private conversations

The alpha.5 checkpoint described by this ADR had no DMs/groups. Alpha.6's opt-in direct conversations and their lifecycle behavior are now recorded in [ADR 0012](0012-private-conversations-consent-and-shared-storage.md); groups remain unimplemented. Before broad private-message exposure, ratify ownership transfer, membership removal, retained-history visibility and the distinction between hiding a conversation and erasing authored content. Do not let administrators acquire private membership through this lifecycle operation. Schema 6 membership rows alone did not establish a private-conversation feature.

## Evidence

`internal/api/account_lifecycle_test.go` covers retained posts and replies, ended sessions, removed membership/receipts, immutable tombstones, typed confirmation, hierarchy, audit rollback and subsequent explicit erasure. Report tests cover retained evidence after author deletion and current-access review. Credential, delivery and notification regressions cover requests and sockets that were admitted before removal/revocation.
