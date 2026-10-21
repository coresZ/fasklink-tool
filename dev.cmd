@echo off
rem FastLink-tool one-click dev launcher for Windows.
rem Passes every argument to scripts\dev.ps1, e.g.  dev.cmd -Port 8790 -NoOpen
setlocal
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\dev.ps1" %*
if errorlevel 1 pause
endlocal
