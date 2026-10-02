# Contributing to BrrpBrrp

BrrpBrrp is being built as a self-hosted community chat application. Start with the [roadmap](docs/roadmap.md), [architecture](docs/architecture.md) and [production runbook](docs/production-readiness.md).

BrrpBrrp is released under the [MIT License](LICENSE), and everything a release redistributes is listed with its own license in [THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md). Outside contributions are covered by the project's [Contributor License Agreement](CLA.md), which you accept when you open a pull request. This development guide itself grants no license.

## Development

Install Go 1.27.1 or later and Node.js 22.12 or later. Node 24 is used in CI. Install and build the frontend before building Go:

```sh
npm --prefix web ci
npm --prefix web run build
go build -o dist/brrpbrrp ./cmd/brrpbrrp
```

Run a local server using a disposable data directory and the documented [development configuration](docs/hosting.md). Never use real server data in tests. The Go binary embeds `web/dist`; rebuild the frontend and Go binary after UI changes. The frontend development proxy is configured in `web/vite.config.ts`.

## Design expectations

- Keep installation practical for someone running a single server. Explain any new runtime service, port or external account.
- Enforce permissions on the server. Cover REST, search, uploads, WebSockets and notifications when adding visibility rules.
- Bound requests, queues and client caches. Preserve existing data during migrations and reject unsupported newer schemas.
- Preserve drafts and reading positions through reconnects, navigation and updates. Retry mutations with the same idempotency key.
- Support keyboards, visible focus, screen readers, narrow screens and both color themes. Use existing components and CSS variables.
- Document changed behavior, backup implications and remaining limits. Record what was actually verified.

## Verification

```sh
npm --prefix web test
npm --prefix web run check
npm --prefix web run format:check
npm --prefix web run build
go vet ./...
go test -race -timeout 60m ./...
make notices
```

`go test -race` is the long gate: the API package alone takes about 43 minutes under
the race detector on a sixteen-core machine, because it drives real WebSocket, HTTP and
media loops. The 60-minute budget is what `make test` uses and it is deliberate — a
budget that races the runtime reports a timeout where the suite would have passed.
Run focused packages (`go test ./internal/rtc -race`) while working and the whole sweep
at a checkpoint.

For browser flows, install Chromium once from `web` with `npx playwright install chromium`, then run `make browser-check` from the source root. Browser tests start a temporary server, include accessibility checks and remove their test data afterward. Go integration tests need local TCP and UDP sockets.

CI defines native Windows/macOS/Linux and Docker checks. Changes to voice, installation, backups or networking also need the relevant manual checks in [acceptance.md](docs/acceptance.md) and the production runbook.

## Licensing and contributions

The project is MIT-licensed, so anyone may use, modify and redistribute it. To keep that possible, contributions are accepted under the [Contributor License Agreement](CLA.md), which you sign by including the sentence in `CLA.md` in your pull request. A maintainer records the signature in [CLA-SIGNATORIES.md](CLA-SIGNATORIES.md) as part of the same pull request. Signing keeps your copyright, and it is what allows the project to change license later without tracking down every past contributor.

Adding a dependency means adding its license to a release. `make notices` regenerates [THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md) from the modules the compiler actually links and the packages the web bundle contains, and `make check` fails when that file is stale. The generator stops with an error rather than guess at a license it cannot read, so a new dependency with unfamiliar terms cannot slip through unnoticed.

## Review and reporting

Describe the user problem and expected behavior before a large change. Keep pull requests focused and include the result, verification, any migration and remaining risks. Use screenshots when they help explain UI changes.

Use issue templates for reproducible bugs and feature proposals. Keep credentials, setup tokens, private conversations, database files and backups out of issues and screenshots. Report vulnerabilities privately through the channel in [SECURITY.md](SECURITY.md); until that intake is live, send them to the account that published the release you are running. Never post exploit details in a public issue.
