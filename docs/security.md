# Security and network setup

BrrpBrrp is intended for a small group whose server operator is trusted. It uses HTTPS, authenticated sessions and administrator checks, but does not provide end-to-end encryption or hostile multi-tenant isolation. The operator can read stored messages and relay audio. Protect the host and its backups.

## Build and dependency checks

Build with Go 1.27.2 or newer. The module minimum rejects older compilers, including
when automatic toolchain downloads are disabled. CI runs the pinned Go vulnerability
scanner and a moderate-or-higher npm audit before native tests; scanner/network failures
block qualification.

The 12 October patched-source scan reports zero affected application symbols and zero
findings in imported packages. Its one module-only advisory,
[GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932), concerns the unmaintained
`golang.org/x/crypto/openpgp` package, which has no fixed version. The application uses
other parts of `x/crypto`; OpenPGP and its subpackages are absent from the command's
dependency graph for all six supported release targets. They are not linked into the
native binaries. This records the scan's scope; it is not a blanket claim that the entire
dependency module is free of advisories.

## HTTPS choices

1. **Existing certificate:** set `BRRP_TLS_MODE=https` and place the certificate chain and private key inside the data directory. Set `BRRP_CERT_FILE` and `BRRP_KEY_FILE` to their relative paths.
2. **Automatic certificate:** set `BRRP_TLS_MODE=https`, `BRRP_DOMAIN`, `BRRP_ACME_EMAIL` and the matching HTTPS base URL. DNS must reach this server and public TCP 443 must reach the app's TLS listener for the ACME TLS-ALPN challenge. The app has no HTTP-01 listener. Certificate issuance accepts the certificate authority's subscriber terms when configured by the operator. If a supplied certificate exists, it takes precedence.
3. **TLS reverse proxy:** use a trusted HTTPS proxy and `BRRP_TLS_MODE=http-behind-proxy`, `BRRP_BEHIND_PROXY=true`, and exact `BRRP_TRUSTED_PROXIES`. Set the public base URL to HTTPS and bind the backend to loopback/private networking.
4. **Private network/local CA:** `auto` mode creates a separate per-installation local CA and 90-day server certificate when no certificate exists. Distribute only the public root (`certs/root.crt`, offered as a DER `.cer` in setup). Trust it through the device's normal certificate process; the Windows wizard has an explicit action to trust the displayed root in the Local Machine store. Managed leaves renew before expiry without changing the trusted root. The root lasts ten years; protect `root.key`, which can issue certificates. Existing self-signed certificates remain until the operator explicitly chooses repair. Repair preserves the old pair and refuses certificates it does not recognize. [Trust and repair walkthrough](alpha-testing.md#fix-a-local-certificate-problem).


Use one canonical origin. Arbitrary hostnames and ports are not interchangeable: cookie security and WebSocket/mutation origin checks use `BRRP_BASE_URL`. The wizard's `http-local` mode permits plain HTTP only on a loopback IP with a matching loopback URL and port. It is suitable for using the app on the host itself. Other plain-HTTP base URLs require the explicit local-development override `BRRP_ALLOW_INSECURE_HTTP=true`.

The installation wizard listens only on an ephemeral IPv4 loopback port. Its API requires a random session key, checks Host and browser Origin, sends no CORS permission and refuses framing. Setup expires after one hour and can be closed explicitly after installation. It reads the first-run token locally and creates the administrator through the running app's existing setup API. It never returns the token or password in API responses or stores the password in installation metadata. Preview mode disables installation, service startup and account creation.

## Firewall and voice

| Purpose | Default port | When needed |
| --- | --- | --- |
| App HTTP(S)/chat/WebSocket | TCP 8082 in the wizard; 8080 for unconfigured `serve` | Always; may be behind a proxy on TCP 443 |
| Direct ACME TLS-ALPN | Public TCP 443 → app TLS listener | Automatic certificate mode |
| WebRTC media | UDP 50000–50100 | Direct voice paths |
| TURN listener | UDP 3478 | Built-in relay enabled |
| TURN relay sockets | UDP 49152–49999 | Built-in relay enabled |
| TURN fallback, plain TCP | TCP 3479 by default | Built-in relay with the TCP fallback enabled |
| TURN fallback over TLS | TCP 3480 by default | Built-in relay with the TLS fallback enabled |

Forward UDP ranges through NAT to the server and set `BRRP_VOICE_EXTERNAL_IP` to the reachable IP when necessary. Do not overlap the media and relay ranges. Address discovery uses whatever STUN server an operator configures; none is set by default, so no third party is contacted. It does not carry chat messages or audio. WebRTC encrypts transport with DTLS-SRTP. This SFU terminates transport encryption to forward audio.

TURN credentials are temporary, available to authenticated voice participants and restricted to destinations on the server. The built-in relay carries IPv4 only. It serves UDP by default, and it can additionally serve plain TCP and TLS on the server's own certificate — that is how a client network which blocks UDP gets a path at all. A network that blocks UDP and cannot reach the relay over either TCP transport has no route through this server. Test with two devices on separate networks before inviting the group.

## Application protections

Passwords use bcrypt cost 12. Only a SHA-256 hash of each random session token is stored. Cookies are HttpOnly, SameSite=Strict and Secure on HTTPS. Changing/resetting passwords or banning/removing users invalidates sessions and closes active sockets. Authentication and administrator status are rechecked for every mutation and WebSocket action. Login work is rate-limited with bounded hashing concurrency.

