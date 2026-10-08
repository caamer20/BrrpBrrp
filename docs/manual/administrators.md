# Administrators

**For administrators.** This chapter takes you through running a server day to day, in the order
you are likely to need it. The complete reference, with every limit and every edge case, is the
[administrator guide](../admin.md). Installing a server is covered in [Installation](../installation.md)
and keeping one running in [Deployment](../deployment.md).

Open the administrator panel with **Manage server**, or the gear beside the server name. Every change
is checked on the server for each request, so you cannot give yourself access through the browser,
and an action you are not allowed to take is refused even if its button is visible.

## Before you invite anyone

Open **Manage server → Maintenance** and read the **Is this server ready?** checklist. It has eight
lines: two-factor sign-in on your account, owner, members, HTTPS, voice, scheduled backups, a second
backup destination, and alerts. Each line links to the place that fixes it.

Some of these cannot be verified from inside the server. The checklist can confirm your configured HTTPS
and voice settings, but it cannot prove an external certificate or firewall works. Test those from
another network.

Then, in order:

1. **Turn on two-factor sign-in** for your own account. See [Your account and security](account-and-security.md).
2. **Configure a second backup destination** on separate storage. Until one exists, the server reports
   that a backup kept only on this host is not recovery. See [Backups](#backups-and-restore).
3. **Check voice from two networks.** Voice needs UDP or the relay fallbacks. See
   [Voice and networks](#voice-and-networks).

## People

**Manage server → People** lists the accounts. Use it to invite people, change access and remove
accounts.

**Invitations.** Create an invitation lasting between one hour and seven days. Each link creates one
account, and the person picks their own username and password. An invitation can carry a role; it
grants only what you hold yourself, and the server checks again when the link is used. Revoke an
invitation you no longer need.

**Account actions.** The **Manage** menu of an account can reset its password, sign it out
everywhere, deactivate or reactivate it, ban or unban it, change its administrator access or
erase it. Access changes and erasure require typing the username. You cannot demote, disable or erase
your own administrator account, and at least one active administrator must remain.

**Removing an account.** Deactivation is reversible. **Remove account, keep messages** ends the
identity permanently and keeps its posts under **Deleted member**. **Erase account and messages**
removes the account's messages and files. Both need recent authentication. The owner's account can only be removed after ownership has been
transferred. Neither rewrites existing backups.

**Losing a second factor.** When a member has lost their authenticator and every recovery code, use
**Remove lost second factor** from their **Manage** menu, after typing the username. It needs your
own second factor and a recent sign-in. See [Recovering access](#recovering-access-when-someone-is-locked-out).

## Roles and authority

**Manage server → Roles** shows the capabilities a role can carry. **Administrator**, **Moderator**
and **Member** are built in: their names and positions are fixed, so an upgrade cannot quietly move
authority.

A role only adds authority. You can create a role below your own position and grant only capabilities
you already hold, so a moderator cannot promote anyone above themselves. Assign roles to accounts with
**Manage → Roles and authority** in the People list. The three operator capabilities — archiving the
server, recovering credentials and transferring ownership — can only be granted by the owner.

**Ownership** is separate from roles. It is claimed or transferred through an explicit ceremony in the
panel, never inferred from administrator status.

## Channels

**Manage server → Channels** creates text and voice channels, places them in categories, and sets
their topics and order. A channel's type is fixed once it is created. Voice capacity is 1 to 10 people per
room, and the server admits at most 20 voice participants across all rooms.

For a channel that should be restricted, open its **Manage** menu. Its **Who can access this?** panel
shows the rule in force: visibility, posting, the count of accounts that can read it, and the fact that
community managers can read every community channel. Changes take effect at once, and anyone who loses
access loses it in the same request.

Restricted channels, and direct conversations, are **on by default**. An operator withholds them with
`BRRP_ENABLE_RESTRICTED_CHANNELS=false` or `BRRP_ENABLE_PRIVATE_CONVERSATIONS=false`. Neither has had an
independent review; see [Known limitations](../known-limitations.md).

## Moderation

- **Delete messages.** Anyone with the right ability can delete any message, after a confirmation step.
  You cannot edit someone else's words.
- **Reports.** **Manage server → Reports** shows reports, limited to channels you can currently read. Resolving a report is
  an audited review action; deleting its source message is a separate decision.
- **Timeouts.** A moderator can suspend an account for a set number of minutes, and end it early.
- **Slow mode.** Channel managers can set a posting interval of 0 to 21,600 seconds on a text channel.
- **Purge and delete.** **Purge messages** deletes messages older than a number of days; zero means all.
  **Delete channel** removes a channel and its messages. Both need typing the name. There is no in-app
  recovery; use a backup.

## Settings

**Manage server → Settings** holds the server name, the default theme, the upload limit, the voice
bitrate, previews and GIF embeds, typing and presence, push, auditing and retention. Changes that the
server keeps in its database take effect without a restart.

**Retention is off by default.** To enable it, choose a number of days and type the server's name.
Shortening an active retention period asks again. Cleanup runs hourly, so old messages may disappear at
the next run.

For a modest server, keep voice around 24 to 32 kbps, and turn off previews, GIF embeds, typing or
presence if you do not need them. Disabling voice ends current calls.

## Backups and restore

**Manage server → Maintenance** holds the backup schedule and the backups themselves.

- A new installation starts with **scheduled backups on**, every 12 hours, keeping seven copies with
  uploads included. The first is written within a minute of setup. Seven copies at 12 hours is three and
  a half days; set **Backups to keep** to 14 for a week.
- An installation created before this release keeps the backup setting it already has. If backups were
  off, switch them on here.
- Set `BRRP_BACKUP_COPY_DIR` in the host's environment to keep a copy on another disk. The browser
  cannot change this setting. Copy failures are reported separately from local ones.
- **Create backup** makes an archive on demand. The download is not encrypted: store it privately.
- Restore is done offline, into a **new** data folder, never over the current installation. Check an
  archive first with `brrpbrrp verify-backup --file ARCHIVE`. Restore removes sessions and push
  subscriptions, so devices must sign in again.
- Test a restore before you need one. The rehearsal described in [Administrator guide](../admin.md#backups-and-maintenance)
  does it on disposable folders.

## Server details and alerts

**Server details**, in Maintenance, shows the version, the database schema, the backup format and the
realtime protocol. Give those when you report a problem or plan a restore. It also shows memory and
processor time where the platform reports them, the slowest routes, and how much voice media has been
forwarded.

Operator alerts are configured on the host, not in the browser: `BRRP_ALERT_WEBHOOK` and an optional
`BRRP_ALERT_SECRET`. Without a destination, a condition is recorded and shown here, but nothing is sent.

## Voice and networks

The built-in TURN relay serves UDP by default. Enter the server's reachable IPv4 address and open the
TURN port and relay ports. Two fallbacks exist for a network that blocks UDP: a TCP listener and a TLS
listener, each off until switched on, each needing its own open port. Test from two networks; a network
that blocks UDP and cannot reach either fallback has no route through this server. See
[Deployment](../deployment.md) and [Security](../security.md) for the port table.

## Recovering access when someone is locked out

There are four routes, least disruptive first:

1. **A recovery code**, which the member uses once in place of the authenticator.
2. **Remove lost second factor** from **People → Manage**, for a member who has lost everything.
3. **The owner's offline recovery** on the host, with the service stopped:
   `brrpbrrp recover-owner --data-dir DATA --username OWNER --password-stdin`. It supplies the new
   passphrase on standard input, never on the command line, refuses while the service runs, and is
   recorded in the audit log under the host operator.
4. **No usable account at all:** `brrpbrrp create-admin --data-dir DATA --username recovery_admin --password-stdin`
   with the service stopped. It never changes an existing account's password.

The offline commands do not widen the trust boundary: anyone who can read the data folder already has the
database and the backups. They make a legitimate repair possible and recorded.

## Where to read more

- [Administrator guide](../admin.md) — the full reference, with every limit.
- [Installation](../installation.md) and [Deployment](../deployment.md) — setup, services and updates.
- [Hosting](../hosting.md) and [Docker](../docker.md) — where and how the server runs.
- [Security](../security.md) — HTTPS choices, throttling and what the application defends.
- [Upgrading](../upgrading.md) — what changes when you update, and rollback.
- [Production readiness](../production-readiness.md) — the checks before you open a server to a group.
