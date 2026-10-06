# Install BrrpBrrp

BrrpBrrp includes a browser-based installation wizard. It installs the native executable and background service, checks the selected port and storage, starts the server, and creates the first administrator account. All setup assets are included in the package. No VPN, hosting account, database service, Go or Node.js installation is required.

## Start the wizard

Extract the release ZIP into a folder on the server first. Keep all extracted files together while starting setup.

| Platform | Start setup | Background behavior |
| --- | --- | --- |
| Windows x64 / ARM64 | Double-click **Setup.cmd**, or **brrpbrrp.exe**. Accept the normal administrator prompt. | A Windows service starts automatically, including after a reboot. |
| Linux x64 / ARM64 with systemd | Run `chmod +x brrpbrrp`, then `sudo ./brrpbrrp install`. | A dedicated `brrpbrrp` service account runs the system service. |
| macOS Intel / Apple silicon | Open **Setup.command**, or run `./brrpbrrp install` in Terminal. Run as your normal user, without sudo. | A launch agent starts when that user signs in. |

The wizard opens in the browser on that computer. Keep its terminal window open until setup finishes. The setup URL contains a private session key; do not share it. If the browser does not open, copy the displayed URL into a browser on the server. Setup expires after one hour; an already installed background service keeps running.

For a headless Linux machine, use `sudo ./brrpbrrp install --no-browser`. Forward the displayed localhost port through your own SSH connection, keeping the same local port, and open the displayed URL locally. This does not publish the installer on your network.

These release packages are not code-signed. Follow your operating system's normal process for opening an application you have verified. The installer does not disable execution policies, Gatekeeper, certificate validation or the firewall.

## Walk through setup

1. **Connection:** choose who can reach the server. The default port is **8082**, and you can change it. Setup checks for a conflict before installing.
2. **Storage:** choose an empty, dedicated data folder on a local disk. The wizard displays the application location and how its background service runs. It refuses to overwrite an existing database or executable.
3. **Review:** review the address, locations, available disk space and voice ports. Select **Install and start**. Progress shows actual installation steps and verifies that the database and server respond.
4. **Administrator:** choose a server name, username and password. The wizard handles the local first-run token for you. There are no default login credentials.
5. **Ready:** complete device certificate trust for LAN mode, then open the app and sign in. On Windows, choose **Open Windows tray controls**; setup also adds a Start menu shortcut and starts the tray at sign-in. Enable two-factor authentication, invite another person, test voice and configure backups.

## Connection choices

### This computer

The server listens on `127.0.0.1:8082` by default. Open `http://127.0.0.1:8082` on the server itself. The dedicated `http-local` mode only accepts a loopback listener and matching loopback URL. It cannot be configured as a plain HTTP listener on your LAN or public interface.

