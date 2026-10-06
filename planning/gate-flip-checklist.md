# The two feature gates: everything that assumes they are off

**What this is.** `BRRP_ENABLE_RESTRICTED_CHANNELS` and `BRRP_ENABLE_PRIVATE_CONVERSATIONS`
ship **off** and the owner flips them **on** before the beta, after the independent reviews
and their P1 findings. Both features are implemented and locally tested; what the gates
withhold is an unreviewed privacy surface.

**What this is not.** It is not the flip. Nothing in this document has been applied. It is
the list of every place that would be wrong or misleading once the gates are on, with the
exact edit for each, so the flip is one reviewed commit instead of a hunt. Read it with
[the release execution plan](v1-release-execution-plan-2026-10-03.md) and
[the verification queue](verification-queue.md); the browser half of the restricted-channel
work is **W-07**, which this document depends on and does not replace.

**The flip itself is small.** Neither key has a constant to change:

- `internal/app/config.go:89` and `:92` call `boolean("BRRP_ENABLE_RESTRICTED_CHANNELS")`
  and `boolean("BRRP_ENABLE_PRIVATE_CONVERSATIONS")`. `boolean` returns `false` when the
  variable is unset, so "on by default" cannot be expressed by editing these two lines —
  it is expressed by every surface that *sets* a value, which is what the tables below are.
- The gates are read where it matters, not once: `internal/api/roles.go:19`
  (`restrictedChannelsEnabled`, consulted by `requireRestrictedChannels`),
  `internal/api/direct.go:39` (`requirePrivateConversations`), plus direct checks in
  `internal/api/state.go:27,158,181`, `internal/api/conversation_access.go:76,209`,
  `internal/api/export.go:528,913`, `internal/api/voice_access.go:122,315,430` and the
  private handlers. **None of these need an edit**; they are the mechanism, and they are
  listed here so the flip's effect can be traced rather than assumed.

---

## 1. The editors that decide a deployment's value

| File | Current | Becomes |
| --- | --- | --- |
| `compose.yaml:32` | `BRRP_ENABLE_PRIVATE_CONVERSATIONS: ${BRRP_ENABLE_PRIVATE_CONVERSATIONS:-false}` | `BRRP_ENABLE_PRIVATE_CONVERSATIONS: ${BRRP_ENABLE_PRIVATE_CONVERSATIONS:-true}` — plus the missing `BRRP_ENABLE_RESTRICTED_CHANNELS: ${BRRP_ENABLE_RESTRICTED_CHANNELS:-true}` line, which the container file does not pass at all today (W-24 adds the plumbing; this is the default it gets once the gates are on). |
| `.env.example` | `BRRP_ENABLE_RESTRICTED_CHANNELS=false` under a comment ending "...a restricted *voice* channel must not be used until that work lands."; and `BRRP_ENABLE_PRIVATE_CONVERSATIONS=false` under "Experimental consented direct messages; keep disabled pending privacy acceptance." | `=true` in both cases, and both comment blocks rewritten to describe an operator's **opt-out** rather than a pending gate. The restricted-channel comment's warning about restricted voice is a separate, still-open limit (ADR 0003) and stays, reworded so it does not read as the reason the gate exists. |
| `.env.docker.example` | `# Experimental consented direct messages; test installations only.` / `BRRP_ENABLE_PRIVATE_CONVERSATIONS=false` | The comment becomes a normal description of consented direct messages and groups, and the line becomes `=true`. Add the restricted-channels key beside it, which this file does not mention today. |
| `internal/installer/settings.go` (`configText()`, the `settings` literal near line 150) | The list written into `server.env` by the native setup wizard: `BRRP_DATA_DIR`, `BRRP_ADDR`, `BRRP_BASE_URL`, `BRRP_LOG_LEVEL`, `BRRP_LOG_FILE`, `BRRP_VOICE_PORT_MIN`, `BRRP_VOICE_PORT_MAX`, `BRRP_VOICE_EXTERNAL_IP`, and `BRRP_TLS_MODE` (plus `BRRP_BEHIND_PROXY`/`BRRP_TRUSTED_PROXIES` in proxy mode). Neither gate is written. | **No edit, and that is the point.** A native install inherits the shipped default, so the flip reaches it without a wizard change; writing the key explicitly would pin the installation to today's value and make a later change impossible without editing every installation's file. Recorded because this is the one place that decides a native install's value, and the "leave it out" decision should be deliberate rather than accidental. |

