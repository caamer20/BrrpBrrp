$ErrorActionPreference = 'Stop'
Stop-Service BrrpBrrp
& (Join-Path $env:ProgramFiles 'BrrpBrrp\brrpbrrp.exe') service-uninstall
if ($LASTEXITCODE -ne 0) { throw 'Service removal failed.' }
Write-Host 'Service removed. Application, settings and data have been preserved.'
