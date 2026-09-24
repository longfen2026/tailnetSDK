# Windows build helper for the tailnet SDK.
#
# Usage:
#   pwsh -File build/build.ps1 build
#   pwsh -File build/build.ps1 test
#   pwsh -File build/build.ps1 cli
#   pwsh -File build/build.ps1 native          # -> dist\tailnet.dll + dist\tailnet.h
#   pwsh -File build/build.ps1 native -CC C:\tools\w64devkit\w64devkit\bin\gcc.exe
#   pwsh -File build/build.ps1 all
[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet('build', 'vet', 'test', 'cli', 'native', 'tidy', 'all')]
    [string]$Target = 'all',
    [string]$Configuration = 'default',
    # Path to a mingw-w64 gcc (required for -buildmode=c-shared). Auto-detected when empty.
    [string]$CC = ''
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

function Invoke-Go {
    param([string[]]$GoArgs)
    Write-Host "> go $($GoArgs -join ' ')" -ForegroundColor Cyan
    & go @GoArgs
    if ($LASTEXITCODE -ne 0) {
        throw "go $($GoArgs -join ' ') failed with exit code $LASTEXITCODE"
    }
}

function Build-Cli {
    $dist = Join-Path $root 'dist'
    New-Item -ItemType Directory -Force -Path $dist | Out-Null
    Invoke-Go @('build', '-o', (Join-Path $dist 'tailnetctl.exe'), './cli/tailnetctl')
    Write-Host "built $dist\tailnetctl.exe" -ForegroundColor Green
}

# Resolve-CC finds a mingw-w64 gcc: explicit -CC, then $env:CC, then PATH,
# then the well-known portable locations.
function Resolve-CC {
    if ($CC) {
        if (-not (Test-Path $CC)) { throw "-CC not found: $CC" }
        return (Resolve-Path $CC).Path
    }
    if ($env:CC -and (Test-Path $env:CC)) { return (Resolve-Path $env:CC).Path }
    $onPath = Get-Command gcc -ErrorAction SilentlyContinue
    if ($onPath) { return $onPath.Source }
    # w64devkit is often unpacked next to the repo (..\tools\w64devkit) rather
    # than inside it, so check both locations.
    $parent = Split-Path -Parent $root
    $candidates = @()
    if ($env:W64DEVKIT) { $candidates += (Join-Path $env:W64DEVKIT 'bin\gcc.exe') }
    $candidates += @(
        (Join-Path $root 'tools\w64devkit\w64devkit\bin\gcc.exe'),
        (Join-Path $parent 'tools\w64devkit\w64devkit\bin\gcc.exe'),
        (Join-Path $root 'tools\w64devkit\bin\gcc.exe'),
        (Join-Path $parent 'tools\w64devkit\bin\gcc.exe'),
        'C:\msys64\mingw64\bin\gcc.exe',
        'C:\tools\w64devkit\w64devkit\bin\gcc.exe'
    )
    foreach ($cand in $candidates) {
        if (Test-Path $cand) { return (Resolve-Path $cand).Path }
    }
    throw 'no C compiler found. -buildmode=c-shared needs a mingw-w64 gcc; install w64devkit and pass -CC <path> or set $env:CC.'
}

# Build-Native produces the C ABI shared library (dist\tailnet.dll + tailnet.h)
# from bind\ffi, which is its own Go module.
function Build-Native {
    $cc = Resolve-CC
    $ffiDir = Join-Path $root 'bind\ffi'
    $dist = Join-Path $root 'dist'
    New-Item -ItemType Directory -Force -Path $dist | Out-Null
    $dll = Join-Path $dist 'tailnet.dll'
    $hdr = Join-Path $dist 'tailnet.h'
    Write-Host "> (bind/ffi) CGO_ENABLED=1 CC=$cc go build -buildmode=c-shared -ldflags `"-s -w`" -o $dll ." -ForegroundColor Cyan
    Push-Location $ffiDir
    try {
        $env:CGO_ENABLED = '1'
        $env:CC = $cc
        & go build -buildmode=c-shared -ldflags '-s -w' -o $dll .
        if ($LASTEXITCODE -ne 0) { throw "c-shared build failed with exit code $LASTEXITCODE" }
    }
    finally {
        Pop-Location
    }
    foreach ($f in @($dll, $hdr)) {
        if (-not (Test-Path $f)) { throw "expected artifact missing: $f" }
    }
    Write-Host "built $dll + $hdr" -ForegroundColor Green
}

switch ($Target) {
    'build' { Invoke-Go @('build', './...') }
    'vet' { Invoke-Go @('vet', './...') }
    'test' { Invoke-Go @('test', './...') }
    'tidy' { Invoke-Go @('mod', 'tidy') }
    'cli' { Build-Cli }
    'native' { Build-Native }
    'all' {
        Invoke-Go @('build', './...')
        Invoke-Go @('vet', './...')
        Invoke-Go @('test', './...')
        Build-Cli
    }
}
