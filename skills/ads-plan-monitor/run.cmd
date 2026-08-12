@echo off
setlocal
set "PLUGIN_ROOT=%~dp0..\.."
"%PLUGIN_ROOT%\.codex-plugin\bin\ocean-watch_windows_amd64.exe" %*
exit /b %ERRORLEVEL%
