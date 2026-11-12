$ErrorActionPreference = 'Stop'
$repoPath = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$fixture = Join-Path $repoPath ("local-runtime\release-checks\retention-" + [Guid]::NewGuid().ToString('N'))
$runtime = Join-Path $fixture 'runtime'
$trash = Join-Path $fixture 'recycled'
New-Item -ItemType Directory -Path (Join-Path $runtime 'releases'), (Join-Path $runtime 'builds'), (Join-Path $runtime 'backups'), $trash -Force | Out-Null
. (Join-Path $PSScriptRoot 'retention.ps1')
function Assert([bool]$condition, [string]$message) { if (!$condition) { throw $message } }
function Expect-Failure([scriptblock]$operation) {
    $failed = $false
    try { & $operation | Out-Null } catch { $failed = $true }
    Assert $failed 'Unsafe cleanup was not rejected.'
}
# Only fixtures move, into an ignored test directory. The real Recycle Bin is not used.
function Move-ArtifactToRecycleBin([string]$RuntimePath, [string]$Path) {
    $null = Get-ArtifactBytes $RuntimePath $Path
    Move-Item -LiteralPath $Path -Destination (Join-Path $trash ([IO.Path]::GetFileName($Path)))
}
$runningBuild = Join-Path $runtime ('builds\sub2api-running-' + ('a' * 32) + '.exe')
function Get-Process { param($Name, $ErrorAction) [pscustomobject]@{ Path = $runningBuild } }
foreach ($index in 1..8) {
    $id = '{0:x32}' -f $index
    $directory = Join-Path $runtime "releases\$id"
    New-Item -ItemType Directory -Path $directory | Out-Null
    [IO.File]::WriteAllText((Join-Path $directory 'sub2api.exe'), "fixture-$index")
    (Get-Item -LiteralPath $directory).LastWriteTimeUtc = [DateTime]::UtcNow.AddMinutes($index - 10)
}
$installed = Join-Path $runtime 'sub2api.exe'
Copy-Item -LiteralPath (Join-Path $runtime ('releases\' + ('{0:x32}' -f 1) + '\sub2api.exe')) -Destination $installed
$pending = Join-Path $runtime ('releases\' + ('{0:x32}' -f 8) + '\sub2api.exe')
$manifest = @{ executable = 'releases/' + ('{0:x32}' -f 8) + '/sub2api.exe'; sha256 = (Get-FileHash -LiteralPath $pending).Hash }
$manifestPath = Join-Path $runtime 'latest-build.json'
$manifest | ConvertTo-Json | Set-Content -LiteralPath $manifestPath -Encoding UTF8
[IO.File]::WriteAllText((Join-Path $runtime 'sub2api-personal.2.exe'), 'legacy fixture')
[IO.File]::WriteAllText((Join-Path $runtime 'unrelated.exe'), 'do not touch')
[IO.File]::WriteAllText((Join-Path $runtime 'backups\sub2api.exe'), 'backup stays')
[IO.File]::WriteAllText((Join-Path $runtime ('builds\sub2api-test-' + ('b' * 32) + '.exe')), 'intermediate fixture')
[IO.File]::WriteAllText($runningBuild, 'running fixture')
$originalHash = (Get-FileHash -LiteralPath $manifestPath).Hash
$plan = Invoke-BuildRetention -RuntimePath $runtime -CleanBuilds -DryRun
Assert ($plan.recycled_artifacts -eq 7) 'Unexpected retention count.'
Assert ((Get-ChildItem -LiteralPath (Join-Path $runtime 'releases') -Directory).Count -eq 8) 'Dry run mutated files.'
$result = Invoke-BuildRetention -RuntimePath $runtime -CleanBuilds
Assert ($result.recycled_artifacts -eq 7) 'Cleanup did not match plan.'
Assert ((Get-ChildItem -LiteralPath (Join-Path $runtime 'releases') -Directory).Count -eq 3) 'Release count is not bounded.'
foreach ($path in @($installed, $pending, $runningBuild, (Join-Path $runtime 'unrelated.exe'), (Join-Path $runtime 'backups\sub2api.exe'), (Join-Path $runtime ('releases\' + ('{0:x32}' -f 1) + '\sub2api.exe')))) {
    Assert (Test-Path -LiteralPath $path) 'Protected file was removed.'
}
Assert ((Get-FileHash -LiteralPath $manifestPath).Hash -eq $originalHash) 'Cleanup changed publication.'
$again = Invoke-BuildRetention -RuntimePath $runtime -CleanBuilds
Assert ($again.recycled_artifacts -eq 0) 'Cleanup is not idempotent.'
$duplicate = Join-Path $runtime ('releases\' + ('{0:x32}' -f 10))
New-Item -ItemType Directory -Path $duplicate | Out-Null
Copy-Item -LiteralPath $installed -Destination (Join-Path $duplicate 'sub2api.exe')
$duplicates = Invoke-BuildRetention -RuntimePath $runtime -CleanBuilds
Assert ($duplicates.retained_releases -eq 3 -and $duplicates.recycled_artifacts -eq 1) 'Identical re-publishes bypass retention limit.'
Expect-Failure { Assert-ArtifactPath $runtime (Join-Path $fixture 'outside.exe') }
$manifest.sha256 = '0' * 64
$manifest | ConvertTo-Json | Set-Content -LiteralPath $manifestPath -Encoding UTF8
Expect-Failure { Invoke-BuildRetention -RuntimePath $runtime -CleanBuilds }
$manifest.sha256 = (Get-FileHash -LiteralPath $pending).Hash
$manifest | ConvertTo-Json | Set-Content -LiteralPath $manifestPath -Encoding UTF8
$heldLock = [IO.File]::Open((Join-Path $runtime 'build.lock'), 'OpenOrCreate', 'ReadWrite', 'None')
try { Expect-Failure { & (Join-Path $PSScriptRoot 'cleanup.ps1') -RuntimePath $runtime } } finally { $heldLock.Dispose() }
$heldLock = [IO.File]::Open((Join-Path $runtime 'update.lock'), 'OpenOrCreate', 'ReadWrite', 'None')
try { Expect-Failure { & (Join-Path $PSScriptRoot 'cleanup.ps1') -RuntimePath $runtime } } finally { $heldLock.Dispose() }
$unexpectedRelease = Join-Path $runtime ('releases\' + ('{0:x32}' -f 9))
New-Item -ItemType Directory -Path $unexpectedRelease | Out-Null
[IO.File]::WriteAllText((Join-Path $unexpectedRelease 'sub2api.exe'), 'unknown fixture')
[IO.File]::WriteAllText((Join-Path $unexpectedRelease 'private.txt'), 'must stay')
(Get-Item -LiteralPath $unexpectedRelease).LastWriteTimeUtc = [DateTime]::UtcNow.AddDays(-1)
$unexpected = Invoke-BuildRetention -RuntimePath $runtime -CleanBuilds -WarningAction SilentlyContinue
Assert ($unexpected.skipped -eq 1 -and (Test-Path -LiteralPath (Join-Path $unexpectedRelease 'private.txt'))) 'Unknown content was not protected.'
$outside = Join-Path $fixture 'outside'
New-Item -ItemType Directory -Path $outside | Out-Null
$junction = Join-Path $runtime 'linked'
New-Item -ItemType Junction -Path $junction -Target $outside | Out-Null
Expect-Failure { Assert-ArtifactPath $runtime $junction }
Write-Host 'PASS: bounded retention, installed/pending/running protection, intermediate cleanup, dry run, idempotency, manifest integrity, locks, unexpected content and path/junction checks. Live service unchanged.'
