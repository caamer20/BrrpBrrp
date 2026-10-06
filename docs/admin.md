# Administrator guide

Open the gear next to the server name. Administrative changes are checked on the server for every request, including live voice controls. A member cannot grant themselves access.

## People

Under People, create a private invitation lasting 1 hour to 7 days. Each link can create one member account; the person chooses their own username and password. Revoke unused invitations when needed. An invitation can also carry a role selection: the new account receives those grants when it accepts. You can only promise authority you hold, so a moderator can never hand out administration through a link, and the server checks the promise again when the link is used — an invitation written before you were demoted delivers the default role and nothing more. The pending list shows what each link will grant. Leave the selection empty for an ordinary member. Promising roles needs role management; an account that may only invite can still invite, without a role selection. You can also create an account directly with an initial passphrase. Usernames contain 3–32 ASCII letters, numbers or underscores. Passwords are 12–72 UTF-8 bytes. Password changes end all sessions.

The account's **Manage** menu can reset its password, sign it out everywhere, deactivate/reactivate it, ban/unban it, change administrator access or permanently erase it. Deactivation preserves the account and messages while preventing access. Erasure also deletes messages, attachments and reactions. Access changes and erasure require typing the username. You cannot disable, erase or demote your own administrator account, and at least one active administrator must remain.

Administrators can enable an authenticator in **Your preferences → Two-factor authentication**. Add the displayed setup key to a TOTP app and enter its six-digit code within ten minutes. Save the ten single-use recovery codes; they are shown once. Enabling or disabling MFA signs out other sessions. Sign-in requires a password and an unused authenticator/recovery code. Password reset does not remove MFA. Keep the host clock synchronized. Active authenticator keys are stored in the private database, so backups must remain protected.

Locked-out accounts are recovered as described below. Every path needs the server host: this application has no e-mail address, no account-recovery service and no support desk, so nobody can restore access from a browser, a phone or a third party.

## Recovering access when someone is locked out

Sign-in needs a password and, once a factor is enrolled, an unused authenticator or recovery code. There are four ways back in, and the first one that applies is the least disruptive.

**1. A recovery code.** Ten single-use codes are shown once at enrollment and each works in place of the authenticator, once. This is what they are for; keep them where you keep other secrets.

**2. An operator removes a lost second factor.** In **Manage server → People**, open the account's **Manage** menu and choose **Remove lost second factor**, then type the username. Use it only when the authenticator *and* every recovery code are gone. It needs an operator account that has enrolled its own factor and signed in within the last 15 minutes, it is recorded in **Recent activity**, and it ends that account's sessions: the person then signs in with their password alone and must enroll a new factor immediately. An ordinary password reset deliberately keeps the factor, so a borrowed administrator session cannot strip the second factor that protects somebody else's account.

**3. The owner restores its own access on the host.** Full archive, chosen-password reset, ownership transfer and this kind of recovery are the non-delegable operator powers, and only the owner holds them. If the owner has lost both the authenticator and every recovery code, no account still inside the running server can repair that lockout, so the host operator does it with the service stopped:

```bash
brrpbrrp recover-owner --data-dir YOUR_DATA --username OWNER --password-stdin
```

Supply the replacement passphrase on standard input from a private terminal or secret manager; do not put it in command arguments or shell history. The command refuses to run while the service holds the data folder, refuses any account that is not the current owner, and refuses while the installation has no owner yet. It replaces that account's password, removes the enrolled factor and its recovery codes, signs out every device and push subscription, and lifts a deactivation or ban that would otherwise still refuse the sign-in. It leaves roles, ownership, messages and files alone: recovery restores the owner's own credentials, not authority over anyone else. It prints everything it changed and records one row in the audit log (`owner.recovered_offline`) attributed to the host operator rather than to the owner. Afterwards, sign in, enroll a new authenticator and save the new recovery codes: because the old factor is gone, the owner's first archive, reset or ownership change is refused until a new one is enrolled.

**4. No usable account at all.** `brrpbrrp create-admin --data-dir YOUR_DATA --username recovery_admin --password-stdin` adds an administrator while the service is stopped. It grants the administrator role and never changes an existing account's password. Prefer first-time browser setup for a normal installation.

The offline commands do not widen the trust boundary: anyone who can read and write the data folder can already read the private database, its backups and the attachment files. They make the legitimate repair possible and recorded instead of improvised. Protect the folder, `server.env`, TLS keys and the backups, and note who ran which command.

## Permanent account removal

Deactivation is reversible. **Remove account, keep messages** ends the identity permanently and preserves conversation history under **Deleted member**. It requires recent authentication, appropriate hierarchy and typed username confirmation. The owner must transfer ownership first. **Erase account and messages** is a separate choice that removes the live account's authored messages and files. Neither choice rewrites existing backups, and reported evidence may remain available for 90 days. See the [account policy](adr/0011-account-removal-and-retained-evidence.md).

## Channels and moderation

Create text or voice channels with names, categories, topics and an order number. A channel's type cannot be changed after creation. Voice capacity is 1–10, with a server-wide cap of 10. Reducing capacity prevents new joins but does not evict existing participants.

