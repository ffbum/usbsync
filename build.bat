@echo off
setlocal

if defined GOEXE (
  set "GO_CMD=%GOEXE%"
  if not exist "%GO_CMD%" (
    echo GOEXE points to a missing file: %GO_CMD%
    exit /b 1
  )
) else (
  set "GO_CMD=go"
  where go >nul 2>nul
  if errorlevel 1 (
    echo Go not found. Install Go 1.22+ and make sure "go" is in PATH, or set GOEXE.
    exit /b 1
  )
)

"%GO_CMD%" version
if errorlevel 1 exit /b 1

"%GO_CMD%" build -ldflags="-H windowsgui" -o USBSync.exe ./cmd/usbsync
if errorlevel 1 exit /b 1

copy /Y "%~dp0packaging\USBSync.exe.manifest" "%~dp0USBSync.exe.manifest" >nul

echo Build success: %~dp0USBSync.exe
