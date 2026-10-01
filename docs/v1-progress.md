# v1 implementation progress and evidence ledger

**Last updated:** 28 September 2026 (P10 tranche; review 11)
**Baseline reviewed:** 0.4.0-alpha.3 (schema 2)
**Candidate:** **0.4.0-alpha.7** (schema **21**, backup format **2**)
**Plan:** [v1-implementation-plan.md](v1-implementation-plan.md)
**Current evidence:** [alpha.7 release record](releases/0.4.0-alpha.7.md)
**Prior packaged checkpoint:** [alpha.6](releases/0.4.0-alpha.6.md)
**Historical finding record:** [completion-remediation-2026-09-24.md](completion-remediation-2026-09-24.md)
**Assessment:** substantial local implementation progress; no G0–G8 release gate has passed. Earlier dated reviews describe their inspected snapshot. They do not override this candidate's implementation or supply missing host/independent acceptance.

This ledger records what was actually implemented, what evidence exists, and what
is deliberately still missing. It is the honest companion to the plan: the plan
describes the required work, and this file records progress against it. Where a
plan target is a proposed number, the number is unchanged and unmeasured.

**Status vocabulary.** A package can be implemented, tested locally, accepted on a
platform, and approved, and these are different things. This ledger is written in
that order: "complete on this machine" means code plus local tests, never a host
rehearsal, platform acceptance or review. The finding-by-finding record, including
the evidence level of each claim and the acceptance test that pins it, is in
[the remediation ledger](completion-remediation-2026-09-24.md). The dated completion reviews remain historical evidence; the release record above is the current implementation checkpoint.

### M3 — threads, moderation, export and everyday polish (schema 19–21)

**Organized discussion (WP-13).** A thread is a channel conversation with a subject,
not a second message store. It inherits the channel's authorization wholesale, so a
thread cannot become a way around a channel restriction, and a member's access to one
changes exactly when their access to the channel does. A member starts one from any
message, names it, is followed by default, can follow and unfollow, and a moderator
locks or archives it. A locked thread is still readable and followed; an archived one
is out of the list but not gone. A 1,000-reply fixture is walked one bounded page at a
time and every reply is reached exactly once, in order, with no overlap between pages
and no change to the channel listing.

**Moderation (WP-15).** A report can be assigned, and the assignment is a claim about
who is expected to look at it — reviewing still requires the reviewer's own current
capability, so an assignment to someone who has since lost it is a stale claim rather
than a way to review. A timeout suspends an account without touching its roles and
restores exactly the assignments recorded when it was applied, so nothing a moderator
did while it ran is silently reversed; it answers 403 and says what it is, where a ban
answers 401. Invitations can admit a bounded number of people, expire, and be revoked,
with the use count consumed by the write itself.

**Composer (WP-16).** Message text is tokenized rather than split on a regex: a fenced
code block is literal, so a pasted snippet cannot notify the wrong person or turn a
sample URL into a link; only `http` and `https` become links; a mention must be a whole
token and is never the local part of an email address; and nothing is ever handed to
the DOM as markup.

**A member's own data export (WP-12).** The requirement named a bounded per-user
export and the product had none. There is one now, and the contract is in
`docs/adr/0019b-member-export.md`. It is a **snapshot**, not a query: every row is
read in one transaction, so the file holds what the account could read at the moment
it asked. It contains **only permitted personal data** — the conversation list is
resolved through the same rules a message list uses, so it cannot contain a direct
pair whose acceptance was withdrawn after the messages were written, and the account
block carries five non-secret fields with password hashes, sessions, second-factor
secrets and recovery codes structurally absent. The bounds (50,000 messages, 2,000
attachments, 256 MiB) are applied **while it is built**, and when one is reached the
export stops and says so, in the file and in the interface. It is gated on recent
strong authentication because it is the one read path whose output leaves the
building, and authority is re-read at download as well as at build. A produced file
has a seven-day server lifetime, and the member can delete theirs sooner.

**Recovery at schema 21.** All three consumers pass — 23 community, 113 private, and
57 rollback from alpha.6 (schema 12) to alpha.7 (schema 21). The schema-13 rehearsal
derives the pending set from the registry and reports "expected 14…21 beyond 13".

### M4 — voice, calls, screen sharing and relay paths (schema 20)

**The decision is made and written down.** `docs/adr/0019-media-architecture.md`
chooses to **extend the embedded Pion SFU and rewrite its publication model** rather
than introduce a separately self-hosted SFU. The reasoning is visible in the code
rather than preferred: the control plane that actually matters — per-channel
publication limits, resume-token ownership, TURN credential issuance *and
revocation*, non-cooperative speaker enforcement at the RTP boundary, moderated rooms
— is already built and tested, and an external SFU would mean re-implementing all of
it across a network boundary, each re-implementation a new way for authorization to
disagree with itself. The one-binary deployment is also most of what makes the product
approachable.

**The decision was conditional on a video-forwarding measurement, and the measurement
is made — but not on the hardware that decides it.**
`planning/m4-checkpoint/video-forwarding.json` records one publisher to nine viewers at
2,500 kbit/s on a **sixteen-core development host**: 36,369 packets forwarded, mean
egress 23.28 Mbit/s against the plan's arithmetic of 22.50, peak heap 37.4 MiB. That is
not the four-core, 8 GiB reference allocation the plan's budgets are stated for, so the
media profile is **not qualified** and the same measurement is on the host checklist to
re-run there.

The measurement also found two gaps, both recorded in the ADR. `MaxParticipants` bounds
the **whole server** at ten where the plan targets twenty serverwide, ten per room — a
decision about a community's capacity that belongs to the owner rather than to a
benchmark. And a client could not ask to *watch* a screen at all; that is now fixed,
with the refusal for an unmarked offer kept.

**All five media capabilities are built.**

- **Community voice** was already present and is unchanged.
- **TURN TCP fallback.** The embedded relay listened on UDP only and forced
  `transport=udp`, so a client behind a network that blocks UDP had *no path at all* —
  the ordinary case on corporate networks, and invisible locally because nothing here
  blocks UDP. The relay now offers UDP and TCP on one ICE server and advertises a
  configurable host, because a relay behind NAT that advertises its listening address
  produces a call that works locally and fails for everyone else.
- **Screen sharing.** A publication is an object with a stable id, owner, kind and
  lifecycle, replacing the fixed `voice-0..8` slot array that encoded "one track,
  never changes, position is identity" — all three of which a screen share breaks.
  Renegotiation is allowed, bounded, and gated on the same `voice.speak` decision as
  the microphone. The screen is carried by a **separate forwarder on the video
  clock**; the audio forwarder's constants are right for 20 ms of Opus and meaningless
  for 90 kHz video, and reusing them turns a connected screen into noise. The client
  explains every way a share does not happen, and a capture that ends on its own — the
  user stopping it, the tab closing, the browser revoking it — stops forwarding.
- **Private calls.** A room is a conversation, not a channel. A direct pair or a group
  goes through the same membership resolver a private message uses, so a call ends
  when that membership does. A call's participants go to its members and nobody else,
  and the call is offered from the conversation header.

**What is still not established.** No physical microphone, no real browser capture
prompt, and no call across a network that actually blocks UDP — the browser suite
substitutes a generated stream because a picker cannot be driven by a test, and
synthetic RTP proves the forwarding path rather than the media. The 40-minute race
suite has not been run for this schema. The recorded recovery and browser figures
were taken at schema 20, before the publication model; the Go and frontend suites are
current for schema 22.

**The Linux Docker runtime is written and unrun.** `scripts/check-docker.mjs` is a
**runtime** check, not a static one: it creates a container on a disposable volume with
**`--read-only`, `--cap-drop=ALL` and `no-new-privileges`**, walks start → healthcheck
→ stop → restart, proves data survives the restart, runs a backup from *inside* the
container and brings the archive back out. Thirteen assertions in all. It runs in the
project's Docker CI job on a Linux runner.

**What is still missing for that deployment** is the parts a container cannot answer:
the media and TURN topology reachable from a real network, the fault injections the
master plan lists (full disk, termination, slow clients, network faults), and the
monitoring and alert delivery that M5 requires. `docs/host-qualification.md` §2 has
the procedure for each.

**Blocked on the owner.** `docs/host-qualification.md` is written step by step so both
required deployments can be qualified the moment hosts exist. This environment has no
Docker, no PowerShell, no Windows host and no Tailscale peer, so **none of it has been
run** — the Docker script included.

### M5 — operations, capacity and public release (in progress)

**Host qualification procedures are written, not run.** `docs/host-qualification.md`
gives the exact commands, what each one observes, and an evidence template whose
required `not_established` field keeps a passing run from being read as proving more
than it does. It also says the two things this work is usually misreported as:
static Dockerfile checks are **not** Docker runtime qualification, and a page loading
over Tailscale says nothing about whether media established. This environment has no
Docker, no PowerShell, no Windows host and no Tailscale peer, so **none of it has been
run**.

**Release engineering, closed locally:**

- The **schema-13 rehearsal ships** in the source package. It did not, so a source
  archive could not reproduce the qualified upgrade from a real prior executable.
- **CI now runs it**, building its own seed from the pinned baseline executable and
  **failing rather than skipping** when that baseline is missing — a rehearsal that
  did not run is not a passing rehearsal. Building a seed with the *current* binary
  produces a current database, and the rehearsal's own check rejects it: a lookalike
  fixture is refused, not accepted.
- A **machine-readable SBOM** (CycloneDX 1.5) is generated from the same discovered
  component set as the third-party notices, so the two inventories cannot disagree.
  `--check` verifies both files and writes neither. A release stamps the version it
  was built for.
- A call now reports **which path it took** — direct, via STUN, or relayed through
  TURN. Latency cannot distinguish those, and the case this project is most likely to
  hit is a relay behind NAT that connects on the host and fails for everyone else.
- **Operator alerts now exist and are verified by delivery, not configuration.**
  `GET /api/admin/alerts` reports when an alert last *reached* the destination, the
  current conditions and the delivery history with attempt counts and the last error.
  A webhook that has never delivered looks like coverage, and the operator finds that
  out during a restore. The conditions are the ones that mean recovery is unavailable:
  a verified backup older than three times the interval, consecutive failed attempts, a
  copy that is behind, and low space. Six tests run a real HTTP receiver and check what
  arrived. **Not established:** no alert has been delivered to an operator's own
  receiver on a real deployment, so the mechanism is proven in tests and not in
  production.
- A **notification's tap now opens the activity** and the worker **deduplicates on the
  tag the server actually sends**. Both were defects an independent review named: the
  worker read a field the payload does not have, so no notice was ever deduplicated, and
  the announced destination was a path no route matched, so pressing one went nowhere.
  Deduplication is retained for 24 hours per account per event, and a browser that
  refuses storage still gets notifications.

- **A capacity harness exists and has been run once, on a development host.**
  `scripts/capacity-fixture.mjs` builds a population through the public API — a
  fixture written straight into the database would skip authorization, receipts, the
  outbox, search indexing, retention and the revision, and would report numbers for a
  system nobody ships. `scripts/capacity-load.mjs` measures delivery and search latency
  over the real WebSocket, and `make capacity-fixture` / `make capacity-load` are
  defaulted to the master plan's own figures.

  The recorded run is annotated with what it does not establish: one host, loopback
  clients, 30 messages, delivery p95 9 ms and search p95 4 ms. **No budget is claimed
  as met** — the reference profile is 250 accounts, 100 channels and a million
  messages, and this measured a few dozen on an idle developer machine. There is no
  ramp, no two-hour peak, no 24-hour soak, no mixed private traffic under load, and no
  media in the measurement. The tool reports a `null` verdict when it measured nothing
  rather than `true`, because a run that measured nothing must not read as a run that
  passed.

**The race suite passes at this schema.** `.tools/go/bin/go test -race -count=1
-timeout 40m ./...` — ten packages, the API package at 1556.7 s, 1558.3 s overall,
**no data races**. The first run found one: a test helper I had added iterated the
SFU's peer map without its lock while a connection-state callback removed a peer.
Production code holds that lock; the test reached past it. The recorded figure
supersedes the older schema-14 one.

