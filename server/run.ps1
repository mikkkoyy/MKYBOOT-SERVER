# MKYBOOT Windows-native server — launch script
#
#   .\run.ps1                        run in console (foreground) with defaults
#   .\run.ps1 -Config C:\path.json   use an alternate config file
#   .\run.ps1 -Build                 build first, then run
#
# It builds the executable if missing, prints the data directory and health
# URL, and surfaces any startup error with a non-zero exit code.

param(
    [string]$Config = '',
    [switch]$Build
)

$ErrorActionPreference = 'Stop'
Set-Location -Path $PSScriptRoot

$exe = Join-Path $PSScriptRoot 'dist\mkyboot-server.exe'
if ($Build -or -not (Test-Path $exe)) {
    Write-Host 'Building server...'
    & (Join-Path $PSScriptRoot 'build.ps1')
    if ($LASTEXITCODE -ne 0) { throw 'build failed' }
}

# Show where state will live and confirm the data root is writable.
$dataDir = if ($env:MKYBOOT_DATA_DIR) { $env:MKYBOOT_DATA_DIR } else { Join-Path $env:ProgramData 'MKYBOOT' }
Write-Host "Data directory: $dataDir"
try {
    New-Item -ItemType Directory -Force -Path $dataDir | Out-Null
} catch {
    Write-Error "Cannot create data directory $dataDir. Run as Administrator or set MKYBOOT_DATA_DIR to a writable path. Details: $_"
    exit 1
}

$srvArgs = @()
if ($Config) { $srvArgs += @('-config', $Config) }

Write-Host "Starting mkyboot-server$($srvArgs -join ' ')... (Ctrl+C to stop)"
& $exe @srvArgs
$code = $LASTEXITCODE
if ($code -ne 0) {
    Write-Error "mkyboot-server exited with code $code"
}
exit $code