Authors can edit and delete their own messages. Administrators can delete any message, with confirmation, but cannot edit someone else's words. In a voice panel, administrators can remove a participant or restrict their microphone/audio. Restrictions are enforced in the server's forwarding path, not just in browser controls.

**Purge messages** deletes messages older than the chosen number of days. Zero means all messages. **Delete channel** permanently deletes the channel and its messages, and disconnects its voice participants. Both require typing the channel name. Deleted data has no in-app recovery; use a prior backup.

### Reports and slow mode

**Manage server → Reports** is available to holders of `message.delete_any`. It shows selected-message evidence only in channels the reviewer can currently read. Resolving a report is an audited review action; deleting its source message is separate. Reports expire after 90 days and are removed physically in hourly batches. Channel managers can set a text channel's posting interval to 0–21,600 seconds; 0 disables slow mode, and channel managers bypass it. The interval survives deletion and restarts.

Members can mute each channel's mention popups and push without changing unread counts or history. The [community guide](community-guide.md) explains these controls and failed-send recovery.

## Roles, categories and restricted channels

The **Roles** tab is the capability catalog. Roles only add authority; they never remove it. An account may create a role below its own position and grant only capabilities it already holds, so an administrator cannot create a peer of itself or hand out something it lacks and a moderator cannot promote anyone above itself. **Administrator**, **Moderator** and **Member** are built in: their names and positions are fixed so an upgrade cannot silently move authority, though their capabilities can be adjusted within the caller's own authority. Their **Member** role is the default that every account holds without an explicit assignment. A custom role can be deleted only while nobody holds it. The three operator capabilities — archiving the server, recovering credentials and transferring ownership — are shown but only the installation owner can grant them.

Assign or remove roles per account with **Manage → Roles and authority** in the People list. Removing a role takes effect on that account's next request, and its open client is told to refetch; if the account is in a voice channel the role admitted it to, the call ends at the same moment. The last-administrator guard refuses a change that would leave the installation without an administrator, and a peer administrator cannot rewrite an equal's roles. Ownership is separate: it is claimed or transferred through the explicit ceremony, not by assigning a role.

Channels are ordered inside durable **categories**. Renaming a category never changes a channel's visibility or access, and a category can be deleted only once its channels have moved elsewhere. New channels are public and members-postable, exactly like channels that existed before this feature.

**Who can access this?** in a channel's **Manage** menu answers the question with the server's own rule. It reports the visibility, the posting policy, how many accounts can read the channel right now, and the standing fact that community managers — anyone holding channel management — can read every community channel. The same panel edits those settings:

- **Visibility.** *Private* restricts reading to the roles and accounts listed below. A conversion in either direction is one authorization change and commits with the authorization revision, so an already-admitted read cannot continue on the old policy.
- **Posting.** *Announcements only* restricts ordinary sends to accounts that can manage channels; everyone who can read the channel can still read it.
- **Grants.** A role grant follows the role: everyone holding it gains access and loses it when the role is removed. Individual accounts can be listed when a role would be too broad. Grants are kept when a channel returns to public, so a later conversion to private does not start from nothing.

Private channels and announcement posting are refused by the API, not merely hidden, until the operator sets `BRRP_ENABLE_RESTRICTED_CHANNELS=true`. That switch is deliberately off: the enforcement is implemented and tested, but the independent privacy review (WP-28) has not run, so restricted channels are unreviewed until the owner decides to open the gate.

A restricted **voice** channel now enforces the same rule as a restricted text channel. Who is in it reaches only the accounts that can read it, joining or moving into it is refused exactly like a missing channel, and an account that loses access — because a grant, its role or its administrator status changed — is removed from the call in the same request, told why, and recorded in the audit log as `voice.access_revoked`. What is still missing is the private-call contract of its own (WP-19/WP-20), so there is no ring or per-call admission step yet. The panel says all of this when you open a private voice channel.

## Features and resources

Settings control server name, default theme, upload limit, Opus bitrate, previews/GIF embeds, typing, presence, voice, push and auditing. New devices follow their system light/dark preference; users can select an explicit theme. Private-preview and low-resource defaults apply until a device saves its own preference.

For a modest server, keep voice near 24–32 kbps and encourage low resource mode. Turn off previews, GIF embeds, typing or presence as needed. Disabling voice ends current calls. Large uploads use disk rather than whole-file memory buffering; the installation's environment limit can reduce the UI maximum below 12 MB.

Retention is off by default. To enable it, choose a positive number of days and type the current server name. Shortening an active retention period requires confirmation again. Cleanup runs hourly; once enabled, old messages may disappear at the next run. Keep enough free disk for the database, its WAL, uploads and backup snapshots.

Unclaimed uploads and unreferenced files older than 24 hours are cleaned hourly. Audit records expire after 90 days. Logs sent to a configured file rotate at 10 MB with three archives. Maintenance offers a backup interval of 1–168 hours (default **12**) and retention of 1–30 copies (default 7). The 12-hour default is the cadence the recovery objective assumes: at most 24 hours of loss. Staleness is judged from the newest independently recoverable snapshot, and the thresholds scale with whatever interval you choose — a warning at one and a half intervals and critical at twice it, so 18 and 24 hours by default. Each successful UI/scheduled backup removes older app-created UUID archives. Manually named CLI archives are retained. Export copies you need to keep longer.

