param([string]$RuntimePath = (Join-Path $PSScriptRoot '..\..\local-runtime'))
$ErrorActionPreference = 'Stop'
$runtimePath = [IO.Path]::GetFullPath($RuntimePath)
$redisRoot = 'D:\software\Redis'
$redisBin = Join-Path $redisRoot 'Redis-8.10.3-Windows-x64-cygwin'
$appExe = Join-Path $runtimePath 'sub2api.exe'
function Test-LocalPort([int]$port) {
    $client = New-Object System.Net.Sockets.TcpClient
    try { $task = $client.ConnectAsync('127.0.0.1', $port); return ($task.Wait(500) -and $client.Connected) }
    catch { return $false }
    finally { $client.Dispose() }
}
if (!(Test-LocalPort 5432)) { throw 'PostgreSQL is not listening on 127.0.0.1:5432.' }
if (!(Test-LocalPort 6379)) {
    $redisProcess = Start-Process -FilePath (Join-Path $redisBin 'redis-server.exe') -ArgumentList './redis-local.conf' -WorkingDirectory $redisRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $redisRoot 'stdout.log') -RedirectStandardError (Join-Path $redisRoot 'stderr.log')
    $redisProcess.Id | Set-Content (Join-Path $redisRoot 'local-process.pid')
    for ($attempt = 0; $attempt -lt 40 -and !(Test-LocalPort 6379); $attempt++) { Start-Sleep -Milliseconds 250 }
    if (!(Test-LocalPort 6379)) { throw 'Redis startup failed. Check its local logs.' }
}
$pong = & (Join-Path $redisBin 'redis-cli.exe') -h 127.0.0.1 -p 6379 PING
if ($pong -ne 'PONG') { throw 'Redis PING failed.' }
$appPidFile = Join-Path $runtimePath 'sub2api.pid'
if (Test-LocalPort 8080) {
    if (!(Test-Path -LiteralPath $appPidFile)) { throw 'Port 8080 is occupied by another process.' }
    $runningApp = Get-Process -Id ([int](Get-Content -LiteralPath $appPidFile -Raw)) -ErrorAction SilentlyContinue
    if (!$runningApp -or $runningApp.Path -ne $appExe) { throw 'Port 8080 is occupied by another process.' }
    Write-Host 'Sub2API is already running: http://127.0.0.1:8080'
    return
}
$env:SERVER_HOST = '127.0.0.1'
$env:SERVER_PORT = '8080'
$env:DATA_DIR = Join-Path $runtimePath 'data'
$env:GIN_MODE = 'release'
$env:ZONEINFO = Join-Path $runtimePath 'zoneinfo.zip'
if (!(Test-Path -LiteralPath $env:ZONEINFO)) { throw 'Timezone data is missing: local-runtime\zoneinfo.zip' }
New-Item -ItemType Directory -Path (Join-Path $runtimePath 'logs') -Force | Out-Null
$appProcess = Start-Process -FilePath $appExe -WorkingDirectory $runtimePath -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $runtimePath 'logs\stdout.log') -RedirectStandardError (Join-Path $runtimePath 'logs\stderr.log')
$appProcess.Id | Set-Content -LiteralPath $appPidFile
for ($attempt = 0; $attempt -lt 480 -and !(Test-LocalPort 8080); $attempt++) {
    $appProcess.Refresh()
    if ($appProcess.HasExited) { throw 'Sub2API exited during startup. Check local-runtime\logs\stderr.log.' }
    Start-Sleep -Milliseconds 250
}
if (!(Test-LocalPort 8080)) { throw 'Sub2API startup timed out. Check local-runtime\logs.' }
Write-Host 'Sub2API: http://127.0.0.1:8080'
