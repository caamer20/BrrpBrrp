# P8 stabilization checkpoint — verification record

**Date:** 27 September 2026 (America/Los_Angeles)
**Scope:** review 8's tranche P8-01…P8-05 with the small R8-08 tooling fix. Local implementation and verification only; no hosted CI, host, Docker-runtime, off-host, capacity or independent-review acceptance is claimed.

## Source and artifact identities

| Identity                                                | Value                                                                                                                                                                                                                                              |
| ------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Reviewed source (review 8)                              | `1f762e9a3cee4e1871f434b4e2c7e52360a38511`                                                                                                                                                                                                         |
| Review-8 documentation commit                           | `17893f9…` ("Record review 8 findings, documentation corrections and the P8 handoff")                                                                                                                                                              |
| Implementation commit (this checkpoint)                 | `606cdb120326507c280cf0c1fbc89c85b44598cd`                                                                                                                                                                                                         |
| Working tree during final runs                          | clean except `planning/p8-checkpoint/` (this new evidence)                                                                                                                                                                                         |
| Toolchain                                               | Go 1.27.1 (`.tools/go/bin/go`), Node 24.2.0, Playwright Chromium 1243                                                                                                                                                                              |
| Frontend build identity                                 | `8263e263579a2b47` (same hash when built from the extracted source archive)                                                                                                                                                                        |
| Ad-hoc dev binary `dist/brrpbrrp-checkpoint`            | sha256 `c73dde24ecc960421e08b0380f01f520d280946fe141e29f8e68408c2bd2cb91`                                                                                                                                                                          |
| **Packaged darwin/amd64 executable (tested candidate)** | sha256 `68c2458fbd9fcb248d691d6dcae24fe15d4a5ac0b527220a7634d3274f9d5bc8`, identical in both package builds, from `dist/releases/brrpbrrp-0.4.0-alpha.6-darwin-amd64.zip` |
| Native archives (final, post-docs) | sha256: linux-amd64 `04c03e50fe09f33b86489af5bff58a360f8311df349e520ce6a2e2ccf43bdab5`, linux-arm64 `3057a67555d1c39335ac8ccb604bcd2225ea4c7badc32abdf1a56247f0d6b9ac`, windows-amd64 `08abb0df15f70ad31bce768bcca1d71eac8292f9fd4074e996829649c21d0c53`, windows-arm64 `d83794e2a6ab88a70d4fa783d75406e471aa4fe2b668fa6fba1ce379fabfc973`, darwin-amd64 `096b179ee51c2950e141019ebd356eb0126ee54b9c23b4880272d000ee97580f`, darwin-arm64 `ac3b112606617c4915e96d604ac5906be9972bf25ea76e2ebc0bbde823580b58` (also in `dist/releases/SHA256SUMS`) |
| Source archive (final) | sha256 `623e4d303ea815bcec6bae7db6caf7f18d0315ec0e59b4992a2b7b19d6b87ee0` (`dist/brrpbrrp-docker-source-0.4.0-alpha.6.zip`) |
| Tested old releases                                     | alpha.5 archive sha256 `f186eecd…` (darwin/amd64 zip; all six pinned in `scripts/baseline-releases.json`), rollback-tested old binary sha256 `168e89422b1a52e9…`; legacy alpha.3 archive sha256 `85c13227…`, old binary sha256 `3e08497fddeb669f…` |

The packaged binary and the ad-hoc checkpoint binary differ because the packager
embeds the version stamp; the packaged bytes are the candidate used for the final
consumer, recovery, rollback and browser runs recorded below.

## Corrections (27 September 2026, review 9 / P9-01)

These are dated corrections to the record above. The original execution results
are left exactly as they were recorded; nothing here replaces a run, it corrects
how a recorded value was described. They are what review 9 found while verifying
this checkpoint (R9-03, R9-04), and they are recorded here so the checkpoint stays
self-describing.

**The tested old-release digests on line 20 are mislabeled.** `168e89422b1a52e9…`
and `3e08497fddeb669f…` are the **rollback backup archive** digests, not old
executable digests. Both JSON reports name them as such: `rollback-alpha5.json`
records them at `archives.rollback.sha256`, written by "the prior release, from the
un-migrated installation", and `rollback-alpha3.json` at the same field. The
correct `baseline.binary_sha256` values are:

