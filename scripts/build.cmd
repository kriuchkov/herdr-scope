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
rem   3. Otherwise, or when that build fails, download this version's release
rem      archive and check it against the release's SHA256SUMS: how the plugin
rem      catalog's plain `herdr plugin install` works without Go. PowerShell
rem      does it with -Command, which no execution policy blocks.
rem   4. Otherwise stop and say how to get one of them.
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
  go build -trimpath -o bin\herdr-scope.exe .\cmd\herdr-scope && exit /b 0
  1>&2 echo herdr-scope: go build failed, it needs Go 1.26 or newer; trying the release binary
)

rem The release download. The script reads the version from the manifest
rem (\x22 is a double quote, which cmd would take for the end of the argument)
rem and the architecture from SCOPE_ARCH.
set "SCOPE_ARCH=%ARCH%"
powershell -NoProfile -NonInteractive -Command "$ErrorActionPreference = 'Stop'; $v = (Select-String -Path 'herdr-plugin.toml' -Pattern '^version = \x22(.+)\x22$' | Select-Object -First 1).Matches[0].Groups[1].Value; $name = 'herdr-scope-' + $v + '-windows-' + $env:SCOPE_ARCH; $url = 'https://github.com/kriuchkov/herdr-scope/releases/download/v' + $v; $work = Join-Path ([IO.Path]::GetTempPath()) ([IO.Path]::GetRandomFileName()); New-Item -ItemType Directory -Path $work | Out-Null; try { [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12; Write-Host ('herdr-scope: downloading ' + $name + '.zip from the v' + $v + ' release'); Invoke-WebRequest -UseBasicParsing -Uri ($url + '/SHA256SUMS') -OutFile (Join-Path $work 'SHA256SUMS'); Invoke-WebRequest -UseBasicParsing -Uri ($url + '/' + $name + '.zip') -OutFile (Join-Path $work ($name + '.zip')); $line = Get-Content (Join-Path $work 'SHA256SUMS') | Where-Object { $_.EndsWith('  ' + $name + '.zip') } | Select-Object -First 1; $got = (Get-FileHash -Algorithm SHA256 -Path (Join-Path $work ($name + '.zip'))).Hash; if (-not $line -or $line.Split(' ')[0] -ne $got) { throw ($name + '.zip does not match the release SHA256SUMS') }; Expand-Archive -Path (Join-Path $work ($name + '.zip')) -DestinationPath $work; New-Item -ItemType Directory -Force -Path 'bin' | Out-Null; Copy-Item -Force -Path (Join-Path $work ($name + '\herdr-scope.exe')) -Destination 'bin\herdr-scope.exe'; $out = & 'bin\herdr-scope.exe' version; if (-not ($out -like ('herdr-scope ' + $v + ' *'))) { throw ('the downloaded binary is not version ' + $v) }; Write-Host ('herdr-scope: installed the v' + $v + ' release binary') } finally { Remove-Item -Recurse -Force -Path $work }" && exit /b 0

1>&2 echo herdr-scope: cannot install: this checkout has no prebuilt binary for
1>&2 echo windows-%ARCH%, no Go toolchain built one, and the release binary could
1>&2 echo not be downloaded. Install a release, which ships prebuilt binaries for
1>&2 echo macOS, Linux and Windows on amd64 and arm64: herdr plugin install kriuchkov/herdr-scope --ref release
1>&2 echo -- or install Go 1.26 or newer from https://go.dev/dl/ and run the install again.
exit /b 1
