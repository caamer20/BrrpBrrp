# Accessibility review bundle — the journeys a person must walk

**Status: prepared, not delivered.** No accessibility reviewer, screen reader or physical
device is available in the environment this was written in, and the owner has not supplied
one. This file is the list of journeys to walk and where each one lives in the interface,
so that a reviewer with the devices can start immediately.

It is deliberately *not* a conformance statement. The project has never claimed blanket
WCAG conformance and should not, for the reason in §2.

---

## 1. The claim boundary

- The audit target is **WCAG 2.2** ([docs/v1-implementation-plan.md](../docs/v1-implementation-plan.md) §11.4).
- **Automated checks are supporting evidence, not the audit.** The suite's helper
  (`expectAccessible` in [web/e2e/support.mjs](../web/e2e/support.mjs)) runs axe with the
  tags `wcag2a` and `wcag2aa`. Those are WCAG 2.0 level A and AA rule sets. They do not
  cover the criteria added in 2.1 or 2.2, and no automated rule can settle whether a
  journey is usable. A green sweep means no automatically detectable violation of those
  two rule sets on the surface that was swept — nothing more.
- **What the sweeps do cover today**, so a reviewer knows where to start looking rather
  than re-treading them: the setup screen; the workspace in light and dark; the workspace
  at 390 px; the message search dialog; an Arabic (right-to-left) conversation; and the
  shared-screen stage at phone width. They run inside the release and voice journeys
  ([web/e2e/release.spec.mjs](../web/e2e/release.spec.mjs),
  [web/e2e/voice.spec.mjs](../web/e2e/voice.spec.mjs)).
- **Live media raises alternative requirements that no UI scan can settle** (captions and
  alternatives for live audio). The product offers text chat alongside a live room, which
  is not the same thing. If the reviewer judges an alternative to be required, the honest
  outcome is a published limitation, not a claim.
- The design goal of a 44×44 CSS-pixel touch target is a **product target**, distinct from
  the standard's actual criteria. Report against the criteria; note the target where it
  is or is not met.

## 2. Why this cannot be closed by automation

Three reasons, stated because they are the ones a reader is most likely to assume away:

1. The automated tags stop at WCAG 2.0 A/AA (§1).
2. Focus, reading order, and whether a status message is actually announced are
   behavioural. A tool can see that an element has `role="status"`; it cannot see that a
   screen reader said anything useful.
3. Physical input — a real virtual keyboard, a real screen reader's navigation mode, a
   real phone's reflow under zoom — is not a viewport size.

## 3. Who must walk it

The master plan's T-A11Y-01 requires critical journeys by keyboard and by selected screen
readers, at narrow/zoomed/reduced-motion/forced-color settings and with physical virtual
keyboards, with no inaccessible action and no lost or trapped focus. The execution plan
(V1-13) adds the surfaces: complete journeys, keyboard/focus and dialogs, selected screen
readers, meaningful labels, errors and status updates, zoom/reflow/text spacing,
contrast/forced colors/reduced motion, touch/virtual keyboard, media controls and error
recovery.

An independent reviewer is required. The implementing agent cannot supply this approval.

## 4. Keyboard journeys

Each row is one sitting, not one click. "Where" points at the surface; the component files
are named so a reviewer can read the markup when something behaves oddly.

| # | Journey | Where | What to watch |
| --- | --- | --- | --- |
| K-1 | Sign in, including a wrong password and a second-factor field | Sign-in card ([AuthPage.svelte](../web/src/lib/components/AuthPage.svelte)) | The error is announced, not merely coloured; focus lands in the code field when one is required; the password visibility control is reachable and reports its state. |
| K-2 | First-run setup | The same card in setup mode | Every field labelled; the setup token field is not auto-completed. |
| K-3 | Move between channels and read | Channel sidebar and the message log ([Workspace.svelte](../web/src/lib/components/Workspace.svelte)) | A conversation switch keeps or restores focus sensibly; the log is a log, and a new incoming message does not steal focus while a person is typing. |
| K-4 | Compose and send | Composer ([Composer.svelte](../web/src/lib/components/Composer.svelte)) | Enter sends and Shift+Enter breaks the line (say which is which if it differs); upload progress and failures are announced. |
| K-5 | Act on a message: reply, react, pin, edit, delete, copy link | Message action menu ([MessageItem.svelte](../web/src/lib/components/MessageItem.svelte)) | The menu opens by keyboard, Escape closes it, **focus returns to the control that opened it**. This is the classic trap; test it on a phone-width layout too, where the actions are a different control. |
| K-6 | Reply in a focused thread | Reply thread dialog | Opening and closing returns focus to the message; the thread's own composer keeps its draft. |
| K-7 | Search | Search dialog ([SearchPanel.svelte](../web/src/lib/components/SearchPanel.svelte)) | The input takes focus on open; results and "no results" are announced; Escape closes and restores focus. |
| K-8 | Pins | Pins panel ([PinsPanel.svelte](../web/src/lib/components/PinsPanel.svelte)) | The retry action after a failure is reachable and its result announced. |
| K-9 | Preferences: Appearance, Audio, Notifications, Account | Preferences dialog ([Preferences.svelte](../web/src/lib/components/Preferences.svelte)) | Tabs are navigable by arrow keys; each panel's controls are labelled; the output-device list says what it could not list rather than going empty. |
| K-10 | Member list to a profile, and back | Member list and profile dialog | The profile is reachable from the list **and** from a message author, and both open the same dialog about the same person. |
| K-11 | Manage server: an administration action and the backup panel | Administration ([AdminPanel.svelte](../web/src/lib/components/AdminPanel.svelte), [admin/Maintenance.svelte](../web/src/lib/components/admin/Maintenance.svelte)) | Destructive actions name what they remove and require explicit confirmation; a saved state is announced. |
| K-12 | A voice call: join, mute, deafen, leave | Voice controls ([VoiceControls.svelte](../web/src/lib/components/VoiceControls.svelte)) | Every control's state is exposed (`aria-pressed` or equivalent), not only shown. |
| K-13 | A shared screen: audio-only, show picture, enlarge, fullscreen | Screen stage ([ScreenStage.svelte](../web/src/lib/components/ScreenStage.svelte)) | The audio-only state does not strand the person: the control that brings the picture back stays reachable, and fullscreen can be exited by keyboard. |
| K-14 | A refused send, and its retry | Composer and message row | The recoverable text is reachable and complete; retry does not lose the text. |
| K-15 | Sign out, and the update notice | Header, update notice ([UpdateNotice.svelte](../web/src/lib/components/UpdateNotice.svelte)) | The update notice blocks correctly and preserves drafts; a notice that cannot be dismissed is announced as such. |

