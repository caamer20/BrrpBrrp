# Beta entry checklist

**What must be true before three communities are invited.** Derived from the master plan
§14.1 and §17.3 ([docs/v1-implementation-plan.md](../docs/v1-implementation-plan.md)) and
from the execution plan's V1-13 and V1-16
([v1-release-execution-plan-2026-10-03.md](v1-release-execution-plan-2026-10-03.md)),
with the owner's recorded decisions applied.

Nothing here is checked. No beta has been started, no community has been invited, and no
reviewer has been engaged. This is the gate, written down so that entering the beta is a
decision somebody makes deliberately rather than a drift.

Beta entry is **G6**. The gate is approved by the owner, security, QA and operations; the
implementing agent cannot approve it and does not claim to.

---

## 1. The owner's own inputs

These are the ones no amount of engineering supplies. Each is listed with the exact thing
its absence prevents, so asking for it is a concrete request rather than a status query.

| Input | State | What its absence prevents |
| --- | --- | --- |
| The decision to switch both feature gates on, and the commit that does it | **Owed** — settled in principle, dated to after the independent reviews | The beta must run the product in its shipping configuration. A beta with `BRRP_ENABLE_RESTRICTED_CHANNELS` and `BRRP_ENABLE_PRIVATE_CONVERSATIONS` off does not exercise the two features the reviews are about. |
| A named independent security reviewer and accessibility reviewer | **Absent** | G6 requires their reports. Neither can be self-granted. |
| Three small communities with different operators or usage patterns | **Absent** | The beta is the thing they run. |
| At least one operator and several members who are not the primary developer | **Absent** | A beta run only by its author measures nothing about usability. |
| Support coverage and an incident owner, with a named alternate | **Absent** | The beta's promise is that somebody answers. |
| A separate, independently protected backup destination | **Absent** | Recovery is the one failure a pilot cannot absorb. |
| A remote and a hosted CI run, or a written decision that the beta proceeds without one | **Owed** — the remote is deferred by the owner's decision | Hosted CI is where the OS and browser matrix is actually exercised. Local runs cannot cover Windows. |
| Consent to participate, recorded per community | **Absent** | Participation is by consent; the plan requires it, and so does the product's own rule about not adding anybody without acceptance. |
| Publication of the support policy and known limitations to the pilot operators | **Owed** — the documents are being written | An operator who does not know the recovery boundary cannot consent to it. |

## 2. Engineering prerequisites

Each row names the evidence, not the intention. An item is done when the evidence exists
and is bound to a commit.

| # | Must be true | Evidence | State |
| --- | --- | --- | --- |
| E-1 | Every locally provable check has been run and recorded, with nothing skipped, on one commit | The verification queue's pass ([verification-queue.md](verification-queue.md)), and W-20's sweep | **Owed** — test runs are suspended and the queue exists for this |
| E-2 | Two consecutive fresh-context review rounds found no new P1 | W-19's two rounds, each reviewer bounded to one surface, each asked to reproduce against current source | **Owed** |
| E-3 | Restricted channels are exercised through the interface, with the gate on and off, including the non-disclosing refusal | W-07's journeys | **Owed** — this is the largest product surface never exercised in a browser |
| E-4 | A candidate exists as identified artifacts: `0.4.0-alpha.10`, verified digests, a results manifest | W-20 | **Owed** |
| E-5 | Acceptance results are bound to bytes: an obligation that is unrun, failed, skipped or tied to different bytes makes acceptance incomplete | W-10 | **Owed** |
| E-6 | New installations start with scheduled backups on, at the ratified 12-hour interval, and the recovery verdict a new owner sees is explained where they meet it | W-36, W-37, W-28 | **Written, not run** |
| E-7 | The Docker deployment can be configured to run the beta's configuration — restricted channels, alerts, quota, login attempts, upload bound, durability — without editing the Dockerfile | W-24 | **Written, not run** |
| E-8 | The container publishes the TURN TCP and TLS fallback ports, and the guides stop saying the relay is UDP-only | W-25 | **Written, not run** |
| E-9 | The example configuration files document every key the server reads | W-26 | **Written, not run** |
| E-10 | A member can change their own display name and avatar colour without an administrator | W-27 | **Written, not run** |
| E-11 | The Windows and Docker host procedures match the current build and have been executed on both hosts | [host-qualification.md](../docs/host-qualification.md), corrected by W-21 | **Absent** — no host exists |
| E-12 | Failure coverage exists: a process killed at publication, fsync, row insert, commit, attachment claim and migration boundaries restarts cleanly, and a stale, touched, truncated or wrong-key archive is never called a verified recovery point | W-18 | **Owed** |
| E-13 | The capacity driver measures what it reports | W-17 | **Written, not run** |
| E-14 | Current-database vulnerability scans pass on the candidate bytes | `govulncheck` and `npm audit`, run at candidate time | **Owed** — every recorded scan predates the candidate |
| E-15 | A real alert reaches a responsible operator's own receiver, and a silent scheduled-backup failure is visible | A delivered-alert observation, not a configured webhook | **Owed** — needs a receiver and a host |
| E-16 | Ordinary delegated administration cannot reach a direct or group conversation through files, search, events, backup or export | The negative matrix's runtime half, W-16 | **Owed** |

