# Channels

**For members.** A channel is a named place for a topic. A server holds up to 100 channels,
grouped into categories. Each is either a **text** channel, where people write, or a **voice**
channel, where people talk.

## Find a channel

The sidebar lists the server's channels by category. Text channels and voice channels are kept in
their own groups within each category.

To go somewhere quickly, press **Ctrl/⌘ + K** or choose **Jump to a channel**. Type part of a
channel's name, topic or category, then use the arrow keys and Enter. Channels with unread
messages or mentions are listed first.

Channels you cannot read are not shown to you. If a channel is restricted and you lose access
to it, it disappears from your list, and it returns if access is restored. Your mute setting for
that channel is kept.

## Read and catch up

A channel with unread messages is marked in the sidebar, and a mention is counted separately.
Unread counts are private to you: other members cannot see what you have read.

## Mute a channel

Use the channel header's notification control to mute or unmute the current channel. A muted
channel stops sending you mention popups and push notifications from that channel. Its messages,
your mention history and its unread counts still work, so you can see what you missed.

The setting belongs to your account and follows you to other devices. It takes effect only after
the server confirms the change. Notifications already delivered to a device cannot be recalled.
Browser permission and the server's push setting still apply. See
[Notifications and preferences](notifications-and-preferences.md).

## Slow mode

Some channels have a **slow mode** interval, set by the channel's managers. The composer tells you
the interval, and a refused send tells you how long to wait. Channel managers are exempt. See
[Messages](messages.md#slow-mode).

## Announcement channels

A restricted channel can be set to **announcements only**. Everyone who can read it still can, but
only people with channel-management abilities can post.

## Private channels

A **private** channel is readable only by the roles and accounts its managers have listed. Members
outside that list do not see it: it does not appear in their sidebar, search or
**Jump to a channel**, and it cannot be joined.

Be aware of two facts about private channels:

- **Community managers can read every community channel**, including private ones. This is the
  rule of the community, not a hidden setting, and the channel's access panel says so.
- A private channel's **voice** room works the same way as a private text channel: only people
  with access see who is in it or can join. It has no ring or admission step of its own.

Private channels and announcement posting are part of the restricted-channel feature. An
administrator can switch that feature off, in which case no new restricted channels can be created
or changed, and existing ones keep their rules.

Restricted channels have **not** had an independent privacy review. See
[Known limitations](../known-limitations.md#text-and-interface).

## Categories and order

Channels are ordered within categories by an administrator or a channel manager. Dragging a
channel's handle, or using **Move up** and **Move down** with the keyboard, changes its place for
everyone. Moving a channel into another category keeps text channels with text channels and voice
channels with voice channels.

## Create and manage channels

If you hold the right ability, **Create a channel** appears in the sidebar. A channel's type (text
or voice) is fixed once it is created. Its name, topic, category and posting rules can be changed by
its managers.

Deleting a channel permanently deletes its messages too. There is no in-app recovery.

## Who can access this?

Every channel has an access panel, opened from the channel's **Manage** menu. It answers
**Who can access this?** using the same rule the server applies: the visibility, the posting policy,
how many accounts can currently read the channel, and that community managers can read every
community channel.