- **Orphaned upload files are reclaimed, not just their rows.** Maintenance deleted an
  attachment row whose message was gone and left the bytes on disk, so a server
  reported free space while it filled. The sweep is file-first — a file is removed
  only once no row claims it — and a *young* unclaimed file is left alone, because
  an upload between the response and the message that claims it is exactly the case
  that would otherwise be deleted out from under the person who just uploaded it.
- **Right-to-left text is laid out in the direction it was written.** WP-16 lists RTL
  and the handling was missing rather than wrong: a message beginning in Arabic and
  ending in an English URL rendered its final full stop on the wrong side. The
  composer gets the same treatment, where a caret in the wrong place is the most
  disruptive form of the bug. **Not established by a rendered Arabic journey** — that
  belongs with the device testing M6 already requires.
- **The new surfaces are checked for detectable accessibility violations.** The
  release journey had an axe check and no other did, so the export, alert, thread and
  voice panels were correct and unchecked. The check reaches the thread list by
  **keyboard rather than hover**, because a control that only exists under a pointer
  is invisible to somebody using a keyboard. This is not a conformance claim: a
  screen-reader, keyboard, zoom and reduced-motion pass find what no scanner can, and
  M6 requires them from a person.
- **Composer and navigation (WP-16).** Three items the requirement names were absent:
  a member could not link to a message, could not stop an upload, and a failed upload
  cost them a trip back to the file picker and their typed text. Now: **Copy link**
  copies a *permalink*, not the message — sharing a link is sharing a reference, and
  pasting the text would put a private conversation wherever it was pasted. The
  permalink resolves through the same context path as a reply reference, so it is
  checked against current authority rather than carrying the permissions it was
  created with. **An upload can be cancelled**, because an upload that cannot be
  stopped is one somebody is trapped in: the only way out was to close the tab, which
  cost them the draft too. A deliberate cancel is not reported as a failure. And a
  failed upload is **retryable in one press**, keeping both the file and the draft.
- - **CI parses every shipped script and checks the capacity fixture's routes against
  the router.** The capacity and alert tooling needs a live server, which is exactly
  when somebody reaches for it, so a script that rotted silently used to fail on a
  real deployment. Both gates were run before being committed: a gate that fails on
  arrival teaches people to ignore the one that matters.

**What is not established.** No qualifying capacity or soak measurement — the recorded
figures are a development host over loopback, and the video-forwarding run was on
sixteen cores rather than the reference allocation. No container runtime, no signed
Windows download, and no canonical public repository or registry identity: there is no
Git remote configured in this environment. No alert has been delivered to an operator's
own receiver on a real deployment, though the mechanism is proven against a real HTTP
receiver in tests. The race figure in this section is the current one; the recovery and
browser figures above are recorded at the schema they name.

## Current private-message tranche

Schemas 11–12 and ADR 0012 implement the shared private-message rebuild, canonical direct requests, consent, durable contact limits, blocking, shared message/file/reply/search/read behavior and an opt-in inbox. ADR 0013 and 0014 add selected private-message evidence reporting and recipient-only request evidence, with `report.review_private` as a distinct delegated capability. This is partial progress in WP-09–11, not completed private messaging or v1 acceptance: push/activity, groups, calls, screen sharing and independent review remain. Private conversations default off.

### Review 13 — M1 activity and M2 groups

#### Consenting groups, with an interface

The groups tab is part of the Messages dialog, alongside conversations, requests and
blocked people, and it carries its own badge for invitations this account can still
answer. A group feature nobody can reach is not delivered, so the rendered journey
drives the whole thing: create, see the history rule stated before answering, invite,
accept, hand over, remove, and confirm an outsider is not told the group exists.

Four defects surfaced while building it, three of them in my own work and one a
product limitation I had created:

- the invitation field originally accepted only people **already in the group**, which
  is the opposite of what an invitation is for. The owner is precisely the person who
  knows whom to ask, so the picker offers anyone usable on the server.
- the removal action was gated on the ability to *invite*, so a group with full
  invitations could not remove anybody. Removal and handing over are ownership
  actions; invitation is a separate allowance that can be spent.
- the member list and the invitation list both read an owner flag through a scalar
  subquery, which returns NULL rather than 0 for a non-owner and failed the scan
  into an int as a 500 — indistinguishable from a real fault.
- ownership that moves on one page leaves the other page's copy stale, and stale
  authority correctly shows no removal action. The journey re-reads, which is what a
  person does; the interface does not pretend otherwise.

#### Consenting groups

ADR 0016 and migrations 16–17 add groups on the shared conversation model. A group
is a `conversations` row with `kind='group'` and members in `conversation_members` —
not a second message store. The schema has said `group` since migration 6 and every
access check was written against membership rather than against "is this a direct
pair", so the work was generalizing one existing check rather than adding a parallel
system.

Three contracts are decided in the ADR rather than in whichever handler was written
first. **History**: an accepted member reads the group's earlier history, stated before
acceptance. **Blocking**: a block does *not* hide a group conversation — there is no
per-pair view of a shared room to hide — and the product does not offer an affordance
promising otherwise; membership is the protection and the block governs direct
contact. **Ownership**: one owner, atomic transfer, an owner cannot leave a group that
still has members, and the last member leaving removes the group rather than demanding
a successor who does not exist.

Two defects were found by existing tests rather than by new ones, and both are the
kind a group introduction causes. Generalizing the membership check first required
the partner to still be *usable*, which broke two load-bearing contracts: an erased
peer's history must stay readable under the Deleted member identity, and a deactivated
peer's must too — "blocking is not erasure". The partner check now asks only whether
the peer is still a *member*; usability is a question for the write branch, which is
where it belongs.

#### Durable activity

ADR 0015 and migration 15 add durable account activity and private notification
preferences. Unread and the outstanding request badge stay derived; the missing piece
was a durable record that survives being offline. Push destinations are typed, so a
notice with no destination is refused rather than treated as unchecked — the previous
shape read an empty channel id as "no check needed", which is exactly how a private
notice would have arrived.

Verified: go vet clean, the full Go suite green, 132 frontend tests, svelte-check 0/0
over 135 files, both browser modes (13 default-off, 21 private-enabled).

### Review 14 — the private group integration boundary

Source `d08d16e`, schema 17. Four V13 findings closed, each with a permanent
regression verified failing before its change and passing after.

**V13-02, group metadata in ordinary audit.** A plain `audit.read` account could see
`group.invited` and `group.joined` with the actor's username and the private group id.
Group events are now part of the restricted classification, so the one classifier
covers writers, both readers and cleanup. The audience defect underneath it was
originally recorded rather than patched: the restricted feed was gated on
`report.review_private`, which meant the only accounts able to read who had joined a
group were evidence reviewers — a grant made to see submitted text was in practice a
grant to browse group membership. It was then moved to `audit.read` to fix that, which
was a wider mistake (V14-01, below): an ordinary auditor was refused the evidence
itself and then handed the reporter, the reviewer, the evidence id and the time.

The feed is now gated on `report.review_private` again, and private group lifecycle is
*not* widened by that grant: a group is not submitted evidence. The community audit
capability reads the community feed and nothing private.

**V13-03, membership changes bypassing the revocation protocol.** Accept, leave,
remove and transfer committed as ordinary writes. Reproduced before the fix: a
removal left the authorization revision at 2 → 2 and a registered attachment
response running. They now go through the direct-conversation authority protocol —
revision from the committing transaction, session fencing through it, commit, then
stream cancellation and post-commit publication. On the client the panel cleared
direct state on an authority change but not group, invitation or member arrays, and
the member fetch admitted its late response; both are fenced now, and a group
transfer's success notice was being wiped by the invalidation it caused, so a person
who had just handed over a group saw nothing happen.

**V13-04, invitation and empty-group lifecycle.** The reproduced sequence — sole
owner leaves with an invitation pending, invitee accepts and receives 200, then
reading the group 404s — is now refused. Acceptance verifies the group is still
active and checks current block state inside the granting transaction; a block
closes the invitation rather than leaving it for a later unblock. Ending a group
closes its invitations and removes its owner row in the same transaction. Invitation
creation applies the same contact policy, and an absent `allow_requests` row is the
documented default rather than a refusal.

**V13-01, groups as conversations.** The snapshot carries groups under their own
identity rather than a synthesised peer; `isPrivate` is the single routing decision
the navigation, permissions, paths and deep links share; the sidebar lists them; a
card offers Open conversation; and message, reply, reaction, upload, scoped search
and selected-message reporting all reach the shared handlers. Proven by
`TestAGroupIsUsableThroughTheSharedConversationPipeline`.

**The last direct-only branch was in the components, not the workspace.**
`MessageItem` decided private-versus-community from `isDirect`, so a group message
was treated as a community one and offered the channel report dialog rather than the
private one. A group message reporting that way would have put submitted private text
into a community moderator's queue — the separation `report.review_private` exists to
keep. The composer had the same shape for a staged upload's destination and the search
panel for its scope parameter. All three ask `isPrivate` now.

**Both rendered group journeys pass.** A member creates a group, a second accepts
having been told the history rule, both open it from the card, a message crosses in
each direction with both pages open and no reload, a message is reported through the
private dialog, and a transfer is handed over live. The live-ownership claim is
narrowed to what the sessions observe: the guest's page receives the reply while it
is open, and the owner reads it back after the panel interaction — asserting it live on
the owner's page was asserting a delivery its socket had not registered, which a
websocket trace showed was not the server's doing.

Browser 13 default-off and 23 private-enabled; nine Go packages, 132 frontend tests,
svelte-check 0/0 over 135 files, Prettier clean.

**A note on the process, because it cost real time.** Several edits believed applied
had not been — Prettier reformatted the file between the edit and the run — and
Playwright's `error-context.md` lags the code, so several diagnoses came from a stale
locator. The failure screenshot and a live websocket trace were the only reliable
signals. A next coder debugging here should read the current file and the
screenshots, not the recorded errors.

### Review 13 and the M1 activity foundation

ADR 0015 and migration 15 add durable account activity and private notification
preferences. The missing piece was not a UI: unread and the outstanding request badge
are both derived and stay derived, and the only stored notification state was
channel-scoped, while push dispatch carried a single `channelID` — a value a direct
conversation does not have. An activity row is created inside the transaction that
creates the thing it describes and carries identifiers, a kind and a timestamp, never
a second copy of private text.

Identity is `(source_kind, source_id)` with a unique index, and a request is identified
by its attempt rather than by the pair, so a renewed introduction is a new event.
Acknowledgment is durable, per account and monotonic; delivery stays a device fact.
Preferences are account-level per kind, the ordering is fixed in the ADR, and an
unreadable preference is a refusal rather than a permission.

Push destinations are now typed. A notice with no destination, or one naming a kind
this build does not recognise, is refused at dispatch — which closes the bypass where
an empty channel id read as "no check needed". A private payload is content-free: it
carries the activity's own opaque id as its de-duplication tag, not the conversation,
because a lock screen can read the body.

Verified: go vet clean, the full Go suite green, 132 frontend tests, svelte-check 0/0
over 135 files, both browser modes (13 default-off, 21 private-enabled), and 23
community plus 113 private recovery checks at schema 15.

### Review 11 and the P10 tranche

The reviewer journey runs in its own Playwright project after the ownership journey, because `report.review_private` is not on the pre-ceremony administrator bridge and so must be delegated from an owner. It signs in as the steward the ownership journey leaves in place, and re-establishes that session before delegating: arranging the fixture conversations signs the shared API session in as a participant, which silently ran the role creation as that participant. Generated account names are held within the 32-character username bound, and the private-evidence queue labels each record with its source kind while still omitting the originating conversation.

Verified on the committed source: `go vet` clean, `go test -race -count=1` green across every package, `svelte-check` zero errors and warnings, Prettier clean, frontend build clean, and both browser modes green (13 default-off, 21 with private conversations enabled).

### Review 8 findings and the completed P8 stabilization tranche

The review of clean source `1f762e9` freshly passed all 101 frontend tests, Svelte checking (zero errors/warnings) and `go test ./...` (API 82.083s; some other packages cached). All seven alpha.5 archive hashes still match their stored manifest.

Its findings and their resolution in the P8 tranche (implementation commit `606cdb120326507c280cf0c1fbc89c85b44598cd`, evidence in the repository under `planning/p8-checkpoint/verification.md`):

