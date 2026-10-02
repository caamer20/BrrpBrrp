# Goal prompt — BrrpBrrp to v1.0.0, resuming an interrupted session

Prepared 2 October 2026, 00:20 Pacific, from the repository as it stood at that minute and from [the v1 roadmap](v1-roadmap-2026-10-01.md). It replaces the 1 October kickoff prompt: the previous goal session was ended by accident in the middle of a change, and this prompt starts from exactly where that left the tree. Copy **everything below the line** into the new session. It needs no earlier conversation.

---

## The goal

You are the implementation agent for **BrrpBrrp**, an open-source, self-hosted chat and voice server — one Go binary with an embedded Svelte frontend and SQLite, so a small community can run its own Discord-style space on a Windows machine or in Docker. **The goal is v1.0.0**: a release people can install, trust with private conversations, update and recover. If your environment has a goal facility, create this as the goal and keep it open; do not invent a token budget for it.

There are two milestones, and they are not the same thing:

1. **Engineering candidate (`1.0.0-beta.1`)** — every agreed feature works and every locally provable check passes against identified artifacts. This is yours to deliver in this repository.
2. **Stable v1.0.0** — that candidate also passes qualification on two real hosts, physical devices, an independent security and accessibility review, a 14-day beta and a 7-day frozen-candidate observation, and then receives the owner's publication approval. You prepare these and run whatever the owner gives you access to; you never declare one passed without its evidence.

The previous session was working toward this goal and was ended by accident mid-change. Seventeen reviews and a full roadmap already exist. You are not being asked for another plan or audit. Pick up the interrupted change, finish it, and keep going.

## 1. The tree as it stands (measured 2 October, 00:15 Pacific)

```text
Repository:  /Users/cameronamer/Documents/ChatGPT/BrrpBrrp
Branch:      codex/p9-private-reporting
HEAD:        f8bbc74ce47fd537da848c83bc8b829e3b073eaf  (no commits since the session ended)
Application: 0.4.0-alpha.8, schema 25, backup format 2
Dirty:       4 files, +281/−35 — the interrupted change (section 2)
main:        876260a (alpha.6, schema 12) — 185 commits behind; do not build on it
Git remote:  none configured
```

**Work in the main checkout, on `codex/p9-private-reporting`.** The interrupted change lives there uncommitted, the session that made it is gone, and it is now yours. If your tool has placed you in a separate worktree — these are usually cut from the stale `main` — first `git merge --ff-only codex/p9-private-reporting`, then bring the change across with `git -C /Users/cameronamer/Documents/ChatGPT/BrrpBrrp diff | git apply`, and tell the owner the main checkout still holds the original copy.

Check `git log -1`, `git status` and `cat internal/app/version.go` before anything else. If HEAD has moved past `f8bbc74` or the dirty files differ from section 2, the tree wins; reconcile this prompt with it rather than the other way round. Do not reset, stash or check out over work you did not write until you have read it.

Ten orphaned `brrpbrrp serve --data-dir …/brrp-private-rehearsal-*` processes are still running from earlier rehearsal runs (three from Wednesday evening, seven from Thursday afternoon). Nothing owns them now. Confirm no rehearsal is in progress, stop them, and fix the harness so it tears down on every exit path.

Read only as much as the task in front of you needs:

1. `planning/v1-roadmap-2026-10-01.md` — the gap list (ENG-, HRD-, QUAL-, PUB- IDs) and phases A–I that this prompt summarises. It and this prompt were written in the worktree `/Users/cameronamer/Documents/ChatGPT/BrrpBrrp/.claude/worktrees/v1-roadmap-analysis-ca7727/planning/`; if they are not in `planning/` on your branch, copy them from there and commit them.
2. `docs/v1-implementation-plan.md` — the contract: requirements, work packages, acceptance scenarios T-*, gates G0–G8.
3. `docs/releases/0.4.0-alpha.8.md` and `planning/v1-execution-ledger-2026-10-01.md` — what exists and how it was verified. The ledger's tranches 1–5 are done and committed.
4. The ADR for whatever you are changing (`docs/adr/`; 0009 authority ordering, 0010 send receipts, 0012–0016 private conversations and groups, 0017 threads, 0019 media).

