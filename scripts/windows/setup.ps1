[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$callerLocation = Get-Location
try {
    Set-Location $projectRoot
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw 'Go 1.24+ is required.' }
    if (-not (Get-Command npm -ErrorAction SilentlyContinue)) { throw 'Node.js 24+ and npm are required.' }
    $nodeMajor = [int]((& node --version).TrimStart('v').Split('.')[0])
    if ($nodeMajor -lt 24) { throw 'Node.js 24+ is required.' }
    & npm --prefix frontend ci
    if ($LASTEXITCODE -ne 0) { throw 'Frontend dependency installation failed.' }
    & npm --prefix frontend run build
    if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed.' }
    New-Item -ItemType Directory -Force -Path (Join-Path $projectRoot 'bin') | Out-Null
    Set-Location (Join-Path $projectRoot 'backend')
    & go mod download
    if ($LASTEXITCODE -ne 0) { throw 'Go dependency download failed.' }
    & go build -trimpath -o (Join-Path $projectRoot 'bin/novel-reader.exe') ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw 'Backend build failed.' }
    if (-not (Test-Path -LiteralPath (Join-Path $projectRoot '.env'))) {
        Copy-Item -LiteralPath (Join-Path $projectRoot '.env.example') -Destination (Join-Path $projectRoot '.env')
    }
    Write-Output 'Build complete. Configure .env, then run scripts/windows/start.ps1 start.'
} finally { Set-Location $callerLocation }
