#requires -Version 5.1
#requires -RunAsAdministrator
<#
.SYNOPSIS
Permanently removes the Windows BrrpBrrp service, installation and server data.
.DESCRIPTION
Run from a separate extracted release folder, with setup windows closed.
Use -WhatIf to review without making changes. Normal execution requires typing
DELETE BRRPBRRP. Accounts, messages, uploads, logs and backups inside the listed
folders are permanently deleted. Browser profiles and external backups are not
removed. Clear site data for the printed server addresses in your browser too.
.PARAMETER DataDirectory
Additional old BrrpBrrp data folders outside the default ProgramData locations.
.PARAMETER ConfigFile
Additional server.env files from custom installations to inspect.
.EXAMPLE
.\reset-brrpbrrp.ps1 -WhatIf
.EXAMPLE
.\reset-brrpbrrp.ps1
.EXAMPLE
.\reset-brrpbrrp.ps1 -DataDirectory 'D:\ChatData' -WhatIf
#>
[CmdletBinding(SupportsShouldProcess = $true, ConfirmImpact = 'Medium')]
param([string[]]$DataDirectory = @(), [string[]]$ConfigFile = @())

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$folders = New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::OrdinalIgnoreCase)
$files = New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::OrdinalIgnoreCase)
$configs = New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::OrdinalIgnoreCase)
$origins = New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::OrdinalIgnoreCase)
$externalBackups = New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::OrdinalIgnoreCase)

