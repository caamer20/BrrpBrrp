# Security review bundle — what to hand the independent reviewer

**Status: prepared, not delivered.** No independent reviewer exists yet, and the
execution plan says so plainly in its external dependency register
([v1-release-execution-plan-2026-10-03.md](v1-release-execution-plan-2026-10-03.md) §8).
This file is the index of what is ready to hand over, what must be built first, and what
the reviewer is being asked to decide. It is not a report and it is not a finding.

The plan requires this review before the beta gate (G6) and before the two feature gates
may be switched on. The implementing agent cannot grant that approval itself, and nothing
in this repository claims it.

---

## 1. What the reviewer is being asked to do

From the master plan's WP-28 and the execution plan's V1-13, the review covers:

- SSRF, redirect and DNS defences on the server-side preview and push fetchers.
- Untrusted rendering and upload handling: no message, name, filename or preview becomes
  markup or a filesystem path.
- Sessions, second factors and recent-authentication boundaries.
- Owner and operator tier boundaries; delegation that cannot escalate.
- Authorization timing: authority established inside the mutating transaction, not around
  it ([ADR 0009](../docs/adr/0009-authority-commit-and-admission-ordering.md)).
- Private metadata and events: a community role never admits a direct or group
  conversation, and private actions stay out of the community audit log, community search
  and link-preview fetches.
- Moderation evidence: selected evidence only, and a reviewer sees what the reporter
  selected and nothing else.
- Data durability and recovery: attachment publication before a row promises it, backup
  manifest verification, and what an altered or truncated archive does.
- The release supply chain: license, notices, contribution terms, security intake, and
  what a hosted CI run would have to prove.
- Diagnostics leakage: what the metrics, health and diagnostics surfaces expose, and
  whether anything in them identifies a member or a conversation.

**No unresolved critical or high finding may be hidden behind a default-off feature** if
that feature is required in the shipping beta. Both feature gates are default-off today
and are planned to ship on, so the private-conversation and restricted-channel surfaces
are in scope exactly as if the gates were already flipped.

## 2. What must exist before the bundle can be sent

| Input | State | Why the review cannot start without it |
| --- | --- | --- |
| A named reviewer and contact | **Absent** | The plan requires a signature on a source-bound report. |
| A reviewable candidate artifact | **Absent** | A tag or archive identity to bind the review to (see §7). |
| A destination to receive the bundle | **Absent** | No remote exists by the owner's decision, so the bundle is delivered by hand or by a path the owner chooses. |
| Environment or host access, if the reviewer wants a live instance | **Absent** | Everything below can be run against a local disposable installation; a live deployment has to be supplied. |
| Confirmation of scope and severity policy | **Absent** | The severity policy is in the master plan §14.3; the owner should confirm the reviewer is working to it. |

## 3. The threat model and the trust boundary

[security.md](../docs/security.md) is the threat model as written for an operator:
"intended for a small group whose server operator is trusted… does not provide end-to-end
encryption or hostile multi-tenant isolation. The operator can read stored messages and
relay audio."

Three boundaries it states and a reviewer should test rather than accept:

1. **Host access is out of scope, deliberately.** Anyone who can read or write the data
   folder holds the database, the backups and the attachments. The application's answer is
   to make legitimate host-level recovery recorded rather than silent
   (`brrpbrrp recover-owner` while the service is stopped, attributed to the host
   operator). A review that treats the operator as an attacker is reviewing a different
   product; a review that finds an ordinary member reaching operator-level data is
   finding a Severity 0.
2. **A community role is not authority over a private conversation.** This is the
   product's central privacy claim and it is stated in
   [ADR 0012](../docs/adr/0012-private-conversations-consent-and-shared-storage.md) and
   [ADR 0013](../docs/adr/0013-private-message-evidence-and-request-discovery.md). The
   negative matrix's `audience`, `conversation`, `recipient` and `authorship` classes are
   where it is enforced.
3. **Transport encryption is terminated at the SFU.** DTLS-SRTP protects the transport;
   the server forwards audio, so it can hear the call. Not end-to-end encryption, stated
   as a limitation.

## 4. Decision records

Twenty ADRs are in `docs/adr/` in the source tree. A security reviewer should read these
first, in this order, because they are where the deliberate choices and their consequences
are argued rather than assumed:

