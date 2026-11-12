param(
    [ValidatePattern('^[a-zA-Z0-9._-]+$')][string]$Version,
    [string]$GoExe,
    [string]$RuntimePath = (Join-Path $PSScriptRoot '..\..\local-runtime')
)
$ErrorActionPreference = 'Stop'
$repoPath = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$RuntimePath = [IO.Path]::GetFullPath($RuntimePath)
if (!$Version) {
    $baseVersion = (Get-Content -LiteralPath (Join-Path $repoPath 'backend\cmd\server\VERSION') -Raw).Trim()
    $Version = "$baseVersion-personal.$(Get-Date -Format 'yyyyMMddHHmmss')"
}
if (!$GoExe) {
    $goCommand = Get-Command go -ErrorAction SilentlyContinue
    if ($goCommand) { $GoExe = $goCommand.Source }
    elseif (Test-Path -LiteralPath 'D:\software\go\bin\go.exe') { $GoExe = 'D:\software\go\bin\go.exe' }
    else { throw 'Go was not found. Supply -GoExe.' }
}
$commit = (& git -C $repoPath rev-parse --short HEAD | Out-String).Trim()
if ($LASTEXITCODE -ne 0) { throw 'Cannot determine source commit.' }
if (& git -C $repoPath status --porcelain --untracked-files=normal) { $commit += '-dirty' }
New-Item -ItemType Directory -Path (Join-Path $RuntimePath 'builds') -Force | Out-Null
$candidatePath = Join-Path $RuntimePath "builds\sub2api-$Version-$([Guid]::NewGuid().ToString('N')).exe"
Push-Location (Join-Path $repoPath 'frontend')
try {
    & node node_modules/vitest/vitest.mjs run src/i18n/__tests__/localeKeyCompleteness.spec.ts
    if ($LASTEXITCODE -ne 0) { throw 'Frontend locale checks failed.' }
    & node node_modules/vue-tsc/bin/vue-tsc.js -b
    if ($LASTEXITCODE -ne 0) { throw 'Frontend type check failed.' }
    & node node_modules/vite/bin/vite.js build
    if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed.' }
} finally { Pop-Location }
$previousCGO = $env:CGO_ENABLED
Push-Location (Join-Path $repoPath 'backend')
try {
    $env:CGO_ENABLED = '0'
    $buildTime = [DateTimeOffset]::Now.ToString('yyyy-MM-ddTHH:mm:sszzz')
    & $GoExe build -tags embed -trimpath -ldflags "-s -w -X main.Version=$Version -X main.Commit=$commit -X main.Date=$buildTime" -o $candidatePath ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw 'Backend build failed; previous pending release is unchanged.' }
} finally {
    $env:CGO_ENABLED = $previousCGO
    Pop-Location
}
& (Join-Path $PSScriptRoot 'publish.ps1') -Candidate $candidatePath -Version $Version -RuntimePath $RuntimePath
