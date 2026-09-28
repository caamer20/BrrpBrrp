# P9 checkpoint — private evidence, request discovery and review-9 repairs

**Date:** 27 September 2026 (America/Los_Angeles)
**Branch:** `codex/p9-private-reporting`
**Scope:** P9-01…P9-06 of the review-9 handoff. Local implementation and
verification only. No hosted CI run, Windows/Tailscale host, Docker runtime,
off-host recovery, capacity/soak, independent security or accessibility review,
beta period or publication approval is established here, and no G0–G8 release gate
has passed.

## Source and artifact identities

| Identity | Value |
| --- | --- |
| Base (reviewed by review 9) | `876260ab1c01313efd3358459b4178c010ee2f45` |
| P9 implementation commits | `06e39fe` R9-01 · `ead585c` R9-02 · `d255140` R9-04/05/06 · `ca3e13c` P9-02/03 backend · `8037023` P9-04/05 client · `1f89a81` P9 copy and journeys · `2869db5` P9 checkpoint record |
| Checkpoint commit (clean) | `2869db55394b2b2a004424672517b38d6908586b` |
| Schema | 12 → **13** (additive: two tables, no change to `message_reports`) |
| Backup format | **2**, unchanged — a schema 13 database is still format 2 |
| Version | `0.4.0-alpha.6` → **`0.4.0-alpha.7`** |
| Toolchain | Go 1.27.1 (`.tools/go/bin/go`), Node 24.2.0, Playwright Chromium 1243 |
| `GOCACHE` | `/private/tmp/brrpbrrp-go-build` (module cache `/Users/cameronamer/go/pkg/mod`, `GOPROXY=off`) |
| Frontend build identity | `6b00f391da81a4bc` |
| Candidate binary | `dist/brrpbrrp-p9-checkpoint`, sha256 `fddd1e3f3ef3c9873e822dd96f625c3bcdfd036b76fdff2ba45c5835a4c3b57f`, built from the clean checkpoint commit with `CGO_ENABLED=0 -trimpath -ldflags='-s -w'`. This is the exact binary every recovery, rollback and browser run below consumed, so the recorded results describe these bytes. |

The P8 archives and digests are preserved unchanged. This tranche does not rebuild
alpha.6 packages in place, and the P8 evidence ledger carries dated corrections
rather than edited records.

## P9-01 — review-9 follow-ups

### R9-01 — CI could pass with failing browser tests

**Defect.** Both browser steps piped `npm run test:e2e` into `tee` and relied on
the runner's default shell, which is `bash -e` with no `pipefail`. The step
therefore reported `tee`'s status, and a failing suite that still printed its
project names — including the `[directs]` the step greps for — left the required
job green. `packages` needs `browser`, so the promotion gate could pass without the
journeys having run.

**Fix** (`.github/workflows/check.yml`). Both steps declare `shell: bash`
(`bash --noprofile --norc -eo pipefail`, so the test process's own status decides
the step). The artifact upload is `if: always()` and lists the **original** paths
as well as the renamed ones: with `pipefail` the run body stops at the failing
pipeline, so the `mv` never happens and a failed run leaves its traces and
screenshots under `test-results/`. Uploading only the renamed copies would discard
exactly the evidence a failure produces. `shell:` is applied only to the
Linux-only `browser` job; the `test` matrix keeps its per-OS default.

**Proof** (`scripts/check-browser-steps.mjs`, new; run in the `test` job and
`make check`). It audits the shell, the diagnostics and the downstream
dependency, then **executes each step's literal `run:` body** under the exact
command line GitHub documents for that shell, against a stub `npm` that fails
while printing `[directs]`. Each step must exit non-zero, and a passing control
must exit zero so a harness that refuses everything is not mistaken for a pass.
`--self-test` runs the pre-fix workflow through the same code path and requires
the audit to reject it *and* confirms the mask actually hid a failure.

**Controls run:** `--self-test` and the plain audit both pass; the pre-fix fixture
is rejected and its masking is confirmed to hide an exit-1 test that printed
`[directs]`. A local shell control confirms the underlying fact —
`bash -e -c 'false | tee /dev/null'` exits 0, `bash -eo pipefail` exits 1.

### R9-02 — a mute on one device was invisible on the others

**Defect.** `setChannelNotifications` committed the preference and returned,
signalling nothing. A session re-read its mute map only on first load, on an
authority change, and on a `focus` handler that lived in `ChannelNotifications`
— a component unmounted while a direct conversation is open. A session that
cached a channel as muted and then stayed in a DM dropped eligible mentions
indefinitely.

