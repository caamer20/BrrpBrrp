# Windows full reset

`reset-brrpbrrp.ps1` permanently deletes the BrrpBrrp installation and server data. This includes administrator accounts, messages, uploads, settings, certificates, logs, and backups stored in the selected data folders. It also stops BrrpBrrp processes, removes the service, and removes the installer's two named firewall rules.

1. Close BrrpBrrp setup and Windows Services. Save the script in a separate folder, such as Downloads, outside the installation/data folders.
2. Open **PowerShell as Administrator** in the folder containing the script.
3. Preview the exact targets:

   ```powershell
   powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\reset-brrpbrrp.ps1 -WhatIf
   ```

4. Run the reset, review the targets, then type `DELETE BRRPBRRP`:

   ```powershell
   powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\reset-brrpbrrp.ps1
   ```

`-ExecutionPolicy Bypass` applies only to this invocation; it does not change the machine's saved policy. Managed organizational policy may still prevent execution.

The script discovers the registered service's settings, installer metadata, `%ProgramFiles%\BrrpBrrp`, and `%ProgramData%\BrrpBrrp` plus numbered data folders such as `BrrpBrrp-2`. It also follows absolute `BRRP_DATA_DIR` values in discovered configuration files. For other old locations, pass explicit paths:

```powershell
.\reset-brrpbrrp.ps1 -DataDirectory 'D:\OldChatData','E:\OldChatData' -WhatIf
.\reset-brrpbrrp.ps1 -ConfigFile 'D:\OldChat\server.env' -WhatIf
```

Remove `-WhatIf` after reviewing the result. Paths from configuration are read as literal data, never executed. Drive roots, standard system/profile folders, and trees containing junctions or symbolic links are rejected. Failures stop cleanup and identify what needs attention; rerun after resolving the reported issue. If Windows says service deletion is pending, close Windows Services or restart Windows before trying again.

## Clear the old browser state before reinstalling

Server cleanup does not remove the browser's cached BrrpBrrp page, service worker, cookies, or local preferences. In your browser's site-data settings, remove data for each old BrrpBrrp server address printed by the script, including any old localhost/IP addresses and ports. Also remove an installed BrrpBrrp web app if you used one, then close its old tabs/windows. This avoids opening an offline cached page that still points to an old server.

Browser profiles, unrelated site data, external backup-copy folders, manually installed certificate trust entries, custom proxy/firewall configuration, and downloaded release ZIPs are not automatically erased. Any external backup location found in settings is printed for separate review. The script cannot discover arbitrary old locations without a configuration record; use `-DataDirectory` for those dedicated BrrpBrrp folders.

Extract the latest release into a separate folder, run `Setup.cmd`, and use the **new server address shown at the end of setup**. For a local-only smoke test, choose **this computer**. For local-network mode, trust the newly generated server certificate on each device as described in the installation guide. A full reset is a troubleshooting step; it does not establish that old data caused the original connection failure.

## Validation

PowerShell syntax, literal configuration parsing (including paths with spaces and duplicate settings), malformed-setting rejection, case-insensitive path deduplication, and preview/confirmation ordering passed local checks. The script has not been executed against a Windows installation; those checks do not constitute a live Windows cleanup test.
