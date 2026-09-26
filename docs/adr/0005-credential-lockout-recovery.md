# ADR 0005 — Credential lockout recovery: an offline owner command and an audited operator path

**Status:** accepted for this change
**Date:** 24 September 2026
**Plan references:** WP-05 (deliver "owner-only audited emergency credential recovery"; done when "offline owner recovery is documented without exposing secrets"), D-10, T-IDENT-01, T-AUTH-04

## Context

ADR 0001 made full archive, chosen-password reset and ownership transfer
non-delegable operator powers, and ADR 0004 added the recent-authentication gate
and mandatory enrollment. Both changes made the operator tier harder to abuse and,
in doing so, created a state the application had no way out of:

- Sign-in requires an enrolled factor once one exists. A password reset
  deliberately keeps that factor, so an administrator *cannot* restore a locked-out
  account's access — not a member's, not another administrator's, and not their
  own if a peer reset it.
- After an owner is designated, administrators no longer hold the operator tier
  (ADR 0001's bridge closes). An owner who has lost the authenticator and every
  recovery code therefore cannot be repaired by anyone inside the running server.
- `create-admin` only ever added an account. It could restore *administration* but
  never an existing person's access, and it cannot create a second owner.

The offline escape hatch was therefore the only real decision left, and the plan
already named it: "offline break-glass recovery with audit" (D-10) and "offline
owner recovery is documented without exposing secrets" (WP-05). WP-23/WP-24 own
the surrounding backup/restore drill; this ADR owns the credential path.

## Decision

1. **The owner is recovered on the host, with the service stopped.**
   `brrpbrrp recover-owner --username NAME --password-stdin` replaces that
   account's password, removes its enrolled factor and recovery codes, ends every
   session and push subscription, and lifts a deactivation or ban that would
   otherwise still refuse the sign-in. It refuses to run while the service holds
   the data lock, refuses an install with no owner, and refuses any account that is
   not the current owner. Ownership, role assignments, messages, files and
   attachments are untouched: it restores one account's credentials, not its
   authority over anybody else.

2. **It is recorded, and attributed honestly.** The whole change is one
   transaction that writes one audit row, `owner.recovered_offline`, with a NULL
   actor. The audit surface labels that row *host operator (offline)* rather than
   the owner's name or "former member", because the person acting is whoever holds
   the host. A missing actor on any other action still means an erased account, and
   the two must not read alike.

3. **Any other locked-out account is recovered by an operator, online and
   audited.** `DELETE /api/admin/users/{id}/mfa` removes a lost factor and its
   recovery codes and ends that account's sessions. It carries the same policy as a
   chosen-password reset — the non-delegable `operator.credential_recovery`
   capability plus a fresh sign-in — so it is credential recovery, never ordinary
   moderation, and it is pinned into `internal/api/policy_test.go` alongside the
   other four recent-authentication routes.

4. **An owner's factor cannot be removed through the administration surface.**
   That route answers `403` and names the host command. Otherwise an operator
   account could take over the one account that holds the powers no role can
   delegate. The owner removes their own factor from **Your preferences →
   Two-factor authentication** with a password and a code, which is self-service
   and already required a working session.

5. **The command reports what it changed.** The outcome is printed line by line
   (password replaced, factor and code counts, sessions ended, lockouts lifted) so
   an operator can write the real result into their own record instead of assuming
   success from a zero exit code.

## Consequences

- There is now a documented answer for every lockout: a recovery code, an operator
  removal, the owner's own offline command, or `create-admin` for a fresh
  administrator. The administrative guide and README troubleshooting list them in
  that order.
- The trust boundary is unchanged and is now stated plainly: anybody who can read
  and write the data folder can already read the private database, its backups and
  the attachments. `recover-owner` does not grant a new power; it makes an
  otherwise improvised repair explicit, atomic and recorded.
- Losing the passphrase as well as the factor means the *host* is the only
  remaining lever. An installation whose host is gone with its owner is
  unrecoverable from the application, which is why the backup and off-host copy
  requirements (WP-23, D-14) stay where they are.
- **Not decided here, and still gaps:**
  - A recovery is detected only when someone reads the audit log. There is no
    alert, no out-of-band notification and no lockout counter, so a hostile host
    operator's action is recorded but not surfaced (WP-26 monitoring owns this).
  - The operator removal path ends the recovered account's sessions but cannot end
    one that is mid-flight in an in-progress media or file transfer; that is the
    same limitation ADR 0004 lists for revocation (WP-20/WP-22, T-AUTH-02).
  - A demonstration of `recover-owner` on the Windows service and the Docker
    container, and the quarterly lockout rehearsal, are operator work under WP-24,
    not tests in this repository.

## Alternatives considered

- **Let delegated administrators remove a lost factor, and let them recover an
  owner too.** Rejected as the takeover path ADR 0001 exists to close: the account
  that can archive every conversation and choose anyone's password should not be
  repairable by a role that cannot already do those things.
- **Clear the factor automatically on a password reset.** Rejected: it turns every
  borrowed administrator session into a way to disable another account's second
  factor, and it silently weakens the guarantee that a reset cannot be used to
  strip protection from an account.
- **Store a printable offline recovery key generated at enrollment.** Rejected for
  now: it adds a long-lived credential to protect and rotate, and the host operator
  already holds an equivalent power. It is worth revisiting only with a threat
  model that has to defend against the host itself.
- **Make `create-admin` overwrite an existing account.** Rejected: an offline
  command that silently rewrites a named account is exactly the ambiguity this
  change removes by keeping "add an administrator" (`create-admin`) and "restore the
  owner" (`recover-owner`) as two explicit, differently audited operations.
