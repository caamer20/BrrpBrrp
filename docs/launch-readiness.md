# Project and deployment readiness

Assessment date: **11 October 2026**. BrrpBrrp is at **1.0.0-rc.1**, schema 25. Its planned
feature families are implemented, and a public GitHub repository is configured for
cross-platform qualification. It is ready for structured candidate testing; it is not yet
qualified for a production launch.

## Current assessment

| Area | Assessment | Evidence or remaining work |
| --- | --- | --- |
| Product scope | Substantial release candidate | Channels, roles, private conversations, moderation, voice, screen sharing, backups and installer commands are implemented. See [project overview](project-overview.md). |
| Repository presentation | Public development repository | Publication audit, reporting contacts/intake, policies, templates, review ownership and dependency updates are present. |
| Automated verification | Public CI passed; qualification gates active | The preparation PR passed Windows/macOS/Linux Go, frontend/static and CodeQL checks. Full-main security analysis found baseline issues under review; release qualification blocks until addressed. |
| Upgrade fixtures | Connected for CI | Preserved alpha archives retain their hashes. Linux schema-13 reconstruction has explicit provenance. See [GitHub setup](github-setup.md#ci-fixtures). |
| Distribution | Publication requires full qualification | Six native targets plus source/checksums are defined. Exact tested revision, run, assets and publication status belong in [GitHub Releases](https://github.com/caamer20/BrrpBrrp/releases). No registry image is published. |
| Windows deployment | Unqualified | Real service, ACL, firewall, tray, reboot, update and restore acceptance is outstanding. |
| Linux deployment | Partly verified | Hosted Docker build/lifecycle passed. Systemd lifecycle, real deployment, external networks and off-host recovery need target-host evidence. |
| macOS deployment | Unqualified | Local builds do not establish installation, login/reboot lifecycle, update and restore on a clean host. |
| Security, privacy and accessibility | Reviews outstanding | Automated checks are useful evidence; independent reviews remain open. |
| Community beta and soak | Not started | The roadmap requires real community use and observation of a frozen candidate. |

## Public preparation on 11 October

- Public visibility is confirmed. Private vulnerability reporting, secret scanning and push protection
  are enabled. The owner supplied a real conduct/privacy contact.
- The [publication audit](public-release-audit.md) covers reachable GitHub history, retained logs and
  archival fixture inventories/checksums. Historical scanner exceptions are exact fingerprints, and
  a synthetic new-token control still fails.
- Routine CI now shares one Linux frontend/static job and native Go coverage on three fixed OS labels.
  Full qualification retains its promotion dependencies and adds CodeQL, shipped-default browser
  coverage and the WebKit core subset. The badge writes a separate branch.
- All 406 frontend tests passed locally. The new shipped-default restricted-channel journey and its
  nine setup dependencies passed (10 total). It exposed and regresses an access-inspector confirmation
  that was immediately cleared during refresh; that UI defect is fixed. Individual grants, outsider
  denial and live revocation are covered. Broader real-host and independent review work remains open.

The first full-main scan at `13c990d` found 19 baseline findings that the PR's changed-code analysis
had not reported. The qualification gate blocked packaging. [CodeQL review](codeql-findings-review.md)
records the repairs and the exact-source policy for individually reviewed false positives. Release
notes carry the final frozen-revision qualification result; this checkpoint is not that result.

The repair tree passed 413 frontend tests, including 15 findings-gate controls, plus local
app/backup/installer tests, activity filter/pagination tests and the static/workflow gates.
Fresh hosted full-ref scanning and qualification are still required before publication.

## Verification on 8 October

Results here distinguish completed checks from pending evidence. Full logs are retained by the
development session; hosted logs and artifacts will be available in
[GitHub Actions](https://github.com/caamer20/BrrpBrrp/actions).

- Frontend production build: passed.
- Svelte/TypeScript checks: zero errors and zero warnings; formatting passed.
- Frontend unit suite: 392 passed on rerun. An initial run exposed a test that counted a read-cursor
  request as another send; that test now counts sends separately and awaits the second send explicitly.
- Full Go suite: passed on macOS with `go test -count=1 -timeout 20m ./...`; the API package
  completed in 274 seconds. This pass did not use the race detector.
- `make check`: passed, including Go vet/module/formatting, notices, document links,
  Docker/Compose/environment audits, source-package contents and workflow failure propagation.
- Workflow syntax: passed actionlint 1.7.12.
- Chromium browser journeys, conversation features disabled: 30 passed, zero failed (3.4 minutes).
- Preserved macOS schema-13 seed and upgrade: passed; every compared value and identity was retained,
  and provenance appears in the seed and upgrade reports.
- Chromium private-conversation mode: 45 passed, zero failed (5.0 minutes). Both modes keep
  restricted channels disabled. These browser runs preceded the build-dependency patch below.
- Dependency audit found `source-map-js` 1.2.1 affected by
  [GHSA-68fv-2mgg-jv7q](https://github.com/advisories/GHSA-68fv-2mgg-jv7q). The lockfile is
  updated to 1.2.2. `make check`, all 392 frontend tests and the npm audit passed after the patch;
  GitHub marks the alert fixed.
- First hosted Docker build found that the notices generator treated an absolute destination
  as relative. That defect is fixed. The [Docker job on commit `4081dde`](https://github.com/caamer20/BrrpBrrp/actions/runs/37822474840)
  passed Compose validation, the x64 build and container lifecycle/persistence check, and an ARM64
  image build. ARM64 runtime and a real operator deployment remain unqualified.
- Hosted macOS Go/static/frontend tests passed on commit `4081dde`. Linux exposed a host-dependent
  notices comparison; the generator now covers the shared six-target release set. Linux and Windows
  caller environments both verify the same 39-component notices/SBOM, and every target's module set
  is covered. The race workers also now build their own embedded frontend on fresh runners.
- The first hosted runs were superseded during fixes. Final cross-platform, race, browser, recovery
  and rollback qualification is tracked in [GitHub Actions](https://github.com/caamer20/BrrpBrrp/actions).
  Package creation and the optional draft prerelease remain blocked until those jobs pass.
- The [manual qualification on `b956ad3`](https://github.com/caamer20/BrrpBrrp/actions/runs/37824339090)
  passed macOS and Docker but failed Windows and Linux Go tests. Windows exposed backup URI
  encoding and CPU-duration defects; both are repaired, with path/metrics regressions. Unix-only
  test assumptions were corrected. Linux revocation tests now pause the HTTP writer explicitly
  before the authority changes, preserving assertions for interrupted and unaffected streams.
  Local app/database/backup/metrics tests and three repetitions of the affected API tests passed.
  Fresh hosted qualification is still required for those repairs.
- The next [baseline run on `e1dc5f1`](https://github.com/caamer20/BrrpBrrp/actions/runs/37828534690)
  passed macOS, Docker, the Linux API suite and Windows API/app/database/backup/installer/metrics
  packages, but the screen-receiver test failed on Linux and Windows.
  Its two competing RTP readers could consume the same packets before the assertion saw them.
  The test now uses one reader registered before negotiation and checks the offered receiver
  and exact screen payload. The focused test passed 25 repetitions and 10 with the race detector.
- The eight dependency/action updates were checked together in an isolated integration tree:
  all 397 frontend tests, the full Go suite (API 253 seconds), `make check`, workflow syntax
  and redistribution notices passed. Go vulnerability scanning found no affected code;
  one finding was confined to an unused required module. Hosted combined qualification remains
  the gate for preparing the candidate packages.
  The combined suite ran at local integration revision `51b40e56`; the later RTC-only check
  ran ten times at `d21d9da9`. After all eight PRs were squash-merged, main revision
  [`426658f`](https://github.com/caamer20/BrrpBrrp/commit/426658f871c22a9234d64e431107bf3cddb94a78)
  matched the later integration tree byte for byte. The subsequent change to this assessment
  records evidence only. Final hosted qualification will run on the resulting main revision.
- The private-compatible README download-count workflow passed. It counts published application
  archives and excludes draft releases and archival CI fixtures; the initial count is zero.
- Final [hosted qualification at `9d7cb4d`](https://github.com/caamer20/BrrpBrrp/actions/runs/37833439182)
  could not start its runners. GitHub reported an account payment/spending-limit issue; no
  test steps executed. At the owner's request, repository Actions execution and the three
  project workflows were paused until the Actions limit resets. There are no running or queued
  workflows. See [workflow setup](github-setup.md#workflows-and-runner-use).
- The owner authorized a retry within 10% of the Actions allowance. Workflows now share setup
  and static audits, and full qualification is manual-only to prevent automatic spending. The
  new one-job **Budget check** has a bounded timeout and cannot package or qualify a release.
  The owner later approved one attempt with an eight-minute job timeout and ten-minute billable
  allowance. The [budget run at `80a166c`](https://github.com/caamer20/BrrpBrrp/actions/runs/37888802201)
  failed before runner assignment or any steps. GitHub reported recent failed account payments
  or a spending limit needing attention. Repository execution and the budget workflow were paused
  again; this attempt establishes no hosted test results. Account billing must be resolved before retrying.
  Local workflow syntax, failure controls, `make check` and all 397 frontend tests passed.
  Fresh source-package verification found two missing audit inputs; its manifest now includes
  the tracked SBOM and acceptance matrix, along with the new shared action and script.
- Local, **unqualified** candidate archives at source `9d7cb4d7` were built for all six targets,
  with a Docker source ZIP, SBOM and verified checksum manifests. The actual extracted macOS
  package and a fresh build from the extracted source ZIP passed CLI/setup/backup/restore/locking/
  health/UI/clean-shutdown consumer checks. This does not establish Windows or Linux runtime acceptance.
- Final local recovery rehearsals passed: 23 community checks, 172 private-state checks and
  18 checks of the encrypted backup procedure using OpenSSL. These were disposable local destinations,
  not a qualification of a real off-host deployment.
- Firefox media verification passed all five journeys, together with its nine Chromium setup
  dependencies (14 total, 1.5 minutes), on the final merged code. A first attempt used a broken
  cached Firefox that could not load XPCOM; a fresh task-local browser installation passed smoke
  launch and the full invocation. The browser cache failure was not a product test pass.

Local candidate files, provenance, checksum manifests and recovery/consumer/browser reports are
kept under `dist/candidates/9d7cb4d7f2d4/` in the development checkout. They are generated artifacts,
excluded from Git, and are not a published or hosted-qualified release. This assessment's later
documentation changes do not alter that candidate's source revision.

## Next steps in order

1. **Establish a green candidate.** Finish local checks, resolve hosted failures and retain the
   source revision, run URL and artifacts. Keep automated failures blocking package preparation.
2. **Complete conversation acceptance.** Retain the new shipped-default browser proof and extend
   restricted role grants, announcements and voice/device coverage against the documented scenario catalog.
3. **Prepare and qualify the candidate archives.** Use the gated manual workflow, then install
   its actual Windows, macOS and Linux packages on clean hosts. Include ARM64 runtime testing where
   it is advertised, Docker persistence and update/rollback on each deployment type.
4. **Prove operation outside one machine.** Trusted HTTPS, two-network voice with relay fallback,
   screen sharing, real phone/tablet behavior, push, monitoring and off-host recovery need recorded evidence.
5. **Complete reviews and the beta.** Resolve blocking independent security/privacy/accessibility
   findings, run the community beta and observe a frozen candidate before stable publication.
6. **Publish the tested candidate deliberately.** Public source and private reporting are available.
   Verify an external test report reaches the maintainer, review the draft candidate, and publish a
   prerelease for beta use before promoting a fully qualified stable build.

The detailed release gates remain in [ROADMAP.md](../ROADMAP.md) and the
[production runbook](production-readiness.md). This assessment records readiness; it does not
change those gates or turn missing evidence into a pass.