## 2. Guides and product documentation

| File | Current | Becomes |
| --- | --- | --- |
| `README.md:5` | "...restricted channels and announcement posting stay switched off at the API until the privacy gate passes." | Drop the clause: the features are on, and the sentence above it already lists them as included. |
| `README.md:131-133` | "Direct conversations and groups are behind the default-off `BRRP_ENABLE_PRIVATE_CONVERSATIONS` gate, and restricted channels behind `BRRP_ENABLE_RESTRICTED_CHANNELS`, until their independent reviews are done; every feature they gate is implemented and locally tested." | State that both features are **on by default** from this release and that an operator may switch either off; keep the sentence that the independent review is what opened them, because that is the provenance of the decision. |
| `docs/roadmap.md:9` | "Consented private conversations ship behind a default-off gate until their independent review is done." | "Consented private conversations ship on by default; an operator can switch them off." |
| `docs/roadmap.md:15` | "Two feature gates ship **off**: `BRRP_ENABLE_PRIVATE_CONVERSATIONS` and `BRRP_ENABLE_RESTRICTED_CHANNELS`. Everything they gate is implemented and locally tested; what is missing is the independent review and the deployment qualification named in the delivery table, not the feature." | "Two feature switches ship **on**: ... Everything they gate is implemented; the independent review that opened them is recorded in `<release record>`, and the deployment qualification named in the delivery table is still outstanding." — do not let this sentence read as qualification. |
| `docs/roadmap.md:24` | "...ships behind `BRRP_ENABLE_RESTRICTED_CHANNELS` until the independent privacy review runs" | "...ships on by default; the independent privacy review that admitted it is recorded in `<release record>`". |
| `docs/roadmap.md:25` | "...ships behind `BRRP_ENABLE_PRIVATE_CONVERSATIONS`, never qualified on either required deployment" | "...ships on by default, never qualified on either required deployment" — keep "never qualified", which the flip does not change. |
| `docs/roadmap.md:35` | "Private conversations and restricted channels are implemented but ship switched off until the gates above open." | "Private conversations and restricted channels are on by default; an operator can switch either off." |
| `docs/admin.md:67` | "Private channels and announcement posting are refused by the API, not merely hidden, until the operator sets `BRRP_ENABLE_RESTRICTED_CHANNELS=true`. That switch is deliberately off: the enforcement is implemented and tested, but the independent privacy review (WP-28) has not run, so restricted channels are unreviewed until the owner decides to open the gate." | "Private channels and announcement posting are refused by the API, not merely hidden, when an operator has switched restricted channels off with `BRRP_ENABLE_RESTRICTED_CHANNELS=false`. The switch is on by default; it was opened after the independent privacy review (WP-28), whose findings are recorded in `<release record>`." |
| `docs/community-guide.md:3` | "These controls are included in **0.4.0-alpha.5**. They apply to community text channels; direct and group messaging are still planned." | The version string and the "still planned" clause both need the flip's release, but whether this living guide is re-versioned per release is a question — see §5. |

## 3. The interface's disabled-state copy

