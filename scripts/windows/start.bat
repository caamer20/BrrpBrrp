@echo off
setlocal
cd /d "%~dp0..\.."
if not defined BRRP_DATA_DIR set "BRRP_DATA_DIR=%CD%\data"
brrpbrrp.exe serve %*
endlocal
