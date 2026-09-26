@echo off
setlocal
title BrrpBrrp Setup
if not exist "%~dp0brrpbrrp.exe" (
  echo BrrpBrrp needs all of the files from the release ZIP.
  echo Right-click the ZIP, choose Extract All, then open Setup.cmd in the extracted folder.
  pause
  exit /b 1
)
"%~dp0brrpbrrp.exe" install
if errorlevel 1 (
  echo.
  echo Setup was canceled or could not finish. See the message above.
  echo Keep your existing data folder. You can reopen setup and retry.
  pause
  exit /b 1
)