- **R8-01 (fixed):** a member could publish a staged file after `message.upload` was revoked. `createMessage` now re-checks the transaction-scoped principal's upload capability before any new attachment claim, after receipt replay; both scopes have permanent regressions with denial-side-effect, same-key-retry, restoration, replay-after-revocation and deleted-replay controls.
- **R8-02 (fixed):** community mention preferences no longer load from the channel header's lifecycle; the account-owned store verifies them after every admitted snapshot, fail-closed with bounded retry and a delayed-read/newer-write race control. Rendered regression proven failing before the fix and passing after.
- **R8-03 (fixed):** an unsent request introduction survives unrelated contact/authority refreshes; targets are revalidated rather than destroyed, and refusal keeps the copyable draft. Rendered regression proven failing before the fix and passing after.
- **R8-04 (defined; hosted run pending):** CI now requires both browser feature modes with a visible directs-project proof, fresh-recovery and extracted native/source consumer checks on every push, and a dispatch-time cross-version rollback gate that stages pinned, sha256-verified archived releases (`scripts/baseline-releases.json` + `scripts/fetch-baseline-release.mjs`) and fails loudly when no approved artifact source is configured.
- **R8-05 (done locally):** `scripts/upgrade-recovery-private.mjs` demonstrates private consent/block/cursor/preference/attachment/receipt and removed-peer state through a real backup/restore, plus post-snapshot security-state loss with an explicit reconciliation step (68 checks).
- **R8-06 (done locally):** the full uncached `go test -race -count=1 -timeout 20m ./...` is green (API 887.109s), both browser modes pass against the packaged binary (13/13 default-off, 16/16 opted-in), recovery/rollback reports are recorded, and packaged artifacts exist for this checkpoint. These are local checkpoint artifacts, not a published release.
- **R8-08 (fixed):** the source-package displayed count no longer includes a phantom entry from a trailing blank line; empty/Windows/space-path self-test controls were added.

Review 8 itself changed documentation only; the fixes above are the P8 tranche's work, recorded at that commit.

## Status summary

| Package                                                    | Status                                                                                                                        | Notes                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| ---------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| WP-01 project identity/rights                              | **Partially complete**                                                                                                        | License, copyright holder and contribution terms are decided and recorded ([ADR 0006](adr/0006-licensing-and-contribution-terms.md)): MIT, an owner-named holder, a CLA with a signatory record, generated dependency notices and a security policy. A local Git source baseline is recorded; missing: the canonical public repository, maintainer access and recovery, release/registry identities (D-15), a _tested_ private security intake, and a code of conduct with a named enforcement contact — none of which this workspace can create.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| WP-02 scope and decisions                                  | **Partially resolved**                                                                                                        | ADR 0001–0012 record the implemented contracts, including community account policy and gated DMs; private-history ratification is still required before broad exposure. D-01 is resolved. D-03, D-05, D-13, D-15, D-17 remain open.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| WP-03 reference environments                               | **Not started**                                                                                                               | Needs the Windows host, Docker host, load generator and reviewers.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| WP-04 migration framework and durability                   | **Complete on this machine, including a maintained cross-version rehearsal**                                                  | Ordered registry, transactional apply, preflight without migration, downgrade refusal, invariant verification, index rebuild, `FULL` durability default, preflight/migrate/verify commands, the non-migrating `backup`/`verify` inspection path and an explicit incomplete-upgrade state, and the attachment-durability contract with tests at each boundary ([ADR 0007](adr/0007-attachment-durability.md)). `make rehearsal` runs a **fresh-install recovery** check on a disposable destination (install → back up → verify → restore into an empty directory → compare bytes); its "touched archive" step flips archive bytes rather than only the archive's mtime. `make rollback-rehearsal` runs the **cross-version** path against a real archived prior release: install with the old executable, its snapshot as the rollback source, the upgraded installation's rows and bytes, a recorded rewrite boundary, and a rollback into a clean folder, plus an injected interruption before the upgrade's postconditions. A real Windows/Docker host rehearsal (WP-24) and an off-host restore destination remain, as does a measured RPO/RTO.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| WP-05 account/session/recovery                             | **Complete for the backend, the account UI and the documented offline path**                                                  | Any member may enroll a factor; operators must enrol one before privileged actions; session list, revoke-one, revoke-all; 15-minute recent-authentication gate with an audited reauthentication path; sign-in re-checks its credential inside the session transaction; an operator removes another account's lost factor under `operator.credential_recovery`; a locked-out owner is recovered on the host with `brrpbrrp recover-owner`, recorded as the host operator. Recovery-code race, demotion, socket-revocation, sign-in-race, factor-removal and offline-recovery tests pass. The revocation-during-stream half of T-IDENT-01 is now explicit rather than implied: streams are registered against the account and session that admitted them, so a revoked session, a changed password, a removed factor, a role change or a ban ends exactly the delivery it should, and a queued event is dropped when its generation is stale. Password changes, operator resets, factor removal, kicks, bans, deactivation, deletion and sign-out commit with invalidation of affected deliveries. Ended sessions are terminally refused; some operations also disconnect a surviving session whose credentials remain valid for reconnect. Deferred cleanup cannot admit a new event to an ended session. What remains is the real-host (Windows/Docker) rehearsal under WP-24.                                                                                                                                                                                                                                                                                                                                                                                                                     |
| WP-06 central authorization                                | **Core implemented; effective-role revocation and delivery ordering locally verified, with one documented acquisition order** | Capability catalog, roles, delegation, owner ceremony, operator tier, non-disclosing denials, transaction-time re-authorization, full route inventory enforced by test. Role changes act on the _effective_ holders of a role — including every account that holds the default role without an assignment row — and end only the streams the new authority no longer admits; a restricted event carries the authorization revision its audience was resolved at, so an audience computed before a revocation cannot be delivered after it, and a streamed download re-establishes the account's authority and session at admission. The commit/admission protocol is now one documented order — the store's connection first, the authority fence second, nothing waiting for the connection under the fence — and it covers credential and session transitions as well as authorization ones, with socket registration itself an admission ([ADR 0009](adr/0009-authority-commit-and-admission-ordering.md), including the route-to-commit/invalidation inventory). Evidence: `internal/api/effective_role_revocation_test.go`, `revocation_boundary_test.go`, `commit_ordering_test.go`, `internal/ws/hub_test.go`. Remaining: an independent review of the ordering contract (WP-28) and the platform host evidence.                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| WP-07 roles/categories/restricted channels                 | **Complete behind the WP-08 gate, capability-driven**                                                                         | Ownership, role CRUD and assignment, durable categories, visibility, grants, posting policy, the access inspector and invite-time role selection all have a management UI, and the UI is now driven by the caller's effective capabilities rather than an administrator flag, so a delegated manager reaches exactly what they were granted; ownership designation and transfer have a ceremony UI. The UI is blocked with the API while `BRRP_ENABLE_RESTRICTED_CHANNELS` is off. A restricted **voice** channel now enforces the same access rule, but it still has no private-call admission contract of its own (WP-19/WP-20, ADR 0003).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| WP-08 privacy across delivery surfaces                     | **Implemented for reads, files, events, queues and voice; locally verified, not independently reviewed**                      | Every delivery path the package lists is enforced and has evidence: filtered state/navigation/search/unread/pins/context/replies/counts, current-access file and preview delivery, scoped mentions/typing/voice presence/events, dispatch-time push checks, and revocation that reaches an in-flight download, the notification backlog, a queued event backlog and both an active and a resumable voice connection. A metadata response already being written can finish (ADR 0003, gap 2). The Go peer-eviction test alone carries no SDP/RTP; the separate browser transport journey negotiates real connections and measures RTP during permission withdrawal and regrant. Its current result and synthetic-capture limitations are recorded in the alpha.5 release record. Physical media qualification remains WP-19/WP-24 work. Screen-share and private-call media do not exist yet, so their scoping belongs to WP-19/WP-21.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| WP-23 trustworthy backups and recoverability               | **Substantially complete on this machine; failed-copy state locally verified**                                                | Every archive carries a versioned manifest with the application and schema version and a size and SHA-256 digest for the database and each attachment ([ADR 0008](adr/0008-backup-manifest-and-verification.md)); restore verifies those digests as it stages, `brrpbrrp verify-backup` checks any archive read-only anywhere on disk, an extra copy is recorded only when its digest matches the archive it copied, and `production-check` verifies the newest archive and copy instead of only dating them. The recorded status now separates the last attempt, the newest verified local generation and the newest generation that is _also_ copied to independent storage: a failed copy counts in its own bounded retry, keeps the previous recoverable identity and its archive protected from count-based retention, and is repaired by retrying that copy rather than by writing another archive. The declared recovery point is taken at the database snapshot boundary, before the vacuum and the compression that follow it, and `production-check` names the last independently recoverable generation when the newest one has no copy. Retention now depends on an explicit successful persistence result rather than on the absence of one error sentinel, and it protects the archive this attempt produced, the archive the pre-attempt durable record named and the local archives of both independently recoverable generations, so a combined copy _and_ status failure cannot prune what the ledger still names. What remains needs other machines: an off-host restore into clean Windows and Docker destinations, RPO/RTO against a reference dataset, disk-pressure and copy-destination faults, and the operator's own encrypted-copy workflow with its key-loss handling. |
| WP-09 shared conversation model and resilient client state | **Shared channel/direct foundation implemented and locally tested; P8 receipt/claim defects repaired with regressions**       | Schema 6 introduced conversations/receipts; schema 11 completes the rowid-preserving required conversation FK and channel/private projection. Schema 12 adds direct read/contact state. Alpha.5 prevents expired-key resurrection. Shared outbox persistence is account-specific with a 50-record/64 KiB UTF-8 budget; permanent refusal/expiry produces text-only recovery, and read cursors stay monotonic. R8-01 claim-time capability checks and the R8-03 draft lifecycle are fixed with regressions (P8 checkpoint). Reconnect/performance qualification remains.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| WP-10 direct messaging                                     | **Opt-in consented slice; request discovery added in P9**                                                        | Canonical requests, explicit acceptance, durable limits, contact opt-out, hide preferences and shared content surfaces exist; outsider administrators do not gain membership. A durable outstanding incoming-request count now appears on the Messages entry with a direct way into Requests; it is derived from current state, survives opening the dialog, and updates across devices through the existing invalidation hint (ADR 0013, P9 checkpoint). A dedicated DM activity/notification system and independent acceptance remain. … |
| WP-14 notifications                                        | **Community mute slice implemented; DM policy incomplete**                                                                    | Schema 10 persists preferences; server suppresses mention events and queued push with late checks. The account-owned policy load means a session that stays in a direct conversation still verifies mention preferences (R8-02, P8 checkpoint). Request/activity discovery, DM defaults/previews, preference precedence, persistent inbox/deduplication and platform qualification remain.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| WP-11 groups and blocking                                  | **Direct blocking implemented; groups not implemented**                                                                       | Direct block/unblock preserves accepted history, stops contact mutations and never replays refused sends. Consenting group invitations, ownership, membership/history lifecycle and shared-group block policy remain.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| WP-12 account lifecycle                                    | **Community removal and direct cleanup implemented**                                                                          | Recent-auth typed confirmation, immutable Deleted member identity, transactional cleanup and retained history; separate account/message erasure path. Alpha.6 clears pending introductions and preserves remaining accepted-peer history. Export, group membership/history, policy ratification and post-snapshot reconciliation remain (ADRs 0011–0012).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| WP-14 notifications                                        | **Community mute slice implemented; DM policy incomplete**                                                                    | Schema 10 persists preferences; server suppresses mention events and queued push with late checks. A mute written on one device now reaches the account's other sessions through an account-owned `preferences.changed` signal, and the store re-verifies policy on that signal, on reconnect and on window focus, so a session sitting in a direct conversation no longer filters on a stale answer (R9-02, P9 checkpoint). Request discoverability is delivered as a badge plus existing DM unread state; **no** new DM OS notification, push, sound or ringing, and no DM activity/dedup/preference-precedence system. DM defaults/previews, persistent inbox and platform qualification remain. … |
| WP-15 moderation                                           | **Community and private selected-message evidence implemented behind separate authorities**                                       | Durable posting intervals, per-reporter deduplication, rate limits, current-readable-channel review, audit and 90-day expiry from report creation for community messages. Private direct-message evidence is a separate store and capability: one server-confirmed snapshot a reporter explicitly confirms, `report.review_private` for review, read (not write) eligibility so a blocked participant can report retained history, edit-before-confirm refusal, durable hourly limits, and nonpunitive resolve/reopen that neither deletes nor bans (ADR 0013, P9 checkpoint). Report assignment/timeouts, invitation-abuse workflow, file evidence and policy qualification remain. … |
| WP-13, WP-16–22, WP-24–33                                  | **Not complete**                                                                                                              | Named/archived threads, broader UX/media work, both host lifecycles, capacity, independent review, beta and final release. Existing UI improvements and local packaging are partial evidence, not completed work packages.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |

