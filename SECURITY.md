# Security policy

BrrpBrrp is a self-hosted server: whoever runs it holds the database, the uploads and the
machine. Read [docs/security.md](docs/security.md) for what the application defends and
what stays outside its boundary, so a report lands on the right side of that line.

## Supported versions

The latest published application version receives fixes. Before the first application
release, the current release candidate is under active development; archival CI fixtures
are unsupported. Older versions are retained only for documented migration and rollback
tests. Read [known limitations](docs/known-limitations.md) before running a candidate.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting: open the repository's **Security** tab and
choose **Report a vulnerability**. That report is visible only to maintainers, and it is
the channel for security reports — do not open a public issue containing exploit details.

The private reporting form is at
[Report a vulnerability](https://github.com/caamer20/BrrpBrrp/security/advisories/new).
If the form is unavailable, email [cameron.amer@gmail.com](mailto:cameron.amer@gmail.com)
with a minimal description and ask for a safe way to share sensitive details. Conduct
reports use [the code of conduct](CODE_OF_CONDUCT.md).

Please include:

- the version (`brrpbrrp version`) or commit, and how it is deployed (native service, Docker, manual);
- whether it needs an account or a specific role, and whether it needs administrator access;
- the smallest reproduction you can manage, and the impact you can demonstrate;
- whether you want to be credited, and under what name.

## What to expect

The project targets an acknowledgement within seven days and a first assessment within
thirty days. That is one maintainer's best effort rather than a service level, with no guaranteed response time. You will be told the outcome:
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
