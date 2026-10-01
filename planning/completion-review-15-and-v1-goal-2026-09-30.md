# BrrpBrrp review 15: current product and path to v1.0.0

**Reviewed:** 30 September 2026, Pacific time.  
**Source:** `fbce8b9cfb67b3efdbcd9c66ea5bec7e6470307c`, branch `codex/p9-private-reporting`.  
**Application:** `0.4.0-alpha.7`; schema **24**; backup format **2**.  
**Boundary:** verification and handoff documents only; no application edits.

## Assessment

There has been substantial implementation progress: **34 commits and 83 changed files** since the previous DeepSeek handoff at `d36ad63`/schema22. Community chat, consented DMs/groups, threads, activity, moderation, export, private calls and desktop sharing all have implementations. Several previous defects are repaired.

**This is an integrated alpha under repair, not a release candidate.** Fresh probes still reproduce incorrect account/destination handling, unauthorized authority increases, continued export delivery after access withdrawal and incomplete user workflows. Recovery and distribution evidence also need local work. The repository still records **no G0–G8 release gate passed**. A completion percentage would obscure these differences.

The [new implementation prompt](handoff-deepseek-v1-goal-review-15-2026-09-30.md) supersedes the earlier DeepSeek prompt. Its first deliverable is account-safe thread submission and recovery, followed by bounded implementation checkpoints through a coherent candidate and the full release gates.

## Source and verification boundary

The working tree already contained a change to `internal/rtc/sfu.go`: `setEmitter` uses the peer's `sendMu` when replacing its callback. It was preserved. All fresh test results below describe **HEAD plus that existing patch**, not a clean committed release.

Patch fingerprint: SHA256 of `git diff -- internal/rtc/sfu.go` was `356ab21e9b5df63c800bf210b548547a54a5a1ed60e58dfcf77aa8625acbe4e7`.

| Fresh verification | Result and limit |
| --- | --- |
| `npm --prefix web test` | **259/259 passed** with local socket access; initial sandbox `listen EPERM` failures were environmental |
| `npm --prefix web run check` | **0 errors, 0 warnings** |
| Selected API repair regressions | **15 passed**, alongside four diagnostic probes reproducing five remaining failures |
| Five focused RTC regressions | **5 passed**: publication ownership, microphone mute with continuing screen video, concurrent publication reservation, screen-audio delivery, failed-negotiation cleanup |
| Actual transpiled client/controller probes | Reproduced account-switch send, wrong restored destination, missing pending-thread rows, incorrect activity acknowledgment and nonterminating evidence pagination; confirmed Activity remount works |
| Preserved schema13 executable | Hash and embedded clean revision verified; read-only preflight returned supported schema **13**, exit **0** |
| Current source and artifacts | Reviewed relevant implementation, migrations, scripts, workflow, existing packages and recorded evidence |

RTC tests initially failed because sandbox socket restrictions prevented peers from connecting. The same batch passed with socket access. Synthetic peer/RTP tests do not establish decoded browser frames, physical capture, audible quality or host capacity.

No fresh full Go race suite, full browser suite, release build, full recovery rehearsal, real Windows/Tailscale or Docker qualification, deployed alert, physical media test, independent review, beta or final observation was run in this review. Historical pass counts remain tied to their original source and fixture.

The API diagnostic batch took **8.195s** and exited nonzero as expected: four probe functions asserted the intended contracts and reproduced five failures. The fixture is outside the repository at `/private/tmp/brrp-review15-_8s0tqsw/export_test.go`, with this reproduction command:

```sh
GOCACHE=/private/tmp/brrpbrrp-go-build \
GOMODCACHE=/Users/cameronamer/go/pkg/mod GOPROXY=off \
.tools/go/bin/go test \
  -overlay /private/tmp/brrp-review15-_8s0tqsw/overlay.json \
  ./internal/api -run '^TestReview15' -count=1 -timeout 3m -v
```

These temporary diagnostics are reproduction aids. Permanent regressions must live in the project and remain usable without those paths. The legacy receipt probe is a focused compatibility fixture, not a full old-binary upgrade. The export probe observes the actual HTTP response body in a test recorder, not an external-network transfer. Client probes execute transpiled implementation with controlled inputs, not rendered browser journeys.

## Repairs to preserve