| ADR | Why a reviewer needs it |
| --- | --- |
| [0001](../docs/adr/0001-roles-ownership-and-operator-tier.md) | Roles, ownership and the nondelegable operator tier. |
| [0004](../docs/adr/0004-sessions-recent-authentication-and-privileged-enrollment.md) | Sessions, recent authentication, privileged enrolment. |
| [0005](../docs/adr/0005-credential-lockout-recovery.md) | Credential lockout and recovery. |
| [0009](../docs/adr/0009-authority-commit-and-admission-ordering.md) | Authority commit and admission ordering — the invariant most likely to be broken by a later change. |
| [0011](../docs/adr/0011-account-removal-and-retained-evidence.md) | Account removal, retained evidence, and what survives deletion. |
| [0012](../docs/adr/0012-private-conversations-consent-and-shared-storage.md), [0016](../docs/adr/0016-consenting-groups.md) | Consent, groups, blocking: the rules that must hold at every delivery path. |
| [0013](../docs/adr/0013-private-message-evidence-and-request-discovery.md), [0014](../docs/adr/0014-pending-request-evidence-and-command-receipts.md) | Selected evidence and command receipts. |
| [0007](../docs/adr/0007-attachment-durability.md), [0008](../docs/adr/0008-backup-manifest-and-verification.md), [0002](../docs/adr/0002-schema-migration-and-durability.md) | Attachment durability, backup manifests, migration and durability. |
| [0003](../docs/adr/0003-restricted-channel-exposure.md) | Restricted channels — the known exposure limit while the gate is off. |
| [0006](../docs/adr/0006-licensing-and-contribution-terms.md) | License, contribution terms and the security intake. |

## 5. Inventory and matrices

- **[negative-matrix.json](negative-matrix.json)** — every registered route and method,
  with the capability it needs if any and the class that denies an outsider:
  `capability`, `audience`, `channel`, `conversation`, `recipient`, `authorship` or
  `self`. The plan and the ledger record **132 routes** as classified; the reviewer should
  take the figure from the file itself rather than from this sentence, because it is
  regenerated. It is produced from the route table by the two tests named in §6.3, so it
  cannot drift from the code without the build failing.
- **[acceptance-matrix.json](acceptance-matrix.json)** — the scenario catalogue from the
  master plan §11.2 and the journeys J-01…J-10, each check named where it lives, each owed
  item naming what is missing and which phase owns it. As last measured: 29 scenarios, 133
  named checks, 14 owed items across 14 scenarios, and 8 owed across the 10 journeys.
- **[verification-queue.md](verification-queue.md)** — the owner's list of what has been
  written but not run. A finding that depends on an unrun check should be read with this
  open, because the suite's last full pass predates the newest commits.

**Owed, and the reviewer should be told so:** the *runtime* half of the negative matrix —
generating the outsider's actual requests from the inventory for the `conversation`,
`channel`, `audience`, `recipient` and `authorship` classes and requiring a passing
request per row — is item W-16 and has not been written. Today the matrix proves the
policy is declared and that named tests cover it, not that every row was exercised.

## 6. Adversarial scenarios that are runnable today

Each of these runs against a disposable installation on the reviewer's own machine.
Build once first, because the frontend is embedded:

```sh
cd <checkout>
go build -trimpath -o dist/brrpbrrp ./cmd/brrpbrrp
```

### 6.1 Authorization and privacy, at the API level

The tests below are the named evidence behind the `audience`, `channel`, `conversation`,
`recipient` and `authorship` classes. Run them by name so the reviewer can see the
assertion rather than a package's summary:

```sh
go test ./internal/api -count=1 -v -run 'TestRestrictedChannelPrivacyAcrossDeliverySurfaces|TestPrivateChannelLifecycleReachesOnlyItsAudience|TestRealtimeEventsAreAudienceScoped|TestTheRevokedAudienceIsNotDeliveredOnARealSocket|TestDirectAcceptBlockRaceNeverAllowsBlockedContact|TestGroupMembershipRequiresTheRecipientsOwnAcceptance|TestPrivateEvidenceNeedsItsOwnCapabilityAndGrantsNoConversationAccess|TestAnExportStopsMidTransferWhenItsScopeIsWithdrawn|TestACredentialCommitRefusesAQueuedWriteOnTheRevokedSession|TestDelegatedMemberManagerCannotEscalateAuthority'
```

### 6.2 The same claims through a browser

What only a browser can show: a revoked member's already-open page, held responses that
must not repopulate after revocation, and the refusal being rendered rather than merely
returned.

```sh
cd web
BRRP_E2E_PRIVATE_CONVERSATIONS=1 npm run test:e2e -- --output /tmp/review/private
```

With the restricted-channel gate on, the journeys that create a private channel and show
an outsider the non-disclosing refusal are item W-07 and are **not yet written**. So the
browser half of the restricted-channel claim is owed; say so rather than letting a green
private-mode run stand in for it.

