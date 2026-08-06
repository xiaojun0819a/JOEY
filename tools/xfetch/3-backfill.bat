@echo off
REM ASCII ONLY - see the note in 1-install.bat about cmd.exe and UTF-8.
REM Scrolls back through ~6 months of posts. X rate-limits scrolling, so it
REM stops when it can no longer load more. Safe to run again - the server
REM de-duplicates by tweet id, and a second pass picks up what the first missed.
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

%PY% xfetch.py backfill --days 180
echo.
pause
