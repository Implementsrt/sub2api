param([Parameter(Mandatory = $true)][string]$Candidate, [Parameter(Mandatory = $true)][string]$Version)
$ErrorActionPreference = 'Stop'
$repoPath = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$fixture = Join-Path $repoPath ("local-runtime\release-checks\" + [Guid]::NewGuid().ToString('N'))
function Expect-Failure([scriptblock]$operation, [string]$message) {
    try { & $operation } catch {
        if ($_.Exception.Message -notlike "*$message*") { throw }
        return
    }
    throw "Expected failure: $message"
}
& (Join-Path $PSScriptRoot 'publish.ps1') -Candidate $Candidate -Version $Version -RuntimePath $fixture
$pendingPath = Join-Path $fixture 'latest-build.json'
$manifest = Get-Content -LiteralPath $pendingPath -Raw | ConvertFrom-Json
$before = (Get-FileHash -LiteralPath $pendingPath).Hash
if ($manifest.version -ne $Version -or !$manifest.migrations.Count) { throw 'Published manifest is incomplete.' }
if ((Get-FileHash -LiteralPath (Join-Path $fixture $manifest.executable)).Hash -ne $manifest.sha256) { throw 'Published bytes differ from candidate.' }
Expect-Failure { & (Join-Path $PSScriptRoot 'publish.ps1') -Candidate $Candidate -Version 'wrong-version' -RuntimePath $fixture } 'version verification failed'
if ((Get-FileHash -LiteralPath $pendingPath).Hash -ne $before) { throw 'Failed build replaced the pending manifest.' }
$heldLock = [IO.File]::Open((Join-Path $fixture 'update.lock'), 'OpenOrCreate', 'ReadWrite', 'None')
try {
    Expect-Failure { & (Join-Path $PSScriptRoot 'publish.ps1') -Candidate $Candidate -Version $Version -RuntimePath $fixture } 'used by another process'
} finally { $heldLock.Dispose() }
if ((Get-FileHash -LiteralPath $pendingPath).Hash -ne $before) { throw 'Concurrent publish replaced manifest.' }
& (Join-Path $PSScriptRoot 'publish.ps1') -Candidate $Candidate -Version $Version -RuntimePath $fixture
$replacement = Get-Content -LiteralPath $pendingPath -Raw | ConvertFrom-Json
if ($replacement.executable -eq $manifest.executable) { throw 'A successful second publish did not replace the manifest.' }
$manifest = $replacement
$manifest.sha256 = '0' * 64
$manifest | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $pendingPath -Encoding UTF8
Expect-Failure { & (Join-Path $PSScriptRoot 'restart.ps1') -RuntimePath $fixture -Check } 'checksum failed'
$manifest.executable = '../sub2api.exe'
$manifest | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $pendingPath -Encoding UTF8
Expect-Failure { & (Join-Path $PSScriptRoot 'restart.ps1') -RuntimePath $fixture -Check } 'Invalid pending build manifest'
if (Test-Path -LiteralPath (Join-Path $fixture 'sub2api.exe')) { throw 'Validation failure changed the active binary.' }
Write-Host 'PASS: publish/hash, failed-build retention, exclusive lock, atomic replacement, corrupt binary rejection, path traversal rejection. Service unchanged.'
