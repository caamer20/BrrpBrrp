# BrrpBrrp 0.4.0-alpha.5

This candidate adds message reporting and review, durable channel slow mode, per-channel notification muting, safe recovery of failed sends, and explicit account removal that preserves conversation history. See the [community guide](community-guide.md) and [release record](releases/0.4.0-alpha.5.md).

**This release upgrades older databases through schema 10; backup format remains 2.** Schemas 3–5 add roles, ownership, durable categories, channel access and session/invitation identity. Schema 6 adds shared conversation descriptors and send receipts. Schemas 7–10 add removed-account identities, slow mode, reports and personal notification preferences. The migration framework applies pending versions transactionally, rebuilds search and checks its postconditions. An interrupted upgrade blocks normal startup until repaired. Existing conversation IDs, message IDs and attachment relationships are retained. **Take an exported backup using the current working executable before updating**, and keep that executable for rollback.

Restricted-channel exposure stays disabled behind `BRRP_ENABLE_RESTRICTED_CHANNELS` pending the privacy gate. Roles and ordinary public-channel moderation work without enabling that gate. Test with a small group and keep an exported backup.

## Install or update

1. Extract the ZIP for your operating system and architecture into a new folder. On Windows, open **Setup.cmd** and accept the administrator prompt.
2. For a new installation, choose the connection type and finish the wizard. Use **My local network** for phones on the same network; **This computer** works only on the host.
3. For an existing wizard installation, export a backup, then open setup from the new release folder. Select **Update this installation**, confirm the brief interruption, and choose **Update and restart**. Your data and settings stay in place. The previous executable is saved under `previous-build-*` in the application folder.
4. If you edited `server.env` manually or used the older script installer, follow [manual updates and rollback](deployment.md#updates-and-rollback). The wizard deliberately refuses to apply stale installation settings.
5. In LAN mode, finish certificate trust below before opening the app on another device. Copy the exact server address from setup.

Updates restart the server and disconnect active calls. This release does change the schema, so take a pre-update backup with the executable you are currently running and keep that executable for rollback. Do not uninstall to update a server whose accounts and messages you want to keep. If the update does not finish, the server refuses to start normally and reports the incomplete upgrade; run `brrpbrrp migrate` and `brrpbrrp verify` against the data folder, or restore the backup you took.

## Fix a local certificate problem

Fresh LAN installations create a separate **BrrpBrrp Local CA** root and a server certificate. Download **BrrpBrrp-local-ca.cer** in setup. It contains only the public trust certificate. Compare its SHA-256 fingerprint with the one displayed in setup before trusting it.

An existing self-signed installation keeps its current certificate until you explicitly select **Repair certificate and restart**. Repair saves the previous certificate and key under `certs/previous-*`. Older self-signed installations receive a new root; each device must trust that root once. An existing local CA is reused when renewing its server certificate or correcting the configured hostname/IP.

| Device | Trust steps |
| --- | --- |
| Windows server | In setup, review the fingerprint, select the trust checkbox, and choose **Trust on this Windows computer**. This installs the displayed certificate in the Local Machine root store for all users. Restart the browser. |
| Another Windows PC | Open the `.cer`, choose Install Certificate → Current User → Place all certificates in the following store → Trusted Root Certification Authorities. Restart the browser. |
| iPhone / iPad | Transfer and open the `.cer`, install its profile under Settings → General → VPN & Device Management, then enable full trust under General → About → Certificate Trust Settings. Reopen Safari at the exact server address. |
| Android | Search Settings for “Install a certificate,” choose **CA certificate**, and select the `.cer`. Device menu names vary; managed devices may block user-installed CAs. Reopen Chrome. |
| macOS | Import the `.cer` in Keychain Access and trust it for SSL, then restart the browser. |
| Linux / Firefox | Import the root into the browser’s Authorities settings or the OS trust store as appropriate. Firefox may have a separate trust store. |

Apple requires the extra full-trust step for manually installed certificate profiles. See [Apple’s instructions](https://support.apple.com/en-us/102390). For Android certificate management, see [Google’s guidance](https://support.google.com/pixelphone/answer/2844832).

The managed server certificate lasts 90 days and renews with the same root when fewer than 30 days remain, at startup or on a TLS handshake checked at most every 12 hours. The root lasts ten years. Protect `root.key` and `tls.key`; never distribute them. Browser trust is per device. Clicking past a security warning is not a replacement for trusting the root. Publicly trusted HTTPS through your own proxy remains an option when installing a private CA is unsuitable.

For a manually managed server in `auto` TLS mode, stop the service, run the new executable with `repair-cert --config /absolute/path/server.env`, then restart it and distribute the trust certificate. Supplied certificates and HTTPS proxies must be maintained through their issuer; repair will not replace them. If the IP changed, update the configured origin/listener/voice IP first and reserve the new address in your router.

## Windows tray

The wizard adds a **BrrpBrrp** Start menu shortcut and a shortcut in the all-users Startup folder. The tray runs in the signed-in desktop session, while the service starts with Windows. Choose **Open Windows tray controls** at the end of setup, or open BrrpBrrp from Start. Windows may place the icon under **Show hidden icons**.

The menu shows service status and offers Open BrrpBrrp, Server settings, Setup & certificates, Start, Restart, Stop, Stop server and exit, and Exit tray (keep server running). Service actions use the normal administrator prompt. Cancelling that prompt leaves the service alone. Stopping the server disconnects everyone; exiting only the tray leaves the service running. A stopped service starts again at the next Windows boot because automatic startup remains enabled.

Use a single interactive desktop session during this first alpha. If multiple users have signed in through RDP, close their tray copies before replacing the executable. Uninstall removes the app’s matching shortcuts and icon but preserves server data and any certificate trust you installed. Remove an obsolete root from your device’s certificate settings when you no longer use that server, checking its fingerprint first.

Run the supplied read-only diagnostic from the extracted release in PowerShell:

```powershell
.\scripts\windows\diagnose-brrpbrrp.ps1
```

It checks the service, configured address, TLS without bypassing validation, local root fingerprint/trust, and tray shortcuts. If script policy blocks it, use setup and the normal Windows tools; no policy bypass is required by setup.

## Alpha rehearsal

Record pass/fail and the OS/browser version for each item. These physical-device and Windows checks are still required; local browser resizing and cross-compilation do not establish them.

- [ ] Fresh Windows install on a standard desktop account; approve UAC, create the administrator, and open the tray. Cancel a separate service-action UAC prompt and confirm no action happened.
- [ ] Use the tray to stop, start and restart. Check sign-in after each start. Test both exit choices, then sign out/in and reboot to confirm the tray and service return at the intended times. Restart Explorer and confirm the icon returns without duplicates.
- [ ] Update an existing 0.3.1 or 0.4.0-alpha.3 wizard installation from the extracted candidate. Take the pre-update backup first, then check the saved executable, existing accounts, messages, search, attachment bytes, second factors and backups after the schema 2 → 10 migration.
- [ ] Compare `brrpbrrp verify-backup` output for the archive taken before the update: it must report the schema *inside* the archive (2), not the schema of the new executable, so the pre-upgrade archive is identifiable as a rollback point.
- [ ] Repair an older self-signed LAN certificate. Confirm old files remain, import the new root on Windows and a phone, restart the browser, and verify there is no TLS warning.
- [ ] At 320–430px phone widths, check channels/members, preferences, server administration, search, replies, reactions and long messages. With a real keyboard open, check that the composer stays above it. Rotate the device, check landscape and Home Screen mode, and verify Return inserts a new line on a touch device while the send button sends.
- [ ] Join voice on two physical devices. Use the phone’s visible mute/deafen/leave controls; test audio playback, room transfer, reconnect, speaker output and a 30-minute call. Test a separate-network route and TURN if your deployment uses them.
- [ ] Invite one additional tester, enable administrator two-factor authentication, make an exported backup, and rehearse restoring to an empty folder.
- [ ] Uninstall the test installation from a separate extracted release. Confirm service/shortcut cleanup and that data and TLS files remain.

Send bug reports with the alpha version, OS/browser, connection mode, expected result, actual result, reproduction steps, and the exact error text. Do not include passwords, setup URLs containing session keys, private keys or full databases.

Release binaries are still unsigned. The LAN HTTPS certificate work does not add Authenticode signing or macOS notarization; normal operating-system download warnings may still appear.

## Local verification

Evidence for this build is recorded in [release acceptance](acceptance.md) for the shipped behaviors and in the repository's remediation ledger for the findings this candidate closes, where each claim names its scenario, evidence level and residual limits. The UI includes keyboard-aware viewport sizing, safe-area padding, touch targets, focus trapping for drawers, multiline composer growth, persistent mobile voice controls, and wrapping/scrolling for narrow admin panels.
