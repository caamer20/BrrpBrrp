# Voice and screen sharing

**For members.** Voice channels carry live audio: your microphone, and the audio of a shared
screen when someone shares one. There is no camera and no recording. A call leaves nothing behind
on the server.

## Before your first call

1. Open **Your preferences → Audio** and choose your microphone. Device names appear once
   your browser has permission to use the microphone.
2. Choose a **Speaker** if your browser lets you pick one. Otherwise the system default is used,
   and the page says so.
3. Select **Test microphone**. The **Input level** meter moves when you speak. Select **Play test
   sound** to check your speakers. The test stays on your device, and joining a call stops it.
4. Use headphones if you can. They prevent a speaker's sound from being picked up again by your
   microphone.

Your browser asks for microphone permission the first time you join. If you refused, change the
site's permission in the browser's settings, then join again.

## Join and leave

Select a voice channel in the sidebar, or choose **Join voice**. You hear a short, gentle chime when
you connect, and others in the room hear one when you arrive. Choose **Leave voice** to leave.

The room's list shows who is in it, and a person who is muted or deafened is marked as such. If
your connection drops, the room offers **Join again**.

If your microphone does not open within 30 seconds, the app stops waiting and offers **Retry
voice**. Check the microphone permission and the chosen input device, then retry.

## Mute and deafen

- **Mute microphone** stops your microphone reaching the room. Choose **Unmute microphone** to
  speak again.
- **Deafen** stops you hearing the room. Choose **Undeafen** to hear again.

Mute and deafen belong to you. Administrators have their own controls, described below.

### Push to talk

In **Your preferences → Audio**, turn on **Push to talk**. Then the microphone is open only while
you hold **Space** with the app focused, or while you hold the **Hold to talk** button. Typing
never opens the microphone.

### Voice sounds

**Voice join sounds** turns the arrival chime on or off for you. **Notification volume** sets the
loudness of the app's notification sounds. Deafening silences the chime.

## Screen sharing

In a voice channel, choose **Share screen**. Your browser asks which window, tab or screen to share.
While you share, the button reads **Stop sharing screen**.

- **Include the sound your computer is playing** sends the sound from the shared screen as well. Use
  it to share a video or a game with its audio.
- **Screen share quality** trades clarity for smoothness. Higher quality uses more bandwidth.
- A room can have one shared screen at a time, and the server can have two in total. If one is
  already being shared, a second is refused until it stops.

A listener's volume control for a sharer applies to that person's voice and to their shared
screen's sound together. The two cannot be set separately.

## Administrators and moderation

Administrators and moderators with voice-moderation abilities can remove a participant from a call,
or restrict their microphone or audio. These restrictions are enforced by the server, not only by
the other person's browser. A restricted participant is marked **Muted by admin**.

## Private voice channels

A voice channel that is restricted to some members works like a private text channel: only the
people with access see who is in it, and only they can join. When someone loses access to a restricted
channel, they are removed from its call at once. There is no ring or approval step before
someone joins. See [Channels](channels.md#private-channels).

## Limits you should know

- **Room size:** 10 people per voice room, and 20 across the whole server.
- **Screen shares:** one per room, two across the server, at up to 2.5 Mbit/s each.
- **Networks:** voice travels over IPv4 through the server's own relay. A network that blocks UDP
  entirely can still connect when the server runs its TCP or TLS relay fallbacks; a network that
  blocks all of those cannot take part. Ask your administrator which applies.
- **Not measured:** these limits are the software's, not a measured capacity, and voice quality on
  every network and device has not been established. See [Known limitations](../known-limitations.md#media).
- **No camera, no recording, not end-to-end encrypted.** The server carries your audio, sothe operator can hear it in principle. See [Privacy and safety](privacy-and-safety.md).

## When voice does not work

See [Troubleshooting](troubleshooting.md#voice-connects-but-nobody-can-hear-anything).
