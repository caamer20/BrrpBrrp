# Host qualification procedures — Windows, Linux and macOS

**Status:** target-host procedures prepared, **not executed**. Local development and hosted
CI evidence are tracked in [deployment readiness](launch-readiness.md). Every procedure below
must record what it observed on the actual installation rather than inherit a build or test result.

Run these before any G-gate is claimed. They are the difference between "the product
works" and "the product works on the two deployments the owner requires".

---

## 0. What has to exist first

| Resource | Why | Who provides it |
| --- | --- | --- |
| Windows x64 host with a supported build | Required deployment target | Owner |
| Linux amd64 host with Docker + Compose | Required deployment target | Owner |
| Clean macOS host/user and Linux systemd host | Native installation qualification | Owner |
| A Tailscale peer on the Windows host | The owner's chosen private-network path | Owner |
| Account able to install a service (Administrator) | The installer registers one | Owner |
| An HTTPS certificate or a trusted reverse proxy | Plain HTTP is not shipped publicly | Owner |

These are deployment procedures. A local binary or a hosted test run is evidence about
that execution; it does not establish a service installation on another host.

---

## 1. Windows x64 native service, over Tailscale

### 1.1 Install from the actual archive

`<version>` below is the release under qualification — the beta bytes, not a build from a
checkout; the point is the bytes people get. Its archive checksums are in the release's
`SHA256SUMS`.

Two hashes are involved and they are **not interchangeable**: the release publishes a
SHA-256 for the **archive**, and an archive checksum is not the checksum of the executable
inside it. Hash the archive first, and hash the executable only against a digest of the
executable that was recorded separately.

```powershell
# 1. Hash the ARCHIVE and compare it with the archive's line in SHA256SUMS. Before
#    extracting. A mismatch is a stop.
Get-FileHash .\brrpbrrp-<version>-windows-amd64.zip -Algorithm SHA256
# 2. Extract, then hash the executable. Compare it only with a separate executable digest
#    recorded at build time, never with the archive's checksum.
Expand-Archive .\brrpbrrp-<version>-windows-amd64.zip -DestinationPath .\extract
Get-FileHash .\extract\brrpbrrp.exe -Algorithm SHA256
```

Record: the archive URL; the published **archive** SHA-256 and the computed archive
SHA-256, and whether they match; and, if a separate executable digest exists, the computed
executable SHA-256 and that digest. A mismatch of either is a stop, not a note.

### 1.2 Install the service

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy RemoteSigned
.\extract\scripts\windows\install.ps1 -Binary .\extract\brrpbrrp.exe
```

Observe and record:

- the service name and its start type (delayed automatic, not automatic — automatic
  makes it contend for IO at boot);
- the account it runs as (expected **LocalService**);
- the data folder (`%ProgramData%\BrrpBrrp`) and its ACLs;
- whether the installer opened any firewall port (it should not — firewall is a
  separate, deliberate step).

### 1.3 Reboot and service recovery

```powershell
Restart-Computer
Get-Service BrrpBrrp | Format-List Name, Status, StartType
```

The service must come back on its own after a reboot, before anyone signs in. Record
the time from boot to the service reporting Running.

Then deliberately kill it and confirm it recovers:

```powershell
Stop-Process -Name brrpbrrp -Force
# The service controller should restart it.
Start-Sleep -Seconds 20
Get-Service BrrpBrrp | Format-List Name, Status
```

### 1.4 The service account and the data-folder ACLs

The service runs as **`NT AUTHORITY\LocalService`** (`cmd/brrpbrrp/platform_windows.go`,
`ServiceStartName`), a built-in low-privilege account. That is not a sandbox, and this
procedure should not pretend it is: like any process running under that account, the server
can read much of the volume and write to locations the operating system provides it — its
own profile and `%TEMP%` among them. A claim that LocalService "writes nowhere outside the
data folder" would be false and would test nothing.

The narrower claim is checkable, and it is what the installer actually does.
`scripts/windows/install.ps1` (and `internal/installer/platform_windows.go`) removes
inherited permissions from `%ProgramData%\BrrpBrrp` and grants exactly three well-known
SIDs — Administrators full, SYSTEM full, LocalService modify. It adds no ACE on any path
outside the data folder, and it does not touch `%ProgramFiles%\BrrpBrrp`, so the executable
stays readable through the standard install-directory permissions.

Observe and record:

- the data folder's ACL is exactly those three entries, with inheritance removed
  (`icacls $env:ProgramData\BrrpBrrp`);
- LocalService can read the executable and write its data folder — the rights the
  application needs;
- the installer added no ACE outside the data folder;
- the application's security boundary is the one the operating system gives the
  LocalService account. A future release that needs the service to write outside the data
  folder would fail here, which is the point of the step.

### 1.5 HTTPS and Tailscale

Configure `server.env` for the Tailscale address, with a trusted certificate. Do not
expose plain HTTP. Confirm from a second device on the same tailnet that the app loads
over `https://`.