**Fix.** Server: after the mute commits, `preferences.changed` goes to the
account's own connections. It carries `{"channel_notifications": true}` and no
policy value, and the authorization revision is deliberately **not** bumped — a
mute is not an authority change, and bumping it would fence every queued event
for a personal setting. A test asserts the revision does not move.

Client: the store re-reads policy on that signal, on `hello`/reconnect (a
same-revision reconnect publishes nothing), and on window focus. Focus ownership
moved from the header component to `App.svelte`, because the header is absent
exactly where the defect lived. The cached map is dropped and the version bumped
*before* the read, so a mention arriving in the gap is withheld rather than
answered from a known-stale answer; the read is rate-limited per focus so it
cannot become a poll. Existing guards are preserved: a delayed read cannot
overwrite a newer write, an answer for a previous account is refused, failures
stay fail-closed with bounded retry, and a pending refresh does not cross
sign-out.

**Regressions** (`web/tests/workspace-state.test.mjs`, 4 new cases):
another device's unmute reaching a session in a DM and the withheld mention in
the gap; reconnect re-verifying with an unmoved revision; focus collapsing three
events into one read; and a pending refresh neither installing across an account
switch nor being inherited by the next account. **All three signal-dependent
cases were confirmed failing on the unfixed source and passing after** (verified
by reverting the two handler branches and re-running: 3 failed, 106 passed).

Server regressions (`internal/api/channel_notifications_test.go`, 2 new): the
signal reaches both devices of one account and no other account, on mute *and*
unmute, without advancing the revision; and the payload carries no policy value a
client could trust. **Both confirmed failing without the fix.**

### R9-04 — evidence corrections

Dated corrections were added to `planning/p8-checkpoint/verification.md` rather
than editing its original records. `168e89422…` and `3e08497f…` are rollback
**archive** digests (`archives.rollback.sha256` in both JSON reports), not old
binary digests; the correct `baseline.binary_sha256` values are alpha.5
`79d90400e39ce2b08bcbfd15af46b4c95436c2977cbe3117b5063423b2960278` and alpha.3
`918c4c873b4d848aaaeeaf30a187db985935472ab8b27d74c1472ab13e365099`. The 367
source-file count predates three later release inputs. The tested P8 candidate is
qualified as a dirty-checkout build.

### R9-05 — two recovery controls that could not fail

**Push registrations.** The rehearsal asserted zero restored registrations
without ever seeding one, so it could not tell a correct restore from a database
that never had one. A real registration is now created through the real API —
which validates the endpoint host and both keys, so the row is one a browser
could have produced — and its presence in the snapshot source is asserted before
the backup. **Removing the seeding still yields a passing 71/71 run, which is
exactly the defect:** the old assertion was vacuous.

The endpoint is `example.com` (RFC 2606, never a push service) and must resolve
to a public address, because the server refuses an endpoint whose host does not.
Nothing is ever delivered: a stored registration is only read when a mention fires
against it, and this fixture never sends one.

**Reconciliation.** The fixture deliberately lost a post-snapshot block *and* a
deactivation but only re-applied the block, so it could declare success with a
deactivated account still able to sign in on the copy an operator is about to put
in front of members. Both are now re-applied and proved: the block refuses new
writes while retained history stays readable, and the deactivation denies sign-in
and invalidates the account's earlier session, before any traffic cutover.

### R9-06 — an unsent draft was only tested after it had been sent

The existing journey switched accounts only after a successful send had already
cleared the text, so it could not show what happens to words that were never sent.
The new journey holds the **real** request at the network layer (the route waits
and then forwards, so the request genuinely commits), switches accounts in place
while the draft is still unsent, and releases the response afterwards. Account B
sees neither the text nor a result. The positive control proves the held request
did commit for the recipient, so the draft was genuinely unsent rather than a
request the harness invented.

## P9-02 — the contract, recorded before the code

`docs/adr/0013-private-message-evidence-and-request-discovery.md` was written
before any schema, route or interface. It records the evidence scope and its
exclusions, the reviewer capability and why it is not a community capability, the
preview-integrity rule, the 90-day lifecycle including source deletion, account
removal, reviewer revocation, backup/restore and feature-off behaviour, the
durable limits, and the explicit exclusions — in particular that pending request
*introductions* are deliberately not reportable, with the reason.

## P9-03/04 — private text reporting

