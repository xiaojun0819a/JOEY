@echo off
REM ASCII ONLY - see the note in 1-install.bat about cmd.exe and UTF-8.
REM
REM Slow backfill: one blogger at a time, 10 minutes apart.
REM
REM Why not 3-backfill.bat: that one runs all six back to back, and X rate-limits
REM hard partway through - five of the six only reached 2 weeks of history before
REM the timeline stopped loading. Spacing them out trades wall-clock for coverage.
REM
REM Takes several hours. Leave it running unattended; the laptop is always on anyway.
REM Safe to re-run: the server de-duplicates by tweet id, so a second pass only
REM adds what the first one missed.
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

REM Order: thinnest history first, so if it gets cut short the neediest ones are done.
call :one naixiaiwangu
call :one Ferhat31162
call :one shachoo_king
call :one ComMurtadha
call :one GusQuijasTJ
call :one Aw3ff_

echo.
echo ============================================
echo   All six done. Now run 2-run-poll.bat and
echo   leave it running.
echo ============================================
pause
exit /b 0

:one
echo.
echo ==== backfill @%1 ====
%PY% xfetch.py backfill --handle %1 --days 180
echo.
echo     cooling down 10 min before the next one (avoids X rate limiting)...
timeout /t 600 >nul
exit /b 0
