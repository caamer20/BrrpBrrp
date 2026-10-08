# Project and deployment readiness

Assessment date: **8 October 2026**. BrrpBrrp is at **1.0.0-rc.1**, schema 25. Its planned
feature families are implemented, and a private GitHub repository is configured for
cross-platform qualification. It is ready for structured candidate testing; it is not yet
qualified for a production launch.

## Current assessment

| Area | Assessment | Evidence or remaining work |
| --- | --- | --- |
| Product scope | Substantial release candidate | Channels, roles, private conversations, moderation, voice, screen sharing, backups and installer commands are implemented. See [project overview](project-overview.md). |
| Repository presentation | Prepared for private development | README, manual, roadmap, policies, issue/PR templates, review ownership and dependency updates are present. |
| Automated verification | In progress on this candidate | Local and hosted outcomes are recorded below; a workflow definition is not a passing run. |
| Upgrade fixtures | Connected for private CI | Preserved alpha archives retain their hashes. Linux schema-13 reconstruction has explicit provenance. See [GitHub setup](github-setup.md#ci-fixtures). |
| Distribution | Gated draft path prepared | Six native targets plus source/checksums; no application release or registry image is published. |
| Windows deployment | Unqualified | Real service, ACL, firewall, tray, reboot, update and restore acceptance is outstanding. |
| Linux deployment | Unqualified | Docker image execution, systemd lifecycle, network and off-host recovery need target-host evidence. |
| macOS deployment | Unqualified | Local builds do not establish installation, login/reboot lifecycle, update and restore on a clean host. |
| Security, privacy and accessibility | Reviews outstanding | Automated checks are useful evidence; independent reviews remain open. |
| Community beta and soak | Not started | The roadmap requires real community use and observation of a frozen candidate. |

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
- Chromium private-conversation mode: running. Both modes keep restricted channels disabled.
- Hosted qualification: [first main run](https://github.com/caamer20/BrrpBrrp/actions/runs/37821452830)
  started for commit `5b20f5d`; Windows/macOS/Linux, race, Docker and recovery results remain pending.

## Next steps in order

1. **Establish a green candidate.** Finish local checks, resolve hosted failures and retain the
   source revision, run URL and artifacts. Keep automated failures blocking package preparation.
2. **Cover the shipped conversation defaults.** Both features ship enabled, while the existing
   browser harness runs restricted channels disabled. Add restricted-channel browser journeys and
   a run using shipped defaults; existing private-conversation off/on results do not cover that gap.
3. **Prepare and qualify the candidate archives.** Use the gated manual workflow, then install
   its actual Windows, macOS and Linux packages on clean hosts. Include ARM64 runtime testing where
   it is advertised, Docker persistence and update/rollback on each deployment type.
4. **Prove operation outside one machine.** Trusted HTTPS, two-network voice with relay fallback,
   screen sharing, real phone/tablet behavior, push, monitoring and off-host recovery need recorded evidence.
5. **Complete reviews and the beta.** Resolve blocking independent security/privacy/accessibility
   findings, run the community beta and observe a frozen candidate before stable publication.
6. **Open the public project deliberately.** Review tracked history, activate and test vulnerability
   intake, choose conduct/support contacts, publish the tested prerelease and later promote the
   qualified build. Keep the repository private until that decision is made.

The detailed release gates remain in [ROADMAP.md](../ROADMAP.md) and the
[production runbook](production-readiness.md). This assessment records readiness; it does not
change those gates or turn missing evidence into a pass.