| Baseline | `baseline.binary_sha256` |
| --- | --- |
| alpha.5 Darwin/amd64 | `79d90400e39ce2b08bcbfd15af46b4c95436c2977cbe3117b5063423b2960278` |
| alpha.3 Darwin/amd64 | `918c4c873b4d848aaaeeaf30a187db985935472ab8b27d74c1472ab13e365099` |

**The source archive file count on the `check-source-package.mjs` row is
stale.** The recorded 367 describes the package list before three later release
inputs were added. This is a later file count, not a reopening of the R8-08
trailing-blank defect that row was written about: the packaging inputs changed
after that run, and the blank-entry fix is unrelated to them.

**The tested candidate is a dirty-checkout build, not a clean-commit build.** The
packaged executable `68c2458f…` embeds source `606cdb1` with
`vcs.modified=true`, and the source archive `623e4d30…` differs from `606cdb1` in
`Makefile`, `scripts/release.go` and three documentation files. Against `876260a`
only two progress-ledger header lines differ, so the application inputs in both
artifacts align; this is a provenance qualification, not evidence of an untested
application change. These artifacts are preserved as tested local bytes. The P9
checkpoint builds from one clean committed source and binds source, frontend,
executable and archive identities to the runs that consume those exact bytes.

## Fresh verification at this checkpoint (all commands from the repository root)

| Check                              | Command                                                                                                                                                                 | Result                                                                                                                                                                          |
| ---------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Frontend unit/state tests          | `npm --prefix web test`                                                                                                                                                 | **105/105 pass** (101 prior + 4 new store tests for R8-02)                                                                                                                      |
| Frontend types/a11y                | `npm --prefix web run check`                                                                                                                                            | **0 errors, 0 warnings**                                                                                                                                                        |
| Formatting                         | `npm --prefix web run format:check`                                                                                                                                     | clean                                                                                                                                                                           |
| Static analysis                    | `go vet ./...`                                                                                                                                                          | clean                                                                                                                                                                           |
| Formatting (Go)                    | `gofmt -l internal cmd scripts`                                                                                                                                         | clean                                                                                                                                                                           |
| Full race suite, uncached          | `go test -race -count=1 -timeout 20m ./...`                                                                                                                             | **all packages ok**; internal/api 887.109s; wall ≈14.9 min. This is the first recorded fully green whole-race run for alpha.6; review 8's attempt exited 1 on a pinning fixture |
| Browser suite, default-off gates   | `BRRP_E2E_PRIVATE_CONVERSATIONS=0 npm --prefix web run test:e2e` against the packaged binary                                                                            | **13/13 pass** (release, authority, privacy ×4, voice, account-switch, ownership)                                                                                               |
| Browser suite, private mode        | `BRRP_E2E_PRIVATE_CONVERSATIONS=1 npm --prefix web run test:e2e` against the packaged binary                                                                            | **16/16 pass** (adds directs ×3: the existing consent journey and both new regressions)                                                                                         |
| Fresh recovery, packaged binary    | `node scripts/upgrade-recovery.mjs --binary … --report planning/p8-checkpoint/recovery.json`                                                                            | **23/23 pass**                                                                                                                                                                  |
| Fresh recovery, private fixture    | `node scripts/upgrade-recovery-private.mjs --binary … --report planning/p8-checkpoint/recovery-private.json`                                                            | **68/68 pass** (new script; see below)                                                                                                                                          |
| Upgrade/rollback vs real alpha.5   | `node scripts/upgrade-rollback.mjs --binary … --old-release dist/releases/brrpbrrp-0.4.0-alpha.5-darwin-amd64.zip --report planning/p8-checkpoint/rollback-alpha5.json` | **57/57 pass**; alpha.5 schema 10 → 12 and back                                                                                                                                 |
| Upgrade/rollback vs legacy alpha.3 | `node scripts/upgrade-rollback.mjs --binary … --old-release dist/releases/brrpbrrp-0.4.0-alpha.3-darwin-amd64.zip --report planning/p8-checkpoint/rollback-alpha3.json` | **55/55 pass**; alpha.3 schema 2 → 12 and back; legacy alpha.3 refuses the candidate's format-2 snapshot as recorded                                                            |
| Native package consumer            | `node scripts/check-package.mjs <extracted darwin/amd64 binary>`                                                                                                        | pass (setup, backup, restore, lock, health, production guard, embedded UI, manifest, graceful stop)                                                                             |
| Source package consumer            | extract source zip → `npm ci --prefix web && npm run build --prefix web && go build ./cmd/brrpbrrp` → `check-package.mjs` on that binary                                | pass; frontend build identity identical (`8263e263579a2b47`)                                                                                                                    |
| Dependency notices                 | `node scripts/licenses.mjs --check`                                                                                                                                     | up to date (39 components)                                                                                                                                                      |
| Docker input audit                 | `node scripts/check-docker-inputs.mjs` + `--self-test`                                                                                                                  | pass (static; no Docker runtime here)                                                                                                                                           |
| Source package audit               | `node scripts/check-source-package.mjs` + `--self-test`                                                                                                                 | pass; **367 files** (the trailing-blank phantom entry is fixed, R8-08)                                                                                                          |

