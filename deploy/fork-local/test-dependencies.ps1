$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'dependencies.ps1')

# Simulate Redis opening its port before it can answer PING.
function Test-LocalPort([int]$port) {
    return ($port -eq 5432 -or $scenario -notin @('cold-start', 'redis-exits'))
}

function Test-LocalRedis {
    $state.pings++
    return ($scenario -eq 'already-ready' -or ($scenario -ne 'redis-exits' -and $state.pings -eq 4))
}

function Start-Process {
    param($FilePath, $ArgumentList, $WorkingDirectory, $WindowStyle, [switch]$PassThru, $RedirectStandardOutput, $RedirectStandardError)
    $state.starts++
    $process = [pscustomobject]@{ Id = 1234; HasExited = $scenario -eq 'redis-exits' }
    $process | Add-Member -MemberType ScriptMethod -Name Refresh -Value {}
    return $process
}

function Set-Content {
    param([Parameter(ValueFromPipeline = $true)]$Value, $LiteralPath)
    if ($Value -ne 1234 -or (Split-Path -Leaf $LiteralPath) -ne 'local-process.pid') { throw 'Unexpected PID file write.' }
}

function Start-Sleep { param($Milliseconds) }

foreach ($scenario in @('already-ready', 'cold-start', 'loading-on-open-port', 'redis-exits')) {
    $state = @{ pings = 0; starts = 0 }
    $expectedStarts = [int]($scenario -in @('cold-start', 'redis-exits'))
    $expectedPings = @{ 'already-ready' = 1; 'cold-start' = 4; 'loading-on-open-port' = 4; 'redis-exits' = 2 }[$scenario]
    $failed = $false
    try { Initialize-LocalDependencies }
    catch {
        if ($scenario -ne 'redis-exits' -or $_.Exception.Message -notlike 'Redis exited during startup.*') { throw }
        $failed = $true
    }
    if ($failed -ne ($scenario -eq 'redis-exits') -or $state.starts -ne $expectedStarts -or $state.pings -ne $expectedPings) {
        throw "Unexpected dependency startup result: $scenario"
    }
    Write-Host "PASS: $scenario"
}
