#requires -Version 5.1
<#
.SYNOPSIS
Read-only BrrpBrrp service, TCP and HTTPS startup checks. Does not change trust,
firewall rules, services or server data. Run as Administrator to read settings.
.PARAMETER ServerUrl
Optional address actually shown in the browser, without a path or setup token.
#>
[CmdletBinding()]
param([string]$ServerUrl = '')
$ErrorActionPreference = 'Stop'

function Check-Server([string]$Address, [string]$Label) {
    $uri = $null
    if (-not [Uri]::TryCreate($Address, [UriKind]::Absolute, [ref]$uri) -or
        $uri.Scheme -notin @('http', 'https') -or $uri.UserInfo -or $uri.Query -or $uri.Fragment -or $uri.AbsolutePath -ne '/') {
        Write-Host "$Label : enter only an http(s) server origin, without a path, credentials or token."
        return
    }
    $origin = $uri.GetLeftPart([UriPartial]::Authority)
    Write-Host "`n$Label : $origin"
    $tcp = New-Object Net.Sockets.TcpClient
    try {
        $pending = $tcp.BeginConnect($uri.DnsSafeHost, $uri.Port, $null, $null)
        if (-not $pending.AsyncWaitHandle.WaitOne(5000)) { throw 'TCP connection timed out after 5 seconds.' }
        $tcp.EndConnect($pending)
        Write-Host 'TCP: reachable'
    } catch {
        Write-Host "TCP: FAILED - $($_.Exception.GetBaseException().Message)"
    } finally { $tcp.Dispose() }
    # Use the platform's certificate validation. Never bypass HTTPS checks.
    try {
        $request = [Net.HttpWebRequest]::Create($origin + '/api/setup/status')
        $request.Method = 'GET'
        $request.Timeout = 10000
        $request.ReadWriteTimeout = 10000
        $request.AllowAutoRedirect = $false
        $request.CachePolicy = New-Object Net.Cache.RequestCachePolicy ([Net.Cache.RequestCacheLevel]::NoCacheNoStore)
        $response = $request.GetResponse()
        try {
            Write-Host "API HTTP status: $([int]$response.StatusCode)"
            if ([int]$response.StatusCode -ge 300 -and [int]$response.StatusCode -lt 400) {
                Write-Host 'Unexpected redirect. Check the proxy and configured server address.'
                return
            }
            $reader = New-Object IO.StreamReader ($response.GetResponseStream())
            try {
                $buffer = New-Object char[] 4096
                $count = $reader.ReadBlock($buffer, 0, $buffer.Length)
                if ($count -eq 0) { throw 'The startup API returned an empty response.' }
                $body = (-join $buffer[0..($count - 1)]) | ConvertFrom-Json
                if ($null -eq $body.setup_required -or $body.setup_required -isnot [bool]) {
                    throw 'The endpoint did not return a BrrpBrrp setup status.'
                }
                Write-Host "API: reachable; setup_required=$($body.setup_required)"
            } finally { $reader.Dispose() }
        } finally { $response.Dispose() }
    } catch {
        Write-Host "API: FAILED - $($_.Exception.Message)"
        $inner = $_.Exception.InnerException
        while ($inner) { Write-Host "  $($inner.Message)"; $inner = $inner.InnerException }
    }
}