| Area | Current progress |
| --- | --- |
| Timeout scope | Real login, permitted private operations and community refusal now have positive acceptance tests; roles are no longer replayed at expiry |
| Private audit | Evidence-review and owner-only group audit filtering now occurs in the query |
| Export | Scope checks, feature checks, limits, interrupted-job recovery and a delivery registration exist; the remaining revocation defect is below |
| Thread storage | Thread-qualified intent, commit-time lock checking and schema23 root tombstones are implemented; schema24 stores separate thread read positions |
| Everyday UI | Message whitespace, Activity remount/destinations and timeout route/duration controls are repaired |
| Private calls and media | Block/unavailable-peer checks, room revalidation, independent media handling and the five tested RTC repairs exist |
| Browser sharing | Source now fences delayed capture by account/room/connection/generation and attaches received streams when the element mounts |
| Operations | Capacity transport supports TLS verification and WebSocket heartbeat; recovery now contains meaningful group and thread fixtures |

These are specific closures or implementation improvements, not acceptance of each entire feature. Do not restart the old review-14 repair list unchanged.

## Remaining findings

### V15-01 Thread submission and recovery can change the author or destination

**P1; reproduced using the actual transpiled Workspace.** In `web/src/lib/workspace.svelte.ts:1380`, `sendIntoThread` awaits its preliminary thread lookup before capturing identity. Starting A's send, switching to B, then resolving the lookup causes the POST to proceed as **B with A's text**.

`OutboxEntry`, `trackOutbox` and restoration omit `thread_id`. A failed thread send persisted and restored through the real Workspace posts to **`/channels/{id}/messages`**, carrying the original thread text/key. An uncommitted operation can therefore move into channel history; a committed one cannot reconcile through its original destination. `pendingReplies` also filters `reply_to_id`, while named-thread sends use `thread_id`, leaving pending thread sends invisible.

**Required:** capture/fence the initiating identity before every asynchronous step; preserve the full destination through persistence, retry and recovery; never infer a missing destination from the currently active view. Verify account switch, lost acknowledgment, definite refusal, reload and panel close/reopen through observable outcomes. Preserve text and the operation key until the outcome is resolved.

### V15-02 Older send receipts no longer recognize unchanged retries

**P1; reproduced.** A durable receipt containing the exact pre-change canonical digest from `d36ad63` returns **409** for an unchanged ordinary channel retry after the digest update. The new thread field changed the digest of existing non-thread sends too; no compatible interpretation/migration covers those receipts.

**Required:** version or safely interpret legacy intent without authorizing different work. An unchanged old operation must resolve to its original result; changed content, thread or conversation must remain a conflict. Test a populated upgrade fixture as well as new receipts and deletion-safe results.

### V15-03 Removing a role can grant powers the actor cannot delegate

**P1; reproduced through the real role API.** An actor holding `role.manage` but lacking `message.send` removes a target's only read-only role with `role_ids: []`. The target's send changes **403 → 201** because default-role fallback grants send. The actor still cannot send.

**Required:** authorize the effective final authority change, including implicit default capabilities and intermediate mutations. Keep legitimate membership changes working; removing a restrictive assignment must not bypass the grant boundary.

Timeout follow-through also remains incomplete by source inspection: apply/lift handlers lack the promised audit calls and live-media enforcement; bulk principal construction used for media revalidation does not load `Suspended`. Their HTTP scope repair is real, but live community restrictions and private-call positive controls still need acceptance.

### V15-04 Export registration does not enforce all revocations

**P1; reproduced.** After export delivery registers, a role-permission change removes the member's access. Ordinary history becomes **404**, but the export still returns **200 containing the withdrawn content**.

`export.go:671` registers the response with an empty channel scope; `endUnreadableStreams` skips empty scopes. There is also a check/commit/register gap to cover. Registration alone does not establish an atomic, scope-aware delivery boundary.

**Required:** register the export's complete scope under the authority admission protocol and cancel it for every applicable loss of access, including role/channel changes. Test revocation before admission, after registration and during transfer, with an unchanged-authority positive control. This concerns server-held delivery, not recalling copies already legitimately downloaded.

### V15-05 Stored thread state is not yet a complete thread experience

**P2; reproduced API behavior.** Marking a thread's sole reply read still leaves channel unread at **1**. Sending after archive returns **201**. `thread_reads` is written, but unread projection still uses channel cursors over thread messages.

Source inspection also finds unbounded page accumulation/re-reading, no direct Reopen control, and incomplete draft/key handling: definite refusal can be interpreted as success, and closing the panel discards an unresolved key.