**What this does not establish:** that media works. Tailscale carries the signalling
over HTTPS; whether audio and screen video establish is a separate question answered in
§3, and a successful page load says nothing about it.

### 1.6 Update, failed-update rollback, uninstall

1. Back up (§2 covers the method).
2. Install the newer archive over the running service.
3. Confirm the service restarts, the schema advances, and the data is intact.
4. **Force a failed update**: install an archive whose schema is *older* against a
   current database, and confirm the binary refuses to start and says why, rather than
   corrupting anything.
5. Roll back using the documented path: restore the pre-upgrade backup with the older
   executable. Never run an old executable against a new-schema database.
6. Uninstall. Confirm whether data is preserved (it should be) and say so explicitly.

### 1.7 Offline owner recovery

With no network and no second machine, the documented owner-recovery procedure must
work from the host alone. This is the procedure an operator uses when everything else
has failed, so it is the one most worth rehearsing — and **undocumented manual database
repair counts as a failure**.

### 1.8 Novice operator rehearsal

Somebody who did not build this follows [installation.md](../docs/installation.md) and
[deployment.md](../docs/deployment.md) on a clean Windows host, with no help. Record
every point where they had to ask a question, and every command that was not in the
docs. That list is the documentation backlog — the rehearsal is worth more than the
test matrix.

---

## 2. Linux amd64 Docker

### 2.1 Build and run the actual image

```sh
# <version> is the release under qualification; build the bytes that ship, not a
# checkout.
docker build -t brrpbrrp:<version> .
docker compose up -d
docker compose ps
docker compose logs --tail 100
```

Static checks of the Dockerfile and compose file are **not** Docker runtime
qualification. This section is the runtime.

### 2.2 Observe

- the container user (must be non-root; record the uid);
- which paths are read-only and which are the volumes;
- the healthcheck: does it report healthy, and does it detect a stopped application
  rather than just a running process;
- restart policy behaviour: `docker compose kill` then wait — does it come back;
- graceful shutdown: `docker compose stop` — does it close cleanly and leave the
  database consistent.

### 2.3 Failure modes that matter

| Fault | How to induce | Expected |
| --- | --- | --- |
| Volume not writable | Point the volume at a read-only mount | Fails loudly at startup, with a message naming the path |
| Disk full | Fill the volume | Writes fail with a clear error; the app does not corrupt the database |
| Container killed mid-write | `docker kill -s KILL` during a send | The message is either committed and readable, or absent — never half-written |
| Image identity | Compare `docker inspect` digests | The running image is the one that was tested |

### 2.4 Backup, update, rollback

The same three steps as Windows (§1.6) in the container. The restore must be into a
**clean** container, not the one being replaced.

### 2.5 Media and TURN reachability

From inside the container, confirm the TURN server is reachable on the configured
ports and that the advertised relay address is one clients can actually reach. A
container that can reach TURN but advertises a private address inside its own network
produces a call that connects on the host and fails for everybody else.

---

## 3. Media on a real network

Run this on **both** deployments. It is the part a local test cannot substitute for.

- Two clients on the same LAN: audio connects, measured one-way delay and packet loss.
- Two clients across the Tailscale tailnet: audio connects.
- One client with UDP blocked: does the call fall back to TURN over TCP/TLS, and how
  long does it take?
- Force relay-only: does the call still work, and is the candidate type recorded?
- Screen share: start, stop, and start again; 100 cycles without a leaked capture.
- Two-hour call: memory and CPU flat, no growing queue.

Record the **actual candidate type chosen** (host / srflx / relay) for each case.
"Tailscale and HTTPS both worked" does not establish that media worked; it establishes
that the page loaded.

### 3.1 Two bounds the local measurement exposed

The video-forwarding measurement (`planning/m4-checkpoint/video-forwarding.json`)
found two gaps between the plan's targets and the product's current limits. Both are
qualified here, on real hardware, rather than in a local benchmark:

- **The server-wide media bound is ten; the plan targets twenty.** `MaxParticipants`
  counts the whole server, so two publishers with nine viewers each is not reachable
  today regardless of how much forwarding headroom exists. Confirm the *intended* bound
  with the owner before changing it: raising it is a product decision about a
  community's capacity, not a performance one.
- **A client can now ask to watch a screen without claiming a publication**, but it has
  never been shown doing it on real hardware. The local test proves the negotiation and
  that a viewer is not recorded as a publisher; it does not prove a picture arrives. A
  real two-client screen-share run is what does, and the browser journey that exercises
  it is what would catch a browser sending something Pion parses differently.

### 3.2 The video forwarding measurement, on the reference allocation

The local run measured 23.28 Mbit/s of egress and 37.4 MiB of heap for one publisher
to nine viewers, on a **sixteen-core development host**. That is not the reference
allocation. Repeat it on the four-core, 8 GiB target and record CPU, RSS and egress:

