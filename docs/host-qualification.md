# Host qualification procedures — Windows and Linux

**Status:** prepared, **not executed**. No Windows host, Docker daemon, PowerShell or
Tailscale peer is available in the environment this was written in, so nothing here has
been run. Every step below is written to be executed by somebody with access to a host,
and every one records what it observed rather than only that it passed.

Run these before any G-gate is claimed. They are the difference between "the product
works" and "the product works on the two deployments the owner requires".

---

## 0. What has to exist first

| Resource | Why | Who provides it |
| --- | --- | --- |
| Windows x64 host with a supported build | Required deployment target | Owner |
| Linux amd64 host with Docker + Compose | Required deployment target | Owner |
| A Tailscale peer on the Windows host | The owner's chosen private-network path | Owner |
| Account able to install a service (Administrator) | The installer registers one | Owner |
| An HTTPS certificate or a trusted reverse proxy | Plain HTTP is not shipped publicly | Owner |

**Nothing below can be qualified locally.** A local binary on a developer machine is
evidence about the binary, not about the deployment.

---

## 1. Windows x64 native service, over Tailscale

### 1.1 Install from the actual archive

```powershell
# Download the released archive and verify its published hash before extracting.
# Do not use an executable built in a checkout: the point is the bytes people get.
Expand-Archive .\brrpbrrp-0.4.0-alpha.7-windows-amd64.zip -DestinationPath .\extract
Get-FileHash .\extract\brrpbrrp.exe -Algorithm SHA256
# Compare against the release's published SHA256. Record both.
```

Record: archive URL, published SHA-256, computed SHA-256, and whether they match. A
mismatch is a stop, not a note.

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

### 1.4 LocalService and data ACLs

LocalService must be able to read the executable, write the data folder, and **nothing
else**. Confirm by attempting, as LocalService, to write outside the data folder. A
service that can write anywhere on the volume is not the configuration this project
ships.

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
docker build -t brrpbrrp:0.4.0-alpha.7 .
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
- **A client cannot yet ask to watch a screen without being treated as a second
  publisher.** A second offer with no publication name is refused. A real two-client
  screen-share run is what shows whether a viewer receives anything, because the local
  harness counts the server's writes rather than a client's receipt.

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

## 4. Evidence template

One JSON file per run, under `planning/m5-checkpoint/`. Record, for each procedure:

```json
{
  "procedure": "windows-service-install",
  "host": { "os": "Windows Server 2022", "build": "20348", "arch": "x86_64" },
  "source_commit": "<40 hex>",
  "artifact": { "name": "brrpbrrp-0.4.0-alpha.7-windows-amd64.zip", "sha256": "<hex>" },
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

## 5. What is blocked, and what is not

**Blocked on the owner:** every section above. No Windows host, no Linux Docker host, no
Tailscale peer, and no physical client devices exist in this environment — `docker` and
`pwsh` are both absent from it.

**Not blocked, and continuing regardless:** all implementation work in M3 through M7,
the local test suites, the recovery rehearsals, the browser journeys, and the release
engineering. None of them wait on a host.

**The right time to ask for the hosts is now**, because a Windows and a Linux machine
are ordinary to provision and the qualification above takes a day of somebody's
attention once they exist. Everything else in the plan is unaffected either way.
