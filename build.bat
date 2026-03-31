@echo off
setlocal
set GOEXE=%~dp0tools\go1.22.12\go\bin\go.exe
if not exist "%GOEXE%" (
  echo Go toolchain not found at %GOEXE%
  exit /b 1
)
"%GOEXE%" build -ldflags="-H windowsgui" -o USBSync.exe ./cmd/usbsync
if errorlevel 1 exit /b 1
copy /Y "%~dp0packaging\USBSync.exe.manifest" "%~dp0USBSync.exe.manifest" >nul
