# Run from an elevated PowerShell terminal. Installs but does not start the service.
param([string]$Binary = (Join-Path $PSScriptRoot '..\..\brrpbrrp.exe'))
$ErrorActionPreference = 'Stop'
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
if (-not ([Security.Principal.WindowsPrincipal]$identity).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { throw 'Run PowerShell as Administrator.' }
if (Get-Service BrrpBrrp -ErrorAction SilentlyContinue) { throw 'A BrrpBrrp service already exists. Follow the update instructions.' }
$installDir = Join-Path $env:ProgramFiles 'BrrpBrrp'
$dataDir = Join-Path $env:ProgramData 'BrrpBrrp'
New-Item -ItemType Directory -Force $installDir, $dataDir | Out-Null
# Private data: Administrators, SYSTEM and LocalService only (well-known SIDs work across locales).
& icacls.exe $dataDir /inheritance:r /grant:r '*S-1-5-32-544:(OI)(CI)F' '*S-1-5-18:(OI)(CI)F' '*S-1-5-19:(OI)(CI)M' | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Could not secure the data folder.' }
Copy-Item -LiteralPath $Binary -Destination (Join-Path $installDir 'brrpbrrp.exe')
$configFile = Join-Path $dataDir 'server.env'
if (-not (Test-Path $configFile)) {
  @("BRRP_DATA_DIR=$dataDir", 'BRRP_ADDR=:8082', 'BRRP_BASE_URL=https://localhost:8082', 'BRRP_TLS_MODE=auto', 'BRRP_LOG_FILE=logs/server.log') | Set-Content -Encoding ASCII $configFile
}
& (Join-Path $installDir 'brrpbrrp.exe') service-install --config $configFile
if ($LASTEXITCODE -ne 0) { throw 'Service installation failed.' }
Write-Host "Edit $configFile for your public URL, then run Start-Service BrrpBrrp. Configure firewall ports as described in docs/security.md."
