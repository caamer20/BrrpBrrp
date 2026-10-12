# CodeQL findings and release review

The first full-main analyses at `13c990d` reported 19 findings (11 Go, 8 JavaScript).
The PR scan had analyzed changed code and reported none. The release findings gate
blocked packaging; a successful analysis job is not a clean security result.

## Repairs

- Activity filters now use bound SQL parameters. The previous caller whitelisted the
  activity kind, but the query still interpolated that value.
- Voice-port bounds are explicit beside the `uint16` conversion. The existing numeric
  configuration helper also enforces the range.
- ZIP restoration checks `filepath.IsLocal` directly before extraction, in addition to
  the existing entry allowlist, manifest/inventory verification and contained staging path.
- Browser-test account identifiers use cryptographic randomness.
- The documentation-link self-test checks the README's failure prefix; its domain-name
  substring was a test assertion, not a URL security filter.
- The rollback rehearsal names the first-factor response accurately. That check logs only
  its HTTP status, not the password, cookie or response body.

## Installer trust boundaries

The installer intentionally accepts an operator-selected data directory and contacts the
server it is installing. These operations are separate from a community member's API access.

`internal/installer/server.go` binds the wizard to `127.0.0.1`, generates a random 32-byte
bearer secret, requires a loopback peer and exact Host, and checks the secret and Origin
before installation API handlers run. `makePlan` in `settings.go` requires an absolute,
dedicated directory, rejects control characters, checks containment/symlink ancestors, and
bounds the port. New network plans accept only a literal private IPv4 address on a local
interface. Computer/proxy plans use a loopback destination; the public proxy URL does not
become the HTTP request destination. `clientFor` disables proxy use and rejects redirects.

Resumed plans come from the locally stored installation record and server configuration.
They do not pass through `makePlan` again. Those files are trusted operator configuration
and must stay protected; config agreement prevents stale plans but is not remote-destination
validation. This review does not claim to resist a hostile local filesystem actor racing
path checks. Host-level access is outside the reporting threat model in [SECURITY.md](../SECURITY.md).

Filesystem findings inside `Contained` concern the metadata checks that implement path and
symlink validation. Installer checks and certificate reads use the authenticated operator's
chosen paths. Each remaining result needs its own reviewed record after a fresh full-ref scan.

## Exact-source release gate

`scripts/codeql-reviewed-findings.json` stores individually reviewed false positives. It
does not accept a rule/path wildcard or an accepted-risk disposition. A record binds the
rule, query-pack identity/version, primary fingerprint, physical location/URI base and
normalized data flows. Duplicate or inconsistent matches fail.

The source digest covers every tracked Git tree entry at the analyzed commit, including
embedded SQL, frontend assets, dependency manifests, scripts and workflows. The disposition
file itself is the sole exclusion, avoiding a circular digest. Any other tree change requires
renewed review; CI never refreshes the digest automatically. A record must describe the exact
result as well as the exact source. Missing or malformed identities, unknown findings and
changed flows remain blockers. GitHub dismissals and SARIF suppressions do not waive them.

The verification controls exercise changed source, embedded SQL and workflow edits, moved or
replaced findings, query-pack/flow/URI changes, duplicate matches and invalid records. Final
analysis IDs, qualification run, commit and publication status belong in the GitHub release
record. This automated source review does not replace the independent security/privacy review
or real-host qualification required for a stable production launch.
