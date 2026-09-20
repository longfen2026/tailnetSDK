# Windows build helper for the tailnet SDK (M0 scope).
#
# Usage:
#   pwsh -File build/build.ps1 build
#   pwsh -File build/build.ps1 test
#   pwsh -File build/build.ps1 cli
#   pwsh -File build/build.ps1 all
[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet('build', 'vet', 'test', 'cli', 'tidy', 'all')]
    [string]$Target = 'all',
    [string]$Configuration = 'default'
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

switch ($Target) {
    'build' { Invoke-Go @('build', './...') }
    'vet' { Invoke-Go @('vet', './...') }
    'test' { Invoke-Go @('test', './...') }
    'tidy' { Invoke-Go @('mod', 'tidy') }
    'cli' { Build-Cli }
    'all' {
        Invoke-Go @('build', './...')
        Invoke-Go @('vet', './...')
        Invoke-Go @('test', './...')
        Build-Cli
    }
}
