# Troubleshooting

Each entry gives what you see, what to check first, and who can fix it. **Members** usually
can fix the first two checks; anything marked **administrator** needs the server's settings or host.

## I cannot sign in

- **Check the address** you are using matches the one your administrator gave you, including
  `https://` and any port number. Cookies and sign-in are tied to the exact address.
- **Check your password.** Passwords are case-sensitive. After a password change, every device
  signed out, so sign in again.
- **Check your second factor.** Use a current code from your authenticator, or one of your
  recovery codes. A code is valid for a short time, so enter the current one.
- **Too many attempts.** The server refuses further attempts from one address for a few minutes
  after many failures. Wait, then try again. (**Administrator:** see
  [Security](../security.md#application-protections) for the limits and `BRRP_LOGIN_ATTEMPTS`.)
- **Locked out of two-factor.** See [Your account and security](account-and-security.md#lost-your-phone-or-your-codes).

## My invitation link does not work

Invitations expire, after one hour to seven days as the administrator chose, and each creates one
account. Ask for a new one. If the person who created an invitation has since lost the authority it promised,
the invitation grants only the default role.

## My message did not send

Look for **Recovered messages**. If a send could not finish, your text is kept there. **Copy** it,
choose where to paste it, and **Dismiss** the item when you no longer need it. Attachments are not
recovered; attach the files again. See [Messages](messages.md#when-a-message-does-not-send).

Your message may also be waiting for slow mode. The composer tells you how long to wait.

## An upload was refused

The file is over the upload limit, which the administrator can lower. Or the server's storage is
full, or nearly so: the server keeps spare room so it can always write a backup. The message says
how much is in use and what the limit is. Try a smaller file, or ask the administrator.

## I cannot see a channel someone mentioned

The channel is probably restricted, and you do not have access. Ask a channel manager to add you, or
an administrator. The channel does not appear in your list at all, which is deliberate.

## Notifications do not arrive

- **Check the browser's permission** for this site: it must allow notifications.
- **Check this device.** In **Your preferences → Notifications**, **Push status** says whether this
  device can receive them. Use **Check again** after changing the browser's setting.
- **Check the channel is not muted.** A muted channel sends no popups or push.
- **iPhone and iPad:** install the app to the Home Screen first, then turn notifications on from
  inside the installed app.
- Notifications the browser has already delivered cannot be recalled.

## The microphone does not work or is not offered

- **Use HTTPS.** Browsers only offer the microphone to a secure page. Your administrator sets this up.
  On the server computer itself, `localhost` also works.
- **Check the site permission** in the browser settings, then join again.
- **Choose the right device** in **Your preferences → Audio**, and use **Test microphone** to see the
  input level move.
- If the browser has not finished opening the microphone after 30 seconds, select **Retry voice**.
- **Local network installs:** the server's certificate must be trusted on your device. Clicking through a
  browser warning may not be enough. Your administrator has the trust steps.

## Voice connects but nobody can hear anything

- **Check mute and deafen.** You may be muted, or deafened, in which case you hear nothing.
- **Check your speaker** in **Your preferences → Audio**, and select **Play test sound**.
- **Click Enable audio playback** if the room offers it. Browsers block sound until you allow it.
- **Check the network** (administrator): voice needs UDP through the server's firewall and router, or
  the relay fallbacks. A reverse proxy that carries only TCP cannot carry live audio. Test from two
  networks; a network that blocks UDP and the relay fallbacks has no route through this server.
- **Administrator:** check `BRRP_VOICE_EXTERNAL_IP` and the UDP ports 50000–50100 (or your configured range).

## I cannot share my screen

- Only one screen is shared per room, and two across the server. If one is already shared, wait for it to stop.
- Use a browser that supports screen capture; choose the window, tab or screen when asked.
- Sharing the sound is optional and needs **Include the sound your computer is playing** ticked when you start sharing.

## Something is slow or the page is stale

- **Reload the page.** Drafts survive a reload only when **Remember drafts and unsent messages on this
  device** is on.
- **Offline:** the app needs the server to be reachable. It does not keep messages to read offline.
- **An update is ready:** the update notice asks you to refresh. It waits until you are not in a call or
  sending something.

## Administrator: the site works at one address but not another

`BRRP_BASE_URL` must match the browser's address exactly, including the scheme and port. Cookies,
WebSockets and the origin checks all use it. See [Installation](../installation.md#changing-a-setting-after-installation).

## Administrator: behind a reverse proxy

Set `BRRP_TRUSTED_PROXIES` to the proxy's exact network, keep WebSocket upgrades working, and forward the
original host. Otherwise every client appears to come from one address and shares its attempt limit. Bind
the backend to loopback or a private interface. See [Security](../security.md).

## Administrator: the data folder is in use

Stop the service before a backup, restore or recovery. The lock file can remain after shutdown; its presence
alone is normal. Do not delete it to bypass an active lock.

## Administrator: I have lost every administrator password

Stop the server and use `brrpbrrp create-admin --data-dir DATA --username recovery_admin --password-stdin`,
supplying the passphrase on standard input. It adds an account and never overwrites an existing one. See
[Administrators](administrators.md#recovering-access-when-someone-is-locked-out).

## Administrator: a backup shows a critical condition

A backup kept only on this host is not recovery. Configure `BRRP_BACKUP_COPY_DIR` on separate storage that
the service account can write to, and wait for a copy to succeed. The condition clears then. See
[Administrators](administrators.md#backups-and-restore).