No gate (G0–G8) has passed, and no capacity, latency, media or recovery target in
section 10 is claimed.

The 24 September 2026 second review added findings R2-01–R2-10 and the P2 plan. Its
progress is recorded item by item in §6 of
[the remediation ledger](completion-remediation-2026-09-24.md): current-authority and
credential transactions, client authorization invalidation, live voice permissions,
delegated-role journeys, unified archive validation, the maintained cross-version
rehearsal, and the package consumer path. Two of those items are explicitly incomplete:
R2-04's negotiated-RTP evidence and the platform lifecycles in P2-06/P2-07 need hosts
this workspace does not have.

The 25 September 2026 third review added findings R3-01–R3-06 and the P3 plan, and it is
recorded in §7 of the same ledger. Its four reproduced defects now have implementation fixes
and permanent regressions that fail without them: late incremental and unread responses can no
longer cross a revocation (and the client withholds counts while a new snapshot is unverified),
an implicit default-role holder's stream is cancelled while an account that keeps the capability
is left alone, a failed independent copy keeps its bounded retry and the previous recoverable
generation, the snapshot's declared time is the moment the database snapshot was taken, and that historical browser run passed with a non-cooperative sender control proving media denial
was server-side. Consult the alpha.5 release record for the current suite and capture evidence. What that review still lists as open is the rendered failed-refresh and
role-only/category-only journeys, the independent foundation review, and every host,
platform and capacity qualification — none of which this workspace can establish by itself.

## Earlier implementation record (schemas 3–6)

The remainder of this historical record describes earlier tranches. Current status and corrections are above and in the alpha.7 release record; alpha.6 remains the prior packaged checkpoint. Present-tense statements below describe those earlier snapshots until the final next-actions section.

Schema 3 adds two stages in one migration:

- **M-A:** `users.is_owner` (unique), `roles`, `role_assignments`,
  `authorization_state`. Backfill gives every existing administrator the
  administrator role and every other account the member role; no owner is
  designated.
- **M-B:** `categories`, `channels.visibility`, `channels.posting`,
  `channels.authorization_revision`, `channel_role_access`,
  `channel_user_access`. Backfill keeps every existing channel public,
  members-postable, in the same order, with the same IDs and history, and turns
  the previous free-text category names into durable categories.

Schema 4 adds session identity: `sessions.last_seen_at`, `sessions.user_agent`,
`sessions.privileged_at` and an index, with existing sessions backfilled so their
last-seen time is their creation time.

Schema 5 adds `invitation_roles`: an invitation may carry the role grants its
recipient receives. An invitation made before the upgrade carries none and its
recipient gets the default role, which is what every new account received
before. The rows are a promise, not an authority: the inviter's delegation
rights are checked when the invitation is written and checked again when it is
accepted, so a link that outlives a demotion delivers the default role and
nothing more.

Schema 6 is the shared conversation descriptor and deletion-safe send receipts
(M-C, [ADR 0010](adr/0010-conversation-model-receipts-outbox-and-cursors.md)).
It creates `conversations` (one per channel, with the channel id as the
conversation id, created by the migration and by a channel trigger so a channel
can never exist without its conversation), `conversation_members`,
`messages.conversation_id` (nullable, no foreign key yet, backfilled from
`channel_id` and read through `COALESCE(conversation_id, channel_id)`) and
`message_operations` receipts with their expiry and outcome indexes. Message
ids, rowids, reply and attachment references and content are untouched, so the
FTS index stays valid, and every existing message is given a receipt whose empty
intent digest means "intent not recorded": a replay is answered from the message
itself and a missing message is a non-recreating conflict. Nothing private is
enabled by this schema, and the `NOT NULL`/foreign-key tightening that direct and
group conversations will need is deliberately a later rowid-preserving rebuild
with foreign keys disabled outside the transaction, as ADR 0010 states.

New code: `internal/db/migrations.go`, `internal/db/migration3.sql`,
`internal/db/migration4.sql`, `internal/db/migration5.sql`, `internal/db/roles.go`,
`internal/db/unread.go`,
`internal/db/users.go`, `internal/db/sessions.go`, `internal/db/accounts.go`
(`RecoverOwner`), `internal/authz/`, `internal/api/authz.go`,
`internal/api/roles.go`, `internal/api/channel_access.go`,
`internal/api/sessions.go`, `internal/api/admin.go` (`removeLostFactor`), the
`recover-owner` command in `cmd/brrpbrrp/main.go`, and the route policy table in
`internal/api/router.go`. The pre-role `routePolicy.Administrator` flag is
deleted; capability plus recent authentication is the only mechanism left.

Voice presence was the last community-wide surface. It is now channel-scoped in
three places: `internal/api/voice_access.go` publishes a channel's participant
list only to that channel's audience (the web client merges one channel at a
time), the snapshot in `internal/api/state.go` filters `voice_states` through the
same policy rule as channel content, and joining or moving resolves the channel through
`s.voiceChannel` so a guessed identifier is refused exactly like a missing channel.
Revocation is no longer deferred to the next request either: withdrawing a grant,
a role or administrator status runs `enforceVoiceAccess`, which ends the media
connection, revokes the account's TURN credentials, sends `voice.access_revoked`
with a reason and records `voice.access_revoked` in the audit log. The web client
merges voice updates per channel and shows that reason instead of looking like a
network drop.

WP-01 stopped being a documentation gap. The owner chose MIT with a personal
copyright holder, and chose a contributor license agreement over a developer
certificate of origin so the project can still be relicensed later without tracking
down every past contributor. `LICENSE`, `CLA.md`, `CLA-SIGNATORIES.md` and
`SECURITY.md` now exist, with `web/package.json` declaring the license.

Dependency notices became generated rather than hand-maintained:
`scripts/licenses.mjs` reads the modules the compiler actually links into the
binary, the frontend packages the web bundle contains, and the Go standard
library, then writes `THIRD-PARTY-NOTICES.md`. It fails when a license cannot be
read instead of guessing, `make check` fails when the file is stale, and the
release helper now embeds that same output next to `LICENSE` in every package
instead of maintaining a second generator.

Revocation now reaches work already in flight rather than only the next request.
`internal/api/delivery.go` registers every streamed response against its channel and
hands it a context that the authorization change cancels, so a slow download of a
9 MB file ends mid-copy instead of after the client has received it;
`http.ServeContent` keeps working for range requests because the reader delegates
`Seek` and re-checks only `Read`. Channel changes end that channel's streams, while
account-wide changes (a role, a ban, a removed factor) end all of them, because the
affected channels are not known at that point. The push worker's dispatch check is
unchanged but now observable: `Server.pushHTTP` is built before the worker starts so
a test can substitute a recording transport and _watch_ a queued notice be dropped.

Attachments are now published the same way a database commit is trusted. A file
is written to a temporary file in its destination directory, flushed, renamed onto
its final name and followed by a directory synchronization, and only then may the
`attachments` row that references it be inserted (`internal/app/publish.go`,
`publish_unix.go`, `publish_windows.go`). An interrupted upload therefore leaves
no final name, no temporary file and no row; a process killed between publication
and the insert leaves an unowned file that no one can read and the hourly sweep
removes; and a committed reference always has its bytes on disk. The upload path
in `internal/api/uploads.go` and the preview writer both go through it, and the
contract, the boundaries it covers and the ones it does not are recorded in
[ADR 0007](adr/0007-attachment-durability.md).

Backups now describe themselves. A format 2 archive records the application and
schema version that produced it and a size and SHA-256 digest for the database and
for every attachment, written last because the inventory is only complete then.
Restore validates structure, manifest agreement and the size budget before writing
anything and hashes each entry as it stages it, so a touched archive is refused
rather than installed; `brrpbrrp verify-backup` runs the same check read-only
against an archive anywhere on disk, which is what an operator needs for the copy
that lives on separate storage. `copyBackup` hashes while it streams and records
the copy only when it matches the archive it copied,`production-check` verifies the newest archive and copy instead of only dating them and judges freshness by the snapshot's recorded time and generation rather than filesystem mtime, a mismatched valid copy fails identity, and Maintenance shows the newest archive's identity. Format 1 archives stay readable, and the command says
so instead of implying a digest check it could not perform. The decision, what it
does and does not prove, and the evidence are in
[ADR 0008](adr/0008-backup-manifest-and-verification.md).

`scripts/upgrade-recovery.mjs` (also `make rehearsal`) turns the **fresh-install
recovery** promise into something anyone can execute: it builds a candidate,
preflights an empty folder, installs, writes a message and an attachment through
the HTTP API, backs the installation up, restores that archive into a second empty
destination, verifies it and serves the restored copy long enough to compare the
account, the channel, the message and the attachment bytes with what was written,
printing every step and failing on the first unmet promise. Scope matters: this is
recovery from a snapshot taken by the _same_ build, on one machine, into a second
directory. It does not exercise a rollback, an off-host destination, or a
changed-mtime copy. Those remain open (WP-04/WP-23/WP-24) and are listed in the
[remediation ledger](completion-remediation-2026-09-24.md).

`scripts/upgrade-rollback.mjs` (also `make rollback-rehearsal`) is the cross-version
rehearsal, and it uses a real archived prior release rather than a schema fixture. It
installs with the old executable and writes a representative dataset (owner, member,
three messages with a reply, a pin, an invitation, an uploaded file and an enrolled
second factor), snapshots with the old executable as the rollback source, proves the
candidate's own snapshot of the un-migrated installation is written in the current
backup format and refused by the old executable, upgrades with this tree and checks
every row, the exact attachment bytes and a working factor, records what changed after
the snapshot, and rolls back with the old executable into a clean folder, where the
snapshot's data is intact and the later writes are absent. It also injects the durable
state an interruption before the upgrade's postconditions leaves behind and proves
verification refuses it while starting the server completes it, rebuilds the search
index and clears the marker. It needs an archived release for the platform, or
`--old-release`/`--old-binary`, and fails rather than skipping when neither exists. It
reports 53 checks and writes its JSON with `--report FILE`. What it still cannot show:
a Windows service or container lifecycle, a representative-scale dataset, a second
platform pair, an off-host destination, or measured RPO/RTO.

Client work for WP-05 lives in `web/src/lib/components/AccountSecurity.svelte`
(enrollment, signed-in devices, revoke-one/revoke-all, sign-in confirmation) and
in the People panel's **Remove lost second factor** action, with
`requiresFreshAuthentication` in `web/src/lib/api.ts` letting the archive and
credential-recovery surfaces explain a refusal and link to that panel.

Client work for WP-07 adds a **Roles** tab (`admin/Roles.svelte`) that edits the
capability catalog within the caller's own authority, role assignment per account
in the People panel, category creation, renaming and ordering plus the
**Who can access this?** panel in the Channels tab (visibility, posting policy,
role and member grants, the effective reader list, and the standing disclosure
that community managers can read every community channel). Invitations carry an
optional role selection. `web/src/lib/permissions.ts` mirrors the server's
capability catalog, and `/api/roles` reports per-role `grantable`, `editable` and
`deletable` so the panel disables a control the engine would refuse rather than
letting the user discover it on submit. The client also reacts to
`authorization.changed` by dropping cached conversations, closing an open reply
thread and refetching the snapshot, which closes the client-cache gap ADR 0003
recorded; the same invalidation runs when a refreshed snapshot carries different
authority, so a refresh that only succeeds later cannot leave a revoked view on
screen.

