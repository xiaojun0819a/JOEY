@echo off
REM ASCII ONLY - see the note in 1-install.bat about cmd.exe and UTF-8.
REM Test mode: grabs the newest posts regardless of date and shows what the
REM parser makes of each one, walking further back until it finds posts that
REM actually contain stock picks. Useful on weekends and holidays when nobody
REM is posting new picks but the pipeline still needs verifying.
REM Runs as dryRun on the server: nothing is stored, pushed, or bought.
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

%PY% xfetch.py test --want 2
echo.
pause
