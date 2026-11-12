param(
    [string]$RuntimePath = (Join-Path $PSScriptRoot '..\..\local-runtime'),
    [string]$PythonExe = 'D:\software\Python\Python313\python.exe',
    [string]$PgBin = 'D:\software\PostgreSQL\18\bin',
    [switch]$Check
)
$ErrorActionPreference = 'Stop'
$RuntimePath = [IO.Path]::GetFullPath($RuntimePath)
$appExe = Join-Path $RuntimePath 'sub2api.exe'
$pidFile = Join-Path $RuntimePath 'sub2api.pid'
$lock = $null
function Get-ManagedProcess {
    if (!(Test-Path -LiteralPath $pidFile)) { return $null }
    $savedProcessId = [int](Get-Content -LiteralPath $pidFile -Raw)
    $managed = Get-Process -Id $savedProcessId -ErrorAction SilentlyContinue
    if ($managed -and $managed.Path -ne $appExe) { throw 'Saved PID belongs to another executable; refusing to stop it.' }
    return $managed
}
function Stop-ManagedProcess {
    $managed = Get-ManagedProcess
    if ($managed) {
        Stop-Process -Id $managed.Id -ErrorAction Stop
        if (!$managed.WaitForExit(10000)) { throw 'Sub2API did not stop in time.' }
    }
}
function Read-Database([string]$action, [string]$directory) {
    $arguments = @((Join-Path $PSScriptRoot 'database.py'), $action, '--runtime', $RuntimePath, '--pg-bin', $PgBin)
    if ($directory) { $arguments += @('--directory', $directory) }
    $json = & $PythonExe @arguments
    if ($LASTEXITCODE -ne 0) { throw 'Database preflight/backup failed; current release will not be replaced.' }
    return ($json | Out-String | ConvertFrom-Json)
}
function Confirm-Health([string]$expectedVersion) {
    $health = Invoke-RestMethod 'http://127.0.0.1:8080/health' -TimeoutSec 10
    if ($health.status -ne 'ok') { throw 'Health verification failed.' }
    if ($expectedVersion) {
        $settings = Invoke-RestMethod 'http://127.0.0.1:8080/api/v1/settings/public' -TimeoutSec 10
        if ($settings.data.version -ne $expectedVersion) { throw 'Running version does not match the published build.' }
    }
}
try {
    $lock = [IO.File]::Open((Join-Path $RuntimePath 'update.lock'), 'OpenOrCreate', 'ReadWrite', 'None')
    $managed = Get-ManagedProcess
    $manifestPath = Join-Path $RuntimePath 'latest-build.json'
    $manifest = $null
    $candidate = $null
    $needsUpgrade = $false
    $pendingMigrations = @()
    if (Test-Path -LiteralPath $manifestPath) {
        $manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
        if ($manifest.version -notmatch '^[a-zA-Z0-9._-]+$' -or $manifest.executable -notmatch '^releases/[a-f0-9]{32}/sub2api\.exe$' -or $manifest.sha256 -notmatch '^[a-fA-F0-9]{64}$') {
            throw 'Invalid pending build manifest.'
        }
        $candidate = Join-Path $RuntimePath $manifest.executable
        if ((Get-FileHash -LiteralPath $candidate -Algorithm SHA256).Hash -ne $manifest.sha256) {
            throw 'Pending executable checksum failed; current service is unchanged.'
        }
        $versionText = (& $candidate -version 2>&1 | Out-String)
        if ($LASTEXITCODE -ne 0 -or $versionText -notmatch ('\bSub2API\s+' + [regex]::Escape($manifest.version) + '\s+\(commit:')) {
            throw 'Pending executable version failed verification.'
        }
        if (!$manifest.migrations -or @($manifest.migrations | Where-Object { $_ -notmatch '^\d+[a-zA-Z]*_[a-zA-Z0-9_]+\.sql$' }).Count) {
            throw 'Invalid migration manifest.'
        }
        $needsUpgrade = !(Test-Path -LiteralPath $appExe) -or (Get-FileHash -LiteralPath $appExe -Algorithm SHA256).Hash -ne $manifest.sha256
    }
    $listeners = @(Get-NetTCPConnection -LocalPort 8080 -State Listen -ErrorAction SilentlyContinue)
    foreach ($listener in $listeners) {
        if (!$managed -or $listener.OwningProcess -ne $managed.Id) {
            throw 'Port 8080 belongs to an unmanaged process; refusing to change the service.'
        }
    }
    if ($needsUpgrade) {
        $snapshot = Read-Database 'inspect' ''
        $pendingMigrations = @($manifest.migrations | Where-Object { $_ -notin $snapshot.migrations })
    }
    if ($Check) {
        [ordered]@{ check = 'ok'; running_pid = $(if ($managed) { $managed.Id } else { $null }); published_version = $(if ($manifest) { $manifest.version } else { $null }); upgrade_on_restart = $needsUpgrade; pending_migrations = $pendingMigrations } | ConvertTo-Json -Depth 4
        return
    }
    $backupPath = $null
    $candidateStarted = $false
    $replaced = $false
    if ($needsUpgrade) {
        $backupPath = Join-Path $RuntimePath ("backups\" + (Get-Date -Format 'yyyyMMdd-HHmmss') + '-' + [Guid]::NewGuid().ToString('N').Substring(0, 8))
        New-Item -ItemType Directory -Path $backupPath | Out-Null
        if (Test-Path -LiteralPath $appExe) { Copy-Item -LiteralPath $appExe -Destination (Join-Path $backupPath 'sub2api.exe') }
        Copy-Item -LiteralPath (Join-Path $RuntimePath 'data') -Destination (Join-Path $backupPath 'data') -Recurse
        Copy-Item -LiteralPath $manifestPath -Destination (Join-Path $backupPath 'candidate.json')
        Write-Host "Activating $($manifest.version). Backup: $backupPath"
    }
    Stop-ManagedProcess
    try {
        if ($needsUpgrade) {
            # Back up after stopping the writer, before the new program can migrate.
            $snapshot = Read-Database 'backup' $backupPath
            $pendingMigrations = @($manifest.migrations | Where-Object { $_ -notin $snapshot.migrations })
            $replaced = $true
            Copy-Item -LiteralPath $candidate -Destination $appExe -Force
            if ((Get-FileHash -LiteralPath $appExe -Algorithm SHA256).Hash -ne $manifest.sha256) { throw 'Installed executable checksum failed.' }
        }
        $candidateStarted = $true
        & (Join-Path $PSScriptRoot 'start.ps1') -RuntimePath $RuntimePath
        Confirm-Health $(if ($manifest) { $manifest.version } else { '' })
        if ($needsUpgrade) {
            $after = Read-Database 'inspect' ''
            if (@($manifest.migrations | Where-Object { $_ -notin $after.migrations }).Count) { throw 'Some bundled migrations were not applied.' }
            Write-Host "Activated $($manifest.version); health and migrations verified."
        } else {
            Write-Host 'Restarted the latest installed build; health verified.'
        }
    } catch {
        if ($needsUpgrade) {
            Stop-ManagedProcess
            $oldExe = Join-Path $backupPath 'sub2api.exe'
            if ((!$candidateStarted -or !$pendingMigrations.Count) -and (Test-Path -LiteralPath $oldExe)) {
                if ($replaced) { Copy-Item -LiteralPath $oldExe -Destination $appExe -Force }
                & (Join-Path $PSScriptRoot 'start.ps1') -RuntimePath $RuntimePath
                Confirm-Health ''
                throw "Activation failed; previous program restarted. Pending build and backup retained at $backupPath"
            }
            throw "Activation failed after possible migration. Service stopped; automatic rollback skipped. Backup: $backupPath . Inspect local-runtime/logs before retrying."
        }
        throw
    }
} finally {
    if ($lock) { $lock.Dispose() }
}