Write-Host 'BrrpBrrp read-only connection report'
$service = Get-CimInstance Win32_Service -Filter "Name='BrrpBrrp'"
$configured = ''
if (-not $service) { Write-Host 'Service: NOT INSTALLED' }
else {
    Write-Host "Service: $($service.State); startup=$($service.StartMode); exit_code=$($service.ExitCode); service_exit_code=$($service.ServiceSpecificExitCode)"
    $pattern = '^\s*(?:"[^"]+\.exe"|\S+\.exe)\s+serve\s+--config\s+(?:"(?<config>[^"]+)"|(?<config>\S+))\s*$'
    if ($service.PathName -match $pattern) {
        try {
            $settings = @{}
            foreach ($line in Get-Content -LiteralPath $Matches['config']) {
                if ($line.Trim() -match '^(BRRP_[A-Z_]+)\s*=(.*)$') {
                    $key, $value = $Matches[1], $Matches[2].Trim()
                    if ($value.StartsWith('"')) { $value = ConvertFrom-Json -InputObject $value }
                    elseif ($value.StartsWith("'") -and $value.EndsWith("'")) { $value = $value.Substring(1, $value.Length - 2) }
                    if (-not $settings.ContainsKey($key)) { $settings[$key] = $value }
                }
            }
            foreach ($key in @('BRRP_ADDR', 'BRRP_BASE_URL', 'BRRP_TLS_MODE', 'BRRP_BEHIND_PROXY')) {
                if ($settings.ContainsKey($key)) { Write-Host "$key=$($settings[$key])" }
            }
            $configured = [string]$settings['BRRP_BASE_URL']
            if ($settings.ContainsKey('BRRP_DATA_DIR')) {
                $rootFile = Join-Path $settings['BRRP_DATA_DIR'] 'certs\root.crt'
                if (Test-Path -LiteralPath $rootFile) {
                    try {
                        $pem = [IO.File]::ReadAllText($rootFile)
                        $der = [Convert]::FromBase64String(($pem -replace '-----BEGIN CERTIFICATE-----|-----END CERTIFICATE-----|\s', ''))
                        $rootCert = New-Object Security.Cryptography.X509Certificates.X509Certificate2 -ArgumentList @(,$der)
                        try {
                            $sha = [Security.Cryptography.SHA256]::Create()
                            try { $fingerprint = ([BitConverter]::ToString($sha.ComputeHash($rootCert.RawData))).Replace('-', '').ToLowerInvariant() }
                            finally { $sha.Dispose() }
                            Write-Host "Local CA: $($rootCert.Subject); expires=$($rootCert.NotAfter.ToString('u'))"
                            Write-Host "CA SHA-256: $fingerprint"
                            $trusted = (Test-Path -LiteralPath ('Cert:\LocalMachine\Root\' + $rootCert.Thumbprint)) -or (Test-Path -LiteralPath ('Cert:\CurrentUser\Root\' + $rootCert.Thumbprint))
                            Write-Host "Local CA trusted on this Windows account/computer: $trusted"
                            if (-not $trusted) { Write-Host 'Open setup and use Trust on this Windows computer, then restart the browser. Other devices need the public root certificate too.' }
                        } finally { $rootCert.Dispose() }
                    } catch { Write-Host "Certificate inspection failed: $($_.Exception.Message)" }
                } elseif ($settings['BRRP_TLS_MODE'] -eq 'auto') {
                    Write-Host 'Legacy self-signed certificate: open the current setup wizard and use Repair certificate and restart to create a device-trustable local CA.'
                }
                $log = $settings['BRRP_LOG_FILE']
                if ($log) {
                    if (-not [IO.Path]::IsPathRooted($log)) { $log = Join-Path $settings['BRRP_DATA_DIR'] $log }
                    Write-Host "Server log location: $log"
                }
            }
        } catch { Write-Host "Settings could not be read: $($_.Exception.Message). Try an elevated PowerShell window." }
    } else { Write-Host 'Service command is not in the expected format.' }
}
$shell = New-Object -ComObject WScript.Shell
foreach ($folder in @([Environment]::GetFolderPath('CommonPrograms'), [Environment]::GetFolderPath('CommonStartup'))) {
    $shortcut = Join-Path $folder 'BrrpBrrp.lnk'
    if (Test-Path -LiteralPath $shortcut) {
        $link = $shell.CreateShortcut($shortcut)
        Write-Host "Tray shortcut: $shortcut -> $($link.TargetPath) $($link.Arguments)"
    } else { Write-Host "Tray shortcut missing: $shortcut. Update using the current setup wizard to add desktop controls." }
}
if ($configured) { Check-Server $configured 'Configured server' }
if ($ServerUrl -and $ServerUrl.TrimEnd('/') -ne $configured.TrimEnd('/')) { Check-Server $ServerUrl 'Browser address' }
if (-not $configured -and -not $ServerUrl) { Write-Host 'Run again with -ServerUrl followed by the address shown in the browser.' }
Write-Host "`nNo service, files, firewall rules or certificate trust settings were changed."
Write-Host 'If the API succeeds here but fails in the browser, open the same origin followed by /api/setup/status in that browser and report its exact result. Browsers can have different certificate trust settings.'