## 3. The gate transition itself

Switching the gates on is a reviewed commit, not a config edit on a running host
(W-35 prepares the checklist for it). Before the beta starts:

- [ ] Every file that assumes the gates are off has been found and listed, including the
      defaults, both example environment files, `compose.yaml`, the guides, the interface's
      disabled-state copy, the test harness defaults and the release notes.
- [ ] The transition commit changes the defaults in one place and says so in the upgrade
      note.
- [ ] **An installation that set a gate to `false` explicitly keeps it off** — the
      transition does not overwrite an operator's deliberate choice, and that is stated
      where they will see it.
- [ ] The whole relevant test set has been run **in the shipping configuration**, with the
      explicit-off refusal tests retained rather than deleted.
- [ ] The examples, installers and guides agree with the shipped defaults, so a
      nominally default-on build is not deployed with silently overriding old off flags.
- [ ] The pilots are told which features are on, because they consented to a product whose
      shape has just changed.

## 4. What the beta itself must exercise

From the plan §14.2, so that entry is judged against what will be measured:

- Every operator completes install, backup/restore and update using the documentation
  alone, with no undocumented application repair.
- Every community exercises invitations, restricted-channel membership, consented DMs and
  groups, threads and the inbox, reporting and moderation, voice and desktop sharing.
- At least 90% of prescribed critical journeys complete without the author's intervention,
  with every failure investigated and every required journey eventually passing on the
  supported matrix.
- At least 30 complete cross-device calls, including six longer than 30 minutes, with the
  route and browser outcome logged. These supplement rather than replace the controlled
  two-hour media test.
- No open unauthorized disclosure, acknowledged data loss, unrecoverable migration, hidden
  forwarding or capture, or core-flow blocker.
- Backup age, independent recovery and delivered-alert objectives are met. **A silently
  failing scheduled backup fails the gate** even while chat stays usable.
- Every reported issue is reproducible or explicitly dispositioned with severity, owner and
  evidence.

## 5. Reset rules

- A Severity 0 or 1 found during the beta restarts that deployment's fourteen clean days
  after the fix and its requalification. Unaffected evidence may be kept only with a
  written rationale.
- Any executable, dependency, schema or protocol change creates a new artifact identity and
  restarts the seven-day final observation.
- A documentation-only change still needs the documentation and link checks if documents
  are packaged; a signing or repacking change needs final-byte verification plus an
  explicit decision about whether the earlier runtime evidence still describes these bytes.
- Record the decision rather than reusing an old pass label.

## 6. What beta is not

- It is not v1.0.0. After the beta there are still the seven frozen days, an independent
  recovery and rollback rehearsal against the final materials, and the owner's publication
  approval.
- It is not the place to discover that a feature exists on only one API. That is what
  E-1 and E-3 are for.
- Fourteen runs or fourteen edited dates are not fourteen days.