## Defect fixes and their proof

### R8-01 — attachment claim no longer survives upload-permission revocation

**Fix** (`internal/api/messages.go`): `createMessage` re-checks the caller's current
`message.upload` capability — using the transaction-scoped principal from
`scopeInTx` — after acknowledged/legacy receipt replay handling and before the
operation is claimed or any message/claim row is written. Denial commits no
message, receipt or claim; `conversationAccess(..., write=true)` deliberately does
NOT gain the upload check, so text/typing/reaction paths are untouched.

**Regressions** (`internal/api/staged_attachment_claim_test.go`, focused race run
50.091s plus the full suite above):

- Reproduced the defect on unfixed source first: `TestStagedAttachmentClaimRechecksUploadCapability` failed with "staged claim survived revocation: 201" in BOTH channel and direct scopes; it passes after the fix.
- Controls that pass: fresh upload 403 and text send 201 while revoked; denied claim leaves no message (counted by `conversation_id`), no `message_operations` receipt, no claim (`attachments.message_id` stays NULL) and no mention rows; the peer cannot download the still-staged file (404); same denied key retries to 403 while revoked; after restoration the same key sends exactly once and replays 200; an already-acknowledged attachment send replays 200 with the file still readable after revocation (`TestAcknowledgedAttachmentSendReplaysAfterUploadRevocation`); a deleted send still replays 409 and never recreates content (`TestDeletedAttachmentSendReplayStaysNonRecreatingAfterUploadRevocation`); cross-destination and unscoped-upload refusals are unchanged (`TestAttachmentClaimKeepsDestinationBindingUnderCapabilityCheck`).

### R8-02 — mention notification policy loads regardless of the selected conversation

**Fix** (`web/src/lib/workspace.svelte.ts`, `web/src/lib/components/ChannelNotifications.svelte`): the account's channel mute map is now loaded by the workspace store after every admitted snapshot while unverified — independent of whether a channel header is mounted (a DM selects `DirectControls`, so the old component never loaded it). Guards: session/account/authorization-generation admission, single-flight version, a mutation counter so a delayed preference GET can never overwrite a newer mute PUT, bounded retry (3 attempts) with fail-closed uncertainty, and full reset on sign-out/invalidation. The header component only renders and edits the current channel's entry.

