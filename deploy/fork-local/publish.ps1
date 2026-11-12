param(
    [Parameter(Mandatory = $true)][string]$Candidate,
    [Parameter(Mandatory = $true)][ValidatePattern('^[a-zA-Z0-9._-]+$')][string]$Version,
    [string]$RuntimePath = (Join-Path $PSScriptRoot '..\..\local-runtime')
)
$ErrorActionPreference = 'Stop'
$RuntimePath = [IO.Path]::GetFullPath($RuntimePath)
New-Item -ItemType Directory -Path $RuntimePath -Force | Out-Null
$lock = $null
try {
    # Shared with restart: never publish a partially copied executable.
    $lock = [IO.File]::Open((Join-Path $RuntimePath 'update.lock'), 'OpenOrCreate', 'ReadWrite', 'None')
    $releaseId = [Guid]::NewGuid().ToString('N')
    $releasePath = Join-Path $RuntimePath "releases\$releaseId"
    New-Item -ItemType Directory -Path $releasePath | Out-Null
    $releaseExe = Join-Path $releasePath 'sub2api.exe'
    Copy-Item -LiteralPath $Candidate -Destination $releaseExe
    $versionText = (& $releaseExe -version 2>&1 | Out-String)
    if ($LASTEXITCODE -ne 0 -or $versionText -notmatch ('\bSub2API\s+' + [regex]::Escape($Version) + '\s+\(commit:')) {
        throw 'Candidate version verification failed; previous pending release is unchanged.'
    }
    $manifest = [ordered]@{
        version = $Version
        executable = "releases/$releaseId/sub2api.exe"
        sha256 = (Get-FileHash -LiteralPath $releaseExe -Algorithm SHA256).Hash
        migrations = @(Get-ChildItem -LiteralPath (Join-Path $PSScriptRoot '..\..\backend\migrations') -Filter '*.sql' | Sort-Object Name | ForEach-Object { $_.Name })
        published_at = [DateTimeOffset]::Now.ToString('o')
    }
    $pending = Join-Path $RuntimePath 'latest-build.json'
    $temporary = Join-Path $releasePath 'manifest.json'
    $manifest | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $temporary -Encoding UTF8
    if (Test-Path -LiteralPath $pending) {
        [IO.File]::Replace($temporary, $pending, (Join-Path $releasePath 'previous-manifest.json'))
    } else {
        [IO.File]::Move($temporary, $pending)
    }
    Write-Host "Build $Version is ready. restart-local.cmd will activate it."
    try {
        . (Join-Path $PSScriptRoot 'retention.ps1')
        $retention = Invoke-BuildRetention -RuntimePath $RuntimePath -PublishedCandidate $Candidate
        Write-Host "Build retention: $($retention.retained_releases) releases kept; $($retention.recycled_artifacts) artifacts moved to Recycle Bin."
    } catch {
        Write-Warning 'Build is published; automatic cleanup skipped. Run cleanup.ps1 later.'
    }
} finally {
    if ($lock) { $lock.Dispose() }
}