Modals are worth one extra pass on their own: [Modal.svelte](../web/src/lib/components/Modal.svelte)
is the shared implementation, and the question is whether focus is moved in, contained
while open, and returned on close for every dialog built on it.

## 5. Screen-reader journeys

Run the same list with a real screen reader — at minimum one desktop reader on the
supported desktop client and one mobile reader on a supported phone. The specific claims
to check, rather than the whole list again:

- The sign-in and setup forms announce their labels and their errors.
- The message log announces new messages without interrupting a person mid-typing, and
  read/unread state is available rather than only visible.
- Search announces the result count and the "changed query" state.
- A message's delivery state ("Sending…", "Message not sent · Retry") is announced.
- The member's own profile dialog, and the profile opened from a message, are the same
  content under the same name.
- The share stage's controls announce their pressed state and their names.
- A refused action — a channel that is gone, a conversation that was revoked, a send that
  will not go — is announced as a refusal, not rendered as silence.
- Nothing on the operator surfaces (health, metrics, diagnostics) reads out a member's
  identity or a conversation's content. That is a privacy statement as much as an
  accessibility one.

## 6. Zoom, reflow, text spacing, forced colors and motion

| Setting | Where to check | Expected |
| --- | --- | --- |
| 200% and 400% zoom on a desktop viewport | Sign-in, workspace, search, preferences, administration | Content reflows without two-dimensional scrolling; no control is clipped or unreachable. |
| 320 px and 390 px at normal zoom | The same surfaces | The sidebar collapses to a drawer that can be opened and closed by keyboard; the composer stays reachable with the keyboard up (a *simulated* phone viewport does not prove this; a physical phone does). |
| Long unbroken text, right-to-left text, code blocks, quotes and emphasis | The message list and the composer | No horizontal page overflow; direction is resolved per paragraph, not inherited from the page. |
| Increased text spacing | Everywhere text is set | No overlap and no loss of content. |
| Forced colors / high contrast | Everywhere | Nothing depends on a background colour to remain readable, including muted, pending and disabled states. |
| Reduced motion | Panels, transitions, the voice chime and animations | Motion is suppressed; nothing important is communicated only by movement. |
| Dark and light themes | The automated sweeps cover both; a person should confirm contrast on the muted, pending and placeholder text | Readable at the criteria, not only at the designed ratio. |

## 7. Media, touch and physical input

- A physical virtual keyboard: the composer, the search field and a dialog's first field,
  including whether the keyboard covers the send control.
- Touch: 44×44 CSS-pixel design target on the message actions, the share controls and the
  voice controls.
- Device rotation and safe areas on a phone, including the installed (Home Screen) app.
- A physical microphone and a physical screen-share picker: permission denial, recovery,
  and the state shown afterwards.
- Background and resume: the call, an in-flight send and an upload across a backgrounded
  tab and a locked screen.

## 8. How to run the automated part

Build the frontend first, because it is embedded, then run the sweeps as part of the
journeys that contain them:

```sh
npm --prefix web run build
BRRP_E2E_PRIVATE_CONVERSATIONS=0 npm --prefix web run test:e2e -- --output /tmp/a11y/default
```

A failure prints the rule, the impact, the affected targets and — for contrast — the
measured ratio and the two colours, because "there is a violation" is not something
anybody can act on. Treat it as a starting point: every violation it reports is real, and
its silence proves only what §1 says.

## 9. What must be supplied before the review can happen

| Input | State |
| --- | --- |
| A named independent reviewer | **Absent** |
| A desktop screen reader and a mobile screen reader | **Absent** |
| A supported physical phone and a physical desktop keyboard | **Absent** |
| A candidate build to review, bound to a commit and an artifact hash | **Absent** — see [security-review-bundle.md](security-review-bundle.md) §7; the same artifact question applies here |
| Confirmed WCAG 2.2 scope and stated exclusions | **Owed by the owner** |

## 10. What a finding must carry

Affected criterion and journey, severity, what a person experienced, the exact steps, the
assistive technology and version, whether a workaround exists, an owner, and the
reviewer's recheck after a fix. A finding with no reproduction is a note, and a note does
not close a gate.