**Proof:** unit tests added to `web/tests/workspace-state.test.mjs` (snapshot verifies policy without a channel header; delayed read cannot overwrite the newer mute; a previous account's answer is never installed; failure stays fail-closed). Rendered journey `mention policy loads while staying in a direct conversation` (`web/e2e/directs.spec.mjs`): a fresh DM deep link provably issues the preference fetch and an allowed community mention toasts while the account stays in the DM; an authority refresh there re-verifies and a follow-up mention arrives; a muted channel stays silent (asserted after a sequenced DM beacon so the absence is ordered); a switch to a fresh account loads that account's policy without inheriting the previous mute. **This journey fails on the unfixed client** (the preference GET never fires; the watcher times out) and passes on the fixed one.

### R8-03 — unsent request intro survives unrelated contact/authority refreshes

**Fix** (`web/src/lib/components/MessagesInbox.svelte`): the draft's account ownership is tracked separately from fetched contact data. The refresh effect clears `target`/`introduction` only when the account actually changed (or on explicit discard/admitted send). After each reload the destination is revalidated from freshly admitted lists: a deactivated/disappeared/blocked target disables submission, explains why and keeps the copyable text — never silently retargeting. `act()` returns admitted success instead of the caller inferring it from "no error".

**Proof:** rendered journey `an unsent request introduction survives unrelated refreshes and a refused send`: text survives an unrelated incoming request and a same-account authority refresh (both observed through the inbox's reload); deactivating the target disables the send with an explanation while keeping the text; a peer-side block surfaces the server refusal with the text intact; after unblock the same text sends, the draft clears only then; a different account finds an empty compose field; a reload preserves nothing (no silent durable storage of private drafts). **Pre-fix failure confirmed:** the compose form disappears after the first unrelated refresh (assertion failed at step "form visible" on the unfixed client).

### R8-04 — CI now gates the actual feature set (definitions; hosted run pending)

`.github/workflows/check.yml`: the browser job runs the suite twice — default-off, then `BRRP_E2E_PRIVATE_CONVERSATIONS=1` with a hard grep that the `directs` project executed — keeping per-mode logs/reports as failure artifacts; `run.mjs` prints the feature mode. A `recovery` job on every push runs both recovery rehearsals and the extracted native + extracted source consumer builds. A `rollback` job runs with the packaging dispatch and stages the pinned archived releases through `scripts/fetch-baseline-release.mjs`, which verifies each archive's sha256 against the checked-in `scripts/baseline-releases.json` and **fails loudly** when no approved source (`BRRP_BASELINE_RELEASE_DIR` / `BRRP_BASELINE_RELEASE_URL`) is configured — no silent skip, no stand-in rebuild. `packages` now requires `[test, browser, docker, recovery, rollback]`. Workflow edits are definitions, not hosted-CI evidence: a GitHub-hosted run remains to be done, and the alpha.5/alpha.3 archives must be placed on an approved host for the rollback gate to pass there (locally they are supplied from `dist/releases` and the same rollback rehearsals pass).

### R8-05 — private-state recovery demonstrated

`scripts/upgrade-recovery-private.mjs` (68 checks, report `planning/p8-checkpoint/recovery-private.json`): seeds consented private state through real HTTP APIs on a disposable gate-opened install — accepted pair with attachment bytes/receipt/cursors, pending request, rejected request in cooldown, blocked pair, hide preference, contact opt-out, a removed participant (MFA-gated anonymize ceremony included) — snapshots, takes post-snapshot decisions (fresh block, deactivation) and proves them live, restores into an empty destination, and asserts through freshly authenticated sessions only: old sessions and push registrations are absent from the restored database; private history/bytes/search/cursors/preferences survive; the owner and an outsider get 404 on private reads; the pre-snapshot block still refuses writes; the removed peer's history presents as Deleted member; a lost-ack replay answers its receipt without duplicating the message; the post-snapshot block and deactivation are absent until the documented reconciliation re-applies the block (writes then refuse again). Scope stays local; off-host recovery is separate acceptance.

### R8-06/R8-08 — checkpoint and tooling defect

Fully green uncached race suite, two-mode browser runs and recovery reports recorded above; the `check-source-package.mjs` count inflation from a trailing blank entry is fixed (filter-before-normalize) with empty-input/Windows-path/space-path self-test controls. Prior alpha.5/alpha.3 archives and their manifests are preserved unchanged; the alpha.5-era checksum files are copied to `planning/alpha5-evidence/` before the alpha.6 packaging run replaced the working copies.

## Not done here (honest remainder)

- No hosted CI run exists (no GitHub remote/host configured); the workflow is a reviewed definition plus locally executed equivalents.
- No Windows/Tailscale service, Docker runtime, physical-device media, capacity/soak or independent security/accessibility evidence; nothing here changes those gates (G0–G8 all remain open).
- Browser voice coverage still uses synthetic capture with real WebRTC/RTP, not microphones and networks.
- The browser journeys added are Chromium; the declared cross-browser matrix is a later qualification step.
