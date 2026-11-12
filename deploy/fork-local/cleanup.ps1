param(
    [string]$RuntimePath = (Join-Path $PSScriptRoot '..\..\local-runtime'),
    [ValidateRange(2, 10)][int]$KeepReleases = 3,
    [switch]$DryRun
)
$ErrorActionPreference = 'Stop'
$RuntimePath = [IO.Path]::GetFullPath($RuntimePath)
. (Join-Path $PSScriptRoot 'retention.ps1')
$buildLock = $null; $updateLock = $null
try {
    # Same lock order as build -> publish; no active build can lose its output.
    $buildLock = [IO.File]::Open((Join-Path $RuntimePath 'build.lock'), 'OpenOrCreate', 'ReadWrite', 'None')
    $updateLock = [IO.File]::Open((Join-Path $RuntimePath 'update.lock'), 'OpenOrCreate', 'ReadWrite', 'None')
    Invoke-BuildRetention -RuntimePath $RuntimePath -KeepReleases $KeepReleases -CleanBuilds -DryRun:$DryRun | ConvertTo-Json
} finally {
    if ($updateLock) { $updateLock.Dispose() }
    if ($buildLock) { $buildLock.Dispose() }
}
