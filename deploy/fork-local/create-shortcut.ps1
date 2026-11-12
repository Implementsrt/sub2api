param([string]$DestinationDirectory = [Environment]::GetFolderPath('Desktop'))
$ErrorActionPreference = 'Stop'
$repoPath = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$target = Join-Path $repoPath 'restart-local.cmd'
$icon = Join-Path $PSScriptRoot 'sub2api.ico'
if (!(Test-Path -LiteralPath $target) -or !(Test-Path -LiteralPath $icon)) {
    throw 'Restart entry point or Sub2API icon is missing.'
}
if (!(Test-Path -LiteralPath $DestinationDirectory -PathType Container)) {
    throw 'Shortcut destination directory does not exist.'
}
$shortcutPath = Join-Path $DestinationDirectory 'Sub2API Restart.lnk'
if (Test-Path -LiteralPath $shortcutPath) {
    $backupPath = Join-Path $repoPath ('local-runtime\backups\shortcuts\' + (Get-Date -Format 'yyyyMMdd-HHmmss') + '-' + [Guid]::NewGuid().ToString('N').Substring(0, 8))
    New-Item -ItemType Directory -Path $backupPath -Force | Out-Null
    Copy-Item -LiteralPath $shortcutPath -Destination (Join-Path $backupPath 'Sub2API Restart.lnk')
}
$shell = New-Object -ComObject WScript.Shell
try {
    $shortcut = $shell.CreateShortcut($shortcutPath)
    $shortcut.TargetPath = $target
    $shortcut.Arguments = ''
    $shortcut.WorkingDirectory = $repoPath
    $shortcut.IconLocation = "$icon,0"
    $shortcut.Description = 'Restart Sub2API and activate the latest published build.'
    $shortcut.WindowStyle = 1
    $shortcut.Save()
    $saved = $shell.CreateShortcut($shortcutPath)
    if ($saved.TargetPath -ne $target -or $saved.WorkingDirectory -ne $repoPath -or $saved.IconLocation -ne "$icon,0" -or $saved.Arguments) {
        throw 'Saved shortcut verification failed.'
    }
    Write-Host "Created Sub2API shortcut: $shortcutPath"
} finally {
    if ($saved) { [Runtime.InteropServices.Marshal]::FinalReleaseComObject($saved) | Out-Null }
    if ($shortcut) { [Runtime.InteropServices.Marshal]::FinalReleaseComObject($shortcut) | Out-Null }
    [Runtime.InteropServices.Marshal]::FinalReleaseComObject($shell) | Out-Null
}
