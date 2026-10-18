[CmdletBinding()]
param([int]$Port = 8789)
$ErrorActionPreference = 'Stop'
$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
if (Test-Path -LiteralPath (Join-Path $projectRoot 'logs/server.json')) { throw 'Stop the managed server before running the startup smoke test.' }
$probe = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, $Port)
try { $probe.Start() } finally { $probe.Stop() }
$testData = Join-Path $projectRoot ('tmp/smoke-' + [guid]::NewGuid().ToString('N'))
$previousAddr = $env:LISTEN_ADDR
$previousData = $env:DATA_DIR
$callerLocation = (Get-Location).Path
$started = $false
try {
    $env:LISTEN_ADDR = "127.0.0.1:$Port"
    $env:DATA_DIR = $testData
    & (Join-Path $PSScriptRoot 'start.ps1') start
    $started = $true
    if ((Get-Location).Path -ne $callerLocation) { throw 'Start script changed caller location.' }
    $healthy = $false
    for ($attempt = 0; $attempt -lt 30; $attempt++) {
        try {
            $health = Invoke-RestMethod -Uri "http://127.0.0.1:$Port/api/health" -TimeoutSec 2
            if ($health.status -eq 'ok') { $healthy = $true; break }
        } catch { Start-Sleep -Milliseconds 300 }
    }
    if (-not $healthy) { throw 'Service health check failed.' }
    $response = Invoke-WebRequest -Uri "http://127.0.0.1:$Port/" -UseBasicParsing
    if ($response.StatusCode -ne 200 -or $response.Content -notmatch 'id="root"') { throw 'Frontend was not served.' }
    $books = Invoke-RestMethod -Uri "http://127.0.0.1:$Port/api/books"
    if ($books.Count -ne 0) { throw 'Smoke test did not start with isolated empty data.' }
    & (Join-Path $PSScriptRoot 'start.ps1') stop
    $started = $false
    if ((Get-Location).Path -ne $callerLocation) { throw 'Stop script changed caller location.' }
    Write-Output 'Startup, frontend, SQLite isolation, shutdown and working-directory checks passed.'
} finally {
    if ($started) { & (Join-Path $PSScriptRoot 'start.ps1') stop }
    if ($null -eq $previousAddr) { Remove-Item Env:LISTEN_ADDR -ErrorAction SilentlyContinue } else { $env:LISTEN_ADDR = $previousAddr }
    if ($null -eq $previousData) { Remove-Item Env:DATA_DIR -ErrorAction SilentlyContinue } else { $env:DATA_DIR = $previousData }
}
