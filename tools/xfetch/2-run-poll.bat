@echo off
REM ASCII ONLY - see the note in 1-install.bat about cmd.exe and UTF-8.
REM Keep this window open. Closing it stops the fetcher.
REM Also set Power Options -> Sleep -> Never, or the laptop sleeping stops it too.
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

:loop
%PY% xfetch.py poll
echo.
echo [!] fetcher exited (network down or X session expired). Restarting in 10s...
timeout /t 10 >nul
goto loop