**Required:** integrate thread/channel read projections, implement the approved archive/reopen contract, preserve author recovery and make all history reachable with bounded client memory and DOM. A thousand-reply fixture must demonstrate this through the member interface.

### V15-06 Activity and evidence still disagree with their helpers

**P2; reproduced using current controllers/component logic.** With visible activity `05,04,03`, acknowledgment updates older server rows `02,01`, while the client paints `04,03` as seen. Reload restores all visible rows as unseen. Returning an update count did not align the acknowledged set.

With 120 message reports and 70 request reports, the evidence component receives all 190 by page three, but never finishes. Skipping an exhausted source synthesizes a page with its old nonempty cursor, which the merge helper treats as active again. Rendered rows/notes/busy state still use only `report.id`.

**Required:** an explicit shared acknowledgment contract with pagination/concurrent-arrival tests; persistent per-source exhaustion and `(source_kind,id)` identity throughout the actual evidence panel.

Additional source-confirmed completion work: notification retention writes `brrp.notified.*` but cleans `brrp.notify.*`, with non-atomic notification admission. The export browser journey can succeed early on 401/403 and seeks a button where the UI renders a link; export retry keys and building-state UX also need completion.

### V15-07 Recovery checks can pass without establishing restoration

**P1 for qualification; source-confirmed.** `upgrade-recovery-private.mjs:839–846` reads `body.exports`, but the API returns a raw array. Both reconstructed lists are therefore empty. “History survives” tests only status200; the other-account check is vacuous. No original export ID/metadata, ready precondition or missing-file410 is proved. Thread follow and activity state are not fully asserted after restore.

The fixture still seeds reviewer privileges through SQL and falls back to SQL when API revocation fails, then records unconditional success. Complete the supported ownership/MFA ceremony in the disposable test installation and require API success. This is locally implementable.

**The schema13 baseline is available:** `dist/brrpbrrp-p9-checkpoint`, SHA256 `fddd1e3f3ef3c9873e822dd96f625c3bcdfd036b76fdff2ba45c5835a4c3b57f`, embedded clean revision `2869db55394b2b2a004424672517b38d6908586b`, supported schema13. Local qualification need not wait for archive hosting.

CI still requests the known-wrong `0.3.0` baseline, the seed omits submitted evidence required by its verifier, and `--report` remains ignored. Hosted Linux acquisition needs appropriate pinned bytes, but seed/report/fixture corrections and local schema13→24 qualification can proceed now. The reported 133-check recovery run has no new matching retained JSON/manifest in the inspected checkpoint.

### V15-08 Current distribution and release evidence are incomplete

Existing alpha.7 archives remain a mixed **schema21/schema14** set; none is a coherent schema24 distribution. Preserve historical bytes and produce a **new uniquely versioned prerelease** after integration. Consume the extracted packages and retain exact source, binary, toolchain and final-byte identities.

The repaired capacity transport removes an old local blocker; it does not supply reference-host measurements. Both required hosts, target load, physical media, actual alerts, independent review, beta and final observation remain unqualified. Current ten-serverwide media places and the private-call participant bound must be reconciled with the agreed v1 target and qualified.

## Execution order and release boundary

1. Preserve/finish the existing SFU patch with focused verification; fix thread author/destination identity and legacy receipt compatibility.
2. Fix effective-role delegation and export admission/revocation; finish timeout audit and live enforcement.
3. Complete thread, Activity, evidence, notification and member-export journeys.
4. Produce reproducible current-schema and real-prior-schema recovery with exact assertions and meaningful failing controls.
5. Run integrated suites on a frozen source, build one coherent new prerelease and verify extracted native/source consumers.
6. Qualify **Windows x64 native/Tailscale and Linux amd64 Docker**, real devices/networks, independent review and operator procedures.
7. Complete the master plan's **three-community fourteen-day beta** and **seven-day frozen-candidate observation**, apply reset rules, then obtain publication approval and verify publication.

Retain the complete scope and acceptance in `docs/v1-implementation-plan.md`: reference load, two-hour peak/media sessions, 24-hour soak on each deployment, independent encrypted backup, RPO ≤24h and RTO ≤60min including operator response and security reconciliation. Missing external resources must have precise procedures and owners; they do not excuse unresolved local defects. This review produced documentation only and did not commit, package or publish application changes.
