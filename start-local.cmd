@echo off
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0deploy\fork-local\start.ps1"
if errorlevel 1 pause