**Throttling of credentials (decision D-17).** The server counts failures, never attempts: a correct password, a valid invitation and a valid setup token spend nothing, so people who arrive together from one address are not refused for arriving. There are three counters, each over five minutes. An address may fail 20 times (`BRRP_LOGIN_ATTEMPTS`) across sign-in, the setup token and invitation tokens; once it has, it is refused until the window ends, whatever it sends, and creates no further counters. One username may be failed 10 times from one address; a failure from another address is counted separately, so nobody can lock a member out of their account from somewhere else, and an unknown username is counted and answered exactly as a known one. A signed-in account may fail 10 password or code re-checks (changing a password, enrolling or removing a second factor, re-authenticating), counted against the account and not against where the request came from. An IPv6 client is counted by its /64. The counters an unauthenticated request can create are kept apart from the ones every signed-in request is checked against, and a full table refuses nobody it already knows, so a flood of failed sign-ins cannot close the server to its members. A correct password is rated as well, per account and far above what a person does — three times the address ceiling, so 60 sign-ins or re-checks in five minutes by default — because each one costs a hash and a sign-in leaves a session behind. What no counter bounds is guessing one account from many addresses: that is limited by the cost of a hash and by the two password checks the server runs at once — a few guesses a second for the whole server — and by the 12-byte minimum password and the second factor. A client address is only as good as the proxy configuration: behind a reverse proxy that is not listed in `BRRP_TRUSTED_PROXIES`, and under Docker Desktop, every client arrives as one address and shares the address counter.

The actions that can defeat a private boundary — a full archive, a chosen-password reset, taking another account over and moving ownership — require a capability no ordinary role can hold, a recent strong sign-in and an enrolled second factor. An owner's own factor is never removed from the administration panel: it is recovered on the host with `brrpbrrp recover-owner` while the service is stopped, and the audit log attributes that action to the host operator rather than to the owner. Anyone who can read or write the data folder already holds the database, the backups and the attachments, so host access is the boundary the application cannot defend against; it makes legitimate recovery recorded rather than silent.

Mutations validate the browser origin; cross-origin requests are rejected. User text is rendered as text, never raw HTML. The app uses a content security policy and frame protection. Attachment paths are server-generated UUIDs; names supplied by users do not select filesystem paths. HTML, SVG and other active formats download as attachments. Uploaded content is not antivirus-scanned: do not open unexpected executables or documents simply because a familiar account posted them.

A deployment whose browser-facing address is real HTTPS on its own name — TLS mode `https`, or `http-behind-proxy` with an `https://` base URL — also sends `Strict-Transport-Security: max-age=15552000` (six months), so a browser that has visited once refuses plain HTTP for that host afterwards. The header carries neither `includeSubDomains` nor `preload`: other names under a community's domain are not this server's to speak for, and a preload submission cannot be withdrawn quickly. The `auto` mode (the per-installation local certificate authority) and `http-local` never send it, because a host pinned to HTTPS while its certificate is still being sorted out cannot be un-pinned by changing modes. Moving a name from `https` back to plain HTTP therefore takes up to six months to be forgotten by browsers that visited it; plan a change of address rather than a downgrade.

Server-side preview/push requests permit HTTPS on port 443, reject credentials, validate all DNS addresses, block private/loopback/link-local/reserved networks, pin the validated address for the connection and validate redirects. Requests have time, body and concurrency limits. Proxy environment settings cannot redirect these fetches. External preview images/videos load directly from their host in the browser; private-preview mode prevents those loads until clicked. That mode does not hide a URL already shared with the server's preview worker—disable server previews if the operator should not fetch shared URLs.

Push is opt-in per device. Browser push providers receive encrypted notification payloads and routing metadata. Payloads contain the sender/mention notice, not the message body. This is separate from end-to-end encryption of chat, which is not provided.

Data files stay under the selected data root, with traversal and symlink checks. Use a private, local filesystem, not a world-writable or network-shared folder. Unix directories/files are created with restrictive permissions; the Windows installer applies private ACLs for Administrators, SYSTEM and LocalService. Existing permissions remain the operator's responsibility. Keep the binary and service configuration writable only by administrators. An exclusive OS file lock prevents two processes opening one data directory through the shipped commands.

Backups are unencrypted and include hashes and private push keys. Restore only backups you trust; validation guards corruption and path escapes, not every possible malicious SQLite schema. Host configuration and certificate keys are separate. Never publish the setup token, session cookie, database or backup ZIP. Every archive carries a manifest with a size and SHA-256 digest for the database and for each attachment, and restore, `verify-backup` and `production-check` all check it, so an altered or truncated archive is refused instead of installed; that detects damage, and it is not a signature, so it does not establish who produced an archive.

## Reporting a vulnerability

[SECURITY.md](../SECURITY.md) is the reporting policy: GitHub's private vulnerability reporting is enabled, with the owner-provided private contact as a fallback. Host-level access is explicitly out of scope because the operator already holds the data. The project also ships under the MIT License with generated dependency notices ([THIRD-PARTY-NOTICES.md](../THIRD-PARTY-NOTICES.md)); a dependency whose license cannot be read fails the notices check rather than being shipped unnoticed.

## Release checks

Run `go test -race ./...`, `go vet ./...`, the frontend checks and current Go/npm vulnerability scans before release. A passing local suite does not replace host patching, certificate renewal, backup rehearsal or separate-network voice testing. See [acceptance.md](acceptance.md) for the current evidence and remaining manual checks.