## TURN and networks

The built-in TURN server is optional and supports UDP. Enter the server's reachable IPv4 address, open the configured TURN port (3478 by default), and open relay UDP ports 49152–49999. Configure the WebRTC range and external IP in the host settings too. Disable TURN and save before changing its address or port.

Relay credentials are issued for a voice session, expire after 24 hours and are revoked when that session ends through the API. Existing allocations can persist until the TURN protocol expires them; permitted destinations are limited to this server's addresses. Disabling TURN immediately closes its relay allocations. UDP-blocking client networks need a different network path; this release does not offer TURN over TCP/TLS.

## Backups and maintenance

Maintenance shows connected sockets, voice participants, message count, database/upload size, free disk, backup success and Go heap use. Heap size is not total process memory. Setup checks show configured HTTPS and voice details; they cannot verify an external certificate or firewall from inside the server. Audit records exclude message contents, passwords and tokens.

Scheduled backups are off until an administrator enables them. For production, enable a schedule with uploads, configure a private extra copy on separate storage, protect a separate copy of host settings and TLS keys, and rehearse a restore. The host-side `production-check` command can signal disabled or failed backups, stale copies, low disk space and expiring HTTPS certificates to a scheduler or monitoring service; see the [production runbook](production-readiness.md).

To keep an extra copy on another disk, configure `BRRP_BACKUP_COPY_DIR` in the host environment and grant the service account write access. This destination cannot be changed through the browser. Copy failure is reported separately from local backup success. Scheduled failures wait until the next configured interval before retrying; inspect the warning and create a manual backup after correcting it.

Create a backup with uploads for a complete chat restore. The SQLite snapshot is consistent while users chat. Files saved after that snapshot appear in the next backup. The ZIP includes the database, settings and push keys, but not host configuration or TLS certificates. Export those separately if you need a complete machine recovery.

Every archive now carries a manifest that names the application and schema version that wrote it and records a size and SHA-256 digest for the database and for each attachment. Restore checks those digests as it extracts, so an archive that was altered or truncated is refused instead of installed, and the same check is available on its own:

```
brrpbrrp verify-backup --file backups/manual.zip
```

That command reads the archive wherever it is, needs no data directory and needs no lock, and prints the format, creation time, versions, entry counts, sizes and the archive's own SHA-256. Run it on the copy that lives on separate storage — that is the copy a recovery would use. Maintenance shows the identity of the newest archive and a copy is only recorded when its digest matches the archive it copied, so the identity you see is the identity you can compare elsewhere. Archives written by an older version carry no digests; they still restore, and the command says so instead of implying a check it could not perform. `production-check` verifies the newest local archive and the newest copy as part of its monitoring, so a corrupt archive fails the check rather than passing as a recently written file.

Restore offline into a new data folder, never over the current installation. Archive limits are 20 GiB total, 2 GiB for the database, 100,000 attachment entries and 12 MB per attachment. Every restored attachment must exist, and database integrity and foreign-key references must pass. Sessions and push subscriptions are removed; devices must opt in again. Verify an older message, search, an attachment and a new login before changing the service's data path.

A database-only backup deliberately drops attachment records on restore. Do not use it when you need shared files. Back up before upgrades and destructive moderation, and periodically rehearse a restore.

`node scripts/upgrade-recovery.mjs` (or `make rehearsal`) rehearses the whole path on disposable data directories against a real server process: it preflights an empty folder and confirms nothing is created, installs, writes a message and an attachment through the API, backs the installation up, restores that archive into a second empty destination, verifies it, then serves the restored copy and compares the message and the attachment bytes with what was written. It prints every step and exits non-zero on the first unmet promise; add `--report FILE` to save the transcript, `--keep` to inspect the data directories, or `--binary PATH` to rehearse a specific build. It rehearses a disposable local destination only, so it does not replace a dry run on the real host: before an upgrade, copy the backup off the machine and prove a restore there, and keep the matching prior executable or image until validation passes. Attachment files are flushed and published under their final name before the database row that references them exists, so a crash mid-upload costs at most that upload and never leaves a referenced file missing.

## Channel ordering and reactions

Administrators can drag the dotted handle beside a sidebar channel to place it before or after another channel. Dropping beside a channel in another category moves it into that category; text channels stay with text channels, and voice channels stay with voice channels. Changes save for the whole server and appear for connected members.

Click or tap the handle for **Move up** and **Move down** within the current category. With the handle focused, **Alt + Up/Down** also moves the channel. Press **Escape** to cancel a drag or close the move controls. Existing channel settings remain available in **Manage server → Channels**.

Messages offer 60 reactions. Choose **Add reaction**, search by name or a word such as “love” or “celebrate,” and select an emoji. Click your existing reaction to remove it. Escape closes the picker and returns keyboard focus to the reaction button.
