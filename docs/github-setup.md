# GitHub and release setup

[BrrpBrrp](https://github.com/caamer20/BrrpBrrp) became public on **11 October 2026**, after
an audit of reachable history, retained Actions logs and fixture assets. See the
[publication audit](public-release-audit.md). Windows, macOS and Linux are build targets
on x64 and ARM64; [deployment readiness](launch-readiness.md) records runtime qualification.

## Repository configuration

MIT licensing, contributor terms, issue/PR templates and maintainer ownership are included.
Use squash merges and delete merged branches. The documentation is the primary guide;
wiki and project boards are disabled. Private vulnerability reporting, secret scanning and
push protection are enabled. Security reports use [SECURITY.md](../SECURITY.md); conduct
and privacy reports use the contact in [the code of conduct](../CODE_OF_CONDUCT.md).

Protect `main` with pull requests, passing CI checks, conversation resolution and linear
history. Use actual completed check names. A single-maintainer repository does not require
its owner to approve their own PR. Release qualification adds gates beyond routine PR checks.

Dependabot checks Go, frontend, Actions and Docker dependencies weekly, with at most one
open PR per ecosystem. Review changes and regenerate notices/SBOM when the linked set changes.
All workflow actions are pinned to immutable commits. PR jobs have read-only source access;
release-write permissions belong only to the optional manual draft job on `main`.

## Workflows and runner use

| Workflow | Trigger | Purpose |
| --- | --- | --- |
| **CI** | PRs and pushes to `main`; reused by qualification | Frontend/static, npm/Go vulnerability and secret audits once on Linux; native Go builds/tests on Ubuntu, Windows and macOS after audits pass |
| **CodeQL** | PRs, `main`, weekly, manual audit; reused by qualification | Go and JavaScript/TypeScript code scanning |
| **Release qualification** | Manual | Complete CI, CodeQL, race shards, browser engines/modes, Docker, recovery, rollback, package consumers and candidate archives |
| **Docker** | Qualification caller or manual | Compose validation, x64 lifecycle/persistence, ARM64 image build |
| **Release download count** | Daily, release changes or manual | Filtered published archive count on the `badges` branch |
| **Budget check** | Manual fallback | One capped Linux job; never qualifies or packages a release |

CI and Docker pin Go 1.27.2. The `go.mod` minimum also rejects older local compilers.
The native test matrix waits for the shared audit job; full release Docker qualification
waits for the complete shared tests. Scanner/network errors fail the audits.

Only standard GitHub-hosted runner labels are used. They are
[free for public repositories](https://docs.github.com/en/actions/concepts/billing-and-usage);
larger runners are billable and are not configured. Native tests use fixed OS release labels.
Superseded PR runs are canceled. Browser diagnostics retain for seven days; recovery and
candidate-package artifacts retain for fourteen days. Caches improve setup time and do not
replace verification of the current commit.

Private runs on 8 October were blocked before runner assignment by GitHub's account
payment/spending-limit message, including the approved eight-minute budget attempt.
Public runner startup was verified on 11 October: the preparation PR reached real build/test
steps and both CodeQL analyses completed. Full results belong in deployment readiness.
If account restrictions block future runs, pause execution and resolve the account issue.

## README download count

The badge at `badges/downloads.svg` includes native and Docker source ZIPs attached to
published version releases, including prereleases. Drafts, archival CI fixtures and ancillary
assets are excluded. It counts archive downloads rather than unique people or installations.
The updater writes only the standalone `badges` branch; it does not bypass protection on `main`.
An API failure preserves the last successful count, and updates are serialized.

The source archive includes the updater and an initial local SVG. With an authenticated `gh` CLI:

```sh
GITHUB_REPOSITORY=caamer20/BrrpBrrp node scripts/update-download-badge.mjs --output /tmp/downloads.svg
```

## CI fixtures

The repository variable `BRRP_CI_BASELINE_TAG` identifies the archival fixture release,
`ci-baselines-2026-10-08`. CI downloads its assets with the read-only workflow token, so it works
for public and private repositories. The published archives are migration inputs, not supported application releases.

- **alpha.3 and alpha.5:** preserved native ZIPs, verified against the unchanged digests in
  `scripts/baseline-releases.json`. They are migration/rollback fixtures, not new supported releases.
  The alpha.5 source commit is recorded there; the alpha.3 source commit is not known.
- **Linux schema 13:** a fixture reconstructed from commit
  `2869db55394b2b2a004424672517b38d6908586b`, using that commit's frontend and locked dependencies.
  Its executable and archive hashes, build tools and commands are recorded in
  `scripts/schema13-baselines.json`, with `reconstructed-from-source` provenance carried into
  seed and upgrade reports. It proves migration from the historical application. It does not
  prove the identity of an actually distributed Linux schema-13 executable.
- **macOS schema 13:** the preserved executable identity remains recorded separately.

The archival fixture release is attached to the schema-13 source checkpoint; that tag does not
claim to identify the source of every archive stored there. Fixture downloads stay outside the
candidate package directory. Missing or mismatched bytes fail the job.

## Prepare a release

1. Commit the version, notes in `docs/releases/`, and notices/SBOM through a reviewed PR.
   The Go application, frontend manifest and release helper must agree on the version.
2. Run **Actions → Release qualification → Run workflow** on `main`. Leave **Create a draft
   prerelease** unchecked to qualify archives, or check it to prepare a draft after every gate passes.
3. Packaging waits for all required job families, including CodeQL analysis and an exact-revision
   SARIF findings gate that blocks unreviewed high/critical security findings. Later branch fixes,
   GitHub dismissals and SARIF suppressions do not waive this gate. Individually reviewed false positives
   require exact result/flow/query-pack identity and a matching digest of the analyzed Git tree;
   see [CodeQL review](codeql-findings-review.md). Browser checks cover feature
   off/private modes, shipped defaults with restricted-channel grants/revocation, Firefox media and
   the documented WebKit core subset. ARM64 packaging is not ARM64 runtime qualification.
4. Package generation signs GitHub/Sigstore build provenance on `main`. The optional draft job
   downloads only this run's artifacts, verifies both checksum manifests and the signed archive
   provenance against the exact source commit, workflow and main ref, then
   creates a **draft prerelease** at the exact tested source commit. It refuses an existing version
   release or tag. Review notes, files, checksums and real-host evidence before publishing.
   After verifying the draft, create the annotated version tag at that qualified commit and
   confirm its peeled SHA. Protect version tags against updates/deletion before publishing
   the candidate as a prerelease. A stable release still requires the remaining production gates.
5. Follow the [production runbook](production-readiness.md). A green automated run does not supply
   independent reviews, real-host/device qualification, capacity measurement or community beta evidence.

Docker CI builds and tests images but does not publish a registry image. Registry publication needs
an agreed owner, versioned image and recorded digest. Native/source publication remains separate.

## Qualify each platform

Windows needs service installation, permissions, firewall, tray and reboot/update/restore checks.
Linux needs systemd and Docker lifecycle, persistent storage and off-host restore.
macOS needs LaunchAgent installation, login/reboot, update, uninstall and off-host restore.
Use real hosts and the exact candidate archives, including ARM64 where advertised. Test trusted
HTTPS, voice across two networks with relay fallback, screen sharing, phones/tablets and push.
Record outcomes in [deployment readiness](launch-readiness.md); build success does not close them.