| File | Current | Becomes |
| --- | --- | --- |
| `web/src/lib/components/MessagesInbox.svelte:411` | `Direct conversations are not enabled on this server.` | `Direct conversations are switched off on this server.` — with the flip, the only way to reach this line is an operator's choice, and "not enabled" reads as "not finished yet". |
| `web/src/lib/components/admin/AdminPanel.svelte:43` | `(entry.gated !== true || workspace.privateConversationsEnabled)` | **No edit.** The gated tab follows the flag, which is exactly right in both directions. Listed because it is the gate's one UI consequence in the admin surface. |
| `web/src/lib/components/Workspace.svelte:143,167,459` | The Messages entry, the DIRECT MESSAGES group and the inbox are all behind `workspace.privateConversationsEnabled`. | **No edit.** Same reason as above: the surfaces appear when the flag is on. |
| `web/src/lib/components/admin/Channels.svelte:423-432` | The private-voice notice ends: "The voice redesign (private-call admission, browser and network qualification) and the independent privacy review are still pending, so treat a restricted voice channel as unreviewed." | **Conditional edit, and the condition matters.** The "independent privacy review ... still pending" clause must be dropped only when the review has actually reported; the rest of the sentence (the voice redesign, browser and network qualification) is a separate open limit and stays. Do not let the flip commit assert a review that has not happened — see §5. |

## 4. The browser harness and CI

| File | Current | Becomes |
| --- | --- | --- |
| `web/e2e/run.mjs:94-95` | `BRRP_ENABLE_PRIVATE_CONVERSATIONS:` / `process.env.BRRP_E2E_PRIVATE_CONVERSATIONS === '1' ? 'true' : 'false'`, under the comment "Private conversations stay disabled in the default suite. The focused DM journey opts this temporary server in explicitly; it never changes shipped defaults." | The harness must be able to run the **configuration that ships**, which after the flip is both gates on. Either the default becomes `'true'` with the variable selecting the opted-*out* mode, or the variable stays and every job says which mode it is. This changes what the job named "default feature set" means, so the two must move together — see §5. `BRRP_ENABLE_RESTRICTED_CHANNELS` is **not passed at all** today, which is **W-07**'s work. |
| `web/playwright.config.mjs` (`BRRP_E2E_PRIVATE_CONVERSATIONS === '1'` gating `directs` and `timeout`) | The `directs` and `timeout` projects exist only when the variable is `'1'`. | Follows `run.mjs`'s decision above. If the default flips, these two projects run in the default job and the opted-in job must still be distinguishable in the log. |
| `.github/workflows/check.yml:203-213` | The step `Browser journeys (private conversations enabled)` sets `BRRP_E2E_PRIVATE_CONVERSATIONS: '1'` and greps the log for `[directs]` to prove the project ran. | Keep the assertion — it is what proves the opted-in project executed — but re-point it at whichever job now carries the non-default mode. If both gates are on by default, the step to assert against is the *opted-out* one, and the grep must name the project it is asserting about. |
| `.github/workflows/check.yml` (the restricted-channel and WebKit/Firefox steps) | No restricted-channels mode anywhere. | Nothing to flip here until **W-07** adds the mode; the flip commit must not claim a restricted-channel browser pass that has never run. |

## 5. What I could not settle

These are recorded rather than guessed. Each is a question for the owner or the flip's
author, and none of them is an edit I could support by reading.

1. **Does the harness follow the product default, or keep its own explicit value?**
   `run.mjs` currently forces `false` unless asked, which makes "the default browser job" a
   *pre-flip* configuration. Making the harness follow the shipped default is the only way
   the job tests what a new installation gets; keeping it explicit is the only way the
   opted-in/opted-out pair stays symmetrical and cheap in CI. The answer decides two rows in
   §4 and the `check.yml` assertion. I did not choose, because both are defensible and the
   choice changes what existing CI jobs mean.
2. **Is `docs/community-guide.md` a living guide or a dated record?** Its first line pins it
   to `0.4.0-alpha.5` and says direct messages "are still planned". Every other file under
   `docs/` that is dated (`docs/alpha-testing.md`, `docs/releases/0.4.0-alpha.*.md`) is a
   record I would leave untouched, and I have left them out of the tables above for that
   reason. If the community guide is living, it needs an edit beyond the flip; if it is a
   record, it does not. I could not tell from the tree which it is meant to be.
3. **May the flip commit say the privacy review is complete?** Two places state that it has
   not run: `docs/admin.md:67` and the private-voice notice in
   `web/src/lib/components/admin/Channels.svelte`. The flip's stated precondition is that
   the reviews have been entered and their P1 findings fixed, so the edits above assume that
   is true at flip time. If it is not, the flip must not happen, because these two rows
   would then be the product asserting a review that does not exist. Stated here so the
   author checks rather than assumes.
