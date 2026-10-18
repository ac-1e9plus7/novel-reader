[CmdletBinding()]
param([Parameter(Mandatory=$true)][ValidateSet('start','stop')][string]$Action)
$ErrorActionPreference = 'Stop'
$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$executable = Join-Path $projectRoot 'bin/novel-reader.exe'
$logs = Join-Path $projectRoot 'logs'
$statePath = Join-Path $logs 'server.json'
if ($Action -eq 'stop') {
    if (-not (Test-Path -LiteralPath $statePath)) { Write-Output 'No managed server is running.'; return }
    $state = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
    $running = Get-Process -Id $state.pid -ErrorAction SilentlyContinue
    if ($running) {
        if ($running.Path -ne $executable -or $running.StartTime.ToUniversalTime().Ticks.ToString() -ne $state.started) {
            throw 'Process identity changed; refusing to stop an unrelated process.'
        }
        Stop-Process -Id $running.Id
        $running.WaitForExit(10000) | Out-Null
    }
    Remove-Item -LiteralPath $statePath
    Write-Output 'Server stopped. Incomplete tasks can be resumed next time.'
    return
}
if (-not (Test-Path -LiteralPath $executable)) { throw 'Run scripts/windows/setup.ps1 first.' }
New-Item -ItemType Directory -Force -Path $logs | Out-Null
if (Test-Path -LiteralPath $statePath) {
    $state = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
    $existing = Get-Process -Id $state.pid -ErrorAction SilentlyContinue
    if ($existing -and $existing.Path -eq $executable -and $existing.StartTime.ToUniversalTime().Ticks.ToString() -eq $state.started) {
        Write-Output 'This project already has a managed server running.'; return
    }
}
$stamp = Get-Date -Format 'yyyyMMdd-HHmmss-fff'
$stdout = Join-Path $logs "$stamp.stdout.log"
$stderr = Join-Path $logs "$stamp.stderr.log"
$serverProcess = Start-Process -FilePath $executable -WorkingDirectory $projectRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput $stdout -RedirectStandardError $stderr
$startTicks = $serverProcess.StartTime.ToUniversalTime().Ticks.ToString()
Start-Sleep -Milliseconds 1200
$serverProcess.Refresh()
if ($serverProcess.HasExited) { throw "Server did not start. See $stderr" }
@{pid=$serverProcess.Id;started=$startTicks} | ConvertTo-Json | Set-Content -LiteralPath $statePath -Encoding UTF8
Write-Output "Server started. Default URL: http://127.0.0.1:8787 (or LISTEN_ADDR in .env). Logs: $stderr"
