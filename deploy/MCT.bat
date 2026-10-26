@echo off
rem Double-click launcher for Windows stations. It sits beside the .files
rem folder build.sh publishes to, and runs .files\mct.exe with the shared
rem .files\config.yaml: stop the copy this PC started last, start a fresh one,
rem and once the server answers, open it in Firefox if Firefox is installed.
rem The Windows twin of MCT.command. Self-contained: nothing else in .files is
rem a script.
rem
rem   MCT.bat                 start the server, plus Firefox once it is up
rem   MCT.bat login           log in as admin #1 in Firefox (server must be up)
rem   MCT.bat login 3         the same, as user #3
rem   MCT.bat <arguments>     run mct.exe with them instead; no browser
rem                           (MCT.bat manage list-users, version, ...)
rem
rem Closing the window (or Ctrl-C) stops the server.
setlocal
rem pushd, not cd /d: opened from a network or VM shared folder (\\server\share,
rem \\Mac\Home), cmd cannot cd to a UNC path and falls back to C:\Windows.
rem pushd maps the share to a drive letter first.
pushd "%~dp0.files" 2>nul
if not errorlevel 1 goto found
echo There is no .files folder next to MCT.bat in %~dp0 -- MCT.bat has to sit beside it.
pause
exit /b 1
:found

set "BINARY=%CD%\mct.exe"
rem Not a parenthesized block: a ")" in the folder path would end it early.
if exist "%BINARY%" goto have_binary
echo No mct.exe in %CD% -- has build.sh published one here?
goto fail
:have_binary

if "%~1"=="" goto server
if /i "%~1"=="login" goto login
"%BINARY%" %*
set "RC=%errorlevel%"
popd
exit /b %RC%

:server
rem Every station runs on the one config.yaml build.sh publishes here. Without
rem it the binary would quietly make a fresh one (and a fresh database) under
rem %USERPROFILE%\.config instead, which is never what a station wants.
if exist config.yaml goto stop_old
echo No config.yaml in %CD% -- build.sh publishes it beside mct.exe.
goto fail

:stop_old
rem Stop the copy this PC started last: any process whose image is this
rem folder's mct.exe. Windows cannot send it a graceful stop from another
rem console, so it is killed; bolt is crash-safe. The path goes to PowerShell
rem through the environment, so no character in it can break the quoting.
set "RUN_EXE=%BINARY%"
powershell -NoProfile -Command "$old = @(Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -eq $env:RUN_EXE }); foreach ($p in $old) { Write-Host ('Stopping the previous server (pid ' + $p.ProcessId + ')...'); Stop-Process -Id $p.ProcessId -Force -ErrorAction SilentlyContinue }; foreach ($p in $old) { Wait-Process -Id $p.ProcessId -Timeout 15 -ErrorAction SilentlyContinue }"

rem Wait in the background for the server started below, then open Firefox at
rem server.port from config.yaml. "The new server" is a process running this
rem folder's mct.exe that started after this launcher did, so the one just
rem stopped never counts; once one exists and its health check answers, it is
rem up. With no Firefox installed this does nothing, and if no server ever
rem starts it gives up after ten minutes.
start "" /B powershell -NoProfile -Command "$t = Get-Date; $port = 5960; $s = $false; foreach ($l in @(Get-Content 'config.yaml' -ErrorAction SilentlyContinue)) { if ($l -match '^server:') { $s = $true; continue }; if ($l -match '^[^\s#]') { $s = $false }; if ($s -and $l -match '^\s+port:\D*(\d+)') { $port = $matches[1]; break } }; $ff = @(($env:ProgramFiles + '\Mozilla Firefox\firefox.exe'), (${env:ProgramFiles(x86)} + '\Mozilla Firefox\firefox.exe'), ($env:LOCALAPPDATA + '\Mozilla Firefox\firefox.exe')); foreach ($k in 'HKLM:', 'HKCU:') { try { $ff += (Get-ItemProperty ($k + '\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\firefox.exe') -ErrorAction Stop).'(default)' } catch {} }; $ff = $ff | Where-Object { $_ -and (Test-Path $_) } | Select-Object -First 1; if (-not $ff) { exit }; while ((Get-Date) -lt $t.AddMinutes(10)) { Start-Sleep -Milliseconds 500; $new = @(Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -eq $env:RUN_EXE -and $_.CreationDate -gt $t }); if ($new.Count -gt 0) { & $env:RUN_EXE health *> $null; if ($LASTEXITCODE -eq 0) { Start-Process $ff ('http://localhost:' + $port + '/'); exit } } }"

