param(
    [Parameter(Mandatory = $true)][string]$Candidate,
    [Parameter(Mandatory = $true)][string]$Version,
    [Parameter(Mandatory = $true)][string]$OldExecutable
)
$ErrorActionPreference = 'Stop'
$repoPath = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$testRoot = Join-Path $repoPath ("local-runtime\release-checks\" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $testRoot -Force | Out-Null
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'restart.ps1') -Destination $testRoot
@'
param([string]$RuntimePath)
$global:TestStartCount++
if ($global:TestFailStart -and $global:TestStartCount -eq 1) { throw 'Simulated startup failure' }
$global:TestRunning = $true
'@ | Set-Content -LiteralPath (Join-Path $testRoot 'start.ps1') -Encoding UTF8

# All process, network and database commands are fakes. No service is launched.
function Get-Process {
    param($Id, $ErrorAction)
    if ($global:TestRunning) {
        $process = [pscustomobject]@{ Id = 424242; Path = (Join-Path $global:TestRuntime 'sub2api.exe') }
        $process | Add-Member -MemberType ScriptMethod -Name WaitForExit -Value { param($milliseconds) return $true }
        return $process
    }
}
function Stop-Process { param($Id, $ErrorAction) $global:TestRunning = $false }
function Get-NetTCPConnection {
    param($LocalPort, $State, $ErrorAction)
    if ($global:TestRunning) { [pscustomobject]@{ OwningProcess = 424242 } }
}
function Invoke-RestMethod {
    param($Uri, $TimeoutSec)
    if ($Uri -like '*/health') { return @{ status = 'ok' } }
    return @{ data = @{ version = $Version } }
}
function Invoke-TestDatabase {
    $action = $args[1]
    $global:LASTEXITCODE = 0
    if ($action -eq 'backup') {
        if ($global:TestFailBackup) { $global:LASTEXITCODE = 1; return }
        $directory = $args[-1]
        [IO.File]::WriteAllText((Join-Path $directory 'database.dump'), 'simulated verified backup')
    }
    $all = $global:TestMigrations
    if ($global:TestHasMigration -and $global:TestStartCount -eq 0) { $all = @($all | Select-Object -SkipLast 1) }
    @{ migrations = @($all) } | ConvertTo-Json -Depth 3 -Compress
}

foreach ($scenario in @('success', 'backup-failure', 'startup-failure-no-migration', 'startup-failure-with-migration')) {
    $global:TestRuntime = Join-Path $testRoot $scenario
    $global:TestStartCount = 0
    $global:TestRunning = $true
    $global:TestFailBackup = $scenario -eq 'backup-failure'
    $global:TestFailStart = $scenario -like 'startup-failure*'
    $global:TestHasMigration = $scenario -ne 'startup-failure-no-migration'
    & (Join-Path $PSScriptRoot 'publish.ps1') -Candidate $Candidate -Version $Version -RuntimePath $global:TestRuntime
    $manifest = Get-Content -LiteralPath (Join-Path $global:TestRuntime 'latest-build.json') -Raw | ConvertFrom-Json
    $global:TestMigrations = $manifest.migrations
    New-Item -ItemType Directory -Path (Join-Path $global:TestRuntime 'data') | Out-Null
    Copy-Item -LiteralPath $OldExecutable -Destination (Join-Path $global:TestRuntime 'sub2api.exe')
    '424242' | Set-Content -LiteralPath (Join-Path $global:TestRuntime 'sub2api.pid')
    $failed = $false
    try { & (Join-Path $testRoot 'restart.ps1') -RuntimePath $global:TestRuntime -PythonExe Invoke-TestDatabase }
    catch { $failed = $true; if ($scenario -eq 'success') { throw } }
    if ($failed -ne ($scenario -ne 'success')) { throw "Unexpected result: $scenario" }
    $installedHash = (Get-FileHash -LiteralPath (Join-Path $global:TestRuntime 'sub2api.exe')).Hash
    $oldHash = (Get-FileHash -LiteralPath $OldExecutable).Hash
    if ($scenario -in @('backup-failure', 'startup-failure-no-migration')) {
        if (!$global:TestRunning -or $installedHash -ne $oldHash) { throw "Old service was not restored: $scenario" }
    } elseif ($scenario -eq 'startup-failure-with-migration') {
        if ($global:TestRunning -or $installedHash -ne $manifest.sha256) { throw 'Migration failure performed an unsafe rollback.' }
    } elseif (!$global:TestRunning -or $installedHash -ne $manifest.sha256) { throw 'Successful upgrade did not install the new build.' }
    Write-Host "PASS: $scenario"
}
Write-Host 'Restart scenarios passed using fake services and database; live service unchanged.'