Client authority is capability-driven rather than role-labelled. `/state` returns
the caller's effective capabilities, `web/src/lib/workspace.svelte.ts` exposes them
as `can`/`canAny`, and the server-tools entry, each admin tab, the account row menu
and the channel reorder controls are gated by the capability the server actually
checks, so a delegated manager reaches what they were granted and loses it when it
is revoked. Ownership has an interface: `admin/OwnerCeremony.svelte` designates or
transfers the owner from the People tab, requires the target username to be typed in
confirmation, and explains the recent-authentication refusal with a link to account
security. Sidebar grouping follows the durable category position the server returns,
so a reordering survives a reload. Search and pins hold their own results, so they
key on an authorization generation and discard an answer admitted under the old
authority.

The same generation now guards every completion that can arrive late, not just
search and pins: an incremental message page, an unread count, a snapshot refresh
and a channel-scoped search each record the authorization revision they were
admitted under and are discarded if the workspace has moved past it, so a response
that started before a revocation cannot restore a message, a count or a channel
after it. Unread counts are additionally withheld — not merely ignored — while a
newer snapshot is still unverified, so the badge cannot briefly show a number
computed from authority the client has not confirmed. The rendered delegated-manager,
role-change and owner-transfer journeys now exist in the browser suite
(`web/e2e/authority.spec.mjs` and `web/e2e/ownership.spec.mjs`, run by
`npm run test:e2e`), and the store's behavior is covered by
`web/tests/workspace-state.test.mjs`.

## Evidence

Commands run on this working copy, in this order, all passing:

| Check                                                              | Command                                                                                                                          | Result                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| ------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Build                                                              | `go build ./...`                                                                                                                 | clean                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| Static analysis                                                    | `go vet ./...`                                                                                                                   | clean                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| Formatting                                                         | `gofmt -l internal cmd scripts`                                                                                                  | clean                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| Go tests with race detector                                        | `go test -race ./...`                                                                                                            | all packages ok                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| Frontend unit tests                                                | `cd web && npm test`                                                                                                             | 84 passing (73 after P3, plus the four account/request regressions in the P4 tranche, plus four outbox and three client-state regressions in the WP-09 client slice)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| Frontend type/accessibility check                                  | `cd web && npm run check`                                                                                                        | 0 errors, 0 warnings                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| Frontend formatting                                                | `cd web && npx prettier --check …`                                                                                               | clean                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| Upgrade from an alpha.3 fixture                                    | `go test ./internal/db -run TestUpgradeFromAlphaPreservesWorkspace`                                                              | identities, non-ASCII names, channel IDs/order/category, replies, pins, reactions, attachments, sessions, MFA and search all preserved; categories created once each; administrators keep authority; ownership not inferred                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| Interrupted migration                                              | `go test ./internal/db -run TestFailedMigrationLeavesRecoverableState`                                                           | failed upgrade leaves schema 2, no partial objects, and a retry succeeds                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| Downgrade refusal                                                  | `go test ./internal/db -run TestRepeatedMigrationAndDowngradeRefusal`                                                            | newer schema refused by both `Migrate` and `Open`, with an actionable message                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| Search-index repair                                                | `go test ./internal/db -run TestVerifyDetectsGapsAndIndexDamage`                                                                 | a missing index entry is repaired from surviving messages; message rows and IDs unchanged                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| Durability policy                                                  | `go test ./internal/db -run TestDurabilityPolicyIsExplicit`                                                                      | default is FULL (2), NORMAL is opt-in, unknown values rejected                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| Privacy across delivery surfaces                                   | `go test ./internal/api -run TestRestrictedChannelPrivacy`                                                                       | see below                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| Remediation findings (F-01…F-16)                                   | `go test ./internal/api -run 'TestDelegatedMemberManager                                                                         | TestPrivateChannelLifecycle                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       | TestReadSurfacesAgree | TestListenOnlyMember                                                                                                                                                              | TestARevokedSession'`, `go test ./internal/db -run 'TestInspectionAndSnapshotNeverMigrate | TestAnIncompleteUpgradeCannotBeSkipped'`, `go test ./internal/backup -run TestManySmallAttachments`, `go test ./internal/app -run 'TestDirectorySynchronization | TestAbandonedTemporary | TestVersionIdentity'`, `cd web && npm test`, `node scripts/check-docker-inputs.mjs --self-test`, `BRRP_GO=go node scripts/check-source-package.mjs` | each finding's evidence level, acceptance test and residual limits are recorded in [the remediation ledger](completion-remediation-2026-09-24.md); the two static package checks fail if the Docker context or the source package loses an input, and the Docker audit's `--self-test` fails if it stops detecting the incomplete-notices-stage defect |
| Route policy inventory                                             | `go test ./internal/api -run TestEveryRoute`                                                                                     | every route declares a capability or an explanation; no duplicates; public set is exactly the four bootstrap endpoints                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| Operator commands                                                  | `brrpbrrp preflight                                                                                                              | migrate                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           | verify`               | preflight reports schema 0 → pending 1,2,3,4,5,6 without creating or migrating; migrate creates schema 6; a second preflight reports no pending work, `wal`, `FULL`, integrity ok |
| Invite-time role grants (WP-07)                                    | `go test ./internal/api -run TestInvitationCarriesRoleGrants`                                                                    | an invitation with no roles grants only the default role; an administrator cannot promise the administrator role (403) and an unknown role is a client mistake (400); a promised moderator role arrives and is real authority (the audit log opens, invitations stay refused); the pending list reports what it will grant; and a promise written by an administrator who is demoted before acceptance delivers the default role and nothing more                                                                                                                                                                                                                                                                                                                                                                                                                 |
| Role list is policy-aware (WP-07)                                  | `go test ./internal/api -run TestRoleListReportsWhatTheCallerMayDo`                                                              | every role reports `grantable`, `editable` and `deletable` from the same policy the engine enforces (an administrator may grant and edit the member and moderator roles but cannot delete a built-in role or touch the administrator role), so the management UI does not offer a control that would always be refused                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| Licensed source and dependency notices (WP-01)                     | `BRRP_GO=go node scripts/licenses.mjs --check`                                                                                   | the notices cover the 39 components a release redistributes — the linked Go modules, the bundled frontend package and the Go standard library — with each license read from the component itself; the check fails on a stale file, an unrecognised or missing license stops generation, and every component's license text is reproduced                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| Release packaging carries the license                              | `BRRP_GO=go go run scripts/release.go -notices FILE`                                                                             | the release helper now generates its notices through the same script (29,489 bytes for this dependency set) instead of listing the whole module graph, and each native package and source bundle now includes `LICENSE`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| Voice presence follows channel access (WP-08)                      | `go test ./internal/api -run TestVoicePresenceFollowsChannelAccess`                                                              | a granted member's snapshot lists the participant and an account without access sees an empty list; `voice.join` and `voice.move` on the real identifier answer "Voice channel not found." and neither leaves an unauthorized account inside the SFU; a community manager sees the participant, which the access inspector discloses                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| Voice events stay inside the audience (WP-08)                      | `go test ./internal/api -run TestVoiceStateEventsStayInsideTheChannelAudience`                                                   | the granted member and a manager receive the restricted channel's participant list; an account without access receives no voice event at all inside a bounded window; leaving publishes an empty list to the same audience, so no client keeps showing someone who has left                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| A role change reaches the accounts that hold it implicitly (R3-02) | `go test ./internal/api -run TestEffectiveRoleRevocation`                                                                        | an account that holds the default role with no assignment row is in the stream's audience; narrowing the default role ends its stream, its resumable connection, its in-flight download and its voice admission, while an account that still holds the capability through another role is left alone. With the effective-holder expansion removed the first case fails, so the test pins the implicit-holder path rather than a named assignment                                                                                                                                                                                                                                                                                                                                                                                                                  |
| Revocation evicts an active voice peer (T-AUTH-02)                 | `go test ./internal/api -run TestRevocationReachesAnActiveVoiceStream`                                                           | with an SFU peer joined through a no-op signaling callback (no SDP/RTP), withdrawing the role that admitted the account removes it from the voice room while the connection is open, delivers `voice.access_revoked` with a reason, refuses the rejoin and the history request, and records one audit row. This proves **peer eviction and refusal**, not that flowing audio stopped; media qualification belongs to WP-19/WP-24                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| Revocation ends an in-flight download (T-AUTH-02)                  | `go test ./internal/api -run TestRevokedAccessEndsAnInFlightDownload`                                                            | a restricted 6.5 MB file paused after 8 KB and then revoked delivers fewer bytes than the file and the stream ends; a new request answers 404. With the revocation call removed the same test delivers all 6,815,744 bytes and fails, so it pins the behavior rather than the timing                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| A queued notification cannot outlive access (T-AUTH-02)            | `go test ./internal/api -run TestQueuedNotificationCannotOutliveAccess`                                                          | the push worker is held on one notice while a restricted notice waits behind it; the grant is withdrawn, the backlog drains, and the restricted device receives nothing while the trailing notice (and the same notice after the grant is restored) does                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| A resumable voice connection cannot outlive access (T-AUTH-02)     | `go test ./internal/api -run TestADetachedVoicePeerCannotResumeAfterRevocation`                                                  | a detached connection resumes successfully before the revocation, is gone from the voice room after it, and the same token is refused afterwards                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| Client voice handling (WP-08)                                      | `cd web && npm test`                                                                                                             | an update for one channel replaces only that channel's entries, and an access revocation ends the stream with the server's reason on screen instead of a silent disconnect                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| Client authorization handling (WP-08/WP-07)                        | `cd web && npm test`                                                                                                             | an `authorization.changed` event clears every cached conversation and refetches the snapshot, and a channel the snapshot no longer returns is dropped from the cache and the active conversation moves on                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| A late completion cannot cross a revocation (R3-01)                | `cd web && npm test`                                                                                                             | five regressions: an incremental message page, an unread count, a snapshot refresh and a channel-scoped search each admitted under the old revision are discarded after `authorization.changed` (the arriving messages do not enter the conversation, the count does not move, a stale snapshot does not become current, the search results stay as they were), while the same completion admitted under the current revision still applies — so the guard discriminates rather than dropping everything. Removing the generation check makes the first four fail                                                                                                                                                                                                                                                                                                 |
| Unread counts are withheld while a snapshot is unverified (R3-01)  | `cd web && npm test`                                                                                                             | after an `authorization.changed` event the unread total is not displayed from the last known snapshot until a refetch confirms it, and an incremental count admitted in that window does not restore it; the count returns once the confirmed snapshot arrives                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| Recent-authentication gate (T-AUTH-04)                             | `go test ./internal/api -run TestRecentAuthenticationGatesOperatorActions`                                                       | a stale operator session is refused the archive and the ownership ceremony with an actionable 403; a wrong password is refused, a missing factor is refused, and password+code or a recovery code unlocks it                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| Privileged enrollment policy (D-10)                                | `go test ./internal/api -run TestPrivilegedActionsRequireAnEnrolledSecondFactor`                                                 | an unenrolled administrator is refused the archive while an ordinary member keeps sending messages; the policy is a setting, not a silent bypass                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| Sessions and revocation (T-IDENT-01)                               | `go test ./internal/api -run TestSessionListingAndRevocation`                                                                    | three sessions listed with no token material; revoke-one signs out exactly one; another account's session id returns 404; revoke-all keeps the caller and removes the rest                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| Revocation reaches live sockets                                    | `go test ./internal/api -run TestRevokingOneSessionClosesOnlyItsSockets`                                                         | the revoked device's socket closes at the revocation; the surviving session's socket still completes a send                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| Recovery-code race (T-IDENT-01)                                    | `go test ./internal/api -run TestConcurrentRecoveryCodeUseSucceedsOnce`                                                          | 8 concurrent uses of one recovery code consume exactly one; the consumed code cannot sign in afterwards                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| Sign-in races                                                      | `go test ./internal/api -run TestStaleCredentialsCannotCreateASession`                                                           | a password replaced mid-flight produces no session; a factor enrolled mid-flight cannot be skipped                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| Demotion                                                           | `go test ./internal/api -run TestDemotionKeepsSecondFactorAndDropsAuthority`                                                     | a peer administrator cannot rewrite another administrator's roles; after demotion the existing session loses administration on its next request while the account keeps its second factor                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| Lost-factor removal (T-IDENT-01)                                   | `go test ./internal/api -run TestOperatorRemovesALostSecondFactor`                                                               | a member cannot remove a factor at all; an owner's factor is refused from the panel and named as a host-command job; a stale operator session is refused; the operator's own factor survives; the recovered account's sessions end, its password alone signs it in again, a retried removal reports that nothing was left, and the event is recorded against the acting operator                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| Offline owner recovery (T-IDENT-01)                                | `go test ./internal/db -run TestOfflineOwnerRecovery`                                                                            | three tests: recovery replaces the owner's password, removes its factor and three codes, ends exactly its two sessions and its push subscription, and leaves a second administrator's session, the member's history and every role assignment untouched; refusals for an administrator, a member, an unknown username, a missing username, a short password, an unbounded password and an installation with no owner, with no half-applied change and no audit row; and a deactivated, banned owner is unlocked with the audit row attributed to no account                                                                                                                                                                                                                                                                                                       |
| Offline actions read honestly in the audit list                    | `go test ./internal/api -run TestAuditLogNamesOfflineHostActions`                                                                | an actorless `owner.recovered_offline` row is presented as "host operator (offline)", so it is neither mistaken for the owner's own action nor confused with an account that was later erased                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| Account-security UI rehearsal                                      | `brrpbrrp serve` on a disposable data folder, driven in a browser                                                                | setup → Account panel → enrol authenticator (10 recovery codes) → session list narrows to the enrolling device → stale session shows "not recently confirmed" and the confirmation panel → confirm with a TOTP code → archive succeeds (9.7 KB ZIP); forcing a stale session again shows the refusal plus an "Open account security" link that lands on the Account tab                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| Lockout rehearsal                                                  | `brrpbrrp serve` on a disposable data folder, `brrpbrrp recover-owner` between restarts, and the same server driven in a browser | setup → the ownership ceremony is refused until the account enrolls a factor (D-10) → the owner removes dana's lost factor in the browser, dana's session ends, her password alone signs her in again and the panel confirms it → `recover-owner` refuses while the service is running (`data folder is in use`), refuses `dana` (`not the installation owner`), an unknown name and a short password → the real recovery reports the password replacement, 10 removed codes and every device → the old session is 401, the old passphrase is rejected, the new one signs in with no second-factor challenge, `is_owner`/`is_admin` and the data are intact, the audit list reads `owner recovered_offline — host operator (offline)`, and the first privileged action is refused until a new factor is enrolled, after which the archive succeeds (10,733 bytes) |