rem The banner run.bat prints: a smaller cut of misc\ascii-art-example.txt.
rem The version is the binary's own ("Masters Closet Tracker 2026.10.23-1412
rem (...)"). Colour needs the VT support in Windows 10 and later; NO_COLOR
rem (https://no-color.org) or an older console gets plain text. The ESC trick
rem is the usual one: prompt $E expands to the escape character.
set "VERSION="
for /f "tokens=4" %%v in ('call "%BINARY%" version 2^>nul') do set "VERSION=%%v"
set "PORT=5960"
for /f "delims=" %%p in ('powershell -NoProfile -Command "$s = $false; foreach ($l in @(Get-Content 'config.yaml' -ErrorAction SilentlyContinue)) { if ($l -match '^server:') { $s = $true; continue }; if ($l -match '^[^\s#]') { $s = $false }; if ($s -and $l -match '^\s+port:\D*(\d+)') { $matches[1]; break } }" 2^>nul') do set "PORT=%%p"
set "ART=" & set "BOLD=" & set "DIM=" & set "RESET="
if defined NO_COLOR goto banner
for /f %%e in ('echo prompt $E^| cmd') do set "ESC=%%e"
set "ART=%ESC%[36m" & set "BOLD=%ESC%[1m" & set "DIM=%ESC%[2m" & set "RESET=%ESC%[0m"
:banner
echo.
echo(  %ART%        ----------------%RESET%
echo(  %ART%   --------------------------%RESET%
echo(  %ART%       +----------------+%RESET%
echo(  %ART% ------^|     ^|    ^|     ^|------%RESET%
echo(  %ART%   ----+     ^|    ^|     +----%RESET%
echo(  %ART% ------------^|    ^|------------%RESET%
echo(  %ART%     +-------+    +-------+       %RESET%   %BOLD%MASTER'S CLOSET%RESET%
echo(  %ART%-----^|                    ^|-----  %RESET%   %DIM%tracker%RESET%  %VERSION%
echo(  %ART%     +-------+    +-------+       %RESET%   %DIM%from%RESET%     %CD%
echo(  %ART% ------------^|    ^|------------%RESET%
echo(  %ART%   ----+-----^|    ^|-----+----%RESET%
echo(  %ART%       ^|     ^|    ^|     ^|%RESET%
echo(  %ART% ------+-----+    +-----+------%RESET%
echo(  %ART%             +----+%RESET%
echo(  %ART%      --------------------%RESET%
echo(  %ART%  ----------------------------%RESET%
echo.
echo   %DIM%config%RESET%   .\config.yaml
echo   %DIM%state%RESET%    %USERPROFILE%\.config\masters-closet-tracker\ unless server.app_dir says otherwise
echo   %DIM%open%RESET%     http://localhost:%PORT%/
echo.
echo   On a fresh database the first-run login link is printed below.
echo   Locked out later? %BOLD%MCT.bat login%RESET% opens a fresh admin login in Firefox.
echo.

"%BINARY%"
set "RC=%errorlevel%"
popd
rem The server exiting on its own (a config error, say) would close a
rem double-clicked window before anyone could read why.
if not "%RC%"=="0" pause
exit /b %RC%

:login
rem Mint a fresh one-time login link and open it, so whoever is at this PC is
rem signed in with one command -- what launchers\login.bat does for a checkout.
rem Minting a link replaces any unused one the account had, and leaves the
rem sessions it already holds alone, so this never logs anyone else out.
set "USER_ID=%~2"
if "%USER_ID%"=="" set "USER_ID=1"
"%BINARY%" health >nul 2>&1
if not errorlevel 1 goto mint
echo The server is not answering. Start it with MCT.bat, then run MCT.bat login again.
goto fail

:mint
rem "call" keeps cmd from stripping the quotes around a path with spaces.
set "URL="
for /f "usebackq delims=" %%u in (`call "%BINARY%" manage reissue-login -user-id %USER_ID% -url`) do set "URL=%%u"
if not "%URL%"=="" goto find_firefox
echo Could not mint a login link for user #%USER_ID% -- see the error above.
goto fail

:find_firefox
rem The usual install folders, then wherever the installer registered it.
set "FIREFOX=%ProgramFiles%\Mozilla Firefox\firefox.exe"
if exist "%FIREFOX%" goto open_login
set "FIREFOX=%ProgramFiles(x86)%\Mozilla Firefox\firefox.exe"
if exist "%FIREFOX%" goto open_login
set "FIREFOX=%LOCALAPPDATA%\Mozilla Firefox\firefox.exe"
if exist "%FIREFOX%" goto open_login
set "FIREFOX="
for /f "tokens=2,*" %%a in ('reg query "HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\firefox.exe" /ve 2^>nul ^| findstr /i "REG_SZ"') do set "FIREFOX=%%b"
if defined FIREFOX if exist "%FIREFOX%" goto open_login
for /f "tokens=2,*" %%a in ('reg query "HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\firefox.exe" /ve 2^>nul ^| findstr /i "REG_SZ"') do set "FIREFOX=%%b"
if defined FIREFOX if exist "%FIREFOX%" goto open_login

echo Firefox is not installed, so opening the default browser instead.
echo   %URL%
start "" "%URL%"
popd
exit /b 0

:open_login
echo Logging in with Firefox:
echo   %URL%
start "" "%FIREFOX%" "%URL%"
popd
exit /b 0

:fail
popd
pause
exit /b 1
