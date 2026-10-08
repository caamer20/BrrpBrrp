# Privacy and safety

**For members.** This chapter tells you plainly what the server can see, what the app enforces
between members, and what to do when something is wrong. Read it before you put anything sensitive
in a channel.

## Who can see what

**The server is run by someone you trust.** The operator controls the computer the server runs on,
its database, its backups and its files. BrrpBrrp does **not** end-to-end encrypt messages or voice.
Anyone with access to the server's data folder can read stored messages and attachments, can read
backups, and in principle can hear voice that passes through the server.

Within the app, the rules are stricter:

- **Channels** are visible to people who can read them. Community managers can read every community
  channel, including restricted ones; the channel's access panel says so.
- **Direct conversations and groups** are visible only to the people in them. Administrators and
  owners do not gain access by being administrators. (The host operator's access to the data folder
  is the exception, described above.)
- **Voice** is heard only by people in the room, and carried through the server.
- **Your data export** contains only what you are entitled to read.

## What notifications reveal

Push notifications are sent through the browser's push service. The payload says that someone
wrote to you or mentioned you. It does not carry the text of the message. The push service can still
see that a notification was sent to your device, and when.

## Link previews and images

When someone posts a link, the **server** may fetch the page to build a preview. The server refuses
private and internal addresses and limits what it downloads. Your own browser loads external images
only when you choose, if **Private previews** is on in your preferences.

The server's own previews can be turned off by the administrator. Turn them off if you do not want
the server to fetch links that others share.

## Files

Files are stored on the server, and the server does not scan them for viruses or malware. Files
that a browser could run (HTML, SVG and similar) download rather than open, but a downloaded file
is still a file. Do not open an attachment just because a familiar person posted it.

## Reporting someone

**Report message** on a message sends the reviewers the text you selected, your explanation, and
your name. Reviewers see only the channels they can read. A report stays open for 90 days, even if
the message is later deleted. Backups may keep copies beyond that.

A report does not delete the message or punish anyone automatically. A reviewer decides.

**Report and block** does both for a direct conversation: it blocks the person and sends the
reviewers only the messages you selected.

Reporting is for conduct and content. A security problem with the software itself is for the
people who run the software, not the server's reviewers. The [security policy](../../SECURITY.md)
explains that channel, which is not yet live in this release, so ask your administrator how to
reach them.

## Blocking and leaving

**Block person** stops someone from contacting you and you from contacting them, in both
directions. It does not remove them from a shared channel. See
[Direct messages and groups](direct-messages-and-groups.md#block-someone).

Members cannot remove their own accounts from the app. Ask an administrator. Removing an account
keeps messages under **Deleted member** or erases them, as the administrator chooses; in either case
evidence and backups may keep copies for their own retention periods. See [Administrators](administrators.md#people).

## Audit records

Administrative actions are recorded in an audit log: who did what, and when. The log does not hold
message contents, passwords or tokens. Its entries expire after 90 days.

## Retention

Messages are kept until an administrator deletes them or a retention period, if one is set, removes
them. Deleted messages are removed from the live server; backups made before the deletion still
contain them until the backups themselves expire. Ask your administrator what the server's
retention setting is.

## Good habits

- Assume that a message in a channel can be read by the operator, and by everyone in that channel.
- Use direct conversations for things you would not post in a channel, and remember the operator can
  still read them on the server's disk.
- Do not post passwords, recovery codes or someone else's personal details.
- Report content that breaks the community's rules rather than arguing with it in the channel.
