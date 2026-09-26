# ADR 0006 — Licensing, copyright and contribution terms

**Status:** Accepted by the owner; the holder name and the agreement text still need one
review pass before the public repository opens
**Date:** 24 September 2026
**Plan reference:** WP-01, D-01, INV-09, R-14, J-10
**Deciders:** OWNER

## Context

D-01 asks the owner to choose a source license, a copyright holder and contribution
terms, and states that no license is inferred. INV-09 makes an owner-approved license,
buildable source, dependency notices and a real security-reporting process part of
open-source publication, and the plan warns twice against requiring a CLA or a
sign-off by assumption. Nothing in the workspace carried a license: no `LICENSE`, no
`NOTICE`, no security policy, and `web/package.json` had no `license` field.

The owner's instruction was that the project must be open source and available to
everyone, published on GitHub.

## Decision

1. **MIT License**, with the copyright holder named in [LICENSE](../../LICENSE).
2. **Copyright held personally by the owner**, not by a collective or an
   organization, so one identifiable party can enforce the license.
3. **A Contributor License Agreement**, rather than a developer certificate of
   origin and rather than inbound-equals-outbound. The CLA keeps the contributor's
   copyright and grants the project the right to license contributions under other
   terms, which is the only choice that preserves the ability to relicense or
   dual-license later without asking every past contributor again.
4. **No trademark grant.** The code may be forked freely; a fork is not the official
   release and must not present itself as one.
5. **Dependency notices are generated, not maintained by hand.** Every release
   redistributes the Go packages the compiler links into the binary, the frontend
   packages bundled into the web assets, and the Go standard library.
   `scripts/licenses.mjs` reads their licenses from the modules and packages
   themselves and writes `THIRD-PARTY-NOTICES.md`; the release helper embeds the
   same output in each package next to `LICENSE`.

## Alternatives considered

| Option | Why it was not chosen |
| --- | --- |
| Apache-2.0 | Adds an explicit patent grant and a trademark clause, which suits a WebRTC codebase, but also carries NOTICE-propagation duties for every redistributed dependency. The owner wanted the simplest license that everyone can use. |
| AGPL-3.0 | Would require anyone hosting a modified version as a network service to publish their source. Strong for a self-hosted server, but it deters some contributors and adopters, and the project's stated goal was availability. |
| GPL-3.0 | Copyleft for distributed software only, so a hosted service could still close its changes. Neither the simplicity of MIT nor the network protection of the AGPL. |
| No CLA or sign-off | Least friction for contributors, but then the license can never change, including for a security-driven relicensing or a future dual-license arrangement. |

## Consequences

- Anyone may use, modify and redistribute BrrpBrrp, including commercially, provided
  the copyright and permission notices travel with it. A company may also run a
  modified closed version as a service; that is the accepted cost of the permissive
  choice, and MIT does not require sharing those changes back.
- Contributions are accepted only under the CLA, so the repository needs a visible
  signing process from its first pull request. The manual process is recorded in
  [CLA.md](../../CLA.md) with signatures listed in
  [CLA-SIGNATORIES.md](../../CLA-SIGNATORIES.md); collecting them through a signing
  service instead is the owner's call and changes nothing else.
- A dependency change means a notices change: `make check` fails when
  `THIRD-PARTY-NOTICES.md` is stale, and the generator stops with an error when a
  component's license cannot be read, so unfamiliar terms cannot enter a release
  unnoticed.
- Decisions D-15 (signing, repository and registry identities) and the rest of WP-01
  remain open. The license decision does not create a repository, a signing identity,
  a private security intake or a published release; those still need owner-controlled
  destinations, and [SECURITY.md](../../SECURITY.md) says plainly that its channel is
  not live yet.
