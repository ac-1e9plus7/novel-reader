[CmdletBinding()]
param([switch]$Browser)
$ErrorActionPreference = 'Stop'
$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$callerLocation = Get-Location
try {
    Set-Location (Join-Path $projectRoot 'backend')
    & go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'Backend tests failed.' }
    & go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go vet failed.' }
    Set-Location (Join-Path $projectRoot 'frontend')
    & npm run build
    if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed.' }
    if ($Browser) {
        & npm run test:e2e
        if ($LASTEXITCODE -ne 0) { throw 'Browser tests failed.' }
    }
} finally { Set-Location $callerLocation }
