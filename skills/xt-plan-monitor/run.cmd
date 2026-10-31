@echo off
setlocal
set "PLUGIN_ROOT=%~dp0..\.."
set "FALLBACK=%PLUGIN_ROOT%\.codex-plugin\bin\ocean-watch_windows_amd64.exe"
"%FALLBACK%" runtime --plugin-root "%PLUGIN_ROOT%" exec -- %*
exit /b %ERRORLEVEL%
