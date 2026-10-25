@echo off
rem Log in as the admin: mint a fresh one-time login link and open it in
rem Firefox, so whoever is at this machine is signed in with one double-click.
rem
rem   launchers\login.bat        admin #1, the one the first run created
rem   launchers\login.bat 3      user #3 instead (run.bat manage list-users for ids)
rem
rem The server has to be running (run.bat), because the link is redeemed by it.
rem Minting a link replaces any unused one the account had, and leaves the
rem sessions it already holds alone, so this never logs anyone else out.
rem
rem It uses the dist\mct.exe run.bat last started and does not rebuild.
setlocal
cd /d "%~dp0.."

set "BINARY=%CD%\dist\mct.exe"
set "USER_ID=%~1"
if "%USER_ID%"=="" set "USER_ID=1"

if exist "%BINARY%" goto check_server
echo No dist\mct.exe yet. Start the server with run.bat first.
goto fail

:check_server
"%BINARY%" health >nul 2>&1
if not errorlevel 1 goto mint
echo The server is not answering. Start it with run.bat, then run launchers\login.bat again.
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
if exist "%FIREFOX%" goto open
set "FIREFOX=%ProgramFiles(x86)%\Mozilla Firefox\firefox.exe"
if exist "%FIREFOX%" goto open
set "FIREFOX=%LOCALAPPDATA%\Mozilla Firefox\firefox.exe"
if exist "%FIREFOX%" goto open
set "FIREFOX="
for /f "tokens=2,*" %%a in ('reg query "HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\firefox.exe" /ve 2^>nul ^| findstr /i "REG_SZ"') do set "FIREFOX=%%b"
if defined FIREFOX if exist "%FIREFOX%" goto open
for /f "tokens=2,*" %%a in ('reg query "HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\firefox.exe" /ve 2^>nul ^| findstr /i "REG_SZ"') do set "FIREFOX=%%b"
if defined FIREFOX if exist "%FIREFOX%" goto open

echo Firefox is not installed, so opening the default browser instead.
echo   %URL%
start "" "%URL%"
exit /b 0

:open
echo Logging in with Firefox:
echo   %URL%
start "" "%FIREFOX%" "%URL%"
exit /b 0

:fail
rem Double-clicked rather than run from a console: keep the window open long
rem enough to read why.
echo %CMDCMDLINE% | findstr /i /c:"%~nx0" >nul && pause
exit /b 1
