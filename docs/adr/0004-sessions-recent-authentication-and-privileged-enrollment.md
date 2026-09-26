# ADR 0004 — Sessions, recent authentication and privileged enrollment

**Status:** accepted for this change, with one default awaiting owner ratification
**Date:** 24 September 2026
**Plan references:** WP-05, D-10, T-AUTH-03, T-AUTH-04, T-IDENT-01

## Context

Before this change, a valid session cookie was enough to take a full archive of the
server, to set another account's password, and to complete or transfer ownership.
Those three actions can defeat every privacy boundary the server maintains —
including the boundaries it maintains against its own administrators — so a
stolen or borrowed cookie, or a laptop left open, was enough to reach them.

The role model added in ADR 0001 gave those actions a name: the non-delegable
operator tier (`operator.backup`, `operator.credential_recovery`,
`operator.ownership_transfer`). It did not, by itself, require anything more than
the session that already held it. The last pre-role remnant was a route flag
(`routePolicy.Administrator`) that two enrollment routes still used.

## Decision

1. **Sessions record how they were established.** Migration 4 adds
   `sessions.last_seen_at`, `sessions.user_agent` and `sessions.privileged_at`,
   the last being the time of the most recent strong authentication on that
   session. The session token hash is never returned, logged or exported, so an
   account can list and revoke its sessions without ever seeing a credential.

2. **Operator-tier actions need a recent strong authentication.** A password or
   second-factor confirmation inside `recentAuthenticationWindow` (15 minutes)
   stamps the session. The four routes that can defeat a privacy boundary —
   `POST /api/owner`, `POST /api/admin/users/{id}/reset-password`,
   `POST /api/admin/backup` and `GET /api/admin/backups/{id}` — additionally
   require that stamp. `internal/api/policy_test.go` pins the list so it cannot
   grow or shrink unnoticed.

   The refusal is a distinct `403` naming the requirement, not a generic failure,
   and `internal/api/sessions.go` exposes `POST /api/me/reauth` so a stale tab can
   recover: it needs the account's password and, once enrolled, a factor or
   recovery code. Reauthentication is deliberately *not* itself a fresh-auth
   route, or a stale session could never refresh.

3. **Privileged accounts must enroll a second factor.** Owners and administrators
   must have an enrolled factor before the operator tier works
   (`Settings.RequirePrivilegedMFA`, default on). Ordinary members are never
   blocked by this policy: enrollment is available to every account, and their
   normal work is unaffected. An installation that cannot run a second factor can
   turn the policy off deliberately rather than by weakening the default.

4. **Revocation takes effect at the moment it commits.** `DELETE /api/me/sessions/{id}`
   removes one session and closes the websockets bound to it;
   `DELETE /api/me/sessions` removes every other session. Revoking the caller's own
   session also clears its cookie and leaves voice. Enrolling or removing a factor,
   and changing or resetting a password, still invalidate all other sessions.

5. **A sign-in re-checks its own credential.** `completeLogin` reads the password
   hash, the account's active state and its MFA enrollment inside the same
   transaction that inserts the session, so a reset or an enrollment that lands
   mid-flight wins and produces no session. Recovery codes are consumed with a
   single conditional delete, so exactly one concurrent use succeeds.

6. **The pre-role administrator flag is deleted.** Every administrative surface is
   a named capability; `routePolicy.Administrator` and its checks are gone, and no
   route may reintroduce a boolean that bypasses the catalog.

## Consequences

- An operator cannot archive the server, take over another account or move
  ownership from an unattended session, and cannot do it at all without a second
  factor.
- The account-security panel exposes enrollment, the signed-in device list,
  revoke-one/revoke-all and the confirmation step; the archive and credential
  recovery surfaces explain a refusal and link to it instead of presenting it as
  an error.
- **Not enforced by this change**, and therefore still listed as gaps:
  - Revocation does not cancel media or file transfer already admitted; a
    revoked participant's live stream ends at the next authorization check
    (WP-20/WP-22, T-AUTH-02). Voice presence state is still community-wide
    (see ADR 0003), so restricted *voice* channels remain disabled.
  - Authority changes from a role edit take effect on the next request or event.
    The socket is not force-closed for a demotion; clients re-resolve on the
    `authorization.changed` event, so a demoted client keeps receiving events
    until it reconnects or refetches.
  - The 15-minute window and the `RequirePrivilegedMFA` default are code and
    stored-setting values, not environment knobs. Changing the window is a code
    change. The enrollment default needs an owner decision, recorded in
    `docs/v1-progress.md`.
  - The offline "break-glass" path for a locked-out owner was the server command
    line and a restore of a known-good backup. It is now a command of its own, and
    the procedure, its audit attribution and the operator path for every other
    locked-out account are recorded in
    [ADR 0005](0005-credential-lockout-recovery.md).

## Alternatives considered

- **Require a password on every operator action, with no window.** Rejected as
  friction that pushes operators toward storing a password in a script; a short
  window after a real sign-in is the same guarantee with a usable shape.
- **Keep the legacy administrator flag for the two enrollment routes.** Rejected:
  it is the same authority as the administrator role, so two mechanisms would
  drift, and the flag would invite a third use later.
- **Warn instead of refusing when the second factor is missing.** Rejected: a
  warning does not change who can archive every conversation.