**Storage.** Schema 13 adds `private_message_reports` and
`private_report_rate_limits`. It only adds tables: `message_reports` and every
community report row, identifier, expiry, pagination and channel-deletion
behaviour are untouched, and the upgrade from schema 12 is covered by the existing
rollback rehearsals. A separate table rather than a new kind of row in the
community one, because a private message has no channel and one table must not
carry two disclosure rules.

**Authorization.** Submission requires current accepted **read** access, so a
participant who blocked the other person keeps readable history and can still
report it; the write path would deny a blocked victim the one action that matters
most. Pending requesters, outsiders, non-participating owners and community
messages are all refused with one indistinguishable 404, so guessing an id
discloses nothing. Review requires `report.review_private`, checked inside the
transaction that reads or mutates the queue. Owners hold it through the ordinary
owner rule; other reviewers need explicit delegation, and the capability cannot
be granted by an account that does not hold it.

**Preview integrity.** The dialog fetches a server-side preview and confirms
*that* text. The submit body carries a version, never text: the saved evidence is
the server's current text, and an edit between preview and confirmation is
refused with a fresh preview rather than silently disclosed. **Confirmed
load-bearing:** disabling the version check makes
`TestPrivateEvidenceRefusesAMessageEditedAfterPreview` fail with a 201 and a
created report.

**Reviewer isolation.** A reviewer sees the submitted snapshot and nothing more.
A test asserts the conversation history, the original message, the conversation
itself and scoped search all still 404 for a reviewer holding only this
capability, and that the community queue stays closed to them.

**Lifecycle and limits.** 90 days from creation, untouched by resolve or reopen
(asserted on both). Durable hourly bound of ten per account stored in the
database — not memory — with `Retry-After` on refusal and a passing control for an
account with room. Retries deduplicate onto the original evidence and never
replace the reporter's explanation. Review is triage: it deletes nothing, bans
nobody, and the account stays active afterwards. Disabling the feature refuses the
routes and **retains** the evidence, and re-enabling brings the record back intact.
That preservation is covered by the server suite
(`TestDisablingPrivateConversationsRefusesEvidenceWithoutErasingIt`), not by the
recovery rehearsal: the rehearsal runs with the feature always on, so it could never
have shown this (completion review 10, R10-08).

**UI.** Report is offered on direct history, including a blocked peer's read-only
history, and opens a dialog that shows the exact server text, who wrote it, who
can review it and how many accounts can, when it expires, that files are not
included, and that review does not delete or block. A changed-preview challenge
keeps the explanation and re-asks for consent. The private-evidence queue is a
separate admin tab behind its own capability, offering no route into the
conversation, with a revoked reviewer losing the tab and the loaded records.

## P9-05 — outstanding request discovery

The count is derived from durable current requests: pending, addressed to the
account, from an available identity that has not opted out and is not blocked in
either direction. A cold start, a restart and a device that missed events all
agree, because nothing is accumulated. It is carried in the account snapshot so a
closed workspace never fetches introductions to draw a badge.

`direct.changed` remains an invalidation hint; the client then performs an
introduction-draft ownership is preserved across the refreshes — the new
account-switch journey proves an unsent draft never crosses accounts.

The badge is hidden rather than zero while the account or its authority is
unverified, so it never claims there is nothing outstanding when nothing has been
confirmed. The Messages entry opens Requests only when the badge has a count;
with nothing outstanding it opens the conversation list as before.

**The implemented notification contract is the request badge plus existing DM
unread state. There is no new direct-message OS notification, push, sound or
ringing**, and directs are not routed into the community mention push job, whose
authorization assumptions are channel-specific. The settings panel and the dialog
both state this.

## Verification results

All commands from the repository root, with `GOCACHE=/private/tmp/brrpbrrp-go-build`.

