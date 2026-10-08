# BrrpBrrp user manual

BrrpBrrp is a small, self-hosted place to talk: text channels, direct conversations, voice,
and screen sharing, on a server run by someone you trust. This manual is for the people who
use a server — **members** — and for the people who run one — **administrators**. Each
chapter says which applies.

If you are installing a server for the first time, start with
[installation](../installation.md) instead; the chapters here assume a server already
exists and that someone has given you an invitation.

## Contents

**For members**

1. [Getting started](getting-started.md) — accepting an invitation, signing in, the layout of the screen.
2. [Messages](messages.md) — sending, editing, replies and threads, reactions, mentions, files, search, pins, reporting.
3. [Channels](channels.md) — text and voice channels, categories, muting, jumping between channels, restricted channels.
4. [Direct messages and groups](direct-messages-and-groups.md) — asking someone to talk, accepting requests, groups, blocking.
5. [Voice and screen sharing](voice-and-screen-sharing.md) — joining a call, microphone and speaker settings, push to talk, sharing a screen.
6. [Notifications and preferences](notifications-and-preferences.md) — your profile, appearance, audio, and what interrupts you.
7. [Your account and security](account-and-security.md) — password, two-factor sign-in, recovery codes, sessions, your data export.
8. [Privacy and safety](privacy-and-safety.md) — what the server operator can see, what is not encrypted, how to report a problem.

**For administrators**

9. [Administrators](administrators.md) — the Manage server panel, in task order.
10. [Troubleshooting](troubleshooting.md) — the problems people report most, and what to check.

**Reference**

- [Glossary](glossary.md) — the words the interface uses, and what they mean.

## Two things to know before you start

**This server may be a release candidate.** An administrator can read the version a server runs
under **Manage server → Maintenance → Server details**. A release candidate is the feature set intended for
the first full release; it has not been independently reviewed, and your administrator
decides how much to rely on it. [Known limitations](../known-limitations.md) lists what the
software does not do.

**Nothing here is end-to-end encrypted.** The server operator can read messages that are
stored on the server and can hear voice that passes through it. Read
[Privacy and safety](privacy-and-safety.md) before you put anything sensitive in a channel.

## Conventions used in this manual

- **Bold** names a button, menu item or tab exactly as the interface labels it.
- `Manage server → People` is a path through the administrator panel: open the gear beside the
  server name, then choose the tab named after the arrow.
- **Ctrl** means Control on Windows and Linux and **⌘** (Command) on macOS.
- Times and limits quoted here are the defaults. Your administrator may have set different
  values where an administrator can change them.
