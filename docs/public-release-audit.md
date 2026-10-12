# Public repository and release audit

Audit date: **11 October 2026**. This records publication checks; it does not qualify a
production deployment. Current test and release outcomes are in [deployment readiness](launch-readiness.md).

## Publication exposure

- Mirrored the actual GitHub repository, including its 11 advertised refs, and scanned all
  337 reachable commits with checksum-verified Gitleaks 8.30.1.
- Reviewed all 30 scanner findings: deterministic test/rehearsal idempotency identifiers,
  source-file SHA-256 checksums, documentation prose, a SQL statement using a runtime-generated
  MFA secret, and the intentionally public localhost TLS test key. No deployment credential
  was identified. `.gitleaksignore` records exact historical fingerprints, with no broad path
  exclusion. A synthetic new-token control still fails the scanner.
- Historical evidence contains ordinary author filesystem paths. No tracked `.env`, operator
  data directory, database or credential archive was found. The excluded operator-specific
  guides are absent from the published history. Addresses in tests and network-policy code
  are examples or address-range checks.
- Retrieved and scanned retained logs from all 56 project Actions runs; no secret findings.
  GitHub reported no retained Actions artifacts at audit time.
- Verified all 13 archival fixture ZIPs against `CI-FIXTURE-SHA256SUMS`. Their inventories contain
  no operator database or credential files. Scanned text yielded only the same documentation
  prose finding in six copies of the alpha.5 ADR. Original fixture bytes and digests remain intact.

Generated redacted reports and downloaded audit inputs are retained locally under
`dist/public-audit-2026-10-11/`, excluded from Git and release packages. A secret scan is evidence
against known patterns, not proof that a repository can contain no sensitive information.

## Release engineering

The repository is public; private vulnerability reporting, secret scanning and push protection
are enabled. Conduct/privacy reports have an owner-provided private contact.

Routine CI verifies frontend/static checks once on Linux and Go builds/tests on Windows,
macOS and Linux. CodeQL scans Go and JavaScript/TypeScript. A separate release gate requires both analyses at the
exact source/ref and inspects those analysis IDs' SARIF results for unreviewed high/critical security findings;
successful analysis alone is not a clean-findings result. Later branch fixes, dismissals and SARIF
suppressions do not waive the gate. Reviewed false positives require exact finding/flow/query-pack identity
and an unchanged analyzed source-tree digest; [the review record](codeql-findings-review.md) explains the policy.
Lower-severity findings remain review work. Full release qualification additionally requires race shards, browser
journeys, dependency scans, Docker lifecycle/builds, recovery, historical rollback and
extracted-package consumption before archives or an optional draft release are prepared.
Only standard GitHub-hosted runner labels are used; workflow actions are pinned to commit SHAs.
On `main`, package generation signs GitHub/Sigstore build-provenance attestations. The draft job
verifies each archive against the repository, workflow, source commit and main ref before preparing
a draft. Platform code signing and notarization still require separate owner-controlled identities.

The browser harness has a shipped-default mode that leaves both conversation-feature overrides
unset. Its restricted-channel journey checks the rendered access inspector, an explicit reader
grant, outsider denial and live revocation. Engine coverage includes Chromium, Firefox media,
and the documented WebKit core subset. Results belong to the exact invocation recorded in
deployment readiness; adding a journey is not a passing result.

At audit time, the download badge was updated on a separate `badges` branch, so its automation
did not push through protection on `main`. The [current counter](github-setup.md#readme-download-count)
requires no extra branch or workflow. Release artifacts have explicit retention periods, and untrusted
pull-request jobs keep repository contents read-only. CodeQL can write security-analysis results,
while release signing/publication permissions remain confined to manual `main` packaging.

Stable publication still requires the real-host/device, independent review, capacity and beta
evidence in [the production runbook](production-readiness.md). A public repository or green CI
does not supply that evidence.