4. **The refusal messages.** `internal/api/roles.go:25` reads "Restricted channels are
   disabled on this server until the privacy checks are complete. Set
   BRRP_ENABLE_RESTRICTED_CHANNELS=true to enable them." and `internal/api/direct.go:42`
   reads "Private conversations are not enabled on this server." After the flip both are
   reachable only when an operator opted out, so "until the privacy checks are complete"
   becomes false and "not enabled" becomes ambiguous. I have not proposed replacement text:
   the two messages are product copy, they appear in the API rather than in a document, and
   the wording is a decision rather than a correction.

## 6. An installation that set a gate to `false` keeps it off

Both keys are read from the environment (or `server.env`, which is environment
configuration — `internal/app/config.go` reads them through `os.Getenv`). An operator who
wrote `BRRP_ENABLE_RESTRICTED_CHANNELS=false` or `BRRP_ENABLE_PRIVATE_CONVERSATIONS=false`
into `server.env`, a service unit, or `.env.docker` **stays off after the upgrade**, because
an explicitly set variable wins over any default the release ships. That is correct
behaviour and it must be said out loud, because "the gates are now on by default" is false
for those installations and a member who cannot see a private channel deserves to know why.

The upgrade note must therefore say, in the release notes and in the upgrade guide:

> This release turns both conversation features on for installations that did not choose a
> value. If your `server.env` or Docker environment sets `BRRP_ENABLE_RESTRICTED_CHANNELS`
> or `BRRP_ENABLE_PRIVATE_CONVERSATIONS` to `false`, your installation keeps that setting;
> remove the line and restart the service to follow the shipped default.

Written into `docs/upgrading.md` when that document exists, into the upgrade note of the
release that flips them, and into the release notes draft.

## 7. Files deliberately left alone

| File | Why |
| --- | --- |
| `docs/adr/0003-restricted-channel-exposure.md:3` ("Status: Implemented; the gate stays closed until the listed gaps are closed") and its "What is not enforced yet" section | An ADR is a dated decision record. The flip should add a **new** ADR entry (or a dated note appended to this one) recording that the gate was opened and on what evidence, rather than rewriting the record that said it was closed. |
| `docs/adr/0012-private-conversations-consent-and-shared-storage.md:33` | Same reason: it says the feature "defaults to false", which was true when written. Append a dated note. |
| `docs/adr/0013-private-message-evidence-and-request-discovery.md:126` and `docs/adr/0015-durable-activity-and-notification-destinations.md:79` | Both describe the "feature off" branch, which remains a real configuration after the flip. No edit. |
| `docs/alpha-testing.md`, `docs/releases/0.4.0-alpha.6.md`, `-alpha.7.md`, `-alpha.8.md`, `-alpha.9.md` | Dated release records. They say the gates were off, which was true. The flip gets its own record. |
| `docs/v1-progress.md`, the dated files under `planning/`, and the handoff prompts under `planning/` | Historical records and superseded instructions. Their "keep the gates default-off" statements were correct when written and are superseded by the 6 October decisions recorded in the ledger. |
| `scripts/upgrade-recovery-private.mjs:83`, `scripts/schema13-seed.mjs:103` | Both already opt in explicitly (`BRRP_ENABLE_PRIVATE_CONVERSATIONS: "true"` / `"1"`), so they are unaffected in either direction. |
| `scripts/capacity-fixture.mjs:255,275` | It is written to *skip* private traffic when the gate is off and report "this run is community-only", which is still correct when an operator has switched the feature off — but after the flip its message "private conversations are disabled on this server" is reached only by deliberate configuration. Worth a wording pass at flip time; no functional edit. |
| `internal/api/state.go:181` (`"features": {"private_conversations": ...}`), `internal/api/channel_access.go:51` (`"restricted_enabled"`), `web/src/lib/workspace.svelte.ts:303-304` | The mechanisms that report the flags to the client. No edit; recorded so the flip's reach can be traced. |