Browsers treat localhost as a trustworthy origin for microphone access. Other devices cannot connect in this mode. See [MDN's secure-context explanation](https://developer.mozilla.org/en-US/docs/Web/Security/Defenses/Secure_Contexts).

### My local network

Select the server's private IPv4 address. The app listens on that address using HTTPS, creates a local certificate authority and server certificate, and offers its public root as a `.cer` download. Use the wizard’s **Trust on this Windows computer** action on the host, or follow its device-specific instructions to trust the root on each device before using voice; clicking through a browser warning may still leave microphone access unavailable. No private key is downloaded.

Reserve the selected address in your router's DHCP settings so the URL stays stable. The server certificate lasts 90 days and renews automatically under the same local root. The root lasts ten years. Existing self-signed installations can use **Repair certificate and restart** to migrate, keeping the previous keys for recovery. [Device trust and repair steps](alpha-testing.md#fix-a-local-certificate-problem). [Certificate options](security.md).

On Windows, setup can add firewall rules for the selected TCP port and UDP 50000–50100, restricted to the local subnet on Private/Domain network profiles. It does not open Public-profile access. On Linux/macOS, allow these ports from your local network using the host firewall you manage. Router port forwarding is not part of local installation.

### My own HTTPS proxy

Enter the exact public HTTPS origin and the CIDRs of proxies you trust. The app's HTTP listener stays on `127.0.0.1` at your chosen port. Configure your proxy to connect there, including WebSocket forwarding. You can create the administrator locally before the public proxy is ready.

The wizard does not install or configure a proxy, domain, tunnel, router or third-party service. The [hosting guide](hosting.md) covers the app settings and network requirements for your preferred approach.

## Continuing or repairing setup

If service registration or startup fails after files have been installed, setup preserves your files and offers **Check and continue**. It retries registration, service startup and the health check. Reopening the wizard also recognizes an installation it created and can continue first-time account setup. After manually editing `server.env`, use your operating system's service controls; the wizard no longer resumes the old settings.

To keep the same server and accounts, open setup from the newly extracted release and use **Update this installation**. It preserves data/settings, saves the previous executable, and restarts the service. Export a backup first. Manually changed settings and legacy script installations use the [manual update procedure](deployment.md#updates-and-rollback).

## Changing a setting after installation

The wizard writes the settings it asked about into `server.env`, next to the data folder. Every
other key is optional and simply absent; the server uses its built-in default for each.

| Platform | `server.env` |
| --- | --- |
| Windows (wizard, or `scripts\windows\install.ps1`) | `%ProgramData%\BrrpBrrp\server.env` |
| Linux (wizard) | `/var/lib/brrpbrrp/server.env` |
| Linux (legacy `scripts/linux/install.sh`) | `/etc/brrpbrrp/server.env` |
| macOS | `~/Library/Application Support/BrrpBrrp/data/server.env` |

To change or add one:

1. Stop the service.
2. Open that file in an editor with the rights to write it — on Windows and Linux that usually
   means an administrator editor.
3. Add `BRRP_NAME=value`, or change the line that is already there. Values are literal: there is
   no shell expansion, and on Windows a backslash inside a double-quoted value is an escape
   character, so prefer unquoted paths.
4. Start the service again. The file is read once at startup, so a change takes effect then and
   not before.

[.env.example](../.env.example) lists every key the server reads, with its default and a
sentence on what it changes, and [deployment.md](deployment.md#configuration) describes the same
settings in the context of a running service. Settings the server keeps in its database — the
server name, upload limit, TURN configuration, retention and the backup schedule — are changed
in **Manage server → Settings** instead, and take effect without a restart. The two feature
gates, `BRRP_BACKUP_COPY_DIR` and the two alert keys are deliberately host configuration only:
there is no way to set them from the browser, so nobody who can use the admin panel can turn a
feature on or redirect an installation's alerts.

## Uninstall an old version and start fresh

Open setup from a newly extracted release, outside the installed application folder. Choose **Uninstall an existing version**, review the displayed locations and confirm **Uninstall and keep data**. This handles the standard Windows, Linux and macOS locations used by both the wizard and the older installation scripts, including a service that failed to start. A custom data directory is read from the existing settings file.

The wizard stops and unregisters the background service, removes the old executable and setup record, and keeps all data and configuration files in place. It never recursively deletes the data folder. Windows rules created by the wizard are removed only when they still refer to that executable. Custom firewall rules, hosting settings and the Linux service account are preserved.

After completion, choose **Continue with fresh setup**. Setup suggests an unused data folder, such as `BrrpBrrp-2`, so the old database no longer causes a conflict. You can change it in the Storage step. The new server starts with a new administrator account; the previous data remains available for recovery using the saved configuration and a compatible executable.

If another BrrpBrrp process is using the old data, close that process before retrying. If setup itself is running from the installed executable, close it and open the copy in the new release folder first. Unusual service commands or application locations need manual removal through the operating system's service manager. See the [Windows walkthrough](windows-setup.md) for its exact steps.

Use `brrpbrrp install --preview` to inspect the wizard without installing, uninstalling, starting services or creating accounts. For manual/automated deployments, `brrpbrrp serve --config /absolute/path/server.env` remains available.
