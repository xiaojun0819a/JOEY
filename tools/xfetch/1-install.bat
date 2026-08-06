@echo off
REM ============================================================
REM  X blogger fetcher - first time setup
REM
REM  ASCII ONLY. Do not put Chinese characters in this file.
REM  cmd.exe parses .bat using the system ANSI codepage; a UTF-8
REM  file with Chinese text makes it silently abandon the rest of
REM  the script (the window just flashes and closes, `pause` never
REM  runs). All Chinese output lives in xfetch.py instead.
REM ============================================================
setlocal
cd /d "%~dp0"
set LOG=%~dp0install-log.txt
echo ==== install started %DATE% %TIME% ==== > "%LOG%"

echo.
echo   X blogger fetcher - setup
echo   ------------------------------------
echo.

REM --- locate python: prefer the py launcher, fall back to python ---
set PY=
where py >nul 2>nul && set PY=py -3
if not defined PY (
  where python >nul 2>nul && set PY=python
)
if not defined PY goto nopython

REM Microsoft Store ships a fake python.exe that just opens the Store.
REM Running --version is the only reliable way to tell it apart.
%PY% --version >>"%LOG%" 2>&1
if errorlevel 1 goto nopython
echo [ok] python found:
%PY% --version

echo.
echo [1/4] installing playwright ...
%PY% -m pip install --upgrade pip >>"%LOG%" 2>&1
%PY% -m pip install playwright >>"%LOG%" 2>&1
if errorlevel 1 goto fail

echo [2/4] downloading browser engine (about 150MB, be patient) ...
%PY% -m playwright install chromium >>"%LOG%" 2>&1
if errorlevel 1 goto fail

echo [3/4] server token
echo.
set /p TOKEN="Paste your JOEY token here and press Enter: "
if "%TOKEN%"=="" goto notoken
setx JCP_TOKEN "%TOKEN%" >>"%LOG%" 2>&1
set JCP_TOKEN=%TOKEN%
echo [ok] token saved

echo.
echo [4/4] log in to X in the browser window that opens.
echo.
pause
%PY% xfetch.py login
if errorlevel 1 goto fail

echo.
echo   ------------------------------------
echo   Done. Daily use: 2-run-poll.bat
echo   History backfill: 3-backfill.bat
echo   ------------------------------------
pause
exit /b 0

:nopython
echo.
echo   [X] Python not found.
echo.
echo   Install Python 3.10+ from https://www.python.org/downloads/
echo   IMPORTANT: tick "Add Python to PATH" during install.
echo   Then run this file again.
echo.
echo   (If you installed Python from the Microsoft Store, uninstall it
echo    and use the python.org installer instead - the Store version
echo    cannot be used by scripts.)
echo.
pause
exit /b 1

:notoken
echo.
echo   [X] No token entered. Run this file again.
pause
exit /b 1

:fail
echo.
echo   [X] Setup failed. Details in:
echo       %LOG%
echo   Send that file over and it can be diagnosed.
echo.
pause
exit /b 1