### 6.3 The route inventory itself

```sh
go test ./internal/api -count=1 -run 'TestEveryRouteDeclaresHowAnOutsiderIsDenied|TestEveryRouteDeclaresItsPolicy'
```

Add `-update-negative-matrix` only to regenerate [negative-matrix.json](negative-matrix.json)
after a route change. A route added without a policy, or with one that cannot deny an
outsider, fails the run.

### 6.4 Recovery and durability, including hostile input

```sh
node scripts/upgrade-recovery.mjs         --binary dist/brrpbrrp --report /tmp/review/recovery.json
node scripts/upgrade-recovery-private.mjs --binary dist/brrpbrrp --report /tmp/review/private-recovery.json
node scripts/backup-encrypted-rehearsal.mjs --binary dist/brrpbrrp
node scripts/full-disk-rehearsal.mjs        --binary dist/brrpbrrp
node scripts/upgrade-rollback.mjs --binary dist/brrpbrrp \
  --old-release dist/releases/brrpbrrp-0.4.0-alpha.7-darwin-amd64.zip \
  --report /tmp/review/rollback.json
```

The reviewer should add, on top of what these do: a touched ZIP, a truncated archive, a
wrong key, a wrong copy directory, and an archive whose completion marker is missing —
and confirm each is refused rather than reported as a verified recovery point. That set
is item W-18 and is owed.

One disclosure belongs here: the encrypted rehearsal has only ever been run with
`openssl`, because this machine has no `age` or `gpg` and the owner decided none would be
installed. The other two documented tools are **not exercised** and are owed to whoever
first runs the procedure where they exist.

### 6.5 The container and the package

```sh
node scripts/check-docker-inputs.mjs          # static only; not an image build
node scripts/check-docker.mjs                 # requires a Docker host; not available here
BRRP_GO=go node scripts/check-source-package.mjs
BRRP_GO=go node scripts/licenses.mjs --check
```

The Linux image has never been built or run, by the owner's decision, because there is no
container runtime on this machine. The static input audit passes; runtime qualification is
owed to a Docker host.

### 6.6 Dependency and vulnerability state

```sh
govulncheck ./...
npm --prefix web audit --audit-level=moderate
```

**Owed before the reviewer starts:** a fresh run of both against the candidate bytes with
current databases. Every recorded scan in this repository is historical, and a cached
clean scan does not describe today's candidate.

## 7. The candidate artifact

**This is the largest gap.** There is no published candidate: no remote exists by the
owner's decision, and the only archive is the 0.4.0-alpha.9 one produced at `f810f0c`,
which predates later fixes. What exists locally is `dist/releases/` with its
`SHA256SUMS`, and a verified git bundle per checkpoint under `dist/backups/`.

A reviewer must be given, together with the exact bytes:

- the full source commit, the artifact SHA-256, and the app/schema/backup/protocol
  identity;
- the feature modes it was built and run in;
- the command behind every result quoted to them;
- and a statement of whether they are reviewing the artifact or the source tree, because
  those are different claims.

Until the owner supplies a remote and a publication path, the reviewer can review the
source tree and a locally built binary; that is a real review, and it is a weaker one.

## 8. Known findings, so far

The independent review recorded in
[v1-release-execution-plan-2026-10-03.md](v1-release-execution-plan-2026-10-03.md) found
F-01…F-12. For a security reviewer the relevant ones and their disposition are:

| Finding | State |
| --- | --- |
| F-01 concurrent uploads could both pass the storage-quota check | Fixed (W-01) with a regression proven against pre-fix code. |
| F-02 a thread deep link compared a message id with a thread id | Fixed (W-01), with a rendered regression. |
| F-06 upload body-copy errors collapsed into "file too large" | Fixed (W-01); a real full volume has still not been induced. |
| F-10 member-export attachment accounting could exceed its bound | Fixed (W-02). |
| F-11 backup alerts aged the local job, not the recoverable snapshot | Fixed (W-03). |
| F-12 metrics named things they did not measure | Fixed (W-04). |
| F-03, F-04, F-05, F-07, F-08, F-09 | Open or in progress; see the verification queue. |

The ledger's status index is the authoritative list of what is closed, and it says
whether each closure was run or only written.

## 9. What a finding must carry

Every finding needs: a severity from the master plan §14.3 policy, the affected
requirement or version, a reproduction that works from current source, a disposition, an
owner, the fix with its regression, and the reviewer's recheck. Accepted lesser findings
need an explicit rationale, a workaround and a deadline.

The implementing agent and the architect cannot grant the required independent approval,
and no document in this repository should be read as having granted it.