| Check | Command | Result |
| --- | --- | --- |
| Frontend unit/state tests | `npm --prefix web test` | **112/112 pass** |
| Frontend types/a11y | `npm --prefix web run check` | **0 errors, 0 warnings** |
| Frontend format | `npm --prefix web run format:check` | clean |
| Frontend build | `npm --prefix web run build` | `6b00f391da81a4bc` |
| Static analysis | `go vet ./...` | clean |
| Full race suite | `go test -race -count=1 -timeout 25m ./...` | **all 10 packages ok on the committed checkpoint**; `internal/api` 966.515s, `internal/db` 82.295s, `internal/backup` 18.354s, `internal/rtc` 17.438s, `internal/authz` 10.872s, `internal/app` 8.905s, `internal/installer` 6.413s, `internal/scraper` 5.550s, `internal/ws` 5.162s, `web` 1.976s; wall ≈16.2 min. An earlier uncached run on the same code reported `internal/api` 1112.692s. |
| Browser, default-off | `BRRP_E2E_PRIVATE_CONVERSATIONS=0 npm --prefix web run test:e2e` | **13/13 pass**, 1.6 min |
| Browser, opted in | `BRRP_E2E_PRIVATE_CONVERSATIONS=1 npm --prefix web run test:e2e` | **18/18 pass**, 2.2 min (adds the six `directs` journeys) |
| Fresh recovery (community) | `node scripts/upgrade-recovery.mjs --binary … --report planning/p9-checkpoint/recovery.json` | **23/23 pass**, schema 13 |
| Fresh recovery (private) | `node scripts/upgrade-recovery-private.mjs --binary … --report planning/p9-checkpoint/recovery-private.json` | **85/85 pass** (was 68) |
| Upgrade/rollback vs alpha.5 | `node scripts/upgrade-rollback.mjs --old-release dist/releases/brrpbrrp-0.4.0-alpha.5-darwin-amd64.zip --report …` | **57/57 pass**, alpha.5 schema 10 → 13 |
| Upgrade/rollback vs alpha.3 | `node scripts/upgrade-rollback.mjs --old-release dist/releases/brrpbrrp-0.4.0-alpha.3-darwin-amd64.zip --report …` | **55/55 pass**, alpha.3 schema 2 → 13 |
| Browser-step audit | `node scripts/check-browser-steps.mjs --self-test` and the plain run | pass |
| Docker input audit | `node scripts/check-docker-inputs.mjs [--self-test]` | pass |
| Source package audit | `BRRP_GO=… node scripts/check-source-package.mjs [--self-test]` | pass, 379 files |
| Dependency notices | `BRRP_GO=… node scripts/licenses.mjs --check` | up to date |
| Baseline fetch self-test | `node scripts/fetch-baseline-release.mjs --self-test` | pass |

The private recovery rehearsal grew from 68 to 85 checks: the seeded push
registration and its pre-backup proof, both reconciliation decisions with their
consequences, the private evidence submitted through the real two-step flow with
its receipt shape and a duplicate retry after the restore, and the reconstructed
outstanding-request count for an account that has one and one that does not.

## Defects this tranche found and fixed in its own work

- A dropped `Settings` branch in the admin panel dispatch chain, introduced while
  adding the new tab, fell through to `Maintenance`. The existing authority
  journey caught it; it is fixed and the journey passes.
- A `/api/state` 500 from an incorrect `direct_preferences` join in the request
  count, and a preview 500 from `messages.updated_at` where the column is
  `edited_at`. Both are corrected against the real schema.

## Not done here (honest remainder)

- No hosted CI execution. The workflow is a reviewed definition plus locally
  executed equivalents, and `scripts/check-browser-steps.mjs` proves the failure
  propagation property without being a browser run.
- No Windows/Tailscale host, Docker runtime, physical-device media, encrypted
  off-host recovery, measured RPO/RTO, delivered-alert proof, capacity or soak
  result, or independent security/accessibility review. Nothing here changes the
  G0–G8 gates, all of which remain open.
- Browser coverage is Chromium with synthetic media capture; the declared
  cross-browser and physical-media matrix is a later qualification step.
- Pending request introductions are not reportable, by the recorded decision in
  ADR 0013. Request-harassment reporting needs recipient-only, server-verified
  evidence with a snapshot-before-block workflow, and is required before any
  broad private-message pilot.
- No new distribution archives are built here. New package identities are
  produced only from a clean committed source with the evidence recorded
  afterwards, and this tranche's artifacts are local.

## Next bounded tranche

1. Server-verified introduction reporting, then a dedicated DM
   activity/notification milestone with safe previews, preference precedence,
   deduplication and active/background/closed-PWA behavior.
2. Consented groups: invitations, membership/history/owner transfer, account
   export and lifecycle.
3. Named threads, moderation timeouts and invitations, composer/upload/navigation
   and human accessibility.
4. A measured media architecture spike, then private calls and screen sharing.
5. Public repository, security intake, maintainer and signing ownership; both-host
   recovery and operations; reference load/soak.
6. Independent security/privacy and browser/accessibility reviews; newcomer
   install/restore; the 14-day beta period; a frozen candidate; explicit
   publication approval.
