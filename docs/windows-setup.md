# Windows setup

Extract the Windows ZIP and double-click **Setup.cmd**, or run **brrpbrrp.exe**. Accept the normal administrator prompt to open the built-in installation wizard.

Choose the connection type and port (8082 by default), choose an empty data folder, review the settings and select **Install and start**. The wizard checks that the server responds, then walks you through creating the administrator account. It does not require Tailscale or another hosting/VPN provider.

After setup, open **Windows tray controls** or use the BrrpBrrp Start menu shortcut. Right-click the tray icon for settings, certificates, start/restart/stop and exit. LAN installations offer a guided trust action for this computer and a public root certificate for phones and other PCs. [Alpha install, certificate repair and test checklist](alpha-testing.md).

To preserve an existing wizard installation, open setup from the new release folder and choose **Update this installation** after exporting a backup. The old executable is retained for rollback.

See [the full installation walkthrough](installation.md) for local-network certificates, firewall behavior and continuing setup. You can expose the app later through your own domain, certificate, reverse proxy or network arrangement; see [hosting.md](hosting.md).

To start fresh when an older installation is present:

1. Close older setup windows. Extract the **0.3.1 or newer** ZIP into a separate folder, such as a new folder in Downloads. Do not run uninstall from the installed copy in Program Files.
2. Open **Setup.cmd** from the newly extracted folder and accept the administrator prompt.
3. Select **Uninstall an existing version**. This option is also available below installation-conflict messages.
4. Review the application, service, data and settings locations. Select the confirmation checkbox, then **Uninstall and keep data**.
5. Wait for completion. Windows must finish stopping and releasing the service; if asked, close Windows Services and refresh the review before retrying.
6. Choose **Continue with fresh setup**. Setup selects an unused folder (for example, `C:\ProgramData\BrrpBrrp-2`) so the previous database will not block installation. Keep that new folder or select another empty one. Port 8082 remains the default.

Uninstall keeps existing accounts, messages, attachments, backups, certificates and `server.env` in their old locations. A fresh install creates a separate server with a new administrator account. The uninstaller removes only its own local firewall rules; custom network/proxy rules remain under your control.

To continue using the same accounts and messages instead, follow [updates and rollback](deployment.md#updates-and-rollback) to replace only the executable.
