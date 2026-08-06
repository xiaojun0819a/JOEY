@echo off
REM ASCII ONLY - see the note in 1-install.bat about cmd.exe and UTF-8.
REM Connectivity self-check: can the bundled Chromium actually reach x.com?
REM Playwright ships its own Chromium, separate from the Chrome you use daily.
REM Many VPN clients only set the Windows "system proxy", which this separate
REM browser often ignores - the symptom is a blank white page on every site.
REM This script tries a direct connection first, then common local proxy ports.
setlocal
cd /d "%~dp0"

set PY=
where py >nul 2>nul && set PY=py -3
if not defined PY (
  where python >nul 2>nul && set PY=python
)
if not defined PY (
  echo Python not found. Run 1-install.bat first.
  pause
  exit /b 1
)

%PY% xfetch.py doctor
echo.
pause