function Full-Path([string]$Path) {
    if ([string]::IsNullOrWhiteSpace($Path) -or $Path -notmatch '^[A-Za-z]:[\\/]' -or $Path.Substring(2).Contains(':')) {
        throw "Expected a local absolute path, got: $Path"
    }
    return [IO.Path]::GetFullPath($Path).TrimEnd('\', '/')
}
function Assert-SafePath([string]$Path) {
    $full = Full-Path $Path
    $protected = @($env:SystemRoot, $env:ProgramFiles, ${env:ProgramFiles(x86)}, $env:ProgramData,
        $env:USERPROFILE, $env:PUBLIC, $env:APPDATA, $env:LOCALAPPDATA,
        (Join-Path $env:SystemDrive 'Users'), (Join-Path $env:USERPROFILE 'Documents'),
        (Join-Path $env:USERPROFILE 'Desktop'), (Join-Path $env:USERPROFILE 'Downloads'))
    if ($full -match '^[A-Za-z]:$') { throw "Refusing a drive root: $full" }
    foreach ($item in $protected) {
        if ($item) {
            $safe = [IO.Path]::GetFullPath($item).TrimEnd('\', '/')
            if ($safe -eq $full -or $safe.StartsWith($full + '\', [StringComparison]::OrdinalIgnoreCase)) {
                throw "Refusing a system/profile folder or its parent: $full"
            }
        }
    }
    if ($full.StartsWith($env:SystemRoot.TrimEnd('\') + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing a path inside Windows: $full"
    }
    # Reject links and junctions in the path, including its parents.
    $probe = $full
    while ($probe) {
        if (Test-Path -LiteralPath $probe) {
            $entry = Get-Item -LiteralPath $probe -Force
            if ($entry.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw "Refusing a link/junction: $probe" }
        }
        $probe = Split-Path -Parent $probe
    }
    return $full
}
function Add-Folder([string]$Path) {
    $full = Assert-SafePath $Path
    if (Test-Path -LiteralPath $full) {
        if (-not (Test-Path -LiteralPath $full -PathType Container)) { throw "Expected a folder: $full" }
        [void]$folders.Add($full)
        $config = Join-Path $full 'server.env'
        if (Test-Path -LiteralPath $config -PathType Leaf) { [void]$configs.Add($config) }
    }
}
function Read-Settings([string]$Path) {
    $values = @{}
    foreach ($line in Get-Content -LiteralPath $Path) {
        $text = $line.Trim()
        if (-not $text -or $text.StartsWith('#')) { continue }
        if ($text -notmatch '^(BRRP_[A-Z_]+)\s*=(.*)$') { throw "Invalid settings in $Path" }
        $key, $value = $Matches[1], $Matches[2].Trim()
        if ($value.StartsWith('"')) { $value = ConvertFrom-Json -InputObject $value }
        elseif ($value.StartsWith("'")) {
            if (-not $value.EndsWith("'") -or $value.Length -lt 2) { throw "Invalid quoted setting in $Path" }
            $value = $value.Substring(1, $value.Length - 2)
        }
        if (-not $values.ContainsKey($key)) { $values[$key] = $value }
    }
    return $values
}
function Assert-Tree([string]$Path) {
    [void](Assert-SafePath $Path)
    foreach ($entry in Get-ChildItem -LiteralPath $Path -Force) {
        if ($entry.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw "Refusing a tree containing a link/junction: $($entry.FullName)" }
        if ($entry.PSIsContainer) { Assert-Tree $entry.FullName }
    }
}

$service = Get-CimInstance Win32_Service -Filter "Name='BrrpBrrp'"
if ($service) {
    # Accept only the application's known service command; never execute its text.
    $pattern = '^\s*(?:"(?<bin>[^"]+\.exe)"|(?<bin>\S+\.exe))\s+serve\s+--config\s+(?:"(?<config>[^"]+)"|(?<config>\S+))\s*$'
    if ($service.PathName -notmatch $pattern) { throw 'Unrecognized BrrpBrrp service command. No changes made. Inspect it in Windows Services.' }
    $binary = Assert-SafePath $Matches['bin']
    $config = Assert-SafePath $Matches['config']
    if ([IO.Path]::GetFileName($binary) -ine 'brrpbrrp.exe') { throw 'The BrrpBrrp service points to another executable. No changes made.' }
    [void]$files.Add($binary)
    [void]$configs.Add($config)
}
foreach ($root in @($env:ProgramFiles, ${env:ProgramFiles(x86)})) {
    if (-not $root) { continue }
    $install = Join-Path $root 'BrrpBrrp'
    Add-Folder $install
    $metadata = Join-Path $install 'installation.json'
    if (Test-Path -LiteralPath $metadata -PathType Leaf) {
        $plan = Get-Content -LiteralPath $metadata -Raw | ConvertFrom-Json
        Add-Folder ([string]$plan.paths.data_dir)
        [void]$configs.Add((Assert-SafePath ([string]$plan.paths.config)))
    }
}
foreach ($entry in Get-ChildItem -LiteralPath $env:ProgramData -Directory -Force) {
    if ($entry.Name -match '^BrrpBrrp(?:-\d+)?$') { Add-Folder $entry.FullName }
}
foreach ($path in $DataDirectory) { Add-Folder $path }
foreach ($path in $ConfigFile) { [void]$configs.Add((Assert-SafePath $path)) }
$read = New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::OrdinalIgnoreCase)
do {
    $pending = @($configs | Where-Object { -not $read.Contains($_) })
    foreach ($path in $pending) {
        [void]$read.Add($path)
        [void](Assert-SafePath $path)
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { continue }
        $settings = Read-Settings $path
        [void]$files.Add($path)
        if ($settings.ContainsKey('BRRP_DATA_DIR')) { Add-Folder $settings['BRRP_DATA_DIR'] }
        else { throw "BRRP_DATA_DIR is missing in $path. Specify its absolute path in that file before cleanup." }
        if ($settings.ContainsKey('BRRP_BASE_URL')) { [void]$origins.Add($settings['BRRP_BASE_URL']) }
        if ($settings.ContainsKey('BRRP_BACKUP_COPY_DIR') -and $settings['BRRP_BACKUP_COPY_DIR']) {
            [void]$externalBackups.Add($settings['BRRP_BACKUP_COPY_DIR'])
        }
    }
} while ($pending.Count -gt 0)

$targets = @($folders | Sort-Object)
foreach ($path in $targets) {
    if ($PSCommandPath.StartsWith($path + '\', [StringComparison]::OrdinalIgnoreCase)) { throw "Move this script outside $path before running it." }
    Assert-Tree $path
}
foreach ($path in $files) { [void](Assert-SafePath $path) }
$processes = @(Get-CimInstance Win32_Process -Filter "Name='brrpbrrp.exe'")
$rules = @(Get-NetFirewallRule | Where-Object { $_.Name -in @('BrrpBrrp-Local-Web', 'BrrpBrrp-Local-Voice') })
Write-Host "`nBrrpBrrp FULL RESET - permanently deletes accounts, messages, uploads, settings and local backups." -ForegroundColor Yellow
Write-Host 'Service: BrrpBrrp (if installed)'
foreach ($process in $processes) { Write-Host "Stop process $($process.ProcessId): $($process.ExecutablePath)" }
foreach ($path in $targets) { Write-Host "DELETE folder and all contents: $path" }
foreach ($path in $files) { Write-Host "DELETE file (if present): $path" }
foreach ($rule in $rules) { Write-Host "DELETE firewall rule: $($rule.Name)" }
foreach ($origin in $origins) { Write-Host "Clear browser site data separately for: $origin" }
foreach ($path in $externalBackups) { Write-Host "External backup location (not automatically deleted): $path" }
if ($WhatIfPreference) { Write-Host "`nPreview only. Nothing was changed."; return }
if ((Read-Host 'Type DELETE BRRPBRRP to permanently remove the listed items') -cne 'DELETE BRRPBRRP') { Write-Host 'Cancelled. Nothing was changed.'; return }
if (-not $PSCmdlet.ShouldProcess('The listed BrrpBrrp service, processes, rules, files and folders', 'Permanently reset BrrpBrrp')) { return }

if ($service) {
    $current = Get-CimInstance Win32_Service -Filter "Name='BrrpBrrp'"
    if ($current -and $current.PathName -ne $service.PathName) { throw 'The service changed since review. Run the script again.' }
    $controller = Get-Service -Name BrrpBrrp -ErrorAction SilentlyContinue
    if ($controller) {
        try {
            if ($controller.Status -ne 'Stopped') {
                $controller.Stop()
                $controller.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(30))
            }
        } finally { $controller.Dispose() }
    }
    & "$env:SystemRoot\System32\sc.exe" delete BrrpBrrp
    if ($LASTEXITCODE -notin @(0, 1060, 1072)) { throw 'Windows could not remove the service. Files have not been deleted.' }
    $deadline = [DateTime]::UtcNow.AddSeconds(15)
    while (Get-CimInstance Win32_Service -Filter "Name='BrrpBrrp'") {
        if ([DateTime]::UtcNow -gt $deadline) { throw 'Service deletion is pending. Close Windows Services and setup windows, or restart Windows, then run this script again. Files have not been deleted.' }
        Start-Sleep -Milliseconds 250
    }
}
foreach ($process in $processes) {
    $current = Get-CimInstance Win32_Process -Filter "ProcessId=$($process.ProcessId)"
    if ($current -and $current.Name -ieq 'brrpbrrp.exe' -and $current.CreationDate -eq $process.CreationDate) {
        Stop-Process -Id $current.ProcessId -Force
        Wait-Process -Id $current.ProcessId -Timeout 15 -ErrorAction SilentlyContinue
    }
}
if (Get-CimInstance Win32_Process -Filter "Name='brrpbrrp.exe'") { throw 'A BrrpBrrp process is still running. Close it and retry; files have not been deleted.' }
foreach ($rule in $rules) { Remove-NetFirewallRule -Name $rule.Name }
foreach ($path in $files) {
    [void](Assert-SafePath $path)
    if (Test-Path -LiteralPath $path -PathType Leaf) { Remove-Item -LiteralPath $path -Force }
}
foreach ($path in ($targets | Sort-Object Length -Descending)) {
    if (Test-Path -LiteralPath $path) {
        Assert-Tree $path
        Remove-Item -LiteralPath $path -Recurse -Force
    }
}
foreach ($path in @($targets) + @($files)) {
    if (Test-Path -LiteralPath $path) { throw "Cleanup incomplete: $path still exists." }
}
Write-Host "`nBrrpBrrp service and listed server files have been removed." -ForegroundColor Green
Write-Host 'Clear browser site data for every old BrrpBrrp address (including its port), then close the old tabs.'
Write-Host 'Extract the latest release to a separate folder, run Setup.cmd, and open the NEW server address shown by setup.'