`docs/v1-progress.md` has a stale status table that says groups and threads are unbuilt. They are built. Trust the source.

## 2. Resume here: the interrupted screen-view change

**What it is for.** Alpha.8's first known issue: a browser viewer negotiates a shared screen but never decodes a frame, because the server answers the viewer's line as `inactive`. At HEAD this is recorded as `test.fixme('a browser viewer decodes the shared screen')` in `web/e2e/voice.spec.mjs`.

**What the uncommitted change does.**

| File | Change |
| --- | --- |
| `internal/rtc/sfu.go` (+73/−5) | `renegotiate` now reads which lines of an incoming offer ask to *receive* (`viewRequestKinds`: an `a=recvonly` line, or a view stream id) and creates the screen output transceiver **before** `SetRemoteDescription`, so the answer's section is the one carrying the screen track |
| `web/src/lib/voice.svelte.ts` (+8/−10) | `addScreenReceive` asks with `recvonly` instead of a trackless `sendonly` line |
| `internal/rtc/renegotiation_test.go` (+129) | new `TestAViewerReceivesScreenFramesOnTheOfferItMade` |
| `web/e2e/voice.spec.mjs` (+71/−20) | removes the `fixme`; adds decode assertions to the two-browser journey; adds a temporary `DIAG-PUB` `console.log` block |

**Where it stands — I applied it to a clean copy of `f8bbc74` and ran it.** It is not finished, and in its current form it makes the committed tree worse:

| Check | At HEAD (clean) | With the change |
| --- | --- | --- |
| `go test ./internal/rtc -count=1` | all pass | 56 pass, **1 fail**, 2 skip |
| New Go test `TestAViewerReceivesScreenFramesOnTheOfferItMade` | — | passes |
| `TestAnUnmarkedSecondOfferIsStillRefused` | passes | **fails**: "a second offer naming no publication and no view was accepted" |
| `svelte-check` | 0 / 0 | 0 / 0 |
| Voice browser project | 10 pass, 1 skipped | 9 pass, **1 fail** |
| "a shared screen reaches another browser and stops cleanly" | passes | **fails at `voice.spec.mjs:632`**: the *publisher* encodes 0 frames in 30 s — an assertion that passes at HEAD |

Three concrete things are wrong, in the order to take them:

