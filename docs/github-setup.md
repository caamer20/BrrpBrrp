# GitHub and release setup

The development repository is [caamer20/BrrpBrrp](https://github.com/caamer20/BrrpBrrp),
initially private. Windows, macOS and Linux are all intended deployment targets.
Source publication, automated verification and production qualification are separate steps.

## Repository configuration

The repository includes a description and topics, issue and pull-request templates, maintainer
review ownership, MIT licensing, contributor terms, a security policy and support documentation.
Squash merging is enabled and merged branches are deleted. The wiki and unused project boards
are disabled so the repository documentation remains the primary guide.

Dependabot checks Go modules, frontend packages, Docker images and GitHub Actions weekly.
Vulnerability alerts and automated security updates are enabled. Review dependency changes and
regenerate third-party notices when the linked dependency set changes.

GitHub returned HTTP 403 for private branch protection on this account: it requires GitHub Pro
or a public repository. The repository remains private, as requested. Required checks and force-push
protection cannot be enforced by that feature until the account plan or visibility changes.
The package workflow still requires its prerequisite jobs to pass.

When branch protection becomes available, require the hosted checks on `main`.
Use the actual check names from a completed run, including the OS matrix, race shards,
browser, Docker and recovery jobs. Do not require the manual `packages`, `rollback` or
`release-draft` jobs on ordinary pull requests. A single-maintainer repository should not require
its owner to approve their own pull request.

GitHub private vulnerability reporting is unavailable on this private repository. Before opening
the repository publicly, configure and test that intake, choose a private conduct-reporting contact,
review the tracked history and record support expectations. See [SECURITY.md](../SECURITY.md)
and [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md).

## README download count

The About description lists Windows, macOS and Linux on x64 and ARM64. The README's
local SVG badge works while the repository is private: **Release download count** queries
all GitHub release pages with the workflow token and updates only `docs/assets/downloads.svg`.
It runs daily, on release changes and on manual dispatch. An API failure leaves the previous
successful count intact. Updates are serialized and use an ordinary fast-forward push.

The count includes native and Docker source ZIPs attached to published version releases,
including prereleases. Drafts, the archival CI fixture release and ancillary assets are excluded.
It measures archive downloads rather than unique people or installations. Maintainers can also
run `GITHUB_REPOSITORY=caamer20/BrrpBrrp node scripts/update-download-badge.mjs` with an
authenticated `gh` CLI. The script and badge ship in the source archive.

## Actions pause and resume

On 8 October, the owner requested that no runners start before the Actions limit resets.
Repository Actions execution was disabled, and **Check**, **Docker** and **Release download count**
were paused. Superseded runs were canceled, and no runs remain active or queued. Download-count
updates also wait for Actions to resume. The final attempted qualification did not execute any
steps because GitHub reported an account payment/spending-limit issue.

Only after the limit reset is confirmed, restore the previously active configuration and run
qualification on main:

```sh
gh api --method PUT repos/caamer20/BrrpBrrp/actions/permissions -F enabled=true -f allowed_actions=all
gh workflow enable check.yml --repo caamer20/BrrpBrrp
gh workflow enable docker.yml --repo caamer20/BrrpBrrp
gh workflow enable downloads.yml --repo caamer20/BrrpBrrp
gh workflow run check.yml --repo caamer20/BrrpBrrp --ref main -f prepare_release=true
```

These commands can start runners and are documented here, not executed during the pause.
The final workflow still requires all automated gates before packages or a draft prerelease are
prepared. Local archives do not bypass those gates. Billing/usage problems may require account
attention even after a presumed reset; see [GitHub's billing troubleshooting](https://docs.github.com/en/actions/how-tos/troubleshoot-workflows#reviewing-billing-errors).

## CI fixtures

The repository variable `BRRP_CI_BASELINE_TAG` identifies the archival fixture release,
`ci-baselines-2026-10-08`. CI downloads its assets with the read-only workflow token, so it works
while the repository is private. Authentication is required; unauthenticated public download URLs
cannot fetch private release assets.

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

1. Commit the release version and its notes in `docs/releases/`. The Go executable, frontend
   package and release helper must agree on the version.
2. Run **Actions → Check → Run workflow** on `main`. Leave **Create a draft prerelease**
   unchecked for a qualification run, or check it to prepare a draft after all automated gates pass.
3. The workflow runs Go tests on three operating systems, Linux race shards, browser journeys,
   dependency checks, Docker builds/lifecycle, recovery, package consumption and historical rollback.
   Packaging waits for all six job families. Native archives cover x64 and ARM64 on Linux,
   Windows and macOS; ARM64 packaging alone is not an ARM64 runtime qualification.
4. The optional final job downloads only the packages from this run, verifies both checksum
   manifests and creates a **draft prerelease** at the exact tested source commit. It refuses an
   existing version release or tag. It neither publishes a stable release nor deploys a server.
5. Review the draft's notes, files, checksums and real-host evidence. Follow the
   [production runbook](production-readiness.md) before promoting it for production use.

Docker CI builds and tests images but does not publish to a registry. Source builds with Compose
are available now. Registry publication needs a versioned image, its digest and an agreed registry
owner; keep it distinct from native package publication.

## Qualify each platform

Windows needs service installation, permissions, firewall, tray and reboot/update/restore checks.
Linux needs systemd and Docker lifecycle checks, persistent storage and off-host restore.
macOS needs LaunchAgent installation, login/reboot behavior, update, uninstall and off-host restore.
Use real hosts and the exact candidate archives. Test voice across two networks, restricted-channel
access, mobile devices, push and certificate trust. Record these results in
[deployment readiness](launch-readiness.md); build success does not close them.
