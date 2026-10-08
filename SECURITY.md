# Security policy

BrrpBrrp is a self-hosted server: whoever runs it holds the database, the uploads and the
machine. Read [docs/security.md](docs/security.md) for what the application defends and
what stays outside its boundary, so a report lands on the right side of that line.

## Supported versions

The newest published release is the supported one. Fixes ship as a new version rather
than a patch for an older one, because every release carries a schema version and the
upgrade path is forward-only.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting: open the repository's **Security** tab and
choose **Report a vulnerability**. That report is visible only to maintainers, and it is
the channel for security reports — do not open a public issue containing exploit details.

**This intake is not live yet.** [The repository](https://github.com/caamer20/BrrpBrrp) is
private during candidate qualification. GitHub's public vulnerability intake is unavailable
on this private repository. Enable it and deliver a test report before a public launch,
as required by the project's plan (WP-01). Authorized collaborators can use **Security →
Advisories → New draft security advisory** for a private maintainer discussion. A private
reporting contact for people without repository access must be established before public launch.

Please include:

- the version (`brrpbrrp version`) or commit, and how it is deployed (native service, Docker, manual);
- whether it needs an account or a specific role, and whether it needs administrator access;
- the smallest reproduction you can manage, and the impact you can demonstrate;
- whether you want to be credited, and under what name.

## What to expect

The project targets an acknowledgement within seven days and a first assessment within
thirty days. That is one maintainer's best effort rather than a service level, and the
owner should confirm the targets before the public launch. You will be told the outcome:
fixed with a version and a note, accepted as a known limitation, or declined with a
reason. Please allow the project to release a fix before publishing details.

## Out of scope

- **Host-level access.** Anyone who can read the data folder, the process memory or the
  host filesystem already holds everything. [docs/security.md](docs/security.md) states
  this boundary and it is intentional.
- Physically local attacks, an unlocked device, or a compromised browser.
- Findings that require the victim to paste script into their own console, or that rely on
  a malicious browser extension.
- Missing hardening headers without a demonstrated impact, and scanner output without a
  reproduction.
- Denial of service from an account that already holds administrator authority.
