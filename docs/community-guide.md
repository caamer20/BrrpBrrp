# Everyday community controls

These controls are included in **0.4.0-alpha.5**. They apply to community text channels; direct and group messaging are still planned.

## Recover a failed message

An acknowledged send appears in history. A send with an unknown outcome keeps its original operation key while the client reconciles it, so a retry cannot create a second copy. If permission, a deleted destination, an expired operation or another permanent refusal prevents sending, your text appears under **Recovered messages**.

Choose **Copy**, select where you want to write, and paste when ready. **Dismiss** removes that recovery item. The app does not replace your current draft or automatically send recovered text. Files and reply targets from a refused operation are not recovered as sendable claims; attach or select them again if needed.

Preferences lets each account opt into saving unsent text on that browser. Without it, recovery is held only in memory. Signing out or turning persistence off clears saved recovery. Pending and recovered entries share a maximum of 50 records and 64 KiB of UTF-8 text per account. Retry stops after 24 hours; text-only recovery can remain for another 24 hours from its first display. Keep important writing separately: these limits and browser storage clearing can remove it.

## Quiet a channel

Use the channel header's notification control to mute or unmute the current channel. The setting belongs to your account and follows you across devices. Muting suppresses mention popups and push notifications from that channel; messages, mention history and unread counts remain available.

A change is shown as saved only after the server confirms it. Notifications that have already reached a device cannot be recalled. Global browser permission and the server's push setting still apply. A channel you lose access to disappears; your preference is restored if you later regain access.

## Report a message

Open a message's actions, choose **Report message**, and describe what needs review. Your identity, the selected message text and your explanation are shared with moderators who can read that channel. Only the selected text is captured; this does not give moderators access to another conversation.

Reports remain available for 90 days, including when the original message is deleted. Server backups may retain copies beyond that period. Explanations have a 2,000-character limit. Each account can submit up to 10 new reports per hour; retrying an already-open report returns its existing receipt.

Moderators with permission to delete others' messages can open **Manage server → Reports**. They see only reports in channels they can currently read. Review the evidence, add a note, and resolve or reopen the report. Review notes also have a 2,000-character limit. Reports disappear when they expire or their source channel is deleted. Reviewing a report does not automatically delete its source message or punish an account.

## Set a posting interval

Channel managers can set **Slow mode** on a text channel: 0 disables it; 1–21,600 seconds sets the interval between a member's new messages. The composer explains the active interval, and a refused send includes the time remaining.

An acknowledged retry is still returned immediately. Deleting a sent message, refreshing the browser or restarting the server does not reset the interval. Members who can manage channels bypass slow mode. Voice channels do not have this setting.

## Remove or deactivate an account

In **Manage server → People**, deactivation is the reversible way to stop access. **Remove account, keep messages** permanently ends the identity and shows its retained posts as **Deleted member**. **Erase account and messages** removes the live account and its authored messages/files. Both permanent actions require confirmation and the appropriate authority; the owner must transfer ownership first.

Retained posts or files can still contain identifying information. Report evidence and existing backups follow their own retention rules. See the [account policy](adr/0011-account-removal-and-retained-evidence.md) and [administrator guide](admin.md) before making a permanent choice.

**A member cannot remove their own account.** There is no self-service departure: every one of the controls above is an administrator's, so a member who wants to leave asks an operator. Before asking, **Your data export** — the download control beside your name — produces an archive of the conversations you can read, your own account details and your own messages, and it asks for a recent sign-in. Whether v1 adds a way for a member to leave on their own is an open question for the project owner, not a hidden setting.

## Retry microphone access

If the browser cannot finish opening your microphone within 30 seconds, the app stops waiting and offers **Retry voice**. Check the site's microphone permission and chosen input device, then retry. The microphone test also reports a timeout. Leaving or cancelling while permission is pending stops any stream that arrives later, so it cannot unexpectedly keep recording. A successful synthetic transport test does not confirm the microphone or audible quality on your device.
