# Shared by publish and cleanup. Caller must hold update.lock.
function Assert-ArtifactPath([string]$RuntimePath, [string]$Path) {
    $root = [IO.Path]::GetFullPath($RuntimePath).TrimEnd([IO.Path]::DirectorySeparatorChar)
    $target = [IO.Path]::GetFullPath($Path)
    if (!$target.StartsWith($root + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Artifact path escapes runtime directory.'
    }
    # Refuse links/junctions anywhere in the path, including runtime's parents.
    $item = Get-Item -LiteralPath $target -Force -ErrorAction Stop
    while ($item) {
        if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Artifact path contains a link or junction.' }
        $parent = [IO.Path]::GetDirectoryName($item.FullName)
        if (!$parent -or $parent -eq $item.FullName) { break }
        $item = Get-Item -LiteralPath $parent -Force -ErrorAction Stop
    }
    return $target
}

function Get-ArtifactBytes([string]$RuntimePath, [string]$Path) {
    $target = Assert-ArtifactPath $RuntimePath $Path
    $item = Get-Item -LiteralPath $target -Force
    if (!$item.PSIsContainer) { return [long]$item.Length }
    $size = [long]0
    foreach ($child in Get-ChildItem -LiteralPath $target -Force) {
        $null = Assert-ArtifactPath $RuntimePath $child.FullName
        if ($child.PSIsContainer -or $child.Name -notin @('sub2api.exe', 'manifest.json', 'previous-manifest.json')) {
            throw 'Release directory contains unexpected files; retained for manual inspection.'
        }
        $size += $child.Length
    }
    return $size
}

function Move-ArtifactToRecycleBin([string]$RuntimePath, [string]$Path) {
    $target = Assert-ArtifactPath $RuntimePath $Path
    Add-Type -AssemblyName Microsoft.VisualBasic
    $ui = [Microsoft.VisualBasic.FileIO.UIOption]::OnlyErrorDialogs
    $recycle = [Microsoft.VisualBasic.FileIO.RecycleOption]::SendToRecycleBin
    $cancel = [Microsoft.VisualBasic.FileIO.UICancelOption]::ThrowException
    $null = Get-ArtifactBytes $RuntimePath $target
    if ((Get-Item -LiteralPath $target -Force).PSIsContainer) {
        [Microsoft.VisualBasic.FileIO.FileSystem]::DeleteDirectory($target, $ui, $recycle, $cancel)
    } else {
        [Microsoft.VisualBasic.FileIO.FileSystem]::DeleteFile($target, $ui, $recycle, $cancel)
    }
}

function Invoke-BuildRetention {
    [CmdletBinding()]
    param(
        [string]$RuntimePath,
        [ValidateRange(2, 10)][int]$KeepReleases = 3,
        [switch]$CleanBuilds,
        [string]$PublishedCandidate,
        [switch]$DryRun
    )
    $RuntimePath = [IO.Path]::GetFullPath($RuntimePath)
    $pendingFile = Join-Path $RuntimePath 'latest-build.json'
    # A missing/corrupt manifest must not turn everything into a cleanup target.
    if (!(Test-Path -LiteralPath $pendingFile)) { throw 'No pending manifest; cleanup skipped.' }
    $manifest = Get-Content -LiteralPath $pendingFile -Raw | ConvertFrom-Json
    if ($manifest.executable -notmatch '^releases/[a-f0-9]{32}/sub2api\.exe$' -or $manifest.sha256 -notmatch '^[a-fA-F0-9]{64}$') {
        throw 'Invalid pending manifest; cleanup skipped.'
    }
    $pending = Assert-ArtifactPath $RuntimePath (Join-Path $RuntimePath $manifest.executable)
    if ((Get-FileHash -LiteralPath $pending -Algorithm SHA256).Hash -ne $manifest.sha256) { throw 'Pending checksum failed; cleanup skipped.' }
    $protectedPaths = @($pending, (Join-Path $RuntimePath 'sub2api.exe'))
    # Protect any running binary, even one started outside our PID file.
    foreach ($process in Get-Process -Name 'sub2api*' -ErrorAction SilentlyContinue) {
        if (!$process.Path) { throw 'Cannot identify a running Sub2API binary; cleanup skipped.' }
        $protectedPaths += [IO.Path]::GetFullPath($process.Path)
    }
    $installedHash = $null
    $installed = Join-Path $RuntimePath 'sub2api.exe'
    if (Test-Path -LiteralPath $installed) {
        $null = Assert-ArtifactPath $RuntimePath $installed
        $installedHash = (Get-FileHash -LiteralPath $installed -Algorithm SHA256).Hash
    }
    $releases = @()
    $releaseRoot = Join-Path $RuntimePath 'releases'
    if (Test-Path -LiteralPath $releaseRoot) {
        $null = Assert-ArtifactPath $RuntimePath $releaseRoot
        foreach ($dir in Get-ChildItem -LiteralPath $releaseRoot -Directory -Force | Sort-Object LastWriteTimeUtc -Descending) {
            if ($dir.Name -notmatch '^[a-f0-9]{32}$') { continue }
            $null = Assert-ArtifactPath $RuntimePath $dir.FullName
            $exe = Join-Path $dir.FullName 'sub2api.exe'
            if (!(Test-Path -LiteralPath $exe)) { continue }
            $null = Assert-ArtifactPath $RuntimePath $exe
            $hash = (Get-FileHash -LiteralPath $exe -Algorithm SHA256).Hash
            $releases += [pscustomobject]@{ Path = $dir.FullName; Hash = $hash; Protected = ($exe -in $protectedPaths) }
        }
    }
    # Keep one copy matching the installed program, not every identical re-publish.
    if ($installedHash) {
        $installedCopies = @($releases | Where-Object { $_.Hash -eq $installedHash })
        if ($installedCopies.Count -and !@($installedCopies | Where-Object Protected).Count) {
            $installedCopies[0].Protected = $true
        }
    }
    $keep = @($releases | Where-Object Protected | ForEach-Object { $_.Path })
    foreach ($release in $releases) {
        if ($keep.Count -ge $KeepReleases) { break }
        if ($release.Path -notin $keep) { $keep += $release.Path }
    }
    $targets = @($releases | Where-Object { $_.Path -notin $keep } | ForEach-Object { $_.Path })
    # Legacy root-level builds are superseded by the verified release manifest.
    foreach ($file in Get-ChildItem -LiteralPath $RuntimePath -File -Force) {
        if ($file.Name -eq 'sub2api-custom.exe' -or $file.Name -match '^sub2api-personal\.\d+\.exe$') { $targets += $file.FullName }
    }
    $buildRoot = Join-Path $RuntimePath 'builds'
    if (Test-Path -LiteralPath $buildRoot) {
        $null = Assert-ArtifactPath $RuntimePath $buildRoot
        foreach ($file in Get-ChildItem -LiteralPath $buildRoot -File -Force) {
            if ($file.Name -notmatch '^sub2api-[a-zA-Z0-9._-]+-[a-f0-9]{32}\.exe$') { continue }
            if ($CleanBuilds -or ($PublishedCandidate -and $file.FullName -eq [IO.Path]::GetFullPath($PublishedCandidate))) { $targets += $file.FullName }
        }
    }
    $moved = 0; $bytes = [long]0; $skipped = 0
    foreach ($target in $targets) {
        if ($target -in $protectedPaths) { continue }
        # Also protect running paths within a release directory.
        if (@($protectedPaths | Where-Object { $_.StartsWith($target + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase) }).Count) { continue }
        try {
            $null = Assert-ArtifactPath $RuntimePath $target
            $size = Get-ArtifactBytes $RuntimePath $target
            if (!$DryRun) { Move-ArtifactToRecycleBin $RuntimePath $target }
            $moved++; $bytes += $size
        } catch {
            # Locked files or unexpected content never invalidate a publication.
            $skipped++
            Write-Warning "Retained artifact: $target. $($_.Exception.Message)"
        }
    }
    [pscustomobject]@{ dry_run = [bool]$DryRun; retained_releases = $keep.Count; recycled_artifacts = $moved; bytes = $bytes; skipped = $skipped }
}
