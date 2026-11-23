param([string]$RuntimePath = (Join-Path $PSScriptRoot '..\..\local-runtime'))
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'dependencies.ps1')
$runtimePath = [IO.Path]::GetFullPath($RuntimePath)
$appExe = Join-Path $runtimePath 'sub2api.exe'
Initialize-LocalDependencies
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
