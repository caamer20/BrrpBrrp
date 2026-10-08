# Messages

**For members.** This chapter covers writing, changing and finding messages, and the
controls for when something goes wrong.

## Write a message

Open a text channel or a conversation, click the composer at the bottom and type.

- **Enter** sends on a computer. **Shift + Enter** starts a new line.
- On a touch screen, **Enter** starts a new line and **Ctrl/⌘ + Enter** sends.
- Attach files with **Choose attachments** or **Attach files** in the composer, or paste them from the clipboard.

### Formatting

Messages support a small, deliberate set of formatting. Anything else is shown exactly as typed.

| You type | You get |
| --- | --- |
| `**bold**` or `__bold__` | **bold** |
| `*italic*` or `_italic_` | *italic* |
| `~~struck~~` | ~~struck~~ |
| `> quoted text` at the start of a line | a block quote; consecutive quoted lines form one quote |
| text between backticks, like `` `this` `` | code, shown literally and never formatted |

There are no lists, headings, tables or images written as markup. Links need their full
scheme, such as `https://`, before they become clickable. Mentions use `@` and the person's
username, and open a notification popup for them.

Messages in any script are drawn in the direction they read, including mixed-direction text.
The interface itself is English and left to right.

## Edit and delete

Your own messages have **Edit** and **Delete** in the message's actions. Editing changes the text
of your own message.

You can delete your own messages. An administrator or a moderator with the right ability can
delete anyone's message, with a confirmation step, but cannot edit another person's words.
Deleting a message that has replies keeps the replies in the channel.

## Reply, and follow a thread

Use **Reply** in a message's actions to answer it in the channel with a reference to the message
it answers.

To follow a discussion on its own, choose **Reply thread** from a message's actions, or click
its reply count. The thread opens beside the channel with the original message and its replies.
Replies stay visible in the channel's history and in search. Thread drafts and channel drafts
are kept separately, and closing a thread returns you to the place you were in the channel.

Replies load 50 at a time, up to 250 in the thread view. Use search to find older ones.

Named threads are listed from the **Threads in this channel** control in the channel header. Open
one to read it, and follow it to be told about new replies.

## React

Choose **Add reaction** from a message's actions. The picker offers 60 reactions, and you can
type a word such as "love" or "celebrate" to find one. Select a reaction to add it. Select your own
reaction again to remove it. Escape closes the picker and returns focus to the reaction button.

## Files and images

Files attached to a message are limited to 12 MB each by default; the administrator may set a
lower limit, and the server's total storage may be capped too. A file that would exceed the
limit is refused with the amount in use and the limit, and is not kept.

Images from members' uploads show a private preview: a small JPEG made by the server, shown
without fetching the original. The original opens when you ask for it. Animated images show their
first frame in the preview.

Links to other websites may show a preview. Previews are fetched by the server, and an
administrator can switch server previews off. The browser can also be set to hold external
images until you click them.

Files that the browser could run, such as HTML or SVG, download rather than open. Uploaded files
are **not** scanned for viruses or malware. Do not open a file just because a familiar account posted it.

## Search

**Search messages** in a channel's header searches that channel. **Ctrl/⌘ + Shift + K** opens
search across the server's channels. Search finds replies as well as top-level messages.

Searches inside a direct conversation stay inside that conversation. Server-wide search covers
community channels only.

## Pins

**Pinned messages** in the header lists the messages a channel's moderators have pinned, up to 100
per channel. Pinning is a moderation action, so only people with the right ability can pin or unpin.

## Report a message

Open a message's actions and choose **Report message**. Say what needs review in up to 2,000
characters. Moderators who can read that channel see the reported text, your explanation and your
name. They do not see the rest of your conversations.

A report stays open for 90 days, even if the original message is deleted. You can make up to 10
new reports an hour. Reporting the same message again returns your existing report. Reports
do not automatically delete the message or punish anyone; a moderator decides.

## Slow mode

A channel's managers can set a **slow mode** interval of up to six hours between each member's
new messages. The composer shows the active interval, and a refused send tells you how long to
wait. Acknowledged retries are not blocked, and channel managers are exempt.

## Mute a channel

See [Channels](channels.md#mute-a-channel). Muting affects notifications only; messages stay
readable and counted.

## When a message does not send

An acknowledged send appears in the channel and is never duplicated. If the outcome is unknown,
the app keeps the send open and retries it under the same key, so pressing send again cannot make
a second copy.

If a send is refused permanently — for example, the channel was deleted or you lost permission —
your text is kept under **Recovered messages**. **Copy** puts the text on your clipboard. Select
where you want to write it, then paste. **Dismiss** removes the item.

The app never sends recovered text for you and never replaces what you are typing. Files and
reply targets from a refused send are not kept as sendable: attach or select them again.

Recovery items are held in memory. If you have turned on **Remember drafts and unsent messages on
this device** in **Your preferences → Appearance**, they survive a reload on that browser until you
sign out. Keep long writing somewhere else as well;
browsers can clear their storage.