| Attachment durability at every boundary (WP-04, §6.3) | `go test ./internal/app -run TestPublish` and `go test ./internal/api -run 'TestAbortedUpload|TestUploadThatCannotPublish|TestReferenceWithout|TestOrphanFile'` | five publication tests: the final name is absent while the write runs, the temporary file sits in the destination directory, an existing file is never replaced, a failed write leaves the directory exactly as it was, and a path-like name is refused; four upload tests: an aborted body leaves no row and no file, an upload that cannot publish stores nothing (skipped only where directory permissions are not enforced), a row whose file was deleted answers 404 instead of serving something else, and the orphan sweep removes the unclaimed file while the claimed attachment and its bytes survive |
| Backup manifest, inventory and tamper detection (WP-23, §6.3) | `go test ./internal/backup -run 'TestArchive|TestTouched|TestManifest|TestLegacy'`, `go test ./internal/api -run TestBackupCopy`, `go test ./internal/app -run TestProductionCheck` | the manifest records the application and schema version and a correct size and digest for the database and the attachment; verification is stable and repeats; an archive rebuilt with one attachment byte changed — correct ZIP checksum, unchanged manifest — fails verification and is refused by restore, which creates no database; a manifest listing an entry the archive lacks, or omitting one it has, is rejected; a format 1 archive still verifies and restores with identical attachment bytes; a copy whose digest disagrees is refused and leaves no published file while a matching copy is byte-identical; and an archive or copy that fails verification fails `production-check` with the matching reason |
| Fresh-install recovery on a disposable destination (WP-04, WP-23) | `make rehearsal` (`node scripts/upgrade-recovery.mjs --report FILE`) | 23 checks, 0 failed (re-run with schema 6). Scope: same-build recovery on one machine, not an upgrade or rollback rehearsal. preflight on an empty folder reports schema 0 with six pending migrations and creates nothing; setup, a message and a 30-byte attachment are written through the API; the backup archive is produced and the schema is unchanged after it; `restore` into a second empty destination and `verify` report `Schema 6 verified: 1 users, 2 channels, 2 messages, 1 attachments`; the restored server signs the restored account in, serves the same channels, the message, and the attachment bytes with an identical SHA-256, and leaves no WAL side files at shutdown; `verify-backup` accepts the archive as `format 2, written by 0.4.0-alpha.4, schema 6, 1 attachment` and refuses the same archive after a single byte is flipped |
| Cross-version upgrade and rollback with a real archived release (WP-04, WP-23) | `make rollback-rehearsal` (`node scripts/upgrade-rollback.mjs --report FILE`), launched from the archived `0.4.0-alpha.3` archive for this platform | 53 checks, 0 failed (re-run with schema 6). Identity: the baseline archive's SHA-256, the executable's SHA-256, `0.4.0-alpha.3`/schema 2/format 1 against `0.4.0-alpha.4`/schema 6/format 2. Dataset written through the old executable's API and read back before the snapshot: 2 accounts, 2 channels, 3 messages with 1 reply, 1 pin, 1 invitation, 1 attachment (30 bytes, SHA-256 recorded) and an enrolled second factor. The old executable's archive verifies as `format 1` and the candidate's snapshot of the same un-migrated installation as `format 2, written by 0.4.0-alpha.4, schema 2` with the schema unchanged; the old executable refuses that archive with `unsupported backup format`, and refuses to serve the upgraded folder with `database schema 6 is newer than this binary supports`. After the upgrade `verify` reports `Schema 6 verified: 2 users, 2 channels, 4 messages, 1 attachments`, the reply still belongs to its parent, the pin is listed, search finds the pre-upgrade text, the attachment bytes are identical, and the owner signs in with the factor the old executable enrolled. After the rollback the clean folder is schema 2, signs the owner in with the same factor, serves the same message, pin, invitation and attachment bytes, and contains neither the message nor the ban applied after the snapshot. Interruption: with the upgrade-state marker re-injected and the search index emptied, `verify` refuses (`an upgrade to 6 has not finished`), and starting the server rebuilds the index (0 matches before, 4 after), searches the pre-upgrade content and clears the marker. Not covered: Windows/Docker lifecycles, off-host copies, RPO/RTO, and interruption raced against a live migration rather than injected as its durable state |
| Delivery ordering around an authorization change (R3-02) | `go test ./internal/api -run 'TestRevocationBoundary|TestAudienceRevision'`, `go test ./internal/ws -run TestHub` | the contract is pinned at its boundaries: a restricted event carries the authorization revision its audience was resolved at and is dropped when that revision is stale, an audience is resolved and written under one lock so a revocation cannot interleave between the two, a detached peer cannot resume across it, and a streamed download re-establishes the account's authority and session at admission so a revocation during the response ends it |
| Failed independent copies keep their own accounting (R3-03) | `go test ./internal/api -run TestBackupCopy`, `go test ./internal/app -run TestProductionCheck` | a copy that fails verification leaves the generation's archive in place, records the failure as a failed attempt with its own bounded retry rather than as a fresh archive, reports the last verified local generation and the last independently recoverable generation separately, protects the previous recoverable generation from count-based retention until a new copy succeeds, and makes `production-check` name the newest independently recoverable generation instead of dating a broken one |
| The declared recovery point is the snapshot boundary (R3-03) | `go test ./internal/backup -run TestSnapshotTime` | the time recorded in the manifest is the instant the database snapshot was taken, so the vacuum and compression that follow it cannot move the declared recovery point; with the timestamp taken after the vacuum the test fails |
| A committed revocation refuses a pending write (R5-01, T-AUTH-02) | `go test ./internal/api -run TestARevocationCommittingDuringAPendingWriteIsRefused` | the real socket writer is paused immediately before its admission decision, a revocation commits through the API while the voice gate delays enforcement, and the writer is released: the queued private payload is not written, the next event the connection receives is the invalidation, the permitted manager still receives the event queued for it, and a regrant delivers a new event on the same connection. The admission decision runs under an authorization fence that every authorization-changing commit holds around both the commit and its hub invalidation, so the decision cannot interleave with the commit. With the invalidation delayed the test reports that the revoked member's socket received the payload |
| Public voice presence requires `channel.read` (R5-02) | `go test ./internal/api -run TestPublicVoicePresenceStillRequiresChannelRead` | narrowing the default role denies an implicit holder the public voice room in its own `/state`; a permitted account then joins through the real SFU and the denied socket receives no `voice.state_updated`, while the manager keeps receiving it. Against the public broadcast the denied socket received the room and participant identifiers |
| Failed status persistence blocks retention (R5-03) | `go test ./internal/api -run TestAFailedStatusWriteBlocksRetentionOfTheDurableRecoveryPoint` | a database trigger rejects the status write, a second backup runs at retention one, and the job reports the persistence failure while the durable generation's local archive and its independent copy both survive. With the write error swallowed the attempt returned success and pruned the recorded recovery point |
| A missing copy source is a failed repair (R5-04) | `go test ./internal/api -run 'TestAMissingCopySource|TestANonRegularCopySource'` | a removed or non-regular recorded archive leaves a non-empty `CopyError`, a raised `CopyAttempts` and an outstanding copy, records `SourceMissing` and schedules a replacement snapshot on the short clock; a replacement snapshot then clears the state and records its independent copy. With the old no-op branch the missing source was treated as a successful repair |
| One acquisition order removes the lock cycle (R6-01) | `go test ./internal/api -run TestAStandaloneInvalidationNeverWaitsForTheDatabaseUnderTheFence` | a transaction holds the store's single connection and a standalone invalidation reaches its revision read: the authority fence is free there, the concurrent commit completes without cancellation and the invalidation finishes afterwards. The order is documented on `Server.authority` and in [ADR 0009](adr/0009-authority-commit-and-admission-ordering.md): connection first, fence second, and nothing waits for the connection under the fence. With the revision read moved back under the fence the test reports that the invalidation holds the fence while waiting for the connection |
| A committed credential change refuses a queued write, and registration is an admission (R6-02) | `go test ./internal/api -run 'TestACredentialCommitRefusesAQueuedWrite|TestASocketRegisteredAfterItsSessionEnded'` | an operator reset commits while the delivery registry is held (so the post-commit cleanup cannot run) and the real socket writer is paused immediately before admission: the old session's cookie already answers 401 and its queued private payload is still refused, while another member of the channel receives its event, the reset answers success and the replacement credential signs in. Separately, a connection whose session ended between the route guard and registration is closed instead of admitted, and a current session receives the ordinary hello. The per-transition commit helper and invalidation audience are inventoried in ADR 0009 |
| Combined copy and status failure preserves the durable recovery point (R6-03) | `go test ./internal/api -run 'TestACombinedCopyAndStatusFailure|TestAStatusInsertFailure|TestAFinalStatusWriteFailure'` | a rejected status UPDATE, a lost status row with a rejected INSERT, and a failed final update each leave the archive the durable record names on disk with its recorded digest, together with the generation this attempt verified; the combined case is re-checked through a reopened `db.Store`/`api.Server` and then converges on the recorded generation. Retention now depends on an explicit successful persistence result and protects the new, previous and independently recoverable local archives |
| Shared conversation mapping and deletion-safe send receipts (WP-09 first slice, D-11) | `go test ./internal/db -run TestUpgradeFromAlphaPreservesWorkspace`, `go test ./internal/api -run 'TestASendReceipt|TestAnOperationKeyIsBoundToItsIntent|TestAnExpiredReceipt|TestConcurrentRetriesOfOne|TestALegacyBackfilledReceipt|TestAChannelConversationIsCreated'` | schema 6 creates one conversation per channel (id = channel id, by migration and by a channel trigger), `conversation_members`, and `messages.conversation_id` backfilled and read through `COALESCE(conversation_id, channel_id)` while ids, rowids, replies, pins, reactions, attachments and FTS results stay unchanged; one acknowledged send maps to one receipt written in the same transaction, a replay returns the message, a retry after deletion or purge answers a non-recreating conflict (immediately) or an expired outcome after the window, a reused key with different content or another conversation is a conflict, and eight concurrent retries of one key create exactly one message. Pruning is bounded below the retry window. No DM, group or private surface exists; the contract is [ADR 0010](adr/0010-conversation-model-receipts-outbox-and-cursors.md) |
| Delegated roles, rendered privacy and ownership in the browser (WP-06, WP-07, WP-08) | `npm run test:e2e`, which builds `dist/brrpbrrp-e2e` and spawns the server itself; the journeys run in a declared project order against one installation | Eight journey cases across six projects, of which six projects pass and the voice journey stops at microphone acquisition on this host (the capture-environment stall recorded in review 5, reproduced in isolation with the release and voice projects alone). **Release**: first setup, persistent chat, a reply thread with an attachment and a retry, cross-tab edit/delete, the channel switcher, search, sign-out and offline reload. **Account-switch**: a channel move held across an in-place sign-in cannot reorder the next account, a `held-response-only` channel from the old account's response never appears, no position toast leaks, and the same account's move still reorders as the permitted control (this journey fails against the pre-fix default-session admission). **Authority**: an invitation-only account opens the panel and sees only _Invitations_, creates a link, loses `invite.manage` while the panel is open (the tab goes without a reload and the account is told it holds nothing here) and gets it back the same way; the failed-refresh case (second privacy journey) opens pins, a reply thread, search and an inline image preview on content the account itself sent, holds each panel's answer **after** the server admitted it so the answer really carries restricted content, takes `channel.read` away while the confirming refresh fails, and shows that nothing renders at the revocation or when the held answer lands, with the same account recovering by navigation. Three pre-fix mutations fail it: dropping `PinsPanel`'s answer guard, dropping `SearchPanel`'s authorization dependency, and dropping the thread clearing in `clearActiveConversation`; an audit-only account sees only _Maintenance_ with the audit list and none of the health grid, setup checks or backup controls; the owner still sees every tab and the audit log is displayed as always-on and disabled. **Privacy**: an ordinary member reads a channel, quotes a message, then the default role loses `channel.read` while a history response is deliberately held in flight — the message, the quoted reply and the channel entry are gone from the DOM, the held response lands and restores nothing, and restoring the grant lets the account read the conversation again by navigating, with no reload or re-sign-in. A third case proves the durable half of a send: with device persistence turned on through the preference, a send whose delivery is refused and never confirmed is rebuilt from the account's own device scope after a reload, what the person wrote is on screen again, and the retry submits the same operation key so exactly one message exists afterwards (removing the restore fails this journey). **Ownership**: the ceremony reports that no owner is designated, refuses a transfer until the account enrols a factor and offers the place to do so, completes both the first designation and a later transfer, removes the operator surface from the former owner without a reload (no ownership ceremony, no backup control), refuses that account's attempt to delete the new owner with the ownership message, records `owner designated` in the audit list, and shows the new owner the surface although no role grants it. **Voice**: two accounts join `Lounge` with a fake capture device and a real `RTCPeerConnection` per client, the speaker's inbound audio bytes keep arriving, the speaker's role loses `voice.speak` with neither client touched, the inbound bytes stop and `voice.access_revoked` arrives with its reason, and restoring the capability lets them resume; a non-cooperative sender control then shows the server refusing the withdrawn speaker's own outbound media while its socket stays open, so the stop is enforcement rather than the two clients politely agreeing |
| Roles, categories and restricted channels in the browser (WP-07) | `brrpbrrp serve` on a disposable data folder with `BRRP_ENABLE_RESTRICTED_CHANNELS=true`, driven in a browser | setup as the first administrator → **Channels** → create the durable category _Operations_ and the channel _briefing_ inside it (the sidebar regroups under OPERATIONS, and the category's channel count follows) → **Who can access this?** reports "Public — every member can read it", "Everyone who can read it may post", "1 accounts" and the standing disclosure that community managers can read every community channel → set visibility to Private and grant the Moderator role → save, and the row reads `Operations · Text channel · Private` while the sidebar shows a shield on `briefing` → **People** offers only promising roles (the _Administrator_ option is absent because the administrator cannot act on a role at its own position; checking _Moderator_ and creating the link shows `Expires … · Moderator` on the pending entry) → an invitation with no roles accepted in a separate browser context signs that account in as a plain member, whose workspace contains only `general` and `Lounge`, while history, first-unread and channel-scoped search on the real `briefing` id all answer a non-disclosing 404 and the access inspector answers 403 → the invitation that promised _Moderator_ signed that account in holding Member + Moderator, and its workspace contained `briefing` → **Roles** lists Administrator/Moderator/Member with member counts, disables the three operator capabilities for a non-owner administrator, and created the custom role _Announcer_ (position 20, 8 capabilities) → assigning it to a member in the People panel took effect without a reload in that member's open tab: `briefing` appeared in its sidebar with the private flag, and revoking it removed the channel again, moved the tab back to `general` and left none of the private message text on the page |

The voice evidence above is socket- and SFU-level: a real peer in the SFU, real
WebSocket clients, and a bounded read that proves silence. A rehearsal with a real
microphone, a second device and a forced relay is the media qualification the plan
assigns to WP-24/WP-33 and is not claimed here.

The privacy test (`internal/api/privacy_test.go`) is the T-AUTH-01 scenario with a
revocation step: with restricted channels enabled, an authenticated outsider that
knows the real channel, message and attachment identifiers receives a
non-disclosing 404 from history, pins, first-unread, single message, context,
replies, read cursor, send, reaction and file download; receives no search hit from
either global or channel-scoped search; sees no channel title or unread count in
workspace state; and is refused the access inspector with 403. After the grant is
removed, the previously authorized member receives 404 from the same surfaces, the
search no longer returns the message, and the authorization revision has advanced.

## Decisions taken with defaults pending ratification

Recorded as ADRs so they can be reviewed rather than discovered:

1. [Roles, ownership and the operator tier](adr/0001-roles-ownership-and-operator-tier.md)
2. [Schema migration, preflight verification and durability](adr/0002-schema-migration-and-durability.md)
3. [Restricted-channel enforcement ships disabled](adr/0003-restricted-channel-exposure.md)
4. [Sessions, recent authentication and privileged enrollment](adr/0004-sessions-recent-authentication-and-privileged-enrollment.md)
5. [Credential lockout recovery](adr/0005-credential-lockout-recovery.md)
6. [Licensing, copyright and contribution terms](adr/0006-licensing-and-contribution-terms.md)
7. [Attachment durability and publication order](adr/0007-attachment-durability.md)
8. [Backup manifest, inventory and verification](adr/0008-backup-manifest-and-verification.md)

Three defaults need an explicit owner decision: `BRRP_SQLITE_SYNCHRONOUS=FULL` as
the production baseline (D-16), `BRRP_ENABLE_RESTRICTED_CHANNELS=false` as the
exposure gate (D-06/WP-08), and `RequirePrivilegedMFA=true` as the enrollment
requirement (D-10). A fourth was added with the sign-in-budget finding and is
recorded as D-17 in the remediation ledger: `BRRP_LOGIN_ATTEMPTS` now exists (default
20 per address per five minutes, raising it is supported and documented), but whether
that default suits a community server and whether the throttle should also key on the
account instead of counting successful sign-ins against one address budget are open.
`RequirePrivilegedMFA=true` remains the enrollment policy (D-10) — with it on, an
owner or administrator who has not enrolled a factor cannot archive the server, take
over an account or move ownership, and
with it off the 15-minute recent-authentication gate is the only barrier on those
actions. One product disclosure needs owner review: community channel managers
can read every community channel, including restricted ones, and the application
says so.

ADR 0006 needs four owner confirmations rather than a decision: the exact spelling
of the copyright holder in `LICENSE` (written as _Cameron Amer_ from this
workspace), whether the manual CLA signing process stays or a signing service
replaces it, the response targets stated in [SECURITY.md](../SECURITY.md), and one
review pass over the agreement text by whoever advises the owner on licensing. The
security intake itself is not live and cannot be until the repository exists.

The 25 September 2026 sixth and seventh reviews added findings R6-01–R6-04 and the P6 plan;
they are recorded in §9 of the same ledger. All three reproduced code failures now have
implementation fixes and permanent regressions observed failing against the old behavior:
the authorization/database lock cycle is replaced by one documented acquisition order
([ADR 0009](adr/0009-authority-commit-and-admission-ordering.md)), every credential and
session transition commits together with invalidation of affected deliveries
(including socket registration, which is now an admission), and backup retention depends on an
explicit persistence result so a combined copy-and-status failure cannot prune the archive the
durable ledger still names. The WP-09 design contradiction is reconciled in
[ADR 0010](adr/0010-conversation-model-receipts-outbox-and-cursors.md), and the server half of
its first slice is implemented as schema 6. The rendered failed-refresh case the reviews
listed as missing now exists in the privacy journey (pins, an open thread, search and an inline
image preview, each holding an answer the server had already admitted, with the confirming
refresh failing), and the address-wide sign-in budget it exposed is now the operator setting
`BRRP_LOGIN_ATTEMPTS` rather than a fixed 20 per address. What the reviews still list as open is
the independent review, D-08, the voice and admin panels of the rendered failed-refresh case,
and every host, platform, media and capacity qualification.

## Next actions in dependency order

1. ~~Repair R8-01 attachment-claim authorization and R8-02–03 client lifecycle defects~~ Done in the P8 tranche with failing-first regressions and permitted controls; receipts, account isolation and the default-off gates are preserved (see the review-8 section above).
2. ~~Require both browser feature modes, maintained recovery and extracted-consumer checks before CI package promotion; demonstrate private-state recovery/reconciliation; obtain the final uncached race run and clean-source artifact identities~~ Done locally and recorded in the P8 checkpoint. Still absent: an actual hosted-CI run and an approved archive host for the rollback gate's pinned baselines (the gate fails loudly until that hosting exists).
3. Complete selected-evidence private reports, request/activity discovery and DM notification policy; ratify private lifecycle/contact defaults. Then deliver consented groups and account export on the shared conversation pipeline. Do not repeat the already implemented schema 11 rebuild or canonical DM work.
4. Expand organized threads, notification controls, accessibility/mobile journeys and moderation around those shared services. Preserve recoverable text, monotonic cursors and selected-evidence privacy.
5. Start the measured media architecture spike in parallel, then implement private calls and desktop sharing and qualify physical devices/networks/load on the support matrix. Synthetic RTP remains transport evidence only.
6. Review the authority/session/delivery and private-data boundaries independently. In parallel, obtain both Windows/Tailscale and Linux Docker hosts; rehearse install/update/reboot/lockout/rollback and independent restore with exact candidate artifacts, including private state. Measure RPO/RTO, capacity and soak. This workspace has no Docker engine or connected Windows host.
7. Establish the canonical public repository, maintainer/security contacts, hosted CI, immutable artifact identity, signing and release process. Complete the prescribed independent reviews, 14-day beta and seven-day final-candidate observation before proposing v1.0.0 publication.

No unpublished prerelease is a substitute for those acceptance results. The current source is a locally tested alpha.7 candidate whose review-11 findings are repaired; alpha.6 remains the prior packaged checkpoint, and no release gate has passed.


**V14-01, private audit read under the community audit capability.** The restricted
audit feed names the reporter, the reviewer, the evidence id and the time. It was
reachable with ordinary `audit.read`, so an account refused `/api/admin/private-reports`
with 403 received 200 from the audit feed with the reporter's username in plain text —
the exact disclosure the evidence policy exists to prevent.

Reproduced first, through real report and review mutations, and the reproduction named
the leaked value: `request_recipient`, `admin`, and both evidence ids. The feed is now
gated on `report.review_private` in both the route guard and the in-transaction check,
so a grant revoked between the two is still refused. A refusal body is asserted not to
describe the event it refuses.

Three tests had encoded the widened policy, including one that asserted an *evidence
reviewer* must be refused. Those asserted the defect, so they were corrected rather than
satisfied: the evidence capability is now the granting one, and every case checks both
directions, because a policy only ever tested in the denying direction is a policy
nobody has confirmed still works. ADR 0013 records the decision and its reasoning.


**V14-03, timeouts.** Four defects, all reproduced before being fixed.

*The route the interface calls did not exist.* The server registered
`/api/admin/accounts/{id}/timeout` and the interface called
`/api/admin/users/{id}/timeout` — a 405 for every timeout a moderator applied. The
`accounts/` path was the only one in the family; it is now `users/`, like every other
account route.

*500 after committing.* `commitAuthorityChange` commits. Both handlers committed again,
so a moderator saw "the server couldn't complete that request" for a timeout that was
in force and audited — and a retry applied a second authority change on top of the
first. The lift had the same double commit and the same 500 for work already done.

*Two targets.* `applyTimeout` read its account from the request body while `liftTimeout`
read it from the path, so the two halves of the feature disagreed about which account
they were acting on. The route is authoritative now, and a body naming a different
account is refused with both accounts asserted untouched.

*The revision did not advance.* The authority commit protocol reads the revision a
change produces; it does not create one. A timeout reached it without bumping first, so
the revision stayed at 1 → 1: the row committed, the audit was written, and work already
queued for the account was still admissible. Applying and lifting both advance it.

*Expiry replayed a role snapshot, and the ADR said to.* A role revoked through
`/api/admin/users/{id}/roles` while a timeout ran came back when the timeout expired —
reproduced, and the existing test asserted the outcome and called the cost "real". A
timeout does not touch role assignments, so expiry restores nothing; the recorded value
is still written for an operator reading a row, and nothing replays it. ADR 0018
corrected.

*Community scope.* A timeout was enforced on every route, so a community moderator's
timeout denied private conversations, private activity and private calls. `BeyondCommunity`
now marks the surfaces a timeout does not close, defaulting closed so a new route is
suspended until somebody says which kind it is. A timeout also does not mark the account
banned or deactivated and does not end its sessions.

The operator-facing copy claimed the account "regains exactly the access it had when
the timeout started" — true of the defect, false of the fix. It now describes what a
timeout does and does not reach, and the duration is a set of named choices rather than
a number box, which had no input bound to it at all.


**V14-02, export delivery after access withdrawal.** An export leaves the building as a
file, and the server keeps one for seven days — so during that week it is the one
delivering a copy of somebody's history, and the delivery path is a privacy surface.

The download checked that the account owned the *file*, was in a good state, and that
the copy had not expired, then served it. It never asked whether the account could
still read the conversations inside. Ownership of a file is not ownership of its
conversations, and the two came apart when a membership ended.

Reproduced through the real build and the real delivery: a member's group export, then
ownership transferred and the member removed through the supported routes. Group history
returned **404**; the download returned **200 with the removed group's text in the
body**.

Schema 22 records the scope: `data_exports.scope` holds the conversation ids the build
actually wrote, taken from the document rather than from the set it selected, so a
conversation that failed to read is not in the scope of a file that never contained it.
Delivery re-resolves the account's authority in a transaction and checks each
conversation with the *current* access rules.

There is no partial delivery. The file is one document, and a member handed half of one
has been handed content they cannot read by any other route with no marker saying which
half. The refusal says why and points at a new export. A row written before the column
existed is refused rather than served: "we cannot tell what this contains" is not a
reason to hand it over.

The build is gated too. The route's feature check covered the request; the build ran
after the response and selected private conversations by querying the conversation table
directly, so an export on a server with private conversations disabled contained them.
Confirmed by a test that fails with the gate removed.

Unchanged by this: a copy a member downloaded legitimately before a withdrawal is theirs
and cannot be recalled. This is the server's own undelivered copy. Still outstanding
from the same finding: finite ready-export storage per account and server, bounded job
admission, crash/restart reconciliation and orphan-file cleanup after erasure.


**V14-02, the storage half.** Three obligations follow from the server holding a copy,
and none of them were met.

*A lifetime is not a count.* Seven days says how long a copy may exist, not how many.
Finished copies are now bounded at three per account and fifty on the server, and the
oldest are reclaimed rather than the request refused — a member asking for a new export
and being told to delete something first is a worse answer than losing a copy they can
rebuild at no cost. The count is taken before the new build, so the bound accounts for
the copy being created.

*Erasing an account left its export file on disk.* The row cascades with the account,
so any cleanup written afterwards has nothing to name: a file with no record, which no
sweep working from the table can ever reach. The ids are now collected inside the
erasure transaction and the files removed after the commit, so a rollback leaves a file
rather than a row pointing at nothing. An export is the account's own content and
nobody else's.

*Nothing swept an orphaned export file.* A crash between reclaiming a row and unlinking
its file left a copy of somebody's history permanently. Orphans are now removed on the
same grace period as an unclaimed upload, because every other maintenance pass works
from the table and a file with no row is unreachable by construction. A file younger
than the grace is left alone: it may belong to a build whose row committed moments ago.

The clamp that limits how much of one account's excess the server's own slack absorbs
was written backwards at first and silently reclaimed nothing; the tests caught it
because they measure the bound rather than the mechanism.


**V14-07, private call access.** A private conversation's read admission is deliberately
*retained* history — a blocked member can still read what was said before the block,
because erasing the past on request is not what a block is. That decision was being
reused as the admission rule for a call, and produced two defects.

*A blocked pair could open a call.* The message POST returned 403 and the call was
admitted. A block is the user saying "do not contact me", and a call is the most
immediate contact there is. A call now asks the block question in both directions —
the caller's own block list, and the block lists of the other accepted members, because
a block is recorded against the blocker and a conversation has two sides.

*Revalidation evicted every authorized private participant.* The enforcement pass that
runs after any authority change looked the room up as a community channel
unconditionally. A direct conversation has no channel row, the lookup failed, and every
member of every private call was dropped — by a moderator renaming a channel, or a role
being granted three rooms away. Reproduced: two participants in a live direct call, one
unrelated change, both gone.

Both were one mistake: the call path and the message path asked different questions
about the same relationship, and the enforcement pass asked a third. The pass now
resolves the room through the same function that admits a join, so the two cannot
disagree. Because that function asks about blocks, a block applied while two people are
already talking ends the call too — admission and revalidation are the same question
asked at two moments, and answering it at only the first means "the block works" holds
only for people who have not already joined.

The `voice.connect` capability check stayed, and a pre-existing test caught its removal
during this work. The resolver answers "may this account be in this room", not "may this
account make calls at all", and those are different questions.


**V14-10, the schema-13 rehearsal had no baseline to run against.** The CI step
unzipped `dist/releases/brrpbrrp-0.3.0-linux-amd64.zip` from the workspace. Two
problems, and the second is the serious one:

- `/dist/` is in `.gitignore`, so that archive is never in the repository. The step
  depends on a file that exists only if somebody ran a release build first.
- **0.3.0 is not schema 13.** Verified by running the archived Darwin 0.3.0 binary
  against an empty data directory: it reports `BrrpBrrp 0.3.0` and writes a database
  whose `schema_migrations` maximum is **2**, with no `conversations` and no
  `private_message_reports` tables. It predates private conversations entirely, so it
  cannot seed a rehearsal whose whole subject is a consenting direct conversation and a
  pending request. The rehearsal would have failed its own checks — or, worse, passed
  against a fixture that was never what it claimed to be.

Checked every archived release this repository still holds, by running each one and
reading the schema it actually writes:

| Archive | Schema it writes |
| --- | --- |
| 0.3.0, 0.3.1 | 2 |
| 0.4.0-alpha.3 | 2 |
| 0.4.0-alpha.5 | 10 |
| 0.4.0-alpha.6 | 12 |
| 0.4.0-alpha.7 | 21 |

**No archived release in this repository writes schema 13.** The rehearsal therefore
has no baseline here, and the CI step is left failing rather than being made to pass by
pointing it at a different archive: a rehearsal that runs against the wrong bytes
qualifies nothing, and a rehearsal that silently skips is the exact failure this gate
exists to prevent.

**What is needed:** the schema-13 release's own archive, added to
`scripts/baseline-releases.json` with its real SHA-256 and made available through
`BRRP_BASELINE_RELEASE_DIR` or `BRRP_BASELINE_RELEASE_URL`, the way the alpha.5 and
alpha.3 baselines already are. The acquisition path and its digest verification already
exist and are exercised by the self-test; what is missing is the bytes.

## Message shape is content

Line breaks, blank lines and indentation in a message are what the author wrote, and
they are preserved. The tokenizer used to drop every whitespace-only text run and then
join the text either side of a line break with no separator, so `first` and `second` on
consecutive lines tokenized to a single run reading `firstsecond`. A pasted log, a
list, a stanza or a table — every reason somebody types more than one line — rendered
as one run-on line.

It was invisible in the tests because they checked token *kinds* and mentions, and a
concatenation has exactly the same kinds as the text it came from. The line break is now
emitted as its own text token so the merge cannot fold a mention or a link into it, and
whitespace is only dropped when it is the empty string. A message holding nothing but
spaces renders those spaces rather than nothing; an empty message still produces no
tokens at all.

`release.spec.mjs` asserts the rendered text of a shaped message through the real
composer, because the tokenizer could be right and the renderer still fold it — the
element is `pre-wrap`, so the break has to survive all the way to a line box.

## Leaving the Activity panel must not disable it for the session

The activity controller is a module-level singleton, so closing the panel and opening
it again mounts a new panel over the same object. Teardown set `alive` false to discard
a response arriving after the panel is gone — correct as a statement about work *in
flight*, and wrong as a statement about the controller's existence. `alive` gates
`admitted`, `admitted` gates every fetch, and nothing set it back. **The second time a
member opened Activity in a session it rendered an empty panel with no error and no way
to retry**, which from the outside is indistinguishable from a server that returned
nothing.

Teardown and mount are now a pair. The account, authority-generation and version fences
are untouched by a mount — they are what keep one account's private activity metadata out
of the next account's panel, and they now have tests saying so across any number of
mounts. A remount revives admission for *new* work; the response of a request made
before the teardown is still refused, because teardown advanced the controller's version
and that fence does not come back.

The admission question moved into `web/src/lib/admission.ts`. It had to: the controller
is a Svelte module whose imports are extensionless, so node cannot import it and the
existing contract tests could only read its *source* — which is how a permanently dead
controller passed a test suite. The one decision that matters is now directly testable,
and it returns a strict boolean rather than whatever the first falsy operand happened to
be, so a caller and a logger cannot disagree about a state the types say is impossible.

The fix needed a second half, which the unit test could not have told me. `mount()`
revives the controller, but the panel's `$effect` re-runs only when something it *read*
changes — and closing and reopening the panel creates a new effect without changing the
account or the authority. So the reopened panel had a live controller and still asked
for nothing. `activity.revision` is a reactive counter advanced by `mount`, and the
panel's effect reads it, so reopening fetches. It is reactive deliberately; `alive` is
not, because making it reactive would re-run every panel effect that admits work on
every teardown.

The rendered journey proves it by counting `/me/activity` requests rather than rows: an
empty feed and a feed that was never fetched produce the same panel, so rows cannot tell
them apart, but a request that never left the browser can. Removing the `mount()` call
leaves exactly one fetch across two opens.



## The schema-13 rehearsal has no baseline here, and the guard proves it

Re-run at schema 24 against a seed made with the current binary, the rehearsal reports
four failures, and all four are the same fact:

```
FAIL  the seed has 13 applied migrations — highest is 24
FAIL  the seed is still the schema-13 snapshot — highest is 24
FAIL  the request table has no attempt identity yet — ...,attempt_id
FAIL  the seed really holds private evidence — 0 row(s)
```

That is the gate **working**. The rehearsal runs the real prior executable and seeds
through its supported API, so passing the current binary where a schema-13 release
belongs produces a fixture that never was what it claimed — and the script says so in
four different ways rather than rehearsing a migration that did not happen.

**What is needed, and is not here.** The schema-13 release's own archive, with its real
SHA-256 in `scripts/baseline-releases.json`, served through `BRRP_BASELINE_RELEASE_URL` or
placed in `BRRP_BASELINE_RELEASE_DIR`. No archived release in this repository writes
schema 13 — the table above records what each one writes, verified by running them. The
CI step therefore fails, which is the intended outcome: a rehearsal that runs against the
wrong bytes qualifies nothing, and one that silently skips is the exact failure this gate
exists to prevent.

This is the one §5 item that cannot be completed locally. It is not blocked on analysis
or on a missing test; it is blocked on an archive that has to be produced and pinned.