1. **The sharer's own share dies.** The diagnostic the previous session added prints, for the publisher: `share: {grabbed: 1, stopped: 1}`, both screen senders `readyState: "ended"`, `encoded: 0`, and transceivers that include `recvonly:video` and `recvonly:audio` — the publisher has asked to *view its own screen*. The failure screenshot shows the sharer outside the voice room. The client does this by design (the spec's own comment: "the publisher's own view request arrives while its publish offer is still being answered"), and the new server path now treats that offer as a view request and answers it through `answerViewOnly`. Start by proving or disproving that reading: is the publisher's offer — which carries its `sendonly` screen lines *and* `recvonly` view lines — being answered as view-only and thereby dropping the publication? The durable fix is probably two-sided: a publisher does not request a view of its own publication, and the server answers an offer containing both publish and view lines without sacrificing either.
2. **A refusal contract now conflicts with the new rule.** `TestAnUnmarkedSecondOfferIsStillRefused` sends a bare `recvonly` video transceiver with no view marker and requires a refusal; its stated purpose is that "the receive path must not become a way to publish by omitting a field". The change makes `recvonly` the way a viewer asks. Decide which contract survives and write it down in ADR 0019: if a bare `recvonly` line is now a legitimate view request, the test must be rewritten to pin what still has to be refused (an unmarked `sendrecv`/`sendonly` video line naming no publication) **and** gain a control proving that media arriving on a view line is never forwarded or registered as a publication. Do not simply delete the test.
3. **The journey calls a helper that does not exist.** `decodedVideo(viewer)` is used six times in `web/e2e/voice.spec.mjs` and defined nowhere; the run never reached it only because it fails earlier. Write it (frames decoded and frame width from the viewer's inbound video stats), and remove the `DIAG-PUB` block before committing.

**Done when:** the full `internal/rtc` package passes, including under `-race`; the voice project passes with nothing skipped; in the two-browser journey the viewer decodes frames of the shared picture, stops decoding when the share stops, and decodes again on restart, while the sharer stays in the call throughout; and the voice project passes twenty consecutive runs. Commit it as its own change with the failing-first evidence in the ledger.

A byte-for-byte safety copy of the uncommitted change as I found it is at `/Users/cameronamer/Documents/ChatGPT/BrrpBrrp/.claude/worktrees/v1-roadmap-analysis-ca7727/planning/wip-screen-view-decode-2026-10-02.patch` (SHA-256 of `git diff`: `e4fd0dcf1d23712343ebb17ef028ae969d001aefeb9b6da3956d2fa2010f8a24`). It is a fallback, not a second source of truth.

One more media observation to fold into that last criterion: at clean HEAD, `voice.spec.mjs:269` ("a screen share is offered, and every refusal is explained") timed out once in six executions waiting 30 s for **Leave voice** after **Join voice** — in a full-suite run, directly after another voice journey; four isolated runs of the voice project passed. Find the cause from a trace (a join racing the previous peer's teardown is the first thing to rule out). Do not lengthen the wait.

## 3. What is already true — do not rebuild it

Verified in a clean worktree at `f8bbc74`: 304/304 frontend unit tests, svelte-check 0 errors and 0 warnings, formatting, build and `npm audit` clean, `go vet` and `go build` clean, `go test ./... -count=1` green across ten packages (API package 278 s), browser journeys 39 passed / 1 skipped with private conversations enabled and 24 passed / 1 failed / 1 skipped in default mode (the intermittent join above; the skip is the `fixme`). The ledger records, at the same source, a green whole-tree race run, four recovery rehearsals (23, 172, 57 and 28 checks) and packaged alpha.8 archives.

Built and locally tested: roles and delegation, restricted channels, consented direct messages, consenting groups, blocking, private evidence reporting, named threads with their own read state, activity and notification preferences, moderation timeouts, bounded invitations, member data export, community voice, private calls, screen publication with separate screen audio, embedded TURN over UDP/TCP/TLS, verified backups with manifests, operator alerts, capacity tooling.

Extend these; do not restart the receipt, conversation-storage, authority-fence or publication-model designs.

## 4. Then, in order

### Phase A — make the project safe and clean-cloneable (hours)

1. **Off-machine copy.** There is no remote; one disk failure loses everything. Ask the owner for a private repository URL, or for permission to create one. Do not create a repository or push anywhere without that answer. Once you have it: push all branches and tags, and fast-forward `main` to the current tip.
2. **Two CI steps cannot pass on a fresh clone.** `scripts/check-doc-links.mjs` and `scripts/check-schema-docs.mjs` require `docs/next-release.md`, which `.gitignore` excludes as an operator note; both fail in a clean worktree. Make the checks independent of deliberately untracked files and add a self-test that runs them against a tree without those files.
3. **Formatting and module drift.** `gofmt -l internal cmd scripts` lists 15 files; `go mod tidy` wants `pion/stun/v3` as a direct requirement. Fix both and add gates to CI and `make check`.
4. **One truthful status page.** Correct `docs/v1-progress.md`'s status table, `docs/roadmap.md`, the README's scope paragraph and CONTRIBUTING's race budget so they describe the current build. Leave dated historical records alone.

Exit: a fresh clone on any machine passes `make check`.

### Phase B — close the engineering gaps → `0.4.0-alpha.9`

Each row was confirmed by reading the code at `f8bbc74`. Media first: it carries the most risk, and you will already be in that code.

| ID | What to build | Where to look | Done when |
| --- | --- | --- | --- |
| ENG-02 | A video feedback path: register `rtcp-fb` for the video codecs, forward a viewer's keyframe request to the publisher (rate-limited), and request a keyframe when a viewer's output is bound | `internal/rtc/sfu.go` — `h264Codecs()` registers no feedback; viewer RTCP is read and discarded where the screen output is added; nothing imports `pion/rtcp` | a viewer who joins **after** a share started decodes within a few seconds; a viewer recovers after induced loss; stale or foreign requests are ignored |
| ENG-03 | Screen presets and bounds: a motion preset (720p/15) and a text preset (1080p/5), a sender bitrate cap, and a server-side per-publisher ceiling (plan target 2.5 Mbit/s) so a share cannot starve voice | `web/src/lib/screen-share.ts` calls `getDisplayMedia({ video: true })` | presets selectable and applied; an over-budget publisher is bounded at the server |
| ENG-04 | Viewer controls: fullscreen/pin, an audio-only preference, a usable phone layout | `VoicePanel.svelte`, `screen-receive.ts` | keyboard-reachable; axe-clean |
| ENG-05 | Codec policy: prove H.264 across the browser matrix or add VP8 as fallback; amend ADR 0019 | the codec registration in `sfu.go` `New` | Firefox and Chromium both publish and view in automation |
| ENG-06 | Output-device selection where the browser supports it; state the recovery path (resume-token rejoin vs. ICE restart) and measure it against the plan's ≤15 s rejoin budget | `web/src/lib/voice.svelte.ts`, `devices.ts` | documented, tested, measured |
| ENG-11 | Reconnect policy: the socket backoff reaches 30 s with no `online`/resume trigger or half-open detection; the plan's budget is ≤10 s p95 after connectivity returns | `web/src/lib/workspace.svelte.ts`, the `reconnectTimer` | fast retry on `online` and foreground resume; tested with a severed socket |
| ENG-12 | A protocol/compatibility handshake: a version in `hello`, a compatible range, and an update-required state that protects drafts | `internal/api/websocket.go` `hello`, `web/src/lib/updates.svelte.ts` | an incompatible client is told to update and loses no text |
| ENG-13 | Storage quota and a disk guard on writes; gather the literal `100` account/channel limits into single-source constants | `internal/api/uploads.go`, `admin.go`, `channels.go`, `invitations.go`, `db/accounts.go` | an upload that would breach the reserve is refused with a useful message; limits defined once |
| ENG-08 | Formatting: emphasis and block quotes in the existing tokenizer, still never handing markup to the DOM (lists deferred unless the owner says otherwise) | `web/src/lib/message-tokens.ts` | malicious-markup tests stay inert; code blocks stay literal |
| ENG-09 | A member profile surface with current identity fields | `web/src/lib/components/` | reachable by keyboard from a message and the member list |
| ENG-10 | Back/Forward navigation between conversations (today only `replaceState` is used) | `workspace.svelte.ts` `conversationURL` callers | Back returns to the previous conversation without showing a revoked one |
| ENG-14 | Bounded, redacted metrics (HTTP/DB latency and errors, queues, reconnects, jobs, backups, media; process RSS and CPU, not only Go heap) behind operator authority, plus a small external HTTPS/WebSocket probe script | `internal/api/router.go`, `diagnostics.go` | no token, message text or user-supplied label in any metric |
| ENG-15 | An encrypted off-host backup procedure using established operator tooling, with key custody and key-loss handling written down and rehearsed locally | `docs/admin.md`, `docs/deployment.md`, `scripts/` | a second directory restores from the encrypted copy using only the document |
| ENG-07 | External TURN and IPv6: the owner is being asked; with no answer, document "embedded relay only; IPv4 media" as a known limit | `internal/db/models.go` TURN settings | decision recorded |

Exit: every row closed or descoped by a recorded owner decision; both browser modes green with nothing skipped; `0.4.0-alpha.9` packaged from a clean commit and its extracted archive executed.

### Phase C — hardening → `1.0.0-beta.1`

The codebase grew by about half in five days, and each of the last six reviews found five to twelve new defects in recently written code. An outside review and a 14-day beta are expensive places to find those.

1. **A generated negative matrix.** The plan's T-AUTH-01: an authenticated outsider holding valid IDs gets nothing — no content, no metadata — from any surface, for a private channel, a thread inside it, a DM and a group. Drive it from the route inventory (`TestEveryRouteDeclaresItsPolicy` in `internal/api/policy_test.go` already forces every route to declare a policy) so that adding a route without its outsider row fails the build. Include exports, activity, call presence and screen events.
2. **An acceptance matrix file** mapping each T-scenario (plan §11.2) and journey J-01–J-10 to the tests that prove it, the last result, the commit, and what the result does not establish.
3. **Rendered proofs still owed:** the 1,000-reply thread in a real browser, an Arabic right-to-left journey, Activity acknowledgment across two browser profiles, media late-join/stop/restart/move/reconnect, a hundred share cycles.
4. **Firefox and WebKit** projects for the core journeys (Playwright runs Chromium only today).
5. **A race job that fits a hosted runner.** The API package alone takes about 43 minutes under the race detector on a sixteen-core laptop; hosted runners have two to four cores and the job allows 60 minutes. Shard it.
6. **Convergence passes.** Fresh-context review rounds over the newest surfaces until a round reports no P1, each fix with a failing-first regression.
7. **The first hosted CI run** on Linux, Windows and macOS, including building the Docker image and running `scripts/check-docker.mjs`. Expect it to be red the first time and treat that as work.
8. **Flip the two feature gates to default-on — only with the owner's approval.** `BRRP_ENABLE_RESTRICTED_CHANNELS` and `BRRP_ENABLE_PRIVATE_CONVERSATIONS` are off by default; v1 requires both features, and the beta has to exercise the configuration that will ship.
9. Freeze features and cut `1.0.0-beta.1` from a clean commit, with checksums, SBOM and provenance.

Exit — the engineering candidate: every locally provable acceptance scenario passes against identified artifacts; hosted CI green on three operating systems; a container image with a digest; no known P1.

### Phases D–I — the external gates

In parallel with A–C, make each gate a day's work for whoever can run it: keep `docs/host-qualification.md` exact for the current build, script what is scriptable, pre-write the evidence files, define the reference fixture, and draft release notes, the support policy and the compatibility statement. As the owner supplies hosts, devices, reviewers and pilots, run host, media and capacity qualification (G4, G5), take the independent reviews and remediate (entry to beta), support the 14-day beta (G6), freeze `1.0.0-rc.N` for the 7-day observation (G7), and present the final release materials for publication approval (G8). The roadmap's §4.3, §4.4 and §5 have the detail.

## 5. Ask the owner once, early, in one message

Send this near the start, then keep working. Only the remote blocks anything in Phases A–C.

- A private Git remote, or permission to create one.
- Decisions: external TURN and IPv6 in v1, or a documented limit? Lists in message formatting, or emphasis and quotes only? Is a VP8 fallback acceptable? When may the two feature gates become default-on? Keep the 100-account limit for v1, or attempt the 250-account profile?
- Longest lead times, to start now: a Windows code-signing certificate; an independent security reviewer and an accessibility tester; three pilot communities; a reachable Windows x64 host on the tailnet and a Linux amd64 Docker host; phones and a UDP-blocked network for media testing; off-host backup storage and who holds its key; a second person who can release and restore.

Both deployment targets (Windows native over Tailscale, Linux Docker), the MIT license and the CLA are settled. Do not reopen them.

## 6. What must not break

The ADRs carry the reasoning; read the relevant one before changing its area.

- **Authority ordering (ADR 0009).** SQLite has one connection. Take the store's connection first and the authority fence second; never wait for the connection while holding the fence; re-establish session and capability inside the mutating transaction; do slow network or media work outside the fence.
- **One conversation model.** Channels, direct pairs, groups and threads share one message, attachment, receipt, search and notification pipeline. No second stack, no shadow channels.
- **Send identity.** An operation is `(account, idempotency key)` with its destination and content inside the intent. A retry replays; a changed intent conflicts; a deleted or expired send never comes back.
- **Privacy.** Owner or administrator status never grants access to a direct or group conversation. Evidence review shows selected evidence, not a browsable conversation. Private actions stay out of the community audit log, community search and link-preview fetches. A removed member loses every delivery path, including in-flight streams, exports and media.
- **Consent.** Nobody is added to a conversation without accepting. A block is never bypassed, and nothing refused is replayed after an unblock.
- **Recovery of user work.** Drafts and the opt-in outbox belong to one account; never restore another account's text, never move a read cursor backwards, never silently turn an expired operation into a new send.
- **Migrations.** Forward-only, transactional, rowid- and FTS-preserving, with postcondition checks. No schema bump without a reason; qualify a populated upgrade whenever you add one.
- **Media.** Capture starts only from a user action and is never silently reacquired; the server stops forwarding on revocation whether or not the client cooperates; a line that asks to receive can never become a way to publish.

## 7. How to verify, and how to record it

Toolchain on this machine (rediscover if moved): Go 1.27.1 at `/Users/cameronamer/Documents/ChatGPT/BrrpBrrp/.tools/go/bin/go` (not on PATH; the directory is untracked, so a separate worktree has no copy of its own), module cache at `/Users/cameronamer/go/pkg/mod`, Node 24, Playwright Chromium in `/private/tmp/brrpbrrp-playwright-browsers`. No Docker, no PowerShell, no Windows host here.

```sh
export GO=/Users/cameronamer/Documents/ChatGPT/BrrpBrrp/.tools/go/bin/go
export GOMODCACHE=/Users/cameronamer/go/pkg/mod
export PLAYWRIGHT_BROWSERS_PATH=/private/tmp/brrpbrrp-playwright-browsers

npm --prefix web ci                                         # only if node_modules is absent
npm --prefix web test && npm --prefix web run check && npm --prefix web run format:check
npm --prefix web run build            # before any Go build: the frontend is embedded
$GO vet ./... && $GO test ./... -count=1                    # about 5 minutes
$GO test ./internal/rtc -count=1                            # 15 seconds; the media loop
CGO_ENABLED=0 $GO build -trimpath -o dist/brrpbrrp-e2e ./cmd/brrpbrrp
BRRP_E2E_BINARY="$PWD/dist/brrpbrrp-e2e" BRRP_E2E_PRIVATE_CONVERSATIONS=0 npm --prefix web run test:e2e -- --project=voice   # about 1 minute
BRRP_E2E_BINARY="$PWD/dist/brrpbrrp-e2e" BRRP_E2E_PRIVATE_CONVERSATIONS=0 npm --prefix web run test:e2e                      # about 3 minutes
BRRP_E2E_BINARY="$PWD/dist/brrpbrrp-e2e" BRRP_E2E_PRIVATE_CONVERSATIONS=1 npm --prefix web run test:e2e                      # about 3.5 minutes
$GO test -race -count=1 -timeout 60m ./...                  # 45+ minutes: checkpoints only
make rehearsal private-rehearsal rollback-rehearsal schema13-rehearsal   # read the Makefile for inputs
```

Rebuild the frontend and the e2e binary after any change to `web/src` or Go before a browser run; a stale binary is the easiest way to test the wrong thing. Run focused tests while you work and the broad gates at each checkpoint.

For every defect: reproduce it with a test that fails first, fix it, and keep a permitted control beside the denied one so the fix is not simply "refuse everything". Do not make a test pass by deleting an assertion, accepting either outcome, returning early on 401/403, widening a permission, lengthening a wait without a timing diagnosis, or reaching into the database where the product has a route. After two failed attempts at the same failure, stop guessing: rerun the single test on fresh artifacts, read the actual output, state one falsifiable explanation and gather the evidence that would disprove it.

Append one entry per tranche to `planning/v1-execution-ledger-2026-10-01.md` (tranche 6 is next): source commit and dirty state, the failing behaviour and how you reproduced it, files changed and why, exact commands with results and counts, what the evidence proves, and what it does not. Keep evidence categories apart: a unit test, a synthetic RTP run, a generated browser stream and a physical microphone prove four different things.

Commit small, coherent changes under the repository's existing Git identity. Do not add AI authorship, co-author trailers or "generated by" notices to commits, code or documents. Never rewrite a recorded release identity; a rebuilt artifact is a new version.

## 8. Continuing, stopping and reporting

Keep going through checkpoints without waiting for approval of routine work; a passing tranche is a checkpoint, not the goal. Stop and ask only when something is the owner's to give — a credential, a remote, a host, a scope decision, permission to publish — and then say which gate it unblocks and continue with whatever does not depend on the answer. If context or runtime forces a stop, leave the ledger able to restart you without another planning cycle. No gate G0–G8 is passed until its evidence exists on the exact artifact; do not rename an alpha to v1, lower scope silently, or mark missing evidence as present.

Your first reply should state, in a few lines: the commit you are on, the dirty files and whether they match section 2, and what you observe when you run the two failing checks in section 2 yourself. Send the owner the list in section 5. Then fix the screen-view change and carry on through Phase A.