```sh
BRRP_VIDEO_LOAD_SECONDS=120 \
BRRP_VIDEO_LOAD_REPORT=check-results/video-forwarding.json \
go test -count=1 -timeout 10m -run TestVideoForwardingLoad -v ./internal/rtc/
```

Compare against the master plan's budgets — mean CPU ≤65%, one-minute p95 ≤85%, RSS
≤2 GiB, egress below 70% of the reference link. A figure that fails there is a reason
to reduce the qualified media profile or to raise the documented link requirement
**before** release, not afterwards.

---

## 4. macOS service and Linux native service

These procedures extend qualification to all three platforms. They are prepared, not executed.
Use a clean host or a disposable user/VM and the exact candidate archive, with its archive digest
verified first. Record x64 and ARM64 results separately; cross-compiling an archive does not run it.

### 4.1 macOS LaunchAgent

Run `./brrpbrrp install` as the normal signed-in user, without sudo. The wizard installs a
per-user LaunchAgent named `co.brrpbrrp.server`, so it starts when that user signs in, rather
than before login as a machine-wide daemon. Record the actual paths selected by the wizard.
The default binary is under `~/Library/Application Support/BrrpBrrp/`, with configuration in
its `data/server.env` and the agent in `~/Library/LaunchAgents/co.brrpbrrp.server.plist`.

1. Complete setup; confirm the agent and the server's own health check agree that it is running.
   Inspect the agent with `launchctl print "gui/$(id -u)/co.brrpbrrp.server"` and validate its
   plist with `plutil -lint ~/Library/LaunchAgents/co.brrpbrrp.server.plist`.
2. Confirm install/data directories are private to the user, configuration and TLS private keys
   are protected, and the server is using the selected port and trusted browser address.
3. Sign out, sign in, and reboot then sign in. Confirm automatic recovery and retained accounts,
   messages, attachments and certificates. Record the dependency on that user's login.
4. Exercise graceful stop/start and recovery from an unexpected process exit. Confirm logs
   identify the incident and no competing server continues to hold the data directory or port.
5. Back up, install the candidate update through the wizard, and compare login, search and
   attachment bytes. Rehearse rollback with the matching pre-upgrade backup into a fresh folder.
6. Open `./brrpbrrp install` again and use its uninstall flow; confirm the wizard removes only the recognized agent/application
   files and preserves data. Reinstall and prove the preserved installation is usable.
7. Apply the shared network, voice, screen, device and off-host restore procedures above.
   Automated browser tests on a Mac do not replace this service qualification.

### 4.2 Linux systemd installation

On a clean systemd host, run `sudo ./brrpbrrp install` from the extracted archive and record
the wizard's selected paths. The wizard defaults to `/usr/local/lib/brrpbrrp/brrpbrrp`, data
in `/var/lib/brrpbrrp` and configuration in that data directory. The older shell installer
has different binary/configuration paths; qualify the installation method being advertised.

1. Confirm `systemctl status brrpbrrp` and the installed binary's `healthcheck --config PATH`
   succeed. Inspect `systemctl cat brrpbrrp` for the dedicated `brrpbrrp` user, restart policy,
   graceful-stop timeout and write restrictions.
2. Verify directory ownership, private settings/TLS files and the configured listener/firewall.
   Reboot and prove the service starts before an interactive user logs in.
3. Stop/start the service, simulate an unexpected process exit, then inspect recovery and logs.
4. Repeat update, failed-update rollback, off-host restore, uninstall and reinstall checks using
   the candidate's native wizard. Confirm uninstall preserves data and configuration.
5. Execute the same external HTTPS, media, device, push, capacity and monitoring acceptance as
   the Docker deployment. Native and Docker results are separate entries in the evidence record.

## 5. Evidence template

One JSON file per run, under `planning/m5-checkpoint/`. Record, for each procedure:

```json
{
  "procedure": "windows-service-install",
  "host": { "os": "Windows Server 2022", "build": "20348", "arch": "x86_64" },
  "source_commit": "<40 hex>",
  "artifact": { "name": "brrpbrrp-<version>-windows-amd64.zip", "sha256": "<the archive's hex, not the executable's>" },
  "commands": ["<exact command>", "..."],
  "observed": "<what was actually seen, with values>",
  "result": "pass | fail | blocked",
  "duration_seconds": 0,
  "evidence": ["<path to log, screenshot or transcript>"],
  "not_established": "<what this procedure does not prove>"
}
```

The last field is required. A procedure with an empty `not_established` has usually not
been thought about hard enough.

---

## 6. What remains to qualify

Clean Windows, Linux Docker/systemd and macOS service qualification still require target
hosts and physical clients. The development Mac can run local tests, but it has no Docker
daemon or PowerShell and its normal user installation is not a clean service rehearsal.
Hosted runners can supply automated build/container evidence, not the complete device and
operational acceptance above.

Local tests, recovery rehearsals, browser journeys and release engineering can continue
while hosts are arranged. Retain a pass/fail record for each advertised deployment method
and architecture; do not carry a pass from one method or machine to another.
