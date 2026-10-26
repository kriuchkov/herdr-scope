@echo off
rem Puts the plugin binary at bin\herdr-scope.exe. herdr-plugin.toml runs this
rem as the install build step on Windows, as `cmd /d /c scripts\build.cmd`
rem (/d skips the user's cmd AutoRun hooks); scripts/build.sh is the Unix twin.
rem
rem   1. A release checkout (--ref release or --ref vX.Y.Z-bin) carries
rem      prebuilt binaries as bin\herdr-scope-windows-ARCH.exe: copy this
rem      machine's. An arm64 machine falls back to amd64, which Windows runs
rem      under emulation.
rem   2. Otherwise build from source with Go, which fetches the modules on
rem      the first build.
rem   3. Otherwise stop and say how to get one of the two.
rem
rem Kept free of labels and goto, which cmd can mis-parse in a file checked
rem out with LF line endings, and of parentheses inside echo text in blocks.
setlocal
cd /d "%~dp0.."

rem A 32-bit process on 64-bit Windows sees x86 in PROCESSOR_ARCHITECTURE and
rem the machine's architecture in PROCESSOR_ARCHITEW6432.
set "ARCH=%PROCESSOR_ARCHITECTURE%"
if defined PROCESSOR_ARCHITEW6432 set "ARCH=%PROCESSOR_ARCHITEW6432%"
if /i "%ARCH%"=="AMD64" set "ARCH=amd64"
if /i "%ARCH%"=="ARM64" set "ARCH=arm64"

set "PREBUILT="
if exist "bin\herdr-scope-windows-%ARCH%.exe" set "PREBUILT=bin\herdr-scope-windows-%ARCH%.exe"
if not defined PREBUILT if "%ARCH%"=="arm64" if exist "bin\herdr-scope-windows-amd64.exe" set "PREBUILT=bin\herdr-scope-windows-amd64.exe"

if defined PREBUILT (
  copy /y /b "%PREBUILT%" "bin\herdr-scope.exe" >nul || exit /b 1
  echo herdr-scope: installed the prebuilt %PREBUILT%
  exit /b 0
)

where go >nul 2>nul
if not errorlevel 1 (
  echo herdr-scope: no prebuilt binary for windows-%ARCH%, building from source with Go
  go build -trimpath -o bin\herdr-scope.exe .\cmd\herdr-scope || (
    1>&2 echo herdr-scope: go build failed; building needs Go 1.26 or newer, or install a release: herdr plugin install kriuchkov/herdr-scope --ref release
    exit /b 1
  )
  exit /b 0
)

1>&2 echo herdr-scope: cannot install: this checkout has no prebuilt binary for
1>&2 echo windows-%ARCH% and there is no Go toolchain on PATH to build one. Either
1>&2 echo install a release, which ships prebuilt binaries for macOS, Linux and
1>&2 echo Windows on amd64 and arm64: herdr plugin install kriuchkov/herdr-scope --ref release
1>&2 echo -- or install Go 1.26 or newer from https://go.dev/dl/ and run the install again.
exit /b 1
